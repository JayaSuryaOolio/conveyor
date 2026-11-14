package ui

import (
	"errors"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/connectors"
	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/runner"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ListConnectors returns the configured apps.
func (a *App) ListConnectors() ([]store.Connector, error) { return store.ListConnectors(a.svc.DB) }

// ConnectorPresets returns the apps detected on this Mac that can be added in one click.
func (a *App) ConnectorPresets() []store.Connector { return connectors.ConnectorPresets() }

// AddConnector installs a shell command ({cwd}/{key} placeholders) as a manifest app; output makes it a quick action.
func (a *App) AddConnector(name, command string, output bool) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(command) == "" {
		return errors.New("name and command are required")
	}
	_, err := a.svc.InstallCommand(name, command, output)
	return err
}

// DeleteConnector removes an app.
func (a *App) DeleteConnector(name string) error { return a.svc.UninstallConnector(name) }

// InstallManifest installs an app from manifest JSON (text box or uploaded file); replace overwrites a same-named app.
func (a *App) InstallManifest(raw string, replace bool) error {
	_, err := a.svc.InstallManifest(raw, replace)
	return err
}

// SetConnectorDisabled turns an app off or on; its manifest stays so it can be re-enabled in one click.
func (a *App) SetConnectorDisabled(name string, disabled bool) error {
	return store.SetConnectorDisabled(a.svc.DB, name, disabled)
}

// ExportConnector saves the manifest JSON of a manifest app to a file the user picks and returns its path ("" if cancelled).
func (a *App) ExportConnector(name string) (string, error) {
	raw, err := a.svc.ExportConnector(name)
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
	if _, err := connectors.ParseManifest(raw); err != nil {
		return err.Error()
	}
	return ""
}

// ManifestSchema lists what the builder can insert: "vars" (${{ var }} placeholders), "places" and "version".
func (a *App) ManifestSchema() map[string][]string {
	places := make([]string, 0, len(connectors.Placements))
	for p := range connectors.Placements {
		places = append(places, p)
	}
	sort.Strings(places)
	return map[string][]string{"vars": connectors.ManifestVars, "places": places, "version": {strconv.Itoa(connectors.ManifestVersion)}}
}

// ConnectorSettings lists a manifest app's settings with their current values.
func (a *App) ConnectorSettings(name string) ([]connectors.ConnectorSetting, error) {
	return a.svc.ConnectorSettings(name)
}

// SaveConnectorSettings stores the app's setting values (key -> value).
func (a *App) SaveConnectorSettings(name string, vals map[string]string) error {
	return a.svc.SaveConnectorSettings(name, vals)
}

// InstallPreset adds a detected app as a manifest app.
func (a *App) InstallPreset(x store.Connector) error {
	_, err := a.svc.InstallManifest(x.Manifest, false)
	return err
}

// PlacedActions lists manifest actions wanting a UI slot (global_nav, task_sidebar, board_card).
func (a *App) PlacedActions(place string) ([]connectors.Placed, error) {
	return a.svc.PlacedActions(place)
}

// RunConnectorAction runs a manifest action; key "" for global_nav ones.
func (a *App) RunConnectorAction(app, id, place, key string) (string, error) {
	out, err := a.svc.RunConnectorAction(app, id, place, key)
	if err != nil {
		log.Printf("app %s/%s %s: %v", app, id, key, err)
	}
	return out, err
}

// ConnectorServiceRunning reports whether a service action is running for the task.
func (a *App) ConnectorServiceRunning(app, id, place, key string) (bool, error) {
	return a.svc.ConnectorServiceRunning(app, id, place, key)
}

// StopConnectorService stops a running service action.
func (a *App) StopConnectorService(app, id, place, key string) error {
	err := a.svc.StopConnectorService(app, id, place, key)
	if err != nil {
		log.Printf("app %s/%s stop %s: %v", app, id, key, err)
	}
	return err
}

// OpenConnector runs the named app in the task repo's worktree ("" repo = primary); an output action returns its output.
func (a *App) OpenConnector(name, key, repo string) (string, error) {
	out, err := a.svc.OpenConnector(name, key, repo)
	if err != nil {
		log.Printf("app %s %s: %v", name, key, err)
	}
	return out, err
}

// ConnectorIcons maps configured app names to PNG data: URLs of their macOS icons.
func (a *App) ConnectorIcons() (map[string]string, error) {
	list, err := store.ListConnectors(a.svc.DB)
	if err != nil {
		return nil, err
	}
	return connectors.ConnectorIcons(list), nil
}

// GraftRoot returns the root checkout graft mirrors worktree into, "" when inactive for it.
func (a *App) GraftRoot(worktree string) string { return core.GraftRoot(worktree) }

// GraftStop stops the graft instance mirroring into root.
func (a *App) GraftStop(root string) error { return core.GraftStop(root) }

// Plugin reports or changes the Conveyor plugin per tool ("" = all; "status", "install", "uninstall").
func (a *App) Plugin(tool, action string) ([]runner.PluginStatus, error) {
	return a.svc.Plugin(tool, action)
}

// ExampleConnectors lists the bundled example apps (shown under Templates).
func (a *App) ExampleConnectors() []connectors.Example { return connectors.ExampleConnectors() }

// InstallExample installs a bundled example app by file name (no extension).
func (a *App) InstallExample(file string) error { return a.svc.InstallExample(file) }
