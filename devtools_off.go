//go:build !(devtools || dev || debug)

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

// addDevtoolsItem is a no-op in plain release builds (no inspector compiled in).
func addDevtoolsItem(*menu.Menu, func() context.Context) {}
