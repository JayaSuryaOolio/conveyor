package brief

import (
	"database/sql"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// StartCheck is the pre-start check of a run: when the last run started, when BRIEF.md was last
// rendered and when the description last changed, and what the next run must do about it.
type StartCheck struct {
	LastRunAt     string `json:"last_run_at"`    // "" = no run yet
	BriefAt       string `json:"brief_at"`       // BRIEF.md mtime before this render; "" = none
	DescriptionAt string `json:"description_at"` // last description edit; "" = never edited
	BriefStale    bool   `json:"brief_stale"`    // BRIEF.md is missing or older than the description
	Reread        bool   `json:"reread"`         // someone other than the task's agent edited it since the last run started
	Decision      string `json:"decision"`       // added to the run's prompt; "" = nothing to say
}

// RunPrecheck runs Precheck for the task's next run. Call it before brief.Render, which updates BRIEF.md's mtime.
func RunPrecheck(db *sql.DB, home string, task store.Task) (StartCheck, error) {
	runs, err := store.ListRuns(db, task.ID)
	if err != nil {
		return StartCheck{}, err
	}
	briefAt := ""
	if fi, err := os.Stat(Path(home, task)); err == nil {
		briefAt = fi.ModTime().UTC().Format(time.RFC3339)
	}
	return Precheck(task, runs, briefAt), nil
}

// Precheck decides whether the next run must re-read the description: only when someone other than
// the task's own agent edited it after the last run started. The task's own edits wake nothing.
// Times are store RFC3339 strings, so they compare as strings.
func Precheck(task store.Task, runs []store.Run, briefAt string) StartCheck {
	c := StartCheck{BriefAt: briefAt}
	if n := len(task.DescriptionEdits); n > 0 {
		c.DescriptionAt = task.DescriptionEdits[n-1].At
	}
	c.BriefStale = briefAt == "" || briefAt < c.DescriptionAt
	if len(runs) == 0 {
		return c // a first run reads the whole brief anyway
	}
	c.LastRunAt = runs[len(runs)-1].StartedAt
	var since []store.CommentEdit // edits at or after the last start (same-second counts: re-read is the safe side)
	for _, e := range task.DescriptionEdits {
		if e.At >= c.LastRunAt {
			since = append(since, e)
		}
	}
	var others []string
	for _, e := range since {
		if who := editor(e.By); e.By != "agent:"+task.Key && !slices.Contains(others, who) {
			others = append(others, who)
		}
	}
	switch {
	case len(since) == 0:
		c.Decision = "Start check: the description is unchanged since your last run started (" + c.LastRunAt + "); no need to re-read it."
	case len(others) == 0:
		c.Decision = "Start check: only you edited the description since your last run; nothing to re-read."
	default:
		c.Reread = true
		quoted := "> (empty)"
		if s := strings.TrimSpace(since[0].Body); s != "" {
			quoted = "> " + strings.ReplaceAll(s, "\n", "\n> ")
		}
		c.Decision = "Start check: " + strings.Join(others, ", ") + " edited the description at " + c.DescriptionAt +
			", after your last run started (" + c.LastRunAt + "). Re-read the Description in BRIEF.md and update your acceptance criteria to match.\n\nThe description your last run saw:\n\n" + quoted
	}
	return c
}

// editor names who edited a description, for the start check.
func editor(by string) string {
	switch {
	case by == "" || by == "human":
		return "The human"
	case strings.HasPrefix(by, "agent:"):
		return "Agent " + strings.TrimPrefix(by, "agent:")
	}
	return "An agent"
}
