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

// RenderApp previews command for cwd and task key.
func (a *App) RenderApp(command, cwd, key string) string { return core.RenderApp(command, cwd, key) }

// TestApp runs app x (saved or not) on the user's home directory with no task key.
func (a *App) TestApp(x store.App) (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return core.RunApp(x, h, "")
}

// OpenApp runs the named app in the task repo's worktree ("" repo = primary); an output app returns its output.
func (a *App) OpenApp(name, key, repo string) (string, error) { return a.svc.OpenApp(name, key, repo) }

// AppIcons maps configured app names to PNG data: URLs of their macOS icons.
func (a *App) AppIcons() (map[string]string, error) {
	apps, err := store.ListApps(a.svc.DB)
	if err != nil {
		return nil, err
	}
	return core.AppIcons(a.svc.Home, apps), nil
}

// SpotlightRoot returns the root checkout spotlight mirrors worktree into, "" when inactive for it.
func (a *App) SpotlightRoot(worktree string) string { return core.SpotlightRoot(worktree) }

// SpotlightStop stops the spotlight instance mirroring into root.
func (a *App) SpotlightStop(root string) error { return core.SpotlightStop(root) }

// UserHooks reports or changes Conveyor's hooks in ~/.claude/settings.json ("status", "install", "uninstall").
func (a *App) UserHooks(action string) (string, error) { return a.svc.UserHooks(action) }
