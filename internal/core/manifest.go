package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Manifest declares an app as a plugin: actions placed in UI slots, plus agents and
// automations installed with it. Action `run` scripts may use ${{ var }} placeholders
// (like GitHub Actions), filled with shell-quoted values from the Vars table.
type Manifest struct {
	// Version is the manifest schema version; absent means 1. See migrateManifest.
	Version     int              `json:"version,omitempty"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Actions     []ManifestAction `json:"actions,omitempty"`
	Agents      []ManifestAgent  `json:"agents,omitempty"`
	Automations []ManifestAuto   `json:"automations,omitempty"`
}

// ManifestAction is one button. Output actions run to completion and toast their output.
type ManifestAction struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Run    string   `json:"run"`
	Places []string `json:"places"`
	Output bool     `json:"output,omitempty"`
	// Service marks a long-running action: Status (exit 0 = running) lights the icon's dot and
	// a click then runs Stop instead of Run. Task places only; excludes Output.
	Service *ManifestService `json:"service,omitempty"`
}

// ManifestService tells Conveyor how to detect and stop a service action; both are
// ${{ var }} scripts like Run.
type ManifestService struct {
	Status string `json:"status"`
	Stop   string `json:"stop"`
}

type ManifestAgent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model,omitempty"`
}

type ManifestAuto struct {
	Name       string `json:"name"`
	Schedule   string `json:"schedule"`
	Precheck   string `json:"precheck,omitempty"`
	ActionKind string `json:"action_kind"`
	ActionArg  string `json:"action_arg"`
}

// ManifestVersion is the schema version this build writes and understands.
const ManifestVersion = 1

// migrateManifest upgrades m to ManifestVersion in place. When the schema changes, bump
// ManifestVersion and add a `case N:` that converts vN to vN+1; installed apps then keep working.
func migrateManifest(m *Manifest) error {
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Version > ManifestVersion {
		return fmt.Errorf("manifest: version %d is newer than this Conveyor supports (%d)", m.Version, ManifestVersion)
	}
	return nil
}

// Placements are the UI slots an action can ask for; the first needs no task.
var Placements = map[string]bool{"global_nav": false, "task_sidebar": true, "board_card": true}

// ManifestVars are the placeholders available to `run`, e.g. ${{ task.key }}.
var ManifestVars = []string{"app.name", "task.key", "task.title", "task.status", "repo.name", "worktree.path"}

var (
	varRe = regexp.MustCompile(`\$\{\{\s*([\w.]+)\s*\}\}`)
	idRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// ParseManifest decodes and validates manifest JSON.
func ParseManifest(raw string) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("manifest: %w", err)
	}
	if err := migrateManifest(&m); err != nil {
		return m, err
	}
	if strings.TrimSpace(m.Name) == "" {
		return m, fmt.Errorf("manifest: name is required")
	}
	if len(m.Actions)+len(m.Agents)+len(m.Automations) == 0 {
		return m, fmt.Errorf("manifest: declare at least one action, agent or automation")
	}
	seen := map[string]bool{}
	for i, a := range m.Actions {
		if !idRe.MatchString(a.ID) || seen[a.ID] {
			return m, fmt.Errorf("manifest: action %d: id %q must be unique and match [a-z0-9-]+", i, a.ID)
		}
		seen[a.ID] = true
		if strings.TrimSpace(a.Label) == "" || strings.TrimSpace(a.Run) == "" || len(a.Places) == 0 {
			return m, fmt.Errorf("manifest: action %q needs label, run and places", a.ID)
		}
		for _, p := range a.Places {
			if _, ok := Placements[p]; !ok {
				return m, fmt.Errorf("manifest: action %q: unknown place %q (have global_nav, task_sidebar, board_card)", a.ID, p)
			}
		}
		scripts := a.Run
		if a.Service != nil {
			if a.Output || strings.TrimSpace(a.Service.Status) == "" || strings.TrimSpace(a.Service.Stop) == "" {
				return m, fmt.Errorf("manifest: action %q: service needs status and stop, and cannot be output", a.ID)
			}
			if len(a.Places) != 1 || a.Places[0] != "task_sidebar" {
				return m, fmt.Errorf("manifest: action %q: service actions only go in task_sidebar", a.ID)
			}
			scripts += "\n" + a.Service.Status + "\n" + a.Service.Stop
		}
		for _, v := range varRe.FindAllStringSubmatch(scripts, -1) {
			if !contains(ManifestVars, v[1]) {
				return m, fmt.Errorf("manifest: action %q: unknown variable %q (have %s)", a.ID, v[1], strings.Join(ManifestVars, ", "))
			}
		}
	}
	for _, a := range m.Automations {
		if a.Name == "" || a.ActionKind == "" {
			return m, fmt.Errorf("manifest: automations need name and action_kind")
		}
	}
	return m, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// RenderManifest fills ${{ var }} placeholders in a run script with shell-quoted values.
