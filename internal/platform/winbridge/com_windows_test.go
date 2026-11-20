//go:build windows && (amd64 || arm64)

package winbridge

import (
	"bytes"
	"errors"
	"syscall"
	"testing"
	"unsafe"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

func stubObject(slots map[int]any) *comObject {
	table := new([64]uintptr)
	table[2] = syscall.NewCallback(func(uintptr) uintptr { return 0 })
	for slot, callback := range slots {
		table[slot] = syscall.NewCallback(callback)
	}
	return &comObject{vtable: table}
}

func stubHString(value string) func(uintptr, *uintptr) uintptr {
	text, err := syscall.UTF16FromString(value)
	if err != nil {
		panic(err)
	}
	return func(_ uintptr, out *uintptr) uintptr {
		hr, _, _ := windowsCreateString.Call(uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), uintptr(unsafe.Pointer(out)))
		return hr
	}
}

func stubOut[T any](value T) func(uintptr, *T) uintptr {
	return func(_ uintptr, out *T) uintptr {
		*out = value
		return 0
	}
}

func stubAsync[T any](value T) *comObject {
	info := stubObject(map[int]any{7: stubOut(int32(1))})
	return stubObject(map[int]any{
		0: func(_ uintptr, _ *syscall.GUID, out **comObject) uintptr {
			*out = info
			return 0
		},
		8: stubOut(value),
	})
}

func TestMediaSessionSelection(t *testing.T) {
	const path = `C:\Program Files\MyGo Music\MyGo Music.exe`
	pids := map[uint32]string{13: path}
	host := stubObject(map[int]any{6: stubHString("MyGo Music.exe")})
	browser := stubObject(map[int]any{6: stubHString("chrome.exe")})
	similar := stubObject(map[int]any{6: stubHString("MyGo Music.exe.helper")})
	items := []*comObject{browser, similar, host}
	calls := 0
	vector := stubObject(map[int]any{
		6: func(_ uintptr, index uintptr, out **comObject) uintptr {
			*out = items[index]
			return 0
		},
		7: func(_ uintptr, size *uint32) uintptr {
			*size = uint32(len(items))
			return 0
		},
	})
	use := func(session *comObject) error {
		calls++
		if session != host {
			t.Error("another application's session was selected")
		}
		return nil
	}
	if err := useHostSession(vector, pids, use); err != nil || calls != 1 {
		t.Fatalf("selected=%d, err=%v", calls, err)
	}
	items = append(items, host)
	if err := useHostSession(vector, pids, use); !errors.Is(err, bridge.ErrUnavailable) || calls != 1 {
		t.Fatalf("ambiguous sessions invoked command: selected=%d, err=%v", calls, err)
	}
	items = items[:2]
	if err := useHostSession(vector, pids, use); !errors.Is(err, bridge.ErrUnavailable) || calls != 1 {
		t.Fatalf("third-party sessions invoked command: selected=%d, err=%v", calls, err)
	}
	if isHostSession("MyGo Music.exe", nil) ||
		isHostSession("MyGo Music.exe", map[uint32]string{13: path, 19: `C:\other\MyGo Music.exe`}) ||
		isHostSession(`C:\other\MyGo Music.exe`, pids) ||
		!isHostSession(`c:\program files\mygo music\mygo music.exe`, pids) {
		t.Fatal("session identity was matched without an unambiguous process or exact path")
	}
	t.Log("ignored browser/partial identities and ambiguous sessions; selected one verified host")
}

