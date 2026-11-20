package linuxbridge

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
)

const pulseNormal = 65536

type pulseStream struct {
	Index      *uint32
	Properties map[string]string
	Volume     map[string]struct{ Value *uint32 }
}

func matchStream(data []byte, isHost func(uint32) bool) (pulseStream, error) {
	var streams []pulseStream
	if err := json.Unmarshal(data, &streams); err != nil {
		return pulseStream{}, fmt.Errorf("decode pactl sink inputs: %w", err)
	}
	var host pulseStream
	for _, stream := range streams {
		pid, err := strconv.ParseUint(stream.Properties["application.process.id"], 10, 32)
		if err != nil || pid == 0 || stream.Index == nil {
			continue
		}
		if strings.TrimSuffix(stream.Properties["application.process.binary"], " (deleted)") != hostSlug {
			continue
		}
		if !isHost(uint32(pid)) {
			continue
		}
		if host.Index != nil {
			return pulseStream{}, fmt.Errorf("multiple MyGo Music audio streams: %w", bridge.ErrUnavailable)
		}
		host = stream
	}
	if host.Index == nil {
		return pulseStream{}, bridge.ErrUnavailable
	}
	return host, nil
}

func streamLevel(stream pulseStream) (float64, error) {
	var total float64
	for _, channel := range stream.Volume {
		if channel.Value == nil {
			return 0, bridge.ErrUnavailable
		}
		total += float64(*channel.Value)
	}
	if len(stream.Volume) == 0 {
		return 0, bridge.ErrUnavailable
	}
	return min(total/float64(len(stream.Volume))/pulseNormal, 1), nil
}

func pulseVolume(level float64) (string, error) {
	if math.IsNaN(level) || level < 0 || level > 1 {
		return "", fmt.Errorf("audio level %v is outside [0, 1]", level)
	}
	return strconv.FormatUint(uint64(math.Round(level*pulseNormal)), 10), nil
}
