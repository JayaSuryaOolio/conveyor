package hook

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/harness"
	"github.com/JayaSuryaOolio/conveyor/internal/statusblock"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/transcript"
)

// completionGaps lists what an auto run reporting status done still owes (the checks live in internal/harness).
// prSynced is false when the PR refresh failed, which skips the PR check rather than nag about a PR gh could
// not see. Runs once per stop: a session already continuing from this hook (stop_hook_active) finishes
// whatever it still lacks.
func completionGaps(db *sql.DB, task store.Task, run store.Run, p payload, prSynced bool) ([]string, error) {
	block, _, ok := statusblock.Parse(p.LastAssistantMessage)
	if run.Kind != "auto" || !ok || block.Status != "done" || p.StopHookActive {
		return nil, nil
	}
	task, err := store.GetTask(db, task.ID) // refreshPR may have just set pr_url
	if err != nil {
		return nil, err
	}
	def, err := store.RepoMap(db, func(r store.Repo) string { return r.DefaultBranch })
	if err != nil {
		return nil, err
	}
	events, err := store.ListEvents(db, task.ID)
	if err != nil {
		return nil, err
	}
	kind := store.LabelValue(task, store.PrefixKind)
	f := &harness.Facts{Task: task, Default: def, RunStart: run.StartedAt, Events: events,
		WantPR: prSynced && kind != "prototype" && kind != "research"} // their kind rules say open no PR
	if p.TranscriptPath != "" {
		f.Uses, f.UsesErr = transcript.ToolUses(p.TranscriptPath)
	} else {
		f.UsesErr = errors.New("no transcript")
	}
	if f.Stale(harness.MaxAge) {
		f.Refresh()
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	var gaps []string
	for _, g := range harness.Check(f) {
		gaps = append(gaps, g.Msg)
	}
	return gaps, nil
}

// untestedEdit is kept for the hook tests.
var untestedEdit = harness.UntestedEdit

// stopBlock is the Stop hook answer that keeps the session going with the gaps as its next prompt.
func stopBlock(gaps []string) string {
	reason := "Conveyor's completion check found work left before this run can finish:\n- " + strings.Join(gaps, "\n- ") +
		"\nFix these, then end with the status block again. If one cannot be fixed, say why and report status: failed."
	b, _ := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	return string(b)
}
