package harness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// gradeModels maps a difficulty grade to the model per runner. agy bakes the effort level into the model id.
var gradeModels = map[string]map[string]string{
	"claude": {"easy": "sonnet", "medium": "sonnet", "hard": "opus"},
	"agy":    {"easy": "gemini-4-hydrogen-low", "medium": "gemini-4-hydrogen-medium", "hard": "gemini-4-hydrogen-high"},
}

// ModelFor returns the model for a difficulty grade on a runner ("" means claude, the default).
func ModelFor(runner, grade string) (string, error) {
	if runner == "" {
		runner = "claude"
	}
	m, ok := gradeModels[runner][grade]
	if !ok {
		return "", fmt.Errorf("no model for runner %q grade %q (grades: easy, medium, hard)", runner, grade)
	}
	return m, nil
}

// ModelStat is the outcome of finished runs for one (runner, model); Model "" is a run from before runs.model.
type ModelStat struct {
	Runner, Model string
	Runs, Done    int
	Failed        int
	AvgPeak       int64
}

// ModelByOutcome groups ended auto runs (Done and Failed come from the status block; Status "failed" is a crash) by (runner, model), sorted by runner then model.
func ModelByOutcome(runs []store.Run) []ModelStat {
	idx := map[[2]string]*ModelStat{}
	peak := map[[2]string]int64{}
	var keys [][2]string
	for _, r := range runs {
		if r.Kind != "auto" || r.Status == "launching" || r.Status == "running" {
			continue // interactive runs have no resolved model
		}
		k := [2]string{r.Runner, r.Model}
		s := idx[k]
		if s == nil {
			s = &ModelStat{Runner: r.Runner, Model: r.Model}
			idx[k] = s
			keys = append(keys, k)
		}
		s.Runs++
		bs := deref(r.BlockStatus)
		if bs == "done" {
			s.Done++
		} else if r.Status == "failed" || bs == "failed" {
			s.Failed++
		}
		peak[k] += r.CtxPeak
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+"\x00"+keys[i][1] < keys[j][0]+"\x00"+keys[j][1] })
	out := make([]ModelStat, len(keys))
	for i, k := range keys {
		out[i] = *idx[k]
		out[i].AvgPeak = peak[k] / int64(out[i].Runs)
	}
	return out
}

// StaleModelEntries returns the PR/task ids of ledger attempt rows tagged with a model other than current.
// Rows tagged unknown are skipped: there is nothing to compare. A row whose cells contain " | " is skipped.
func StaleModelEntries(ledger, current string) []string {
	var out []string
	for _, l := range strings.Split(ledger, "\n") {
		if !strings.HasPrefix(l, "| #") && !strings.HasPrefix(l, "| CV-") {
			continue
		}
		c := strings.Split(strings.Trim(l, "| "), " | ")
		if len(c) != 6 {
			continue
		}
		if m := strings.TrimSpace(c[4]); !strings.EqualFold(m, current) && !strings.EqualFold(m, "unknown") {
			out = append(out, strings.TrimSpace(c[0]))
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