func RenderManifest(run string, vars map[string]string) string {
	return varRe.ReplaceAllStringFunc(run, func(m string) string {
		return shq(vars[varRe.FindStringSubmatch(m)[1]])
	})
}

// InstallManifest validates raw, stores the app and creates its agents (user scope) and
// automations (disabled until the user enables them). A failure rolls the app back.
func (s *Service) InstallManifest(raw string) (store.App, error) {
	m, err := ParseManifest(raw)
	if err != nil {
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

// UninstallApp deletes the app; a manifest app's automations and agents go with it.
func (s *Service) UninstallApp(name string) error {
	a, err := store.GetApp(s.DB, name)
	if err != nil {
		return err
	}
	if a.Manifest != "" {
		if m, err := ParseManifest(a.Manifest); err == nil {
			for _, x := range m.Automations {
				_ = store.DeleteAutomation(s.DB, x.Name)
			}
			for _, g := range m.Agents {
				_ = os.Remove(userAgentsDir() + "/" + g.Name + ".md")
			}
		}
	}
	return store.DeleteApp(s.DB, name)
}

// Placed is one manifest action ready to show in a UI slot.
type Placed struct {
	App     string `json:"app"`
	ID      string `json:"id"`
	Label   string `json:"label"`
	Output  bool   `json:"output"`
	Service bool   `json:"service"`
}

// PlacedActions lists the manifest actions that asked for place.
func (s *Service) PlacedActions(place string) ([]Placed, error) {
	apps, err := store.ListApps(s.DB)
	if err != nil {
		return nil, err
	}
	out := []Placed{}
	for _, a := range apps {
		if a.Manifest == "" {
			continue
		}
		m, err := ParseManifest(a.Manifest)
		if err != nil {
			continue
		}
		for _, x := range m.Actions {
			if contains(x.Places, place) {
				out = append(out, Placed{a.Name, x.ID, x.Label, x.Output, x.Service != nil})
			}
		}
	}
	return out, nil
}

// RunAppAction runs one manifest action. key "" is only valid for global_nav actions
// (worktree.path is then the home directory). Returns an output action's output.
func (s *Service) RunAppAction(app, id, key string) (string, error) {
	act, vars, cwd, err := s.resolveAction(app, id, key)
	if err != nil {
		return "", err
	}
	return RunApp(store.App{Name: act.Label, Command: RenderManifest(act.Run, vars), Output: act.Output}, cwd, key)
}

// AppServiceRunning reports whether a service action's status script exits 0 for the task.
func (s *Service) AppServiceRunning(app, id, key string) (bool, error) {
	act, vars, cwd, err := s.resolveAction(app, id, key)
	if err != nil || act.Service == nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", RenderManifest(act.Service.Status, vars))
	c.Dir = cwd
	return c.Run() == nil, nil
}

// StopAppService runs a service action's stop script for the task.
func (s *Service) StopAppService(app, id, key string) error {
	act, vars, _, err := s.resolveAction(app, id, key)
	if err != nil {
		return err
	}
	if act.Service == nil {
		return fmt.Errorf("action %q is not a service", id)
	}
	return runShell("stop "+act.Label, RenderManifest(act.Service.Stop, vars))
}

// resolveAction finds the action and the variables and directory its scripts run with.
func (s *Service) resolveAction(app, id, key string) (*ManifestAction, map[string]string, string, error) {
	a, err := store.GetApp(s.DB, app)
	if err != nil {
		return nil, nil, "", err
	}
	m, err := ParseManifest(a.Manifest)
	if err != nil {
		return nil, nil, "", err
	}
	var act *ManifestAction
	for i := range m.Actions {
		if m.Actions[i].ID == id {
			act = &m.Actions[i]
		}
	}
	if act == nil {
		return nil, nil, "", fmt.Errorf("app %q has no action %q", app, id)
	}
	vars := map[string]string{"app.name": m.Name}
	cwd := ""
	if key == "" {
		if cwd, err = os.UserHomeDir(); err != nil {
			return nil, nil, "", err
		}
	} else {
		t, err := store.GetTaskByKey(s.DB, key)
		if err != nil {
			return nil, nil, "", err
		}
		if cwd, err = s.WorktreePath(key, ""); err != nil {
			return nil, nil, "", err
		}
		vars["task.key"], vars["task.title"], vars["task.status"] = t.Key, t.Title, string(t.Status)
		for _, r := range t.Repos {
			if r.IsPrimary {
				vars["repo.name"] = r.RepoName
			}
		}
	}
	vars["worktree.path"] = cwd
	return act, vars, cwd, nil
}

// spotlightService detects and stops the spotlight instance mirroring ${{ worktree.path }}:
// `spotlight list` prints "ROOT  <-  WORKTREE  (pid N)" and stop takes the root.
func spotlightService() *ManifestService {
	bin := shq(spotlightBin())
	root := bin + ` list | awk -v w=${{ worktree.path }} -F '  <-  ' '{sub(/  \(pid [0-9]+\)$/, "", $2); if ($2 == w) print $1}'`
	return &ManifestService{Status: root + " | grep -q .", Stop: `r=$(` + root + `); [ -n "$r" ] && ` + bin + ` stop "$r"`}
}

// LegacyManifest expresses a plain command app (the old {cwd}/{key} template) as a manifest:
// one action in the nav bar (global) or task sidebar. Unlike the legacy sidebar icons, the
// action runs in the task's primary worktree only.
func LegacyManifest(a store.App) (string, error) {
	place := "task_sidebar"
	if a.Global {
		place = "global_nav"
	}
	run := strings.NewReplacer("{cwd}", "${{ worktree.path }}", "{key}", "${{ task.key }}").Replace(a.Command)
	act := ManifestAction{ID: "open", Label: a.Name, Run: run, Places: []string{place}, Output: a.Output}
	if a.Name == "Spotlight" && !a.Global && !a.Output {
		act.Service = spotlightService()
	}
	b, err := json.MarshalIndent(Manifest{Version: ManifestVersion, Name: a.Name, Actions: []ManifestAction{act}}, "", "  ")
	return string(b), err
}

// ConvertApp turns the plain command app into a manifest app in place.
func (s *Service) ConvertApp(name string) error {
	a, err := store.GetApp(s.DB, name)
	if err != nil {
		return err
	}
	if a.Manifest != "" {
		return fmt.Errorf("app %q is already a manifest app", name)
	}
	raw, err := LegacyManifest(a)
	if err != nil {
		return err
	}
	if _, err := ParseManifest(raw); err != nil {
		return err
	}
	return store.UpdateApp(s.DB, store.App{ID: a.ID, Name: a.Name, Manifest: raw})
}

// InstallLegacy installs a plain command app (e.g. a detected preset) as a manifest app.
func (s *Service) InstallLegacy(a store.App) (store.App, error) {
	raw, err := LegacyManifest(a)
	if err != nil {
		return store.App{}, err
	}
	return s.InstallManifest(raw)
}
