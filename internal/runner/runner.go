// Package runner defines the Runner interface that abstracts agent backends
// (Claude Code and Antigravity/agy). Both backends implement the same lifecycle:
// Launch (fresh background session), Resume (continue an earlier session), Stop,
// Remove, and terminal command helpers.
package runner

import (
	"context"
	"fmt"
)

// Runner abstracts the agent CLI backend (Claude Code or Antigravity).
type Runner interface {
	// Launch starts a fresh background session and returns the bg id and session id (if immediately known).
	Launch(ctx context.Context, s LaunchSpec) (bgID string, sessionID string, err error)
	// Resume continues session sessionID (stopping bgID first) with s and returns the new bg id.
	Resume(ctx context.Context, bgID, sessionID string, s LaunchSpec) (string, error)
	// Stop signals the background session to stop.
	Stop(ctx context.Context, bgID string) error
	// Remove removes / cleans up the background session record.
	Remove(ctx context.Context, bgID string) error
	// AttachCommand returns the shell command to attach to a running bg session.
	AttachCommand(bgID string) string
	// ResumeCommand returns the shell command to resume a finished session interactively.
	ResumeCommand(sessionID, settingsPath string, addDirs []string) string
	// FreshCommand returns the shell command to start a fresh interactive session primed with the brief.
	FreshCommand(settingsPath, briefPath string, addDirs []string) string
	// WriteSettings writes the backend's settings/hook-config file under home and returns its path.
	// conveyorBin is the path to the conveyor CLI that hook commands call back.
	WriteSettings(home, conveyorBin string) (string, error)
	// Name returns the short name of the backend ("claude" or "agy").
	Name() string
}

// LaunchSpec describes a fresh background launch, backend-agnostic.
type LaunchSpec struct {
	Cwd          string
	AddDirs      []string
	Prompt       string
	TaskKey      string // used as the session name
	SettingsPath string
	Model        string
	Agent        string
	MCPConfig    string
	Bypass       bool // skip permission prompts (auto runs)
}

// ForTask returns the appropriate Runner for a task, given the task's runner
// field and the default runner from settings. home is the conveyor home dir.
func ForTask(taskRunner, defaultRunner, home string) Runner {
	r := taskRunner
	if r == "" {
		r = defaultRunner
	}
	switch r {
	case "agy":
		return NewAgyRunner(home)
	default:
		return NewClaudeRunner()
	}
}

// ValidateRunner checks that runner is a known backend name or empty.
func ValidateRunner(r string) error {
	switch r {
	case "", "claude", "agy":
		return nil
	}
	return fmt.Errorf("unknown runner %q: must be \"claude\" or \"agy\"", r)
}
