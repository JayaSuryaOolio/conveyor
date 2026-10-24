package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// State is the portable, repo-free configuration: settings, apps, automations and
// user-level agents. Repos are not exported; the names automations use become
// placeholders A, B, C... that import maps back to registered repos.
type State struct {
	Version     int               `json:"version"`
	Repos       []string          `json:"repos"`
	Settings    map[string]string `json:"settings"`
	Apps        []StateApp        `json:"apps"`
	Automations []StateAutomation `json:"automations"`
	Agents      []StateAgent      `json:"agents"`
}

type StateApp struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Global  bool   `json:"global"`
	Output  bool   `json:"output"`
}

type StateAutomation struct {
	Name       string `json:"name"`
	Schedule   string `json:"schedule"`
	Precheck   string `json:"precheck"`
	ActionKind string `json:"action_kind"`
	ActionArg  string `json:"action_arg"`
	Enabled    bool   `json:"enabled"`
}

// StateAgent is a ~/.claude/agents file, kept whole.
type StateAgent struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// StateFile is where uninstall leaves the export and install looks for it.
func StateFile(home string) string { return filepath.Join(home, "state.json") }

// placeholder is the n-th spreadsheet-style name: A..Z, AA, AB...
func placeholder(n int) string {
	s := ""
	for n++; n > 0; n = (n - 1) / 26 {
		s = string(rune('A'+(n-1)%26)) + s
	}
	return s
}

// mapRepos rewrites the "repos" list of an automation's action JSON with f; other fields are kept as is.
func mapRepos(arg string, f func(string) string) (string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arg), &m); err != nil {
		return "", err
	}
	var repos []string
	if raw, ok := m["repos"]; !ok || json.Unmarshal(raw, &repos) != nil || len(repos) == 0 {
		return arg, nil
	}
	for i, r := range repos {
		repos[i] = f(r)
	}
	m["repos"], _ = json.Marshal(repos)
	b, err := json.Marshal(m)
	return string(b), err
}

// ExportState collects the stored settings, apps, automations (repos replaced by placeholders) and user agents.
func (s *Service) ExportState() (State, error) {
	st := State{Version: 1, Repos: []string{}, Settings: map[string]string{}, Apps: []StateApp{}, Automations: []StateAutomation{}, Agents: []StateAgent{}}
	rows, err := s.DB.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return st, err
		}
		st.Settings[k] = v
	}
	rows.Close()
	apps, err := store.ListApps(s.DB)
	if err != nil {
		return st, err
	}
	for _, a := range apps {
		st.Apps = append(st.Apps, StateApp{a.Name, a.Command, a.Global, a.Output})
	}
	autos, err := store.ListAutomations(s.DB)
	if err != nil {
		return st, err
	}
	ph := map[string]string{}
	for _, a := range autos {
		arg, err := mapRepos(a.ActionArg, func(r string) string {
			if ph[r] == "" {
				ph[r] = placeholder(len(ph))
				st.Repos = append(st.Repos, ph[r])
			}
			return ph[r]
		})
		if err != nil {
			return st, fmt.Errorf("automation %q: %w", a.Name, err)
		}
		st.Automations = append(st.Automations, StateAutomation{a.Name, a.Schedule, a.Precheck, a.ActionKind, arg, a.Enabled})
	}
	files, _ := filepath.Glob(filepath.Join(userAgentsDir(), "*.md"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if fm, _ := claude.Frontmatter(b); fm["name"] != "" {
			st.Agents = append(st.Agents, StateAgent{strings.TrimSuffix(filepath.Base(f), ".md"), string(b)})
		}
	}
	return st, nil
}

// ImportState adds what the state has and this install lacks; it never overwrites.
// repoMap maps placeholders to registered repo names; an automation with an
// unmapped placeholder is added disabled. It returns one line per change or skip.
func (s *Service) ImportState(st State, repoMap map[string]string) ([]string, error) {
	var out []string
	for p, r := range repoMap {
		if _, err := store.GetRepoByName(s.DB, r); err != nil {
			return nil, fmt.Errorf("%s=%s: repo %q is not registered", p, r, r)
		}
	}
	keys := make([]string, 0, len(st.Settings))
	for k := range st.Settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var n int
		if err := s.DB.QueryRow(`SELECT count(*) FROM settings WHERE key=?`, k).Scan(&n); err != nil {
			return out, err
		}
		if n > 0 {
			out = append(out, "skipped setting "+k+": already set")
			continue
		}
		if err := store.SetSetting(s.DB, k, st.Settings[k]); err != nil {
			return out, err
		}
		out = append(out, "added setting "+k)
	}
	for _, a := range st.Apps {
		if _, err := store.GetApp(s.DB, a.Name); err == nil {
			out = append(out, "skipped app "+a.Name+": exists")
			continue
		}
		if _, err := store.CreateApp(s.DB, store.App{Name: a.Name, Command: a.Command, Global: a.Global, Output: a.Output}); err != nil {
			return out, err
		}
		out = append(out, "added app "+a.Name)
	}
	for _, a := range st.Automations {
		if _, err := store.GetAutomation(s.DB, a.Name); err == nil {
			out = append(out, "skipped automation "+a.Name+": exists")
			continue
		}
		var missing []string
		arg, err := mapRepos(a.ActionArg, func(p string) string {
			if r := repoMap[p]; r != "" {
				return r
			}
			missing = append(missing, p)
			return p
		})
		if err != nil {
			return out, fmt.Errorf("automation %q: %w", a.Name, err)
		}
		line := "added automation " + a.Name
		if len(missing) > 0 {
			a.Enabled = false
			line += fmt.Sprintf(" (disabled: map repo %s with --repo %s=<name>, then re-import after rm)", strings.Join(missing, ","), missing[0])
		}
		if _, err := store.CreateAutomation(s.DB, store.Automation{Name: a.Name, Schedule: a.Schedule, Precheck: a.Precheck, ActionKind: a.ActionKind, ActionArg: arg, Enabled: a.Enabled}); err != nil {
			return out, err
		}
		out = append(out, line)
	}
	for _, a := range st.Agents {
		if !agentName.MatchString(a.Name) {
			out = append(out, "skipped agent "+a.Name+": name must match [a-z0-9-]+")
			continue
		}
		if err := os.MkdirAll(userAgentsDir(), 0o755); err != nil {
			return out, err
		}
		f, err := os.OpenFile(filepath.Join(userAgentsDir(), a.Name+".md"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			out = append(out, "skipped agent "+a.Name+": exists")
			continue
		}
		if err != nil {
			return out, err
		}
		_, err = f.WriteString(a.Content)
		f.Close()
		if err != nil {
			return out, err
		}
		out = append(out, "added agent "+a.Name)
	}
	return out, nil
}
