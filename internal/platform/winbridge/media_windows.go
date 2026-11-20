//go:build windows && (amd64 || arm64)

package winbridge

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

const (
	sessionManagerClass = "Windows.Media.Control.GlobalSystemMediaTransportControlsSessionManager"
	ticksPerMS          = 10000
	snapshotInterval    = 250 * time.Millisecond
)

var (
	roGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	windowsCreateString    = combase.NewProc("WindowsCreateString")
	windowsDeleteString    = combase.NewProc("WindowsDeleteString")
	windowsStringBuffer    = combase.NewProc("WindowsGetStringRawBuffer")
	streamOverRandom       = syscall.NewLazyDLL("shcore.dll").NewProc("CreateStreamOverRandomAccessStream")

	iidManagerStatics = guid(0x2050c4ee, 0x11a0, 0x57de, [8]byte{0xae, 0xd7, 0xc9, 0x7c, 0x70, 0x33, 0x82, 0x45})
	iidAsyncInfo      = guid(0x00000036, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46})
	iidRandomStream   = guid(0x905a0fe1, 0xbc53, 0x11df, [8]byte{0x8c, 0x49, 0, 0x1e, 0x4f, 0xc6, 0x86, 0xda})
	iidStream         = guid(0x0000000c, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46})
)

func mediaManager() (*comObject, error) {
	name, _ := syscall.UTF16FromString(sessionManagerClass)
	var hstr uintptr
	ret, _, _ := windowsCreateString.Call(uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)-1), uintptr(unsafe.Pointer(&hstr)))
	if err := checkHR(ret); err != nil {
		return nil, err
	}
	defer windowsDeleteString.Call(hstr)
	var statics *comObject
	ret, _, _ = roGetActivationFactory.Call(hstr, uintptr(unsafe.Pointer(&iidManagerStatics)), uintptr(unsafe.Pointer(&statics)))
	if err := checkHR(ret); err != nil {
		return nil, err
	}
	defer statics.release()
	return awaitObject(statics, 6) // RequestAsync
}

