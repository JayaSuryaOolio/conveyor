package agy

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
)

// PluginDir is ~/.gemini/config/plugins/conveyor: a directory there is discovered by agy
// as an installed and enabled plugin.
func PluginDir() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".gemini", "config", "plugins", "conveyor"), nil
}

// pluginFiles builds the files for the agy conveyor plugin: plugin.json, hooks.json, skills, and agents.
func pluginFiles(skills, agents fs.FS, conveyorBin, home string) (map[string][]byte, error) {
	hooks := map[string]any{
		"conveyor": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "'" + conveyorBin + "' --home '" + home + "' hook PreToolUse",
						},
					},
				},
			},
			"Stop": []any{
				map[string]any{
					"type":    "command",
					"command": "'" + conveyorBin + "' --home '" + home + "' hook Stop",
				},
			},
		},
	}
	files := map[string][]byte{}
	manifest := map[string]any{
		"name":        "conveyor",
		"version":     "1.0.0",
		"description": "Reports agy sessions in Conveyor task worktrees to Conveyor, plus the skills and agents its tasks use.",
	}
	for p, v := range map[string]any{
		"plugin.json": manifest,
		"hooks.json":  hooks,
	} {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		files[p] = append(b, '\n')
	}

	if skills != nil {
		err := fs.WalkDir(skills, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(skills, p)
			if err != nil {
				return err
			}
			files["skills/"+p] = b
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	if agents != nil {
		err := fs.WalkDir(agents, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(agents, p)
			if err != nil {
				return err
			}
			files["agents/"+p] = b
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	// Also ensure epic-builder is in agents/
	if files["agents/epic-builder.md"] == nil {
		files["agents/epic-builder.md"] = []byte("---\nname: epic-builder\ndescription: \"Runs a Conveyor epic: plans it, waits at the gate, drives its sub-tasks to PRs.\"\n---\n\nYou run an epic. Load the `epic` skill and follow it.\n")
	}

	return files, nil
}

// SetPlugin installs or uninstalls the agy Conveyor plugin.
func SetPlugin(dir string, skills, agents fs.FS, conveyorBin, home string, install bool) error {
	if err := os.RemoveAll(dir); err != nil || !install {
		return err
	}
	files, err := pluginFiles(skills, agents, conveyorBin, home)
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

// PluginStatus returns "installed", "outdated", or "not installed".
func PluginStatus(dir string, skills, agents fs.FS, conveyorBin, home string) (string, error) {
	if _, err := os.Stat(dir); err != nil {
		return "not installed", nil
	}
	files, err := pluginFiles(skills, agents, conveyorBin, home)
	if err != nil {
		return "", err
	}
	for p, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil || !bytes.Equal(got, want) {
			return "outdated", nil
		}
	}
	return "installed", nil
}
