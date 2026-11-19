//go:build devtools || dev || debug

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// addDevtoolsItem adds Window > Inspect. Compiled in via wails.json `build:tags: devtools`
// (every wails build/dev); a plain go build omits it. Wails v2 exposes no Go API for the inspector, so we post the
// same message its own Cmd+Shift+Fn+F12 hotkey does. Idle cost is nil: the
// inspector is a WebKit window that only exists once opened.
func addDevtoolsItem(win *menu.Menu, getCtx func() context.Context) {
	win.AddSeparator()
	win.AddText("Inspect", keys.Combo("i", keys.OptionOrAltKey, keys.CmdOrCtrlKey), func(*menu.CallbackData) {
		runtime.WindowExecJS(getCtx(), `window.WailsInvoke("wails:openInspector")`)
	})
}
