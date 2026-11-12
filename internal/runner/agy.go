package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/JayaSuryaOolio/conveyor/internal/agy"
)

// AgyRunner wraps the agy package as a Runner.
type AgyRunner struct {
	// ConveyorHome is the conveyor home directory (used for log files).
	ConveyorHome string
}

// NewAgyRunner returns an Antigravity (agy) runner.
func NewAgyRunner(home string) Runner { return &AgyRunner{ConveyorHome: home} }

func (a *AgyRunner) Name() string { return "agy" }

// Launch ignores ctx: the agy process must outlive Conveyor.
func (a *AgyRunner) Launch(_ context.Context, s LaunchSpec) (string, string, error) {
	return agy.Launch(a.ConveyorHome, toAgy(s))
}

// Resume ignores ctx: the agy process must outlive Conveyor.
func (a *AgyRunner) Resume(_ context.Context, bgID, sessionID string, s LaunchSpec) (string, error) {
	return agy.Resume(a.ConveyorHome, bgID, sessionID, toAgy(s))
}

// toAgy maps s to agy's spec; Name is the task key, which names the log the reaper reads.
func toAgy(s LaunchSpec) agy.LaunchSpec {
	return agy.LaunchSpec{
		Cwd:          s.Cwd,
		AddDirs:      s.AddDirs,
		Prompt:       s.Prompt,
		Name:         s.TaskKey,
		SettingsPath: s.SettingsPath,
		Model:        s.Model,
		Agent:        s.Agent,
		Bypass:       s.Bypass,
	}
}

func (a *AgyRunner) Stop(ctx context.Context, bgID string) error {
	return agy.Stop(ctx, bgID)
}

func (a *AgyRunner) Remove(_ context.Context, _ string) error { return nil }

func (a *AgyRunner) AttachCommand(bgID string) string {
	return agy.AttachCommand(bgID)
}

func (a *AgyRunner) ResumeCommand(sessionID, settingsPath string, addDirs []string) string {
	return agy.ResumeCommand(sessionID, settingsPath, addDirs)
}

func (a *AgyRunner) FreshCommand(settingsPath, briefPath string, addDirs []string) string {
	return agy.FreshCommand(settingsPath, briefPath, addDirs)
}

func (a *AgyRunner) WriteSettings(home, conveyorBin string) (string, error) {
	return agy.WriteSettings(home, conveyorBin)
}

// IsPIDRunning reports whether an agy bg-id (a PID string) is still running.
func IsPIDRunning(bgID string) bool {
	pid, err := strconv.Atoi(bgID)
	if err != nil {
		return false
	}
	return agy.IsPIDRunning(pid)
}

// DetectConversationID returns the agy conversation UUID process pid has open, or "".
func DetectConversationID(pid int) string {
	return agy.DetectConversationID(pid)
}

// BgLogPath returns the path to the agy background session log for a task.
func BgLogPath(home, taskKey string) string {
	return filepath.Join(home, "agy-logs", taskKey+".log")
}

// Plugin installs the conveyor plugin for agy: hooks, skills, and agents.
func (a *AgyRunner) Plugin(action string, p PluginSpec) (PluginStatus, error) {
	st := PluginStatus{Tool: a.Name()}
	dir, err := agy.PluginDir()
	if err != nil {
		return st, err
	}
	st.Path = dir
	switch action {
	case "status":
	case "install", "uninstall":
		if err := agy.SetPlugin(dir, p.Skills, p.Agents, p.ConveyorBin, p.Home, action == "install"); err != nil {
			return st, err
		}
	default:
		return st, fmt.Errorf("unknown plugin action %q", action)
	}
	st.Status, err = agy.PluginStatus(dir, p.Skills, p.Agents, p.ConveyorBin, p.Home)
	return st, err
}
