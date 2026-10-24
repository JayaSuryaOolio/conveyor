package main

import (
	"embed"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	useLoginShellPath()
	app := newApp()
	home, err := core.Home("") // same resolution as App.Startup
	if err != nil {
		println("Error:", err.Error())
		return
	}
	err = wails.Run(&options.App{
		Title:            "Conveyor",
		Width:            1280,
		Height:           820,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: core.AttachmentHandler(home)},
		Mac:              &mac.Options{}, // without it Wails disables the zoom/fullscreen button
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.Startup,
		OnShutdown:       app.Shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
