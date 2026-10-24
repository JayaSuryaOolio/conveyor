package core

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JayaSuryaOolio/conveyor/internal/brief"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/terminal"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

// Action is one thing the operator can open from the Workspace panel.
// Kind is attach, resume, fresh, shell or editor.
type Action struct {
	Label string `json:"label"`
	Repo  string `json:"repo"`
	Cwd   string `json:"cwd"`
	Cmd   string `json:"cmd"`
	Kind  string `json:"kind"`
}

// Session is a manual Claude session found under ~/.claude/projects.
type Session struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	MTime  string `json:"mtime"`
	Prompt string `json:"prompt"`
	Action Action `json:"action"`
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func isActive(r store.Run) bool { return r.Status == "launching" || r.Status == "running" }

// settingsPath makes sure claude-settings.json exists and returns its path.
func (s *Service) settingsPath() (string, error) {
	if s.Bin != "" {
		return claude.WriteSettings(s.Home, s.Bin)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find conveyor binary: %w", err)
	}
	return claude.WriteSettings(s.Home, exe)
}

// Actions lists what can be opened for a task. Without a primary worktree it returns none.
func (s *Service) Actions(key string) ([]Action, error) {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return nil, err
	}
	cwd := workspace.PrimaryPath(t)
	if cwd == "" {
		return []Action{}, nil
	}
	settings, err := s.settingsPath()
	if err != nil {
		return nil, err
	}
	var primary string
	for _, r := range t.Repos {
		if r.IsPrimary {
			primary = r.RepoName
		}
	}
	var out []Action
	run, err := store.LatestRun(s.DB, t.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	switch {
	case err != nil:
	case isActive(run) && run.BgID != nil:
		out = append(out, Action{"Attach to session", primary, cwd, claude.AttachCommand(*run.BgID), "attach"})
	case !isActive(run) && run.SessionID != nil:
		out = append(out, Action{"Resume in terminal", primary, cwd, claude.ResumeCommand(*run.SessionID, settings), "resume"})
	}
	briefPath, err := brief.Render(s.DB, s.Home, t)
	if err != nil {
		return nil, err
	}
	out = append(out, Action{"New session from brief", primary, cwd, claude.FreshCommand(settings, briefPath, workspace.OtherPaths(t)), "fresh"})
	editor := store.GetSetting(s.DB, "editor_command")
	for _, r := range t.Repos {
		if r.WorktreePath == nil {
			continue
		}
		out = append(out,
			Action{"Shell in " + r.RepoName, r.RepoName, *r.WorktreePath, "", "shell"},
			Action{"Editor: " + r.RepoName, r.RepoName, *r.WorktreePath, strings.ReplaceAll(editor, "{cwd}", shq(*r.WorktreePath)), "editor"})
	}
	return out, nil
}

var nonAlnumPath = regexp.MustCompile(`[^a-zA-Z0-9]`)

func (s *Service) projectsDir() string {
	if s.ProjectsDir != "" {
		return s.ProjectsDir
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude", "projects")
}

// firstPrompt returns the first real user prompt of a session transcript, truncated to 80 chars.
func firstPrompt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		var m struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &m) == nil && m.Type == "user" {
			var text string
			if json.Unmarshal(m.Message.Content, &text) != nil {
				var parts []struct{ Text string }
				json.Unmarshal(m.Message.Content, &parts)
				for _, p := range parts {
					text += p.Text
				}
			}
			if text = strings.TrimSpace(text); text != "" && !strings.HasPrefix(text, "<") {
				if rs := []rune(text); len(rs) > 80 {
					text = string(rs[:80])
				}
				return text
			}
		}
		if err != nil {
			return ""
		}
	}
}

// Sessions returns manual Claude sessions in the task's worktrees (newest first),
// leaving out the sessions Conveyor's own runs already show.
func (s *Service) Sessions(key string) ([]Session, error) {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return nil, err
	}
	runs, err := store.ListRuns(s.DB, t.ID)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, r := range runs {
		if r.SessionID != nil {
			known[*r.SessionID] = true
		}
	}
	settings, err := s.settingsPath()
	if err != nil {
		return nil, err
	}
	out := []Session{}
	for _, r := range t.Repos {
		if r.WorktreePath == nil {
			continue
		}
		dir := filepath.Join(s.projectsDir(), nonAlnumPath.ReplaceAllString(*r.WorktreePath, "-"))
		files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		for _, f := range files {
			id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
			fi, err := os.Stat(f)
			if known[id] || err != nil {
				continue
			}
			out = append(out, Session{
				ID: id, Repo: r.RepoName, MTime: fi.ModTime().UTC().Format(time.RFC3339), Prompt: firstPrompt(f),
				Action: Action{"Resume " + id[:min(8, len(id))], r.RepoName, *r.WorktreePath, claude.ResumeCommand(id, settings), "resume"},
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MTime > out[j].MTime })
	return out, nil
}

// FindAction picks an action by kind and repo ("" means the primary repo for shell and editor).
// Kind "resume:<id>" selects a manual session's resume action.
func (s *Service) FindAction(key, kind, repo string) (Action, error) {
	if id, ok := strings.CutPrefix(kind, "resume:"); ok {
		ss, err := s.Sessions(key)
		if err != nil {
			return Action{}, err
		}
		for _, x := range ss {
			if x.ID == id {
				return x.Action, nil
			}
		}
		return Action{}, fmt.Errorf("no session %q for %s", id, key)
	}
	as, err := s.Actions(key)
	if err != nil {
		return Action{}, err
	}
	if len(as) == 0 {
		return Action{}, fmt.Errorf("%s has no workspace yet", key)
	}
	for _, a := range as {
		if a.Kind != kind {
			continue
		}
		if repo == "" && (kind == "shell" || kind == "editor") {
			repo = as[0].Repo // the primary
		}
		if repo == "" || a.Repo == repo {
			return a, nil
		}
	}
	return Action{}, fmt.Errorf("%s: no %q action available", key, kind)
}

// Render is the command line that Launch would run, for --print.
func (s *Service) Render(a Action) string {
	if a.Kind == "editor" {
		return a.Cmd
	}
	return terminal.Render(store.GetSetting(s.DB, "terminal_template"), a.Cwd, a.Cmd)
}

// Launch runs the action: the editor directly, everything else in a terminal.
func (s *Service) Launch(a Action) error {
	if a.Kind == "editor" {
		if out, err := exec.Command("sh", "-c", a.Cmd).CombinedOutput(); err != nil {
			return fmt.Errorf("editor: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return terminal.Open(store.GetSetting(s.DB, "terminal_template"), a.Cwd, a.Cmd)
}

// RunAction finds and launches an action.
func (s *Service) RunAction(key, kind, repo string) error {
	a, err := s.FindAction(key, kind, repo)
	if err != nil {
		return err
	}
	return s.Launch(a)
}
