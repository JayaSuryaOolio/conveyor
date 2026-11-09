package claude

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
)

// PluginDir is ~/.claude/skills/conveyor: a directory there holding .claude-plugin/plugin.json is
// a "skills-dir" plugin, which Claude Code loads as conveyor@skills-dir with no marketplace or CLI step.
func PluginDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".claude", "skills", "conveyor"), nil
}

// pluginFiles is the plugin's content by slash path: the manifest, the user hooks and skills under skills/.
// The hook commands equal the --settings ones; `conveyor hook` skips the plugin's copy in sessions whose
// --settings already run them (Claude Code runs both, it only dedupes across settings files).
func pluginFiles(skills fs.FS, conveyorBin, home string) (map[string][]byte, error) {
	hooks := map[string]any{}
	for _, ev := range UserHookEvents {
		hooks[ev] = []any{hookEntry(conveyorBin, home, ev)}
	}
	files := map[string][]byte{}
	for p, v := range map[string]any{
		".claude-plugin/plugin.json": map[string]any{"name": "conveyor", "description": "Reports Claude sessions in Conveyor task worktrees to Conveyor, plus the skills its agents use."},
		"hooks/hooks.json":           map[string]any{"hooks": hooks},
	} {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		files[p] = append(b, '\n')
	}
	err := fs.WalkDir(skills, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(skills, p)
		files["skills/"+p] = b
		return err
	})
	return files, err
}

// legacy reports what the hooks-only install left: hooks in settings.json or ~/.claude/skills/epic.
func legacy(settings, conveyorBin, home string) (bool, error) {
	st, err := UserHooksStatus(settings, conveyorBin, home)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(settings), "skills", "epic"))
	return st != "not installed" || err == nil, nil
}

// SetPlugin replaces dir with the plugin or, if !install, removes it. Either way it removes the
// hooks-only install (settings.json hooks, ~/.claude/skills/epic) so sessions don't report twice.
func SetPlugin(dir, settings string, skills fs.FS, conveyorBin, home string, install bool) error {
	if old, err := legacy(settings, conveyorBin, home); err != nil {
		return err
	} else if old {
		if err := SetUserHooks(settings, conveyorBin, home, false); err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(filepath.Dir(settings), "skills", "epic")); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(dir); err != nil || !install {
		return err
	}
	files, err := pluginFiles(skills, conveyorBin, home)
	if err != nil {
		return err
	}
	for p, b := range files {
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// PluginStatus is "installed", "outdated" (a stale or partial plugin, or the hooks-only install still
// present) or "not installed".
func PluginStatus(dir, settings string, skills fs.FS, conveyorBin, home string) (string, error) {
	old, err := legacy(settings, conveyorBin, home)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); err != nil {
		if old {
			return "outdated", nil
		}
		return "not installed", nil
	}
	files, err := pluginFiles(skills, conveyorBin, home)
	if err != nil {
		return "", err
	}
	for p, want := range files {
		if got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p))); err != nil || !bytes.Equal(got, want) {
			return "outdated", nil
		}
	}
	if old {
		return "outdated", nil
	}
	return "installed", nil
}
