package linuxbridge

import (
	"errors"
	"math"
	"testing"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

func TestPulseStream(t *testing.T) {
	mygo := `{"index":0,"properties":{"application.process.id":"42","application.process.binary":"mygo-music"},
		"volume":{"front-left":{"value":32768},"front-right":{"value":65536}}}`
	browser := `{"index":9,"properties":{"application.process.id":"42","application.process.binary":"firefox"}}`
	stale := `{"index":3,"properties":{"application.process.id":"900","application.process.binary":"mygo-music"}}`
	host := func(pid uint32) bool { return pid == 42 }
	stream, err := matchStream([]byte("["+browser+","+stale+","+mygo+"]"), host)
	if err != nil || stream.Index == nil || *stream.Index != 0 {
		t.Fatalf("stream = %+v, %v", stream, err)
	}
	if level, err := streamLevel(stream); err != nil || level != 0.75 {
		t.Fatalf("volume = %v, %v", level, err)
	}
	for _, data := range []string{"[" + browser + "," + stale + "]", "[" + mygo + "," + mygo + "]"} {
		if _, err := matchStream([]byte(data), host); !errors.Is(err, bridge.ErrUnavailable) {
			t.Fatalf("absent or ambiguous stream error = %v", err)
		}
	}
	if volume, err := pulseVolume(0.53); err != nil || volume != "34734" {
		t.Fatalf("pactl volume = %q, %v", volume, err)
	}
	for _, invalid := range []float64{-0.1, 1.1, math.NaN(), math.Inf(1)} {
		if _, err := pulseVolume(invalid); err == nil {
			t.Fatalf("accepted invalid volume %v", invalid)
		}
	}
}