func TestMediaSnapshotAndSeekABI(t *testing.T) {
	props := stubObject(map[int]any{
		6:  stubHString("Track"),
		9:  stubHString("Artist"),
		10: stubHString("Album"),
		15: stubOut((*comObject)(nil)),
	})
	playback := stubObject(map[int]any{7: stubOut(int32(5))})
	timeline := stubObject(map[int]any{
		6: stubOut(int64(0)), 7: stubOut(int64(183456 * ticksPerMS)),
		10: stubOut(int64(24567 * ticksPerMS)), 11: stubOut(int64(0)),
	})
	var seekTicks uintptr
	session := stubObject(map[int]any{
		7: stubOut(stubAsync(props)), 8: stubOut(timeline), 9: stubOut(playback),
		10: stubOut(stubAsync(uint8(1))), 11: stubOut(stubAsync(uint8(0))),
		16: stubOut(stubAsync(uint8(1))), 17: stubOut(stubAsync(uint8(1))),
		24: func(_ uintptr, ticks uintptr, out **comObject) uintptr {
			seekTicks = ticks
			*out = stubAsync(uint8(1))
			return 0
		},
	})
	var snapshot bridge.MediaSnapshot
	if err := readMediaSnapshot(session, &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.Available || snapshot.Playing || snapshot.Title != "Track" || snapshot.Artist != "Artist" ||
		snapshot.Album != "Album" || snapshot.DurationMS != 183456 || snapshot.PositionMS != 24567 || snapshot.Artwork != nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	for _, slot := range []int{10, 16, 17} {
		if err := sendMediaCommand(session, slot); err != nil {
			t.Fatal(err)
		}
	}
	if err := sendMediaCommand(session, 11); err == nil {
		t.Fatal("the operation's false result was ignored")
	}
	const seekMS = 4321
	if err := sendMediaCommand(session, 24, seekMS*ticksPerMS); err != nil || seekTicks != seekMS*ticksPerMS {
		t.Fatalf("Seek %d ms → %d ticks, err=%v", seekMS, seekTicks, err)
	}
	t.Logf("snapshot %+v; play/next/previous accepted; pause rejected; %d ms = %d ticks", snapshot, seekMS, seekTicks)
}

func TestSMTCArtworkReadFromStream(t *testing.T) {
	err := runInMTA(true, func() error {
		const name = "Windows.Storage.Streams.InMemoryRandomAccessStream"
		class, _ := syscall.UTF16FromString(name)
		var hstr uintptr
		ret, _, _ := windowsCreateString.Call(uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)-1), uintptr(unsafe.Pointer(&hstr)))
		if err := checkHR(ret); err != nil {
			return err
		}
		defer windowsDeleteString.Call(hstr)
		var content *comObject
		activate := combase.NewProc("RoActivateInstance")
		ret, _, _ = activate.Call(hstr, uintptr(unsafe.Pointer(&content)))
		if err := checkHR(ret); err != nil {
			return err
		}
		defer content.release()
		random, err := content.query(&iidRandomStream)
		if err != nil {
			return err
		}
		defer random.release()
		var stream *comObject
		ret, _, _ = streamOverRandom.Call(uintptr(unsafe.Pointer(random)), uintptr(unsafe.Pointer(&iidStream)), uintptr(unsafe.Pointer(&stream)))
		if err := checkHR(ret); err != nil {
			return err
		}
		defer stream.release()
		want := []byte{0x89, 0x50, 0x4e, 0x47, 1, 2, 3}
		var written uint32
		if err := checkHR(stream.call(4, uintptr(unsafe.Pointer(&want[0])), uintptr(len(want)), uintptr(unsafe.Pointer(&written)))); err != nil {
			return err
		}
		if err := checkHR(random.call(11, 0)); err != nil { // Seek
			return err
		}
		ref := stubObject(map[int]any{6: stubOut(stubAsync(content))})
		props := stubObject(map[int]any{15: stubOut(ref)})
		content.call(1) // The fake OpenReadAsync result transfers an owned reference.
		got, err := mediaArtwork(props)
		if err == nil && !bytes.Equal(got, want) {
			t.Errorf("SMTC artwork = %x, want %x", got, want)
		}
		t.Logf("SMTC thumbnail returned %d expected bytes through the WinRT stream", len(got))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAudioSessionProcessIsolation(t *testing.T) {
	const localSession = 2
	var pid uint32 = 300
	var state int32 = sessionActive
	var status uintptr
	control := stubObject(map[int]any{
		3:  func(_ uintptr, out *int32) uintptr { *out = state; return 0 },
		14: func(_ uintptr, out *uint32) uintptr { *out = pid; return status },
	})
	session := stubObject(map[int]any{
		0: func(_ uintptr, iid *syscall.GUID, out **comObject) uintptr {
			if *iid != iidSessionCtrl2 {
				return uintptr(eNoInterface)
			}
			*out = control
			return 0
		},
	})
	lookup := func(pid, desktopSession uint32) string {
		if pid == 300 && desktopSession == localSession {
			return hostExe
		}
		return ""
	}
	var calls int
	visit := func(_ *comObject, state int32) error {
		calls++
		if state != sessionActive {
			t.Errorf("selected state = %d", state)
		}
		return nil
	}
	run := func() {
		t.Helper()
		if err := useHostAudioSession(session, localSession, lookup, visit); err != nil {
			t.Fatal(err)
		}
	}
	run()
	pid = 301
	run()
	pid = 300
	status = 1 // A positive HRESULT reports a shared session.
	run()
	status, state = 0, 2
	run()
	if calls != 1 {
		t.Fatalf("audio visits = %d, want 1", calls)
	}
	t.Log("only active host PID 300 visited; foreign, cross-process and expired sessions ignored")
}
