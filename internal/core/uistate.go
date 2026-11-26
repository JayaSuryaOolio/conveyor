package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// UIView is the app's current view; the same shape as the frontend's View type.
type UIView struct {
	Name  string `json:"name"`
	Key   string `json:"key,omitempty"`
	Label string `json:"label,omitempty"`
	Repo  string `json:"repo,omitempty"`
	Task  string `json:"task,omitempty"`
	Q     string `json:"q,omitempty"` // automations: search text
}

// UIState is <home>/ui-state.json: what the running app shows (docs/ui-control-spec.md §4).
type UIState struct {
	View UIView `json:"view"`
	PID  int    `json:"pid"`
	At   string `json:"at"`
}

// ErrNoApp means no live app wrote the state file.
var ErrNoApp = errors.New("Conveyor app is not running")

// ErrNoTask means the app shows no task.
var ErrNoTask = errors.New("no task is open in Conveyor")

func uiStatePath(home string) string { return filepath.Join(home, "ui-state.json") }

// WriteUIState writes the state atomically (temp file, then rename) so a reader never sees half a file.
func WriteUIState(home string, v UIView, pid int, now time.Time) error {
	b, err := json.Marshal(UIState{View: v, PID: pid, At: now.UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	tmp := uiStatePath(home) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, uiStatePath(home))
}

// ReadUIState returns the state, or ErrNoApp when the file is missing, unreadable or its pid is dead.
func ReadUIState(home string) (UIState, error) {
	var s UIState
	b, err := os.ReadFile(uiStatePath(home))
	if err != nil || json.Unmarshal(b, &s) != nil || s.PID <= 0 {
		return s, ErrNoApp
	}
	// EPERM means the process exists but is not ours: still alive.
	if err := syscall.Kill(s.PID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return s, ErrNoApp
	}
	return s, nil
}

// CurrentTask is view.task (the epic view's open modal), else view.key on a task or epic view.
func (s UIState) CurrentTask() (string, error) {
	switch {
	case s.View.Task != "":
		return s.View.Task, nil
	case s.View.Key != "" && (s.View.Name == "task" || s.View.Name == "epic"):
		return s.View.Key, nil
	}
	return "", ErrNoTask
}
