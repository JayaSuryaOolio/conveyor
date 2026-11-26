package harness

import (
	"reflect"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestModelFor(t *testing.T) {
	for _, c := range []struct{ runner, grade, want string }{
		{"claude", "easy", "sonnet"}, {"claude", "medium", "sonnet"}, {"claude", "hard", "opus"},
		{"agy", "easy", "gemini-4-hydrogen-low"}, {"agy", "medium", "gemini-4-hydrogen-medium"}, {"agy", "hard", "gemini-4-hydrogen-high"},
		{"", "hard", "opus"}, // default runner is claude
	} {
		if got, err := ModelFor(c.runner, c.grade); err != nil || got != c.want {
			t.Errorf("%s/%s: got %q, %v want %q", c.runner, c.grade, got, err, c.want)
		}
	}
	for _, c := range [][2]string{{"claude", "trivial"}, {"codex", "easy"}} {
		if _, err := ModelFor(c[0], c[1]); err == nil {
			t.Errorf("%v: want error", c)
		}
	}
}

// Runs group by (runner, model) so a claude sonnet never mixes with an agy model; unfinished runs are skipped.
func TestModelByOutcome(t *testing.T) {
	p := func(s string) *string { return &s }
	runs := []store.Run{
		{Kind: "auto", Runner: "claude", Model: "sonnet", Status: "finished", BlockStatus: p("done"), CtxPeak: 100},
		{Kind: "auto", Runner: "claude", Model: "sonnet", Status: "failed", CtxPeak: 300},
		{Kind: "auto", Runner: "claude", Model: "sonnet", Status: "running"},
		{Kind: "auto", Runner: "claude", Model: "opus", Status: "finished", BlockStatus: p("done"), CtxPeak: 50},
		{Kind: "auto", Runner: "agy", Model: "gemini-4-hydrogen-low", Status: "finished", BlockStatus: p("done"), CtxPeak: 10},
		{Kind: "auto", Runner: "claude", Status: "finished", BlockStatus: p("done")}, // from before runs.model
		{Kind: "interactive", Runner: "claude", Status: "finished"},                // no resolved model: skipped
	}
	want := []ModelStat{
		{"agy", "gemini-4-hydrogen-low", 1, 1, 0, 10},
		{"claude", "", 1, 1, 0, 0},
		{"claude", "opus", 1, 1, 0, 50},
		{"claude", "sonnet", 2, 1, 1, 200},
	}
	if got := ModelByOutcome(runs); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestStaleModelEntries(t *testing.T) {
	ledger := `| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-1 | a | x | pending | unknown | 2026-11 |
| CV-2 | b | x | pending | Sonnet 5.5 | 2026-11 |
| #3 | c | x | pending | Opus 4.6 | 2026-11 |
`
	got := StaleModelEntries(ledger, "Sonnet 5.5")
	if want := []string{"#3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
