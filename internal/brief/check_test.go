package brief

import (
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestPrecheck(t *testing.T) {
	runs := []store.Run{{StartedAt: "2026-01-01T09:00:00Z"}, {StartedAt: "2026-01-01T10:00:00Z"}}
	task := func(edits ...store.CommentEdit) store.Task {
		return store.Task{Key: "CV-1", Description: "now", DescriptionEdits: edits}
	}
	before := store.CommentEdit{Body: "v0", At: "2026-01-01T09:30:00Z", By: "human"}
	cases := []struct {
		name  string
		task  store.Task
		runs  []store.Run
		want  string // substring of the decision; "" = empty decision
		stale bool
	}{
		{"first run", task(), nil, "", false},
		{"unchanged", task(before), runs, "unchanged since your last run", false},
		{"own edit", task(before, store.CommentEdit{Body: "v1", At: "2026-01-01T10:05:00Z", By: "agent:CV-1"}), runs, "only you edited", true},
		{"human edit", task(before, store.CommentEdit{Body: "v1", At: "2026-01-01T10:05:00Z", By: "human"}), runs, "The human edited the description", true},
		{"other agent", task(store.CommentEdit{Body: "v1", At: "2026-01-01T10:05:00Z", By: "agent:CV-9"}, store.CommentEdit{Body: "v2", At: "2026-01-01T10:06:00Z", By: "agent:CV-1"}), runs, "Agent CV-9 edited", true},
	}
	for _, c := range cases {
		got := Precheck(c.task, c.runs, "2026-01-01T10:00:00Z")
		if c.want == "" && got.Decision != "" || c.want != "" && !strings.Contains(got.Decision, c.want) {
			t.Errorf("%s: decision %q, want %q", c.name, got.Decision, c.want)
		}
		if got.BriefStale != c.stale {
			t.Errorf("%s: brief_stale %v, want %v", c.name, got.BriefStale, c.stale)
		}
	}
	// The quote is the version the last run saw, not the latest replaced one.
	got := Precheck(task(store.CommentEdit{Body: "seen", At: "2026-01-01T10:05:00Z", By: "human"}, store.CommentEdit{Body: "mid", At: "2026-01-01T10:06:00Z", By: "human"}), runs, "")
	if !strings.Contains(got.Decision, "> seen") || strings.Contains(got.Decision, "> mid") {
		t.Errorf("quote: %q", got.Decision)
	}
}
