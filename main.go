package main

import (
	"context"
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
	var ctx context.Context // set in OnStartup, read by menu callbacks
	// Mac options must be set, or Wails disables the zoom/fullscreen button. TabFocusesLinks makes Tab reach buttons and checkboxes
	// even when macOS "Keyboard navigation" is off; without it WebKit only tabs between text fields and lists (CV-202).
	err = wails.Run(&options.App{
		Title:            "Conveyor",
		Width:            1280,
		Height:           820,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: core.AttachmentHandler(home)},
		Mac:              &mac.Options{Preferences: &mac.Preferences{TabFocusesLinks: mac.Enabled}},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		Menu:             appMenu(func() context.Context { return ctx }),
		OnStartup:        func(c context.Context) { ctx = c; app.Startup(c) },
		OnShutdown:       app.Shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
