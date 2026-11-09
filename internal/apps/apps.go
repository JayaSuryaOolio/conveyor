// Package apps holds app manifests, presets and macOS icon extraction: the external tools a
// worktree can be opened in or a task can run actions with.
package apps

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// presetApps are macOS apps offered as one-click presets when installed. Each opens
// with `open -a NAME {cwd}`, so supporting another app is one more name here.
var presetApps = append(EditorApps,
	"Terminal", "iTerm", "Ghostty", "Warp", "WezTerm", "kitty",
	"GitHub Desktop", "Fork", "Tower", "Sourcetree",
)

// EditorApps are the presets that can also open one file (`open -a NAME file`); used by @file links.
var EditorApps = []string{
	"Visual Studio Code", "Cursor", "Zed", "Windsurf", "Antigravity", "Sublime Text", "Xcode",
	"GoLand", "IntelliJ IDEA", "WebStorm", "PyCharm",
}

// appDirs is where presets are looked for (swapped in tests).
var appDirs = func() []string {
	h, _ := os.UserHomeDir()
	return []string{"/Applications", "/System/Applications", "/System/Applications/Utilities", filepath.Join(h, "Applications")}
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

var nonAlnumPath = regexp.MustCompile(`[^a-zA-Z0-9]`)

// GraftBin finds the graft CLI (the GUI's PATH often lacks ~/.local/bin).
func GraftBin() string {
	if p, err := exec.LookPath("graft"); err == nil {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "bin", "graft")
}

// AppPresets returns Finder plus every preset app installed on this Mac.
func AppPresets() []store.App {
	out := []store.App{{Name: "Finder", Command: "open {cwd}"}}
	for _, name := range presetApps {
		for _, d := range appDirs() {
			if _, err := os.Stat(filepath.Join(d, name+".app")); err == nil {
				out = append(out, store.App{Name: name, Command: "open -a " + shq(name) + " {cwd}"})
				break
			}
		}
	}
	// graft (github.com/JayaSuryaOolio/graft) mirrors a worktree into its repo's main checkout.
	h, _ := os.UserHomeDir()
	if _, err := exec.LookPath("graft"); err == nil {
		out = append(out, store.App{Name: "Graft", Command: "cd {cwd} && graft start"})
	} else if _, err := os.Stat(filepath.Join(h, ".local", "bin", "graft")); err == nil {
		out = append(out, store.App{Name: "Graft", Command: "cd {cwd} && ~/.local/bin/graft start"})
	}
	return out
}

// RenderApp fills {cwd} and {key} (shell-quoted; key is "" without a task) in an app command.
func RenderApp(command, cwd, key string) string {
	return strings.NewReplacer("{cwd}", shq(cwd), "{key}", shq(key)).Replace(command)
}

// runShell runs cmd via `sh -c`, folding its output into the error.
func runShell(what, cmd string) error {
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", what, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// appTimeout bounds an output app (a quick action); launching apps return at once.
var appTimeout = 2 * time.Minute

// RunApp runs a rendered app. An output app's stdout+stderr is returned (≤ 4 KB);
// other apps return "".
func RunApp(a store.App, cwd, key string) (string, error) {
	cmd := RenderApp(a.Command, cwd, key)
	if !a.Output {
		return "", runShell("open app", cmd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = cwd
	b, err := c.CombinedOutput()
	out := strings.TrimSpace(string(b))
	if len(out) > 4096 {
		out = out[:4096] + "…"
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", a.Name, err, out)
	}
	return out, nil
}

// WorktreePath returns the task repo's worktree ("" repo means the primary).
func WorktreePath(db *sql.DB, key, repo string) (string, error) {
	t, err := store.GetTaskByKey(db, key)
	if err != nil {
		return "", err
	}
	for _, r := range t.Repos {
		if (r.RepoName == repo || repo == "" && r.IsPrimary) && r.WorktreePath != nil {
			return *r.WorktreePath, nil
		}
	}
	if repo == "" {
		return "", fmt.Errorf("%s has no workspace yet", key)
	}
	return "", fmt.Errorf("%s: no worktree for repo %q", key, repo)
}

// OpenApp runs the named app in the task repo's worktree and returns an output app's output.
func OpenApp(db *sql.DB, name, key, repo string) (string, error) {
	a, err := store.GetApp(db, name)
	if err != nil {
		return "", err
	}
	if a.Disabled {
		return "", fmt.Errorf("app %q is disabled", name)
	}
	cwd, err := WorktreePath(db, key, repo)
	if err != nil {
		return "", err
	}
	return RunApp(a, cwd, key)
}
