package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UserHookEvents are the events installed in the user's ~/.claude/settings.json so
// sessions Conveyor didn't launch (e.g. `claude` typed in a worktree opened in an
// app) still report to it. PreToolUse is left out: it doesn't filter by cwd and
// would apply the PR comment prefix to every Claude session on the machine.
var UserHookEvents = []string{"UserPromptSubmit", "Stop", "Notification"}

// UserSettingsPath is ~/.claude/settings.json.
func UserSettingsPath() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".claude", "settings.json"), nil
}

// ours reports whether a hook command is Conveyor's for home, whatever binary path it calls.
func ours(cmd, home, ev string) bool {
	return strings.Contains(cmd, " --home "+q(home)+" hook ") && strings.HasSuffix(cmd, " hook "+ev)
}

func readUserSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if len(strings.TrimSpace(string(b))) > 0 {
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON, not touching it: %w", path, err)
		}
	}
	return m, nil
}

// commands lists the hook commands under settings.hooks[ev].
func commands(m map[string]any, ev string) []string {
	var out []string
	hooks, _ := m["hooks"].(map[string]any)
	groups, _ := hooks[ev].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if c, ok := hm["command"].(string); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// UserHooksStatus is "installed", "outdated" (ours, but calling another binary or
// missing an event) or "not installed".
func UserHooksStatus(path, conveyorBin, home string) (string, error) {
	m, err := readUserSettings(path)
	if err != nil {
		return "", err
	}
	exact, found := 0, false
	for _, ev := range UserHookEvents {
		for _, c := range commands(m, ev) {
			if ours(c, home, ev) {
				found = true
				if c == hookCommand(conveyorBin, home, ev) {
					exact++
					break
				}
			}
		}
	}
	switch {
	case exact == len(UserHookEvents):
		return "installed", nil
	case found:
		return "outdated", nil
	}
	return "not installed", nil
}

// SetUserHooks removes Conveyor's hooks for home from the user settings file and,
// if install, adds the current ones. Other keys and hooks are kept, and the old
// file is copied to <path>.conveyor.bak first.
func SetUserHooks(path, conveyorBin, home string, install bool) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // keep a dotfiles symlink intact
	}
	m, err := readUserSettings(path)
	if err != nil {
		return err
	}
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range UserHookEvents {
		groups, _ := hooks[ev].([]any)
		var kept []any
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			hs, _ := gm["hooks"].([]any)
			var keep []any
			for _, h := range hs {
				hm, _ := h.(map[string]any)
				if c, _ := hm["command"].(string); !ours(c, home, ev) {
					keep = append(keep, h)
				}
			}
			if len(keep) > 0 {
				gm["hooks"] = keep
				kept = append(kept, gm)
			}
		}
		if install {
			kept = append(kept, hookEntry(conveyorBin, home, ev))
		}
		if len(kept) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = kept
		}
	}
	if len(hooks) == 0 {
		delete(m, "hooks")
	} else {
		m["hooks"] = hooks
	}

	mode := os.FileMode(0o644)
	if old, err := os.ReadFile(path); err == nil {
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(path+".conveyor.bak", old, mode); err != nil {
			return fmt.Errorf("back up %s: %w", path, err)
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'), mode)
}
