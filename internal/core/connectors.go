package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/examples"
	"github.com/JayaSuryaOolio/conveyor/internal/connectors"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Thin delegates to internal/connectors for the CLI, UI and dispatcher.

func (s *Service) WorktreePath(key, repo string) (string, error) {
	return connectors.WorktreePath(s.DB, key, repo)
}
func (s *Service) OpenConnector(name, key, repo string) (string, error) {
	return connectors.OpenConnector(s.DB, name, key, repo)
}
func (s *Service) ConnectorSettings(name string) ([]connectors.ConnectorSetting, error) {
	return connectors.ConnectorSettings(s.DB, name)
}
func (s *Service) SaveConnectorSettings(name string, vals map[string]string) error {
	return connectors.SaveConnectorSettings(s.DB, name, vals)
}
func (s *Service) PlacedActions(place string) ([]connectors.Placed, error) {
	return connectors.PlacedActions(s.DB, place)
}
func (s *Service) RunConnectorAction(app, id, place, key string) (string, error) {
	return connectors.RunConnectorAction(s.DB, app, id, place, key)
}
func (s *Service) ConnectorServiceRunning(app, id, place, key string) (bool, error) {
	return connectors.ConnectorServiceRunning(s.DB, app, id, place, key)
}
func (s *Service) StopConnectorService(app, id, place, key string) error {
	return connectors.StopConnectorService(s.DB, app, id, place, key)
}
func (s *Service) ExportConnector(name string) (string, error) {
	return connectors.ExportConnector(s.DB, name)
}
func (s *Service) EnsureServices() error { return connectors.EnsureServices(s.DB) }

// InstallExample installs a bundled example by its connectors.ExampleConnectors File (the same path as importing a manifest).
func (s *Service) InstallExample(file string) error {
	raw, err := examples.FS.ReadFile(file + ".json")
	if err != nil {
		return err
	}
	_, err = s.InstallManifest(string(raw), false)
	return err
}

// InstallManifest validates raw, stores the app and creates its agents (user scope) and
// automations (disabled until the user enables them). A failure rolls the app back.
// An app of the same name is an error unless replace, which uninstalls it first.
func (s *Service) InstallManifest(raw string, replace bool) (store.Connector, error) {
	m, err := connectors.ParseManifest(raw)
	if err != nil {
		return store.Connector{}, err
	}
	if m.Check != "" {
		if err := runCheck(m.Check); err != nil {
			return store.Connector{}, fmt.Errorf("%s: sample run failed (is the tool installed and on PATH?): %w", m.Name, err)
		}
	}
	// read icons before a replace uninstalls the copies an exported manifest points at
	icons, err := connectors.ReadIcons(m)
	if err != nil {
		return store.Connector{}, err
	}
	if _, err := store.GetConnector(s.DB, m.Name); err == nil && replace {
		// ponytail: not atomic; a failed install after this leaves the old app gone
		if err := s.UninstallConnector(m.Name); err != nil {
			return store.Connector{}, err
		}
	}
	if raw, err = connectors.WriteIcons(s.Home, &m, icons, raw); err != nil {
		return store.Connector{}, err
	}
	a, err := store.CreateConnector(s.DB, store.Connector{Name: m.Name, Manifest: raw})
	if err != nil {
		return a, err
	}
	for _, g := range m.Agents {
		if err = s.AddAgent(g.Name, g.Description, g.Prompt, "user", g.Model); err != nil {
			break
		}
	}
	if err == nil {
		for _, x := range m.Automations {
			if _, err = store.CreateAutomation(s.DB, store.Automation{Name: x.Name, Schedule: x.Schedule, Precheck: x.Precheck, ActionKind: x.ActionKind, ActionArg: x.ActionArg}); err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = s.UninstallConnector(m.Name)
		return store.Connector{}, err
	}
	return a, nil
}

func runCheck(script string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "sh", "-c", script).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UninstallConnector deletes the app; a manifest app's automations and agents go with it.
func (s *Service) UninstallConnector(name string) error {
	a, err := store.GetConnector(s.DB, name)
	if err != nil {
		return err
	}
	if a.Manifest != "" {
		if m, err := connectors.ParseManifest(a.Manifest); err == nil {
			for _, x := range m.Automations {
				_ = store.DeleteAutomation(s.DB, x.Name)
			}
			for _, x := range m.Actions {
				if x.IconPath != "" && filepath.Dir(x.IconPath) == connectors.IconsDir(s.Home) {
					_ = os.Remove(x.IconPath)
				}
			}
			for _, g := range m.Agents {
				_ = os.Remove(userAgentsDir() + "/" + g.Name + ".md")
			}
		}
	}
	return store.DeleteConnector(s.DB, name)
}

// InstallCommand installs a shell command ({cwd}/{key} placeholders) as a manifest app.
func (s *Service) InstallCommand(name, command string, output bool) (store.Connector, error) {
	return s.InstallManifest(connectors.CommandManifest(name, command, output), false)
}

// SessionIDs returns the task's Claude session ids: its runs' (oldest first), then
// manual sessions found in its worktrees.
func (s *Service) SessionIDs(key string) ([]string, error) {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return nil, err
	}
	runs, err := store.ListRuns(s.DB, t.ID)
	if err != nil {
		return nil, err
	}
	out, seen := []string{}, map[string]bool{}
	for _, r := range runs {
		if r.SessionID != nil && *r.SessionID != "" && !seen[*r.SessionID] {
			seen[*r.SessionID] = true
			out = append(out, *r.SessionID)
		}
	}
	manual, err := s.Sessions(key)
	if err != nil {
		return nil, err
	}
	for _, m := range manual {
		out = append(out, m.ID)
	}
	return out, nil
}
