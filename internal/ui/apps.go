package ui

import (
	"errors"
	"os"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ListApps returns the configured apps.
func (a *App) ListApps() ([]store.App, error) { return store.ListApps(a.svc.DB) }

// AppPresets returns the apps detected on this Mac that can be added in one click.
func (a *App) AppPresets() []store.App { return core.AppPresets() }

func validApp(x store.App) error {
	if strings.TrimSpace(x.Name) == "" || strings.TrimSpace(x.Command) == "" {
		return errors.New("name and command are required")
	}
	return nil
}

// AddApp creates an app.
func (a *App) AddApp(x store.App) error {
	if err := validApp(x); err != nil {
		return err
	}
	_, err := store.CreateApp(a.svc.DB, x)
	return err
}

// UpdateApp edits the app with x.ID.
func (a *App) UpdateApp(x store.App) error {
	if err := validApp(x); err != nil {
		return err
	}
	return store.UpdateApp(a.svc.DB, x)
}

// DeleteApp removes an app.
func (a *App) DeleteApp(name string) error { return store.DeleteApp(a.svc.DB, name) }

// RenderApp previews command for cwd.
func (a *App) RenderApp(command, cwd string) string { return core.RenderApp(command, cwd) }

// TestApp runs command (saved or not) on the user's home directory.
func (a *App) TestApp(command string) error {
	h, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return core.LaunchApp(command, h)
}

// OpenApp opens the named app in the task repo's worktree ("" repo = primary).
func (a *App) OpenApp(name, key, repo string) error { return a.svc.OpenApp(name, key, repo) }

// UserHooks reports or changes Conveyor's hooks in ~/.claude/settings.json ("status", "install", "uninstall").
func (a *App) UserHooks(action string) (string, error) { return a.svc.UserHooks(action) }
