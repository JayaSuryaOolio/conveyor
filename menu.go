package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// appMenu is the macOS menu bar. Wails' WindowMenu() is a native role whose
// submenu can't be extended, so Window is spelled out to leave room for the
// devtools item. ctx is set by OnStartup (via getCtx) before any click.
// Refactor: once Wails v3 lands, devtools become a first-class option and this
// file can shrink back to role menus.
func appMenu(getCtx func() context.Context) *menu.Menu {
	win := menu.NewMenu()
	win.AddText("Minimize", keys.CmdOrCtrl("m"), func(*menu.CallbackData) { runtime.WindowMinimise(getCtx()) })
	win.AddText("Zoom", nil, func(*menu.CallbackData) { runtime.WindowToggleMaximise(getCtx()) })
	addDevtoolsItem(win, getCtx)

	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	m.Append(menu.SubMenu("Window", win))
	return m
}
