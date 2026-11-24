package harness

import (
	"os"
	"strings"
	"testing"
)

// The ledger is read by conveyor-doctor and extended by every harness PR: keep its shape stable.
func TestLedgerShape(t *testing.T) {
	b, err := os.ReadFile("../../docs/harness-ledger.md")
	if err != nil {
		t.Fatal(err)
	}
	cols := []string{"PR", "Reason", "Predicted runs", "Observed", "Model", "Date"}
	header := "| " + strings.Join(cols, " | ") + " |"
	if !strings.Contains(string(b), header) {
		t.Fatalf("ledger lacks attempts header %q", header)
	}
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "| #") && !strings.HasPrefix(l, "| CV-") {
			continue
		}
		cells := strings.Split(strings.Trim(l, "| "), " | ")
		if len(cells) != len(cols) || strings.TrimSpace(cells[2]) == "" {
			t.Errorf("attempt row needs %d cells and a prediction: %s", len(cols), l)
		}
	}
}

func TestDoctorReadsLedger(t *testing.T) {
	b, err := os.ReadFile("../../skills/conveyor-doctor/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"docs/harness-ledger.md", "failed attempt", "without rework", "steering", "requeue"} {
		if !strings.Contains(string(b), w) {
			t.Errorf("doctor skill must mention %q", w)
		}
	}
}