func await(op *comObject, out unsafe.Pointer) error {
	defer op.release()
	info, err := op.query(&iidAsyncInfo)
	if err != nil {
		return err
	}
	defer info.release()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var status int32
		if err := checkHR(info.call(7, uintptr(unsafe.Pointer(&status)))); err != nil {
			return err
		}
		switch status {
		case 1, 3: // Completed or Error; GetResults supplies the operation's HRESULT.
			return checkHR(op.call(8, uintptr(out)))
		case 2: // Canceled
			return errors.New("Windows media operation was canceled")
		}
		if !time.Now().Before(deadline) {
			info.call(9) // Cancel
			return errors.New("Windows media operation timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

//go:uintptrescapes
func awaitObject(obj *comObject, slot int, args ...uintptr) (*comObject, error) {
	op, err := obj.object(slot, args...)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, bridge.ErrUnavailable
	}
	var result *comObject
	if err := await(op, unsafe.Pointer(&result)); err != nil {
		return nil, err
	}
	return result, nil
}

func stringProperty(obj *comObject, slot int) (string, error) {
	var hstr uintptr
	if err := checkHR(obj.call(slot, uintptr(unsafe.Pointer(&hstr)))); err != nil {
		return "", err
	}
	if hstr == 0 {
		return "", nil
	}
	defer windowsDeleteString.Call(hstr)
	var size uint32
	addr, _, _ := windowsStringBuffer.Call(hstr, uintptr(unsafe.Pointer(&size)))
	buf := *(**uint16)(unsafe.Pointer(&addr))
	return string(syscall.UTF16ToString(unsafe.Slice(buf, size))), nil
}

func isHostSession(id, installedImage string, pids map[uint32]string) bool {
	for _, image := range pids {
		if strings.EqualFold(image, installedImage) {
			// The unpackaged release normally exposes only the basename.
			return strings.EqualFold(id, image) || len(pids) == 1 && strings.EqualFold(id, hostExe)
		}
	}
	return false
}

func withMediaSession(use func(*comObject) error) error {
	image := registeredHostImage()
	if image == "" {
		return bridge.ErrUnavailable
	}
	pids, err := hostProcesses()
	if err != nil {
		return err
	}
	if !hasHostProcess(image, pids) {
		return bridge.ErrUnavailable
	}
	return inApartment(true, func() error {
		manager, err := mediaManager()
		if err != nil {
			return err
		}
		defer manager.release()
		sessions, err := manager.object(7) // GetSessions
		if err != nil {
			return err
		}
		defer sessions.release()
		return useHostSession(sessions, image, pids, use)
	})
}

func useHostSession(sessions *comObject, image string, pids map[uint32]string, use func(*comObject) error) error {
	var size uint32
	if err := checkHR(sessions.call(7, uintptr(unsafe.Pointer(&size)))); err != nil {
		return err
	}
	var selected *comObject
	defer func() {
		if selected != nil {
			selected.release()
		}
	}()
	for i := uint32(0); i < size; i++ {
		session, err := sessions.object(6, uintptr(i)) // IVectorView.GetAt
		if err != nil {
			return err
		}
		id, err := stringProperty(session, 6) // SourceAppUserModelId
		if err != nil || !isHostSession(id, image, pids) {
			session.release()
			continue
		}
		if selected != nil {
			session.release()
			return bridge.ErrUnavailable
		}
		selected = session
	}
	if selected == nil {
		return bridge.ErrUnavailable
	}
	return use(selected)
}

func (b *Bridge) Media() (bridge.MediaSnapshot, error) {
	b.mediaMu.Lock()
	defer b.mediaMu.Unlock()
	if time.Since(b.mediaAt) >= snapshotInterval {
		snapshot := bridge.MediaSnapshot{}
		err := withMediaSession(func(session *comObject) error {
			return readMediaSnapshot(session, &snapshot)
		})
		if err != nil && err != bridge.ErrUnavailable {
			return snapshot, fmt.Errorf("read MyGo Music media: %w", err)
		}
		b.media, b.mediaAt = snapshot, time.Now()
	}
	snapshot := b.media
	snapshot.Artwork = slices.Clone(snapshot.Artwork)
	return snapshot, nil
}

func readMediaSnapshot(session *comObject, snapshot *bridge.MediaSnapshot) error {
	snapshot.Available = true
	props, err := awaitObject(session, 7) // TryGetMediaPropertiesAsync
	if err != nil {
		return err
	}
	if props != nil {
		defer props.release()
		for _, field := range []struct {
			slot int
			out  *string
		}{{6, &snapshot.Title}, {9, &snapshot.Artist}, {10, &snapshot.Album}} {
			*field.out, err = stringProperty(props, field.slot)
			if err != nil {
				return err
			}
		}
	}
	if err := readTimeline(session, snapshot); err != nil {
		return err
	}
	if props != nil {
		// A vanished or unreadable thumbnail can use the companion's artwork fallback.
		snapshot.Artwork, _ = mediaArtwork(props)
	}
	return nil
}

func readTimeline(session *comObject, snapshot *bridge.MediaSnapshot) error {
	playback, err := session.object(9) // GetPlaybackInfo
	if err != nil {
		return err
	}
	defer playback.release()
	var status int32
	if err := checkHR(playback.call(7, uintptr(unsafe.Pointer(&status)))); err != nil {
		return err
	}
	snapshot.Playing = status == 4 // Playing

	timeline, err := session.object(8) // GetTimelineProperties
	if err != nil {
		return err
	}
	defer timeline.release()
	var start, end, pos, updated int64
	for _, field := range []struct {
		slot int
		out  *int64
	}{{6, &start}, {7, &end}, {10, &pos}, {11, &updated}} {
		if err := checkHR(timeline.call(field.slot, uintptr(unsafe.Pointer(field.out)))); err != nil {
			return err
		}
	}
	if snapshot.Playing && updated > 0 {
		var now syscall.Filetime
		syscall.GetSystemTimeAsFileTime(&now)
		ticks := int64(now.HighDateTime)<<32 | int64(now.LowDateTime)
		if elapsed := ticks - updated; elapsed > 0 && pos <= math.MaxInt64-elapsed {
			pos += elapsed // MyGo Music publishes a rate of 1.
		}
	}
	snapshot.DurationMS = max(0, end/ticksPerMS-start/ticksPerMS)
	snapshot.PositionMS = max(0, pos/ticksPerMS-start/ticksPerMS)
	if snapshot.DurationMS > 0 {
		snapshot.PositionMS = min(snapshot.PositionMS, snapshot.DurationMS)
	}
	return nil
}

func mediaArtwork(props *comObject) ([]byte, error) {
	ref, err := props.object(15) // Thumbnail
	if err != nil || ref == nil {
		return nil, err
	}
	defer ref.release()
	content, err := awaitObject(ref, 6) // OpenReadAsync
	if err != nil || content == nil {
		return nil, err
	}
	defer content.release()
	random, err := content.query(&iidRandomStream)
	if err != nil {
		return nil, err
	}
	defer random.release()
	var size uint64
	if err := checkHR(random.call(6, uintptr(unsafe.Pointer(&size)))); err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	if size > 16<<20 {
		return nil, errors.New("SMTC artwork exceeds 16 MiB")
	}
	var stream *comObject
	ret, _, _ := streamOverRandom.Call(uintptr(unsafe.Pointer(random)), uintptr(unsafe.Pointer(&iidStream)), uintptr(unsafe.Pointer(&stream)))
	if err := checkHR(ret); err != nil {
		return nil, err
	}
	defer stream.release()
	return readStream(stream, int(size))
}

func readStream(stream *comObject, size int) ([]byte, error) {
	buf := make([]byte, size)
	for remaining := buf; len(remaining) > 0; {
		var n uint32
		if err := checkHR(stream.call(3, uintptr(unsafe.Pointer(&remaining[0])), uintptr(len(remaining)), uintptr(unsafe.Pointer(&n)))); err != nil {
			return nil, err
		}
		if n == 0 || int(n) > len(remaining) {
			return nil, io.ErrUnexpectedEOF
		}
		remaining = remaining[n:]
	}
	return buf, nil
}

func (b *Bridge) ControlPlayback(action bridge.PlaybackAction) error {
	slot := map[bridge.PlaybackAction]int{
		bridge.Play: 10, bridge.Pause: 11, bridge.Next: 16, bridge.Previous: 17,
	}[action]
	if slot == 0 {
		return fmt.Errorf("unknown playback action %q", action)
	}
	return b.mediaCommand(slot)
}

func (b *Bridge) SeekMS(positionMS int64) error {
	if positionMS < 0 || positionMS > math.MaxInt64/ticksPerMS {
		return fmt.Errorf("invalid seek position %d ms", positionMS)
	}
	return b.mediaCommand(24, uintptr(positionMS*ticksPerMS))
}

func (b *Bridge) mediaCommand(slot int, args ...uintptr) error {
	b.mediaMu.Lock()
	defer b.mediaMu.Unlock()
	err := withMediaSession(func(session *comObject) error { return sendMediaCommand(session, slot, args...) })
	b.mediaAt = time.Time{}
	if err != nil {
		return fmt.Errorf("control MyGo Music media: %w", err)
	}
	return nil
}

func sendMediaCommand(session *comObject, slot int, args ...uintptr) error {
	op, err := session.object(slot, args...)
	if err != nil {
		return err
	}
	if op == nil {
		return bridge.ErrUnavailable
	}
	var accepted uint8
	if err := await(op, unsafe.Pointer(&accepted)); err != nil {
		return err
	}
	if accepted == 0 {
		return errors.New("MyGo Music rejected the media command")
	}
	return nil
}
