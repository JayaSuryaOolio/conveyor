package runner

import (
	"context"
	"fmt"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
)

// ClaudeRunner wraps the claude package as a Runner.
type ClaudeRunner struct{}

// NewClaudeRunner returns the Claude Code runner.
func NewClaudeRunner() Runner { return &ClaudeRunner{} }

func (c *ClaudeRunner) Name() string { return "claude" }

func (c *ClaudeRunner) Launch(ctx context.Context, s LaunchSpec) (string, string, error) {
	bgID, err := claude.Launch(ctx, claude.LaunchSpec{
		Cwd:          s.Cwd,
		AddDirs:      s.AddDirs,
		Prompt:       s.Prompt,
		Name:         s.TaskKey,
		SettingsPath: s.SettingsPath,
		Model:        s.Model,
		Agent:        s.Agent,
		MCPConfig:    s.MCPConfig,
		Bypass:       s.Bypass,
	})
	return bgID, "", err
}

func (c *ClaudeRunner) Resume(ctx context.Context, bgID, sessionID string, s LaunchSpec) (string, error) {
	return claude.Resume(ctx, s.Cwd, bgID, sessionID, s.SettingsPath, s.Prompt, s.AddDirs)
}

func (c *ClaudeRunner) Stop(ctx context.Context, bgID string) error {
	return claude.Stop(ctx, bgID)
}

func (c *ClaudeRunner) Remove(ctx context.Context, bgID string) error {
	return claude.Remove(ctx, bgID)
}

func (c *ClaudeRunner) AttachCommand(bgID string) string {
	return claude.AttachCommand(bgID)
}

func (c *ClaudeRunner) ResumeCommand(sessionID, settingsPath string, addDirs []string) string {
	return claude.ResumeCommand(sessionID, settingsPath, addDirs)
}

func (c *ClaudeRunner) FreshCommand(settingsPath, briefPath string, addDirs []string) string {
	return claude.FreshCommand(settingsPath, briefPath, addDirs)
}

func (c *ClaudeRunner) WriteSettings(home, conveyorBin string) (string, error) {
	return claude.WriteSettings(home, conveyorBin)
}

// Plugin installs the conveyor@skills-dir plugin: the user hooks plus the skills.
func (c *ClaudeRunner) Plugin(action string, p PluginSpec) (PluginStatus, error) {
	st := PluginStatus{Tool: c.Name()}
	dir, err := claude.PluginDir()
	if err != nil {
		return st, err
	}
	st.Path = dir
	settings, err := claude.UserSettingsPath()
	if err != nil {
		return st, err
	}
	switch action {
	case "status":
	case "install", "uninstall":
		if err := claude.SetPlugin(dir, settings, p.Skills, p.ConveyorBin, p.Home, action == "install"); err != nil {
			return st, err
		}
	default:
		return st, fmt.Errorf("unknown plugin action %q", action)
	}
	st.Status, err = claude.PluginStatus(dir, settings, p.Skills, p.ConveyorBin, p.Home)
	return st, err
}
