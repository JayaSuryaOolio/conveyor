package core

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Manifest declares an app as a plugin: actions placed in UI slots, plus agents and
// automations installed with it. Action `run` scripts may use ${{ var }} placeholders
// (like GitHub Actions), filled with shell-quoted values from the Vars table.
type Manifest struct {
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
		for _, v := range varRe.FindAllStringSubmatch(a.Run, -1) {
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
	App    string `json:"app"`
	ID     string `json:"id"`
	Label  string `json:"label"`
	Output bool   `json:"output"`
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
				out = append(out, Placed{a.Name, x.ID, x.Label, x.Output})
			}
		}
	}
	return out, nil
}

// RunAppAction runs one manifest action. key "" is only valid for global_nav actions
// (worktree.path is then the home directory). Returns an output action's output.
func (s *Service) RunAppAction(app, id, key string) (string, error) {
	a, err := store.GetApp(s.DB, app)
	if err != nil {
		return "", err
	}
	m, err := ParseManifest(a.Manifest)
	if err != nil {
		return "", err
	}
	var act *ManifestAction
	for i := range m.Actions {
		if m.Actions[i].ID == id {
			act = &m.Actions[i]
		}
	}
	if act == nil {
		return "", fmt.Errorf("app %q has no action %q", app, id)
	}
	vars := map[string]string{"app.name": m.Name}
	cwd := ""
	if key == "" {
		if cwd, err = os.UserHomeDir(); err != nil {
			return "", err
		}
	} else {
		t, err := store.GetTaskByKey(s.DB, key)
		if err != nil {
			return "", err
		}
		if cwd, err = s.WorktreePath(key, ""); err != nil {
			return "", err
		}
		vars["task.key"], vars["task.title"], vars["task.status"] = t.Key, t.Title, string(t.Status)
		for _, r := range t.Repos {
			if r.IsPrimary {
				vars["repo.name"] = r.RepoName
			}
		}
	}
	vars["worktree.path"] = cwd
	return RunApp(store.App{Name: act.Label, Command: RenderManifest(act.Run, vars), Output: act.Output}, cwd, key)
}
