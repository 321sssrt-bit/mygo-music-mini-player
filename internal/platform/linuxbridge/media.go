package linuxbridge

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
	"github.com/godbus/dbus/v5"
)

const (
	mprisRoot = "org.mpris.MediaPlayer2"
	player    = mprisRoot + ".Player"
	mprisPath = "/org/mpris/MediaPlayer2"
	busName   = "org.freedesktop.DBus"
	props     = "org.freedesktop.DBus.Properties"
	hostName  = "MyGo Music"
	hostSlug  = "mygo-music"
	hostID    = "dev.egoist.music"
)

type mediaBus func(destination, method string, args ...any) *dbus.Call

func hostService(name string) (instance uint32, ok bool) {
	for _, id := range []string{"MyGo_Music", hostSlug, hostID} {
		base := mprisRoot + "." + id
		if name == base {
			return 0, true
		}
		if suffix, found := strings.CutPrefix(name, base+".instance"); found {
			pid, err := strconv.ParseUint(suffix, 10, 32)
			return uint32(pid), err == nil && pid > 0
		}
	}
	return 0, false
}

func mediaOwner(bus mediaBus, isHost func(uint32) bool) (string, error) {
	var names []string
	if err := bus(busName, busName+".ListNames").Store(&names); err != nil {
		return "", err
	}
	owner := ""
	for _, name := range names {
		instance, ok := hostService(name)
		if !ok {
			continue
		}
		var unique string
		if err := bus(busName, busName+".GetNameOwner", name).Store(&unique); err != nil {
			if gone(err) {
				continue
			}
			return "", err
		}
		var pid uint32
		if err := bus(busName, busName+".GetConnectionUnixProcessID", unique).Store(&pid); err != nil {
			if gone(err) {
				continue
			}
			return "", err
		}
		if (instance != 0 && instance != pid) || !isHost(pid) {
			continue
		}
		if owner != "" && owner != unique {
			return "", fmt.Errorf("multiple MyGo Music MPRIS owners: %w", bridge.ErrUnavailable)
		}
		// Subsequent calls address this connection even if the service name gets reassigned.
		owner = unique
	}
	if owner == "" {
		return "", bridge.ErrUnavailable
	}
	return owner, nil
}

func gone(err error) bool {
	switch e := err.(type) {
	case dbus.Error:
		return e.Name == busName+".Error.NameHasNoOwner" || e.Name == busName+".Error.ServiceUnknown"
	case *dbus.Error:
		return gone(*e)
	default:
		return false
	}
}

func property[T any](properties map[string]dbus.Variant, key string) T {
	value, _ := properties[key].Value().(T)
	return value
}

func mediaSnapshot(properties map[string]dbus.Variant) (bridge.MediaSnapshot, string) {
	meta := property[map[string]dbus.Variant](properties, "Metadata")
	return bridge.MediaSnapshot{
		Available:  true,
		Title:      property[string](meta, "xesam:title"),
		Artist:     strings.Join(property[[]string](meta, "xesam:artist"), ", "),
		Album:      property[string](meta, "xesam:album"),
		Playing:    property[string](properties, "PlaybackStatus") == "Playing",
		DurationMS: max(0, property[int64](meta, "mpris:length")/1000),
		PositionMS: max(0, property[int64](properties, "Position")/1000),
	}, property[string](meta, "mpris:artUrl")
}

func controlPlayback(bus mediaBus, owner string, action bridge.PlaybackAction) error {
	var method string
	switch action {
	case bridge.Play:
		method = "Play"
	case bridge.Pause:
		method = "Pause"
	case bridge.Previous:
		method = "Previous"
	case bridge.Next:
		method = "Next"
	default:
		return fmt.Errorf("unknown playback action %q", action)
	}
	return bus(owner, player+"."+method).Err
}

func seekMS(bus mediaBus, owner string, position int64) error {
	if position < 0 || position > math.MaxInt64/1000 {
		return fmt.Errorf("invalid playback position %d ms", position)
	}
	var metadata dbus.Variant
	if err := bus(owner, props+".Get", player, "Metadata").Store(&metadata); err != nil {
		return err
	}
	meta, _ := metadata.Value().(map[string]dbus.Variant)
	track := property[dbus.ObjectPath](meta, "mpris:trackid")
	if !track.IsValid() || track == mprisPath+"/TrackList/NoTrack" {
		return fmt.Errorf("MyGo Music track ID: %w", bridge.ErrUnavailable)
	}
	return bus(owner, player+".SetPosition", track, position*1000).Err
}
