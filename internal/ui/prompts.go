package ui

import (
	"github.com/JayaSuryaOolio/conveyor/internal/brief"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// PromptDefaults are the built-in texts behind the prompt settings.
type PromptDefaults struct {
	Protocol string `json:"protocol"`
	Fresh    string `json:"fresh"`
	Resume   string `json:"resume"`
}

// GetPromptDefaults returns the built-in status-block protocol, rules and prompt templates.
func (a *App) GetPromptDefaults() PromptDefaults {
	return PromptDefaults{Protocol: brief.Protocol(a.svc.DB), Fresh: brief.DefaultFresh, Resume: brief.DefaultResume}
}

// BriefPreview is what a launch would hand the agent.
type BriefPreview struct {
	Brief  string `json:"brief"`
	Prompt string `json:"prompt"`
}

// PreviewBrief renders the task's brief and fresh prompt without writing BRIEF.md.
func (a *App) PreviewBrief(key string) (BriefPreview, error) {
	t, err := store.GetTaskByKey(a.svc.DB, key)
	if err != nil {
		return BriefPreview{}, err
	}
	b, p, err := brief.Preview(a.svc.DB, a.svc.Home, t)
	return BriefPreview{Brief: b, Prompt: p}, err
}
