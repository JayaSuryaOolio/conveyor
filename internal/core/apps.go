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
	"github.com/JayaSuryaOolio/conveyor/internal/apps"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Thin delegates to internal/apps for the CLI, UI and dispatcher.

func (s *Service) WorktreePath(key, repo string) (string, error) {
	return apps.WorktreePath(s.DB, key, repo)
}
func (s *Service) OpenApp(name, key, repo string) (string, error) {
	return apps.OpenApp(s.DB, name, key, repo)
}
func (s *Service) AppSettings(name string) ([]apps.AppSetting, error) {
	return apps.AppSettings(s.DB, name)
}
func (s *Service) SaveAppSettings(name string, vals map[string]string) error {
	return apps.SaveAppSettings(s.DB, name, vals)
}
func (s *Service) PlacedActions(place string) ([]apps.Placed, error) {
	return apps.PlacedActions(s.DB, place)
}
func (s *Service) RunAppAction(app, id, place, key string) (string, error) {
	return apps.RunAppAction(s.DB, app, id, place, key)
}
func (s *Service) AppServiceRunning(app, id, place, key string) (bool, error) {
	return apps.AppServiceRunning(s.DB, app, id, place, key)
}
func (s *Service) StopAppService(app, id, place, key string) error {
	return apps.StopAppService(s.DB, app, id, place, key)
}
func (s *Service) ConvertApp(name string) error          { return apps.ConvertApp(s.DB, s.Home, name) }
func (s *Service) ExportApp(name string) (string, error) { return apps.ExportApp(s.DB, name) }
func (s *Service) EnsureServices() error                 { return apps.EnsureServices(s.DB) }

// InstallExample installs a bundled example by its apps.ExampleApps File (the same path as importing a manifest).
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
func (s *Service) InstallManifest(raw string, replace bool) (store.App, error) {
	m, err := apps.ParseManifest(raw)
	if err != nil {
		return store.App{}, err
	}
	if m.Check != "" {
		if err := runCheck(m.Check); err != nil {
			return store.App{}, fmt.Errorf("%s: sample run failed (is the tool installed and on PATH?): %w", m.Name, err)
		}
	}
	// read icons before a replace uninstalls the copies an exported manifest points at
	icons, err := apps.ReadIcons(m)
	if err != nil {
		return store.App{}, err
	}
	if _, err := store.GetApp(s.DB, m.Name); err == nil && replace {
		// ponytail: not atomic; a failed install after this leaves the old app gone
		if err := s.UninstallApp(m.Name); err != nil {
			return store.App{}, err
		}
	}
	if raw, err = apps.WriteIcons(s.Home, &m, icons, raw); err != nil {
		return store.App{}, err
	}
	a, err := store.CreateApp(s.DB, store.App{Name: m.Name, Manifest: raw})
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
		_ = s.UninstallApp(m.Name)
		return store.App{}, err
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

// UninstallApp deletes the app; a manifest app's automations and agents go with it.
func (s *Service) UninstallApp(name string) error {
	a, err := store.GetApp(s.DB, name)
	if err != nil {
		return err
	}
	if a.Manifest != "" {
		if m, err := apps.ParseManifest(a.Manifest); err == nil {
			for _, x := range m.Automations {
				_ = store.DeleteAutomation(s.DB, x.Name)
			}
			for _, x := range m.Actions {
				if x.IconPath != "" && filepath.Dir(x.IconPath) == apps.IconsDir(s.Home) {
					_ = os.Remove(x.IconPath)
				}
			}
			for _, g := range m.Agents {
				_ = os.Remove(userAgentsDir() + "/" + g.Name + ".md")
			}
		}
	}
	return store.DeleteApp(s.DB, name)
}

// InstallLegacy installs a plain command app (e.g. a detected preset) as a manifest app.
func (s *Service) InstallLegacy(a store.App) (store.App, error) {
	raw, err := apps.LegacyManifest(a)
	if err != nil {
		return store.App{}, err
	}
	return s.InstallManifest(raw, false)
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
