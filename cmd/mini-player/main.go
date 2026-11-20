package main

import (
	"log"
	"os"
	"runtime"

	"github.com/321sssrt-bit/mygo-music-mini-player/internal/miniplayer"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	switch runtime.GOOS {
	case "windows", "linux":
	case "darwin":
		log.Print("macOS 暂不支持。宿主已内置 Notch Player，且 macOS 15.4+ 限制第三方访问媒体会话。")
		return
	default:
		log.Print("Mini Player supports Windows and Linux.")
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "render-frame" {
		if err := renderFrame(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:             "Mini Player",
			Width:             miniplayer.RestingSize,
			Height:            miniplayer.RestingSize,
			Panel:             true,
			Frameless:         true,
			Transparent:       true,
			DisableShadow:     true,
			DisableResize:     true,
			IgnoreMouseEvents: true,
			Content:           ui.View(miniplayer.View),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
