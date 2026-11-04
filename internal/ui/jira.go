package ui

import (
	"context"

	"github.com/JayaSuryaOolio/conveyor/internal/jira"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// JiraSourceGet returns the epic's Jira source, or nil when none is attached.
func (a *App) JiraSourceGet(epicKey string) (*store.JiraSource, error) {
	return jira.GetSource(a.svc, epicKey)
}

// JiraSourceSet attaches or reconfigures the epic's Jira source.
func (a *App) JiraSourceSet(epicKey string, s store.JiraSource) error {
	return jira.SetSource(a.svc, epicKey, s)
}

// JiraSourceClear detaches the source; synced tasks stay.
func (a *App) JiraSourceClear(epicKey string) error { return jira.ClearSource(a.svc, epicKey) }

// JiraSourceSyncNow pulls now and returns the new task keys; a failure is also on the source's last_error.
func (a *App) JiraSourceSyncNow(epicKey string) ([]string, error) {
	return jira.Pull(context.Background(), a.svc, epicKey)
}
