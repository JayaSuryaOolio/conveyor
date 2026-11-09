// Package agy wraps the `agy` CLI (Antigravity) as an agent runner backend.
// Because agy does not have a built-in background-session mode analogous to
// `claude --bg`, Conveyor runs `agy --print --dangerously-skip-permissions`
// as a regular OS process detached from the dispatcher's lifetime (own
// session via SysProcAttr Setsid, no context). The OS PID (as a
// decimal string) serves as the bg-id. Reaping is done by checking whether
// that PID is still running.
//
// For session resume, agy uses `--conversation <id>` where the id is the
// conversation UUID. agy accepts session-specific configuration via the
// `--settings <settingsPath>` flag, mirroring the claude runner.
//
// agy has no hooks, so Conveyor installs none for it. The dispatcher's reaper
// stands in for the Claude hooks: it promotes the run on PID liveness, reads
// the conversation id from the files the process has open, and on exit runs
// the Stop handling over the log's <conveyor> block. Not covered: live
// per-tool activity and permission/idle notifications mid-run.
package agy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

var (
	// ErrNotFound means the agy binary is not on PATH.
	ErrNotFound = errors.New("agy CLI not found")
	// ErrUntrusted means the workspace is not trusted by Antigravity.
	ErrUntrusted = errors.New("workspace not trusted by Antigravity")
)

var execCommand = exec.Command // swapped in tests

// LaunchSpec describes a fresh agy background launch.
type LaunchSpec struct {
	Cwd          string
	AddDirs      []string
	Prompt       string
	Name         string // task key, used as the conversation label and log name
	SettingsPath string // path to agy-settings.json
	Model        string
	Agent        string
	MCPConfig    string
	Bypass       bool // dangerously-skip-permissions
}

func (s LaunchSpec) args() []string {
	a := []string{"--print", s.Prompt, "--output-format", "text"}
	if s.SettingsPath != "" {
		a = append(a, "--settings", s.SettingsPath)
	}
	if s.Bypass {
		a = append(a, "--dangerously-skip-permissions")
	}
	if s.Model != "" {
		a = append(a, "--model", s.Model)
	}
	if s.Agent != "" {
		ensureAgyAgentAvailable(s.Agent)
		a = append(a, "--agent", s.Agent)
	}
	for _, d := range s.AddDirs {
		a = append(a, "--add-dir", d)
	}
	return a
}

// ensureAgyAgentAvailable ensures that an agent defined in ~/.claude/agents/<name>.md
// is also available in ~/.gemini/config/agents/<name>.md for agy.
func ensureAgyAgentAvailable(name string) {
	h, err := os.UserHomeDir()
	if err != nil {
		return
	}
	geminiAgent := filepath.Join(h, ".gemini", "config", "agents", name+".md")
	if _, err := os.Stat(geminiAgent); err == nil {
		return // already present
	}
	claudeAgent := filepath.Join(h, ".claude", "agents", name+".md")
	b, err := os.ReadFile(claudeAgent)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(geminiAgent), 0o755)
	_ = os.WriteFile(geminiAgent, b, 0o644)
}

// Launch starts `agy --print ...` as a detached OS process, writing its output to
// <conveyorHome>/agy-logs/<Name>.log, and returns the PID as the bg-id string.
func Launch(conveyorHome string, s LaunchSpec) (bgID string, conversationID string, err error) {
	pid, err := start(conveyorHome, s, os.O_TRUNC, s.args())
	if err != nil {
		return "", "", err
	}
	return strconv.Itoa(pid), "", nil // the reaper fills the conversation id once agy opens it
}

// Resume stops bgID (errors ignored) and continues conversationID with a new
// detached `agy --print` that appends to the same <Name>.log the reaper reads,
// keeping the task's model and agent.
func Resume(conveyorHome, bgID, conversationID string, s LaunchSpec) (string, error) {
	_ = Stop(context.Background(), bgID)
	pid, err := start(conveyorHome, s, os.O_APPEND, append([]string{"--conversation", conversationID}, s.args()...))
	if err != nil {
		return "", err
	}
	return strconv.Itoa(pid), nil
}

// start runs agy in its own session with no context, so quitting Conveyor
// does not kill it (a claude --bg session survives the same way).
func start(conveyorHome string, s LaunchSpec, mode int, args []string) (int, error) {
	logPath := bgLogPath(conveyorHome, s.Name)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return 0, fmt.Errorf("agy log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|mode, 0o644)
	if err != nil {
		return 0, fmt.Errorf("agy log file: %w", err)
	}
	defer logFile.Close()

	cmd := execCommand("agy", args...)
	cmd.Dir = s.Cwd
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // own session and process group
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("agy start: %w", err)
	}
	go func() { _ = cmd.Wait() }() // reap while Conveyor runs
	return cmd.Process.Pid, nil
}

