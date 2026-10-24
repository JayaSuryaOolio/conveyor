package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// presetApps are macOS apps offered as one-click presets when installed. Each opens
// with `open -a NAME {cwd}`, so supporting another app is one more name here.
var presetApps = []string{
	"Visual Studio Code", "Cursor", "Zed", "Windsurf", "Antigravity", "Sublime Text", "Xcode",
	"GoLand", "IntelliJ IDEA", "WebStorm", "PyCharm",
	"Terminal", "iTerm", "Ghostty", "Warp", "WezTerm", "kitty",
	"GitHub Desktop", "Fork", "Tower", "Sourcetree",
}

// appDirs is where presets are looked for (swapped in tests).
var appDirs = func() []string {
	h, _ := os.UserHomeDir()
	return []string{"/Applications", "/System/Applications", "/System/Applications/Utilities", filepath.Join(h, "Applications")}
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
	// spotlight (github.com/JayaSuryaOolio/spotlight) mirrors a worktree into its repo's main checkout.
	h, _ := os.UserHomeDir()
	if _, err := exec.LookPath("spotlight"); err == nil {
		out = append(out, store.App{Name: "Spotlight", Command: "cd {cwd} && spotlight start"})
	} else if _, err := os.Stat(filepath.Join(h, ".local", "bin", "spotlight")); err == nil {
		out = append(out, store.App{Name: "Spotlight", Command: "cd {cwd} && ~/.local/bin/spotlight start"})
	}
	return out
}

// RenderApp fills {cwd} (shell-quoted) in an app command.
func RenderApp(command, cwd string) string { return strings.ReplaceAll(command, "{cwd}", shq(cwd)) }

// runShell runs cmd via `sh -c`, folding its output into the error.
func runShell(what, cmd string) error {
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", what, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// LaunchApp runs an app command for cwd.
func LaunchApp(command, cwd string) error { return runShell("open app", RenderApp(command, cwd)) }

// WorktreePath returns the task repo's worktree ("" repo means the primary).
func (s *Service) WorktreePath(key, repo string) (string, error) {
	t, err := store.GetTaskByKey(s.DB, key)
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

// OpenApp opens the named app in the task repo's worktree.
func (s *Service) OpenApp(name, key, repo string) error {
	a, err := store.GetApp(s.DB, name)
	if err != nil {
		return err
	}
	cwd, err := s.WorktreePath(key, repo)
	if err != nil {
		return err
	}
	return LaunchApp(a.Command, cwd)
}
