package ui

import (
	"errors"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
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
func (a *App) DeleteApp(name string) error { return a.svc.UninstallApp(name) }

// InstallManifest installs an app from manifest JSON (text box or uploaded file); replace overwrites a same-named app.
func (a *App) InstallManifest(raw string, replace bool) error {
	_, err := a.svc.InstallManifest(raw, replace)
	return err
}

// ExportApp saves the manifest JSON of a manifest app to a file the user picks and returns its path ("" if cancelled).
func (a *App) ExportApp(name string) (string, error) {
	raw, err := a.svc.ExportApp(name)
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export manifest", DefaultFilename: strings.Join(strings.Fields(name), "-") + ".json"})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, []byte(raw+"\n"), 0o600); err != nil {
		return "", err
	}
	log.Printf("exported manifest of app %q to %s", name, path)
	return path, nil
}

// ValidateManifest returns why raw is not an installable manifest, "" when it is (live check in the builder).
func (a *App) ValidateManifest(raw string) string {
	if _, err := core.ParseManifest(raw); err != nil {
		return err.Error()
	}
	return ""
}

// ManifestSchema lists what the builder can insert: "vars" (${{ var }} placeholders), "places" and "version".
func (a *App) ManifestSchema() map[string][]string {
	places := make([]string, 0, len(core.Placements))
	for p := range core.Placements {
		places = append(places, p)
	}
	sort.Strings(places)
	return map[string][]string{"vars": core.ManifestVars, "places": places, "version": {strconv.Itoa(core.ManifestVersion)}}
}

// ConvertApp turns a plain command app into a manifest app.
func (a *App) ConvertApp(name string) error { return a.svc.ConvertApp(name) }

// InstallPreset adds a detected app as a manifest app.
func (a *App) InstallPreset(x store.App) error {
	_, err := a.svc.InstallLegacy(x)
	return err
}

// PlacedActions lists manifest actions wanting a UI slot (global_nav, task_sidebar, board_card).
func (a *App) PlacedActions(place string) ([]core.Placed, error) { return a.svc.PlacedActions(place) }

// RunAppAction runs a manifest action; key "" for global_nav ones.
func (a *App) RunAppAction(app, id, key string) (string, error) {
	out, err := a.svc.RunAppAction(app, id, key)
	if err != nil {
		log.Printf("app %s/%s %s: %v", app, id, key, err)
	}
	return out, err
}

// AppServiceRunning reports whether a service action is running for the task.
func (a *App) AppServiceRunning(app, id, key string) (bool, error) {
	return a.svc.AppServiceRunning(app, id, key)
}

// StopAppService stops a running service action.
func (a *App) StopAppService(app, id, key string) error {
	err := a.svc.StopAppService(app, id, key)
	if err != nil {
		log.Printf("app %s/%s stop %s: %v", app, id, key, err)
	}
	return err
}

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
func (a *App) OpenApp(name, key, repo string) (string, error) {
	out, err := a.svc.OpenApp(name, key, repo)
	if err != nil {
		log.Printf("app %s %s: %v", name, key, err)
	}
	return out, err
}

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
