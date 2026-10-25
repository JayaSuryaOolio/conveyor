// Package claude wraps the `claude` CLI (tech-spec §7): command building and exec only, no DB access.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// ErrNotFound means the claude binary is not on PATH.
	ErrNotFound = errors.New("claude CLI not found")
	// ErrUntrusted means Claude Code refused to run in an untrusted workspace.
	ErrUntrusted = errors.New("workspace not trusted by Claude Code")
)

var execCommand = exec.CommandContext // swapped in tests

// LaunchSpec describes a fresh background launch.
type LaunchSpec struct {
	Cwd          string   // primary worktree
	AddDirs      []string // other worktrees
	Prompt       string
	Name         string // task key
	SettingsPath string
	Model, Agent string // optional
	MCPConfig    string // optional; set if <Cwd>/.mcp.json exists (caller decides)
	Bypass       bool   // true for auto runs
}

// Args returns argv after "claude", with the prompt before --add-dir (the flag is variadic).
func (s LaunchSpec) Args() []string {
	a := []string{"--bg", s.Prompt, "--name", s.Name, "--settings", s.SettingsPath}
	if s.Bypass {
		a = append(a, "--permission-mode", "bypassPermissions")
	}
	a = append(a, "--strict-mcp-config")
	if s.MCPConfig != "" {
		a = append(a, "--mcp-config", s.MCPConfig)
	}
	if s.Model != "" {
		a = append(a, "--model", s.Model)
	}
	if s.Agent != "" {
		a = append(a, "--agent", s.Agent)
	}
	if len(s.AddDirs) > 0 {
		a = append(a, "--add-dir")
		a = append(a, s.AddDirs...)
	}
	return a
}

var (
	bgRe   = regexp.MustCompile(`backgrounded\s*·\s*(\S+)`)
	copyRe = regexp.MustCompile(`started a copy as\s+([0-9A-Za-z_-]+)`)
)

// run executes claude with args in dir and returns combined output.
func run(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := execCommand(ctx, "claude", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := buf.String()
	if errors.Is(err, exec.ErrNotFound) {
		return out, ErrNotFound
	}
	if strings.Contains(out, "Workspace not trusted") {
		return out, fmt.Errorf("%w: %s", ErrUntrusted, strings.TrimSpace(out))
	}
	if err != nil {
		return out, fmt.Errorf("claude %s: %w: %s", args[0], err, strings.TrimSpace(out))
	}
	return out, nil
}

// Launch runs `claude --bg ...` and returns the background id.
func Launch(ctx context.Context, s LaunchSpec) (string, error) {
	out, err := run(ctx, s.Cwd, []string{"CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1"}, s.Args()...)
	if err != nil {
		return "", err
	}
	m := bgRe.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("claude launch: no background id in output: %s", strings.TrimSpace(out))
	}
	return m[1], nil
}

// Resume stops bgID (errors ignored) then resumes sessionID with only --bg --resume.
// It returns the printed bg id, or X if the output says "started a copy as X".
func Resume(ctx context.Context, cwd, bgID, sessionID, prompt string) (string, error) {
	_ = Stop(ctx, bgID)
	out, err := run(ctx, cwd, nil, "--bg", "--resume", sessionID, prompt)
	if err != nil {
		return "", err
	}
	if m := copyRe.FindStringSubmatch(out); m != nil {
		return m[1], nil
	}
	if m := bgRe.FindStringSubmatch(out); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("claude resume: no background id in output: %s", strings.TrimSpace(out))
}

// Agent is one entry of `claude agents --json`. Interactive sessions may lack ID.
type Agent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	State     string `json:"state"`
	Status    string `json:"status"`
	SessionID string `json:"sessionId"`
	PID       int    `json:"pid"`
}

// Agents runs `claude agents --json --all`.
func Agents(ctx context.Context) ([]Agent, error) {
	out, err := run(ctx, "", nil, "agents", "--json", "--all")
	if err != nil {
		return nil, err
	}
	var as []Agent
	if err := json.Unmarshal([]byte(out), &as); err != nil {
		return nil, fmt.Errorf("parse claude agents output: %w", err)
	}
	return as, nil
}

// Stop runs `claude stop <bgID>`.
func Stop(ctx context.Context, bgID string) error {
	_, err := run(ctx, "", nil, "stop", bgID)
	return err
}

// Remove runs `claude rm <bgID>`.
func Remove(ctx context.Context, bgID string) error {
	_, err := run(ctx, "", nil, "rm", bgID)
	return err
}

// q single-quotes s for a POSIX shell.
func q(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// AttachCommand is the terminal command to attach to a background session.
func AttachCommand(bgID string) string { return "claude attach " + q(bgID) }

// ResumeCommand resumes a session interactively (no bypass, no --bg).
func ResumeCommand(sessionID, settingsPath string) string {
	return "claude --resume " + q(sessionID) + " --settings " + q(settingsPath)
}

// FreshCommand starts an interactive session primed with the brief.
func FreshCommand(settingsPath, briefPath string, addDirs []string) string {
	// The prompt goes before --add-dir, which is variadic.
	c := "claude --settings " + q(settingsPath) + " " + q("Read "+briefPath+" first, then wait for my instructions.")
	for _, d := range addDirs {
		c += " --add-dir " + q(d)
	}
	return c
}

// hookCommand is the command Claude Code runs for ev. The user-level install
// (userhooks.go) writes the same string, so Claude Code runs it once per event.
func hookCommand(conveyorBin, home, ev string) string {
	return q(conveyorBin) + " --home " + q(home) + " hook " + ev
}

func hookEntry(conveyorBin, home, ev string) map[string]any {
	return map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand(conveyorBin, home, ev)}}}
}

// WriteSettings writes <home>/claude-settings.json atomically and returns its path.
func WriteSettings(home, conveyorBin string) (string, error) {
	hook := func(ev string) []any { return []any{hookEntry(conveyorBin, home, ev)} }
	pre := hook("PreToolUse")
	pre[0].(map[string]any)["matcher"] = "Bash"
	b, err := json.MarshalIndent(map[string]any{
		"worktree": map[string]any{"bgIsolation": "none"},
		"hooks": map[string]any{
			"PreToolUse":       pre,
			"UserPromptSubmit": hook("UserPromptSubmit"),
			"Stop":             hook("Stop"),
			"Notification":     hook("Notification"),
		},
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal settings: %w", err)
	}
	path := filepath.Join(home, "claude-settings.json")
	if err := writeAtomic(path, append(b, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write settings: %w", err)
	}
	return path, nil
}

// writeAtomic replaces path via a uniquely named temp file in the same directory,
// so concurrent writers (the sidebar loads actions and sessions at once) never
// rename each other's temp file away.
func writeAtomic(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), mode)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
