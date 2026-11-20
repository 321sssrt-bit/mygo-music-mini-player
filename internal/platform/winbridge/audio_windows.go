//go:build windows && (amd64 || arm64)

package winbridge

import (
	"errors"
	"fmt"
	"math"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

var (
	coCreateInstance = ole32.NewProc("CoCreateInstance")
	clsidMMDevice    = guid(0xbcde0395, 0xe52f, 0x467c, [8]byte{0x8e, 0x3d, 0xc4, 0x57, 0x92, 0x91, 0x69, 0x2e})
	iidMMDeviceEnum  = guid(0xa95664d2, 0x9614, 0x4f35, [8]byte{0xa7, 0x46, 0xde, 0x8d, 0xb6, 0x36, 0x17, 0xe6})
	iidAudioManager  = guid(0x77aa99a0, 0x1bd6, 0x484f, [8]byte{0x8b, 0xc7, 0x2c, 0x65, 0x4c, 0x9a, 0x9b, 0x6f})
	iidSessionCtrl2  = guid(0xbfb7ff88, 0x7239, 0x4fc9, [8]byte{0x8f, 0xa2, 0x07, 0xc9, 0x50, 0xbe, 0x9c, 0x6d})
	iidSimpleVolume  = guid(0x87ce5498, 0x68d6, 0x44e5, [8]byte{0x92, 0x15, 0x6d, 0xa4, 0x7e, 0xf8, 0x83, 0xd8})
	iidAudioMeter    = guid(0xc02216f6, 0x8c67, 0x4b5b, [8]byte{0x9d, 0x00, 0xd0, 0x08, 0xe7, 0x3e, 0x00, 0x64})
)

const (
	clsctxInprocServer = 1
	sessionInactive    = 0
	sessionActive      = 1
	eNoInterface       = hresult(0x80004002)
)

func audioSessions(use func(*comObject, int32) error) error {
	localSession, err := processSession(uint32(os.Getpid()))
	if err != nil {
		return err
	}
	return inApartment(false, func() error {
		var enumerator *comObject
		ret, _, _ := coCreateInstance.Call(
			uintptr(unsafe.Pointer(&clsidMMDevice)), 0, clsctxInprocServer,
			uintptr(unsafe.Pointer(&iidMMDeviceEnum)), uintptr(unsafe.Pointer(&enumerator)),
		)
		if err := checkHR(ret); err != nil {
			return err
		}
		defer enumerator.release()
		devices, err := enumerator.object(3, 0, 1) // EnumAudioEndpoints(eRender, DEVICE_STATE_ACTIVE)
		if err != nil {
			return err
		}
		defer devices.release()
		var count uint32
		if err := checkHR(devices.call(3, uintptr(unsafe.Pointer(&count)))); err != nil {
			return err
		}
		var result error
		for i := uint32(0); i < count; i++ {
			if err := deviceSessions(devices, i, localSession, use); err != nil {
				result = errors.Join(result, err)
			}
		}
		return result
	})
}

func deviceSessions(devices *comObject, index, localSession uint32, use func(*comObject, int32) error) error {
	device, err := devices.object(4, uintptr(index)) // IMMDeviceCollection.Item
	if err != nil {
		return err
	}
	defer device.release()
	manager, err := device.object(3, uintptr(unsafe.Pointer(&iidAudioManager)), clsctxInprocServer, 0) // Activate
	if err != nil {
		return err
	}
	defer manager.release()
	sessions, err := manager.object(5) // IAudioSessionManager2.GetSessionEnumerator
	if err != nil {
		return err
	}
	defer sessions.release()
	var count int32
	if err := checkHR(sessions.call(3, uintptr(unsafe.Pointer(&count)))); err != nil {
		return err
	}
	var result error
	for i := int32(0); i < count; i++ {
		session, err := sessions.object(4, uintptr(i))
		if err == nil {
			err = useHostAudioSession(session, localSession, hostImage, use)
			session.release()
		}
		if err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func useHostAudioSession(session *comObject, localSession uint32, lookup func(uint32, uint32) string, use func(*comObject, int32) error) error {
	control, err := session.query(&iidSessionCtrl2)
	if err != nil {
		return err
	}
	defer control.release()
	var pid uint32
	// A positive GetProcessId HRESULT denotes a session shared across processes.
	if ret := control.call(14, uintptr(unsafe.Pointer(&pid))); ret != 0 || lookup(pid, localSession) == "" {
		return nil
	}
	var state int32
	if err := checkHR(control.call(3, uintptr(unsafe.Pointer(&state)))); err != nil {
		return err
	}
	if state != sessionInactive && state != sessionActive {
		return nil
	}
	return use(control, state)
}

func (b *Bridge) Volume() (float64, error) {
	b.audioMu.Lock()
	defer b.audioMu.Unlock()
	var level float32
	bestState := int32(-1)
	err := audioSessions(func(control *comObject, state int32) error {
		if state <= bestState {
			return nil
		}
		volume, err := control.query(&iidSimpleVolume)
		if err != nil {
			return err
		}
		defer volume.release()
		if err := checkHR(volume.call(4, uintptr(unsafe.Pointer(&level)))); err != nil {
			return err
		}
		bestState = state
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("read MyGo Music volume: %w", err)
	}
	if bestState < 0 {
		return 0, bridge.ErrUnavailable
	}
	return float64(level), nil
}

// volumeThunk returns the platform ABI entry point for ISimpleAudioVolume.SetMasterVolume.
func volumeThunk() uintptr

func (b *Bridge) SetVolume(level float64) error {
	if math.IsNaN(level) || level < 0 || level > 1 {
		return fmt.Errorf("volume %g is outside [0, 1]", level)
	}
	b.audioMu.Lock()
	defer b.audioMu.Unlock()
	b.peakAt = time.Time{}
	found := false
	err := audioSessions(func(control *comObject, _ int32) error {
		volume, err := control.query(&iidSimpleVolume)
		if err != nil {
			return err
		}
		defer volume.release()
		found = true
		ret, _, _ := syscall.SyscallN(volumeThunk(), volume.vtable[3], uintptr(unsafe.Pointer(volume)), uintptr(math.Float32bits(float32(level))))
		return checkHR(ret)
	})
	if err != nil {
		return fmt.Errorf("set MyGo Music volume: %w", err)
	}
	if !found {
		return bridge.ErrUnavailable
	}
	return nil
}

func (b *Bridge) PeakLevel() (float64, bool, error) {
	b.audioMu.Lock()
	defer b.audioMu.Unlock()
	if time.Since(b.peakAt) < 40*time.Millisecond {
		return b.peak, b.peakAvailable, nil
	}
	var peak float64
	available := false
	err := audioSessions(func(control *comObject, state int32) error {
		meter, err := control.query(&iidAudioMeter)
		if err == eNoInterface {
			return nil
		}
		if err != nil {
			return err
		}
		defer meter.release()
		available = true
		if state != sessionActive {
			return nil
		}
		volume, err := control.query(&iidSimpleVolume)
		if err != nil {
			return err
		}
		defer volume.release()
		var muted int32
		if err := checkHR(volume.call(6, uintptr(unsafe.Pointer(&muted)))); err != nil || muted != 0 {
			return err
		}
		var level float32
		if err := checkHR(volume.call(4, uintptr(unsafe.Pointer(&level)))); err != nil || level == 0 {
			return err
		}
		var value float32
		if err := checkHR(meter.call(3, uintptr(unsafe.Pointer(&value)))); err != nil {
			return err
		}
		peak = max(peak, float64(value))
		return nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("read MyGo Music peak: %w", err)
	}
	now := time.Now()
	if available {
		peak = decayPeak(b.peak, min(1, peak), now.Sub(b.peakAt))
	}
	b.peak, b.peakAvailable = peak, available
	b.peakAt = now
	return peak, available, nil
}

func decayPeak(previous, peak float64, elapsed time.Duration) float64 {
	value := max(peak, previous*math.Exp(-float64(elapsed)/float64(120*time.Millisecond)))
	if value < 0.001 {
		return 0
	}
	return value
}
