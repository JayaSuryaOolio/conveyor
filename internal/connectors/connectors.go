// Package apps holds app manifests, presets and macOS icon extraction: the external tools a
// worktree can be opened in or a task can run actions with.
package connectors

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

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

// ConnectorPresets returns Finder plus every preset app installed on this Mac, as manifest apps.
func ConnectorPresets() []store.Connector {
	preset := func(name, command string) store.Connector {
		return store.Connector{Name: name, Manifest: CommandManifest(name, command, false)}
	}
	out := []store.Connector{preset("Finder", "open {cwd}")}
	for _, name := range presetApps {
		for _, d := range appDirs() {
			if _, err := os.Stat(filepath.Join(d, name+".app")); err == nil {
				out = append(out, preset(name, "open -a "+shq(name)+" {cwd}"))
				break
			}
		}
	}
	// graft (github.com/JayaSuryaOolio/graft) mirrors a worktree into its repo's main checkout.
	h, _ := os.UserHomeDir()
	if _, err := exec.LookPath("graft"); err == nil {
		out = append(out, preset("Graft", "cd {cwd} && graft start"))
	} else if _, err := os.Stat(filepath.Join(h, ".local", "bin", "graft")); err == nil {
		out = append(out, preset("Graft", "cd {cwd} && ~/.local/bin/graft start"))
	}
	return out
}

// runShell runs cmd via `sh -c`, folding its output into the error.
func runShell(what, cmd string) error {
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", what, err, strings.TrimSpace(string(out)))
	}
	return nil
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

// OpenConnector runs the app's first action in the task repo's worktree ("" repo = primary)
// and returns an output action's output.
func OpenConnector(db *sql.DB, name, key, repo string) (string, error) {
	a, err := store.GetConnector(db, name)
	if err != nil {
		return "", err
	}
	m, err := ParseManifest(a.Manifest)
	if err != nil {
		return "", err
	}
	if len(m.Actions) == 0 {
		return "", fmt.Errorf("app %q has no actions", name)
	}
	return runConnectorAction(db, name, m.Actions[0].ID, "", key, repo)
}