// DetectConversationID returns the conversation UUID of the agy process pid, read from the
// conversations/<id>.db or presence/<id>.lock file it has open, or "" if it has none open yet.
// It checks once without waiting: the reaper calls it again each tick until it finds one.
func DetectConversationID(pid int) string {
	for _, f := range openFiles(pid) {
		if (strings.Contains(f, "conversations") && strings.HasSuffix(f, ".db")) ||
			(strings.Contains(f, "presence") && strings.HasSuffix(f, ".lock")) {
			if m := convRe.FindString(filepath.Base(f)); m != "" {
				return m
			}
		}
	}
	return ""
}

var convRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// openFiles lists the paths process pid has open: /proc on Linux, lsof elsewhere (macOS).
func openFiles(pid int) []string {
	var out []string
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	if entries, err := os.ReadDir(fdDir); err == nil {
		for _, e := range entries {
			if t, err := os.Readlink(filepath.Join(fdDir, e.Name())); err == nil {
				out = append(out, t)
			}
		}
		return out
	}
	b, _ := exec.Command("lsof", "-n", "-P", "-Fn", "-p", strconv.Itoa(pid)).Output() // exits 1 on partial results; use what it printed
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "n") {
			out = append(out, l[1:])
		}
	}
	return out
}

// bgLogPath returns the log file path for a bg agy run.
func bgLogPath(home, name string) string {
	return filepath.Join(home, "agy-logs", name+".log")
}

// BgLogPath exports bgLogPath for use by the runner package.
func BgLogPath(home, name string) string {
	return bgLogPath(home, name)
}

// IsPIDRunning reports whether the process with the given PID is still alive.
func IsPIDRunning(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

// Stop sends SIGTERM to the process identified by bgID (a PID string).
func Stop(_ context.Context, bgID string) error {
	pid, err := strconv.Atoi(bgID)
	if err != nil {
		return fmt.Errorf("agy stop: invalid bg id %q: %w", bgID, err)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil // already gone
	}
	if err := p.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("agy stop signal: %w", err)
	}
	return nil
}

func q(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// AttachCommand returns a command to stop the background process and attach interactively.
func AttachCommand(bgID string) string {
	if pid, err := strconv.Atoi(bgID); err == nil {
		if convID := DetectConversationID(pid); convID != "" {
			return "kill -TERM " + bgID + " 2>/dev/null; agy --conversation " + q(convID)
		}
	}
	return "echo 'Conveyor: agy runner (PID " + bgID + ") attach not available'"
}

// ResumeCommand returns the agy command to resume a conversation interactively.
func ResumeCommand(sessionID, settingsPath string, addDirs []string) string {
	c := "agy"
	if settingsPath != "" {
		c += " --settings " + q(settingsPath)
	}
	c += " --conversation " + q(sessionID)
	for _, d := range addDirs {
		c += " --add-dir " + q(d)
	}
	return c
}

// FreshCommand returns the agy command to start a fresh interactive session primed with the brief.
func FreshCommand(settingsPath, briefPath string, addDirs []string) string {
	c := "agy"
	if settingsPath != "" {
		c += " --settings " + q(settingsPath)
	}
	c += " --prompt-interactive " + q("Read "+briefPath+" first, then wait for my instructions.")
	for _, d := range addDirs {
		c += " --add-dir " + q(d)
	}
	return c
}

// WriteSettings writes <home>/agy-settings.json atomically and returns its path.
// It loads base settings from ~/.gemini/antigravity-cli/settings.json if present to inherit user preferences
// and adds Conveyor task configuration.
func WriteSettings(home, conveyorBin string) (string, error) {
	cfg := map[string]any{}
	if h, err := os.UserHomeDir(); err == nil {
		basePath := filepath.Join(h, ".gemini", "antigravity-cli", "settings.json")
		if b, err := os.ReadFile(basePath); err == nil {
			_ = json.Unmarshal(b, &cfg)
		}
	}
	delete(cfg, "ide")

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal agy settings: %w", err)
	}

	path := filepath.Join(home, "agy-settings.json")
	if err := writeAtomic(path, append(b, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write agy settings: %w", err)
	}
	return path, nil
}

// writeAtomic writes b to path via a temp file in the same directory.
func writeAtomic(path string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
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
