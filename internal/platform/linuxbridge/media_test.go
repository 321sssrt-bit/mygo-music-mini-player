package linuxbridge

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/bridge"
	"github.com/godbus/dbus/v5"
)

func TestMPRISOwner(t *testing.T) {
	names := []string{
		mprisRoot + ".chromium",
		mprisRoot + ".MyGo_Music_hijack",
		mprisRoot + ".MyGo_Music.instance42",
		mprisRoot + ".MyGo_Music.instance700",
		mprisRoot + ".MyGo_Music.instance900",
		mprisRoot + ".dev.egoist.music",
	}
	owners := map[string]string{names[2]: ":1.42", names[3]: ":1.700", names[4]: ":1.42", names[5]: ":1.42"}
	pids := map[string]uint32{":1.42": 42, ":1.700": 700}
	bus := func(destination, method string, args ...any) *dbus.Call {
		if destination != busName {
			t.Fatalf("enumeration contacted %s", destination)
		}
		switch method {
		case busName + ".ListNames":
			return &dbus.Call{Body: []any{names}}
		case busName + ".GetNameOwner":
			if owner := owners[args[0].(string)]; owner != "" {
				return &dbus.Call{Body: []any{owner}}
			}
		case busName + ".GetConnectionUnixProcessID":
			return &dbus.Call{Body: []any{pids[args[0].(string)]}}
		}
		t.Fatalf("unexpected enumeration call %s %v", method, args)
		return nil
	}
	if got, err := mediaOwner(bus, func(pid uint32) bool { return pid == 42 }); err != nil || got != ":1.42" {
		t.Fatalf("owner = %q, %v", got, err)
	}
	for _, check := range []func(uint32) bool{
		func(uint32) bool { return false },
		func(uint32) bool { return true },
	} {
		if got, err := mediaOwner(bus, check); got != "" || !errors.Is(err, bridge.ErrUnavailable) {
			t.Fatalf("missing or ambiguous owner = %q, %v", got, err)
		}
	}
}

func TestMPRISMediaAndCommands(t *testing.T) {
	track := dbus.ObjectPath("/org/mygo/track/7")
	meta := dbus.MakeVariant(map[string]dbus.Variant{
		"xesam:title":   dbus.MakeVariant("Spring"),
		"xesam:artist":  dbus.MakeVariant([]string{"Vivaldi", "I Musici"}),
		"xesam:album":   dbus.MakeVariant("Concertos"),
		"mpris:length":  dbus.MakeVariant(int64(180000000)),
		"mpris:trackid": dbus.MakeVariant(track),
		"mpris:artUrl":  dbus.MakeVariant("file:///tmp/Spring%20Cover.jpg"),
	})
	snapshot, artURL := mediaSnapshot(map[string]dbus.Variant{
		"Metadata":       meta,
		"PlaybackStatus": dbus.MakeVariant("Playing"),
		"Position":       dbus.MakeVariant(int64(1234999)),
	})
	want := bridge.MediaSnapshot{
		Available: true, Playing: true, Title: "Spring", Artist: "Vivaldi, I Musici",
		Album: "Concertos", DurationMS: 180000, PositionMS: 1234,
	}
	if !reflect.DeepEqual(snapshot, want) || artURL != "file:///tmp/Spring%20Cover.jpg" {
		t.Fatalf("media snapshot = %+v [%s]", snapshot, artURL)
	}
	var calls []string
	bus := func(dest, method string, args ...any) *dbus.Call {
		if dest != ":1.42" {
			t.Fatalf("destination = %q", dest)
		}
		calls = append(calls, method)
		switch method {
		case props + ".Get":
			if !reflect.DeepEqual(args, []any{player, "Metadata"}) {
				t.Fatalf("get arguments = %v", args)
			}
			return &dbus.Call{Body: []any{meta}}
		case player + ".SetPosition":
			if !reflect.DeepEqual(args, []any{track, int64(1234000)}) {
				t.Fatalf("seek arguments = %v", args)
			}
		}
		return &dbus.Call{}
	}
	for _, action := range []bridge.PlaybackAction{bridge.Play, bridge.Pause, bridge.Previous, bridge.Next} {
		if err := controlPlayback(bus, ":1.42", action); err != nil {
			t.Fatal(err)
		}
	}
	if err := seekMS(bus, ":1.42", 1234); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []int64{-1, math.MaxInt64} {
		if err := seekMS(bus, ":1.42", invalid); err == nil {
			t.Fatalf("accepted invalid seek %d", invalid)
		}
	}
	if want := []string{player + ".Play", player + ".Pause", player + ".Previous", player + ".Next", props + ".Get", player + ".SetPosition"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("methods = %v", calls)
	}
	meta = dbus.MakeVariant(map[string]dbus.Variant{"mpris:trackid": dbus.MakeVariant("not an object path")})
	if err := seekMS(bus, ":1.42", 1234); !errors.Is(err, bridge.ErrUnavailable) || calls[len(calls)-1] != props+".Get" {
		t.Fatalf("seek without track = %v, final call %s", err, calls[len(calls)-1])
	}
}
