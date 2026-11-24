// Package harness holds Conveyor's deterministic checks on an agent's work: pure functions over a Facts
// snapshot that return Gaps. The Stop hook gates completion on them; docs/harness-checks.md catalogs them.
package harness

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/transcript"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

// MaxAge is how old Facts may be before the Stop gate refreshes them.
const MaxAge = 30 * time.Second

// Gap is one thing a run still owes. Check names the check, Msg is the prompt text for the agent.
type Gap struct{ Check, Msg string }

// RepoFacts is what git says about one task repo's worktree. Err set means it could not be read
// (not a worktree any more): its checks are skipped.
type RepoFacts struct {
	Name, Branch, Default, Path string
	HasPR, HasMD, HasChangeset  bool // PR recorded; CLAUDE.md exists; .changeset/config.json exists
	Dirty, Unpushed             bool
	NeedsChangeset              bool // the branch changes code but adds no .changeset/*.md
	Ahead                       int  // commits ahead of origin/<default>
	Err                         error
}

// Facts is a snapshot the checks read. The caller fills Task, Default, Run/Events, Uses and WantPR;
// Refresh fills Repos and Collected.
type Facts struct {
	Task      store.Task
	Default   map[int64]string // repo id to default branch
	RunStart  string           // the run's started_at; an instructions_loaded event counts only after it
	Events    []store.Event
	Uses      []transcript.ToolUse // whole-session transcript tool calls
	UsesErr   error                // set when the transcript could not be read: transcript checks are skipped
	WantPR    bool                 // false when the PR refresh failed or the task kind opens no PR
	Repos     []RepoFacts
	Collected time.Time
	Collect   Collector // nil means GitCollector
}

// Collector fills f.Repos from the worktrees.
type Collector func(f *Facts)

// Refresh re-collects the git facts.
func (f *Facts) Refresh() {
	c := f.Collect
	if c == nil {
		c = GitCollector
	}
	c(f)
	f.Collected = time.Now()
}

// Stale reports whether the facts are older than maxAge (or never collected).
func (f *Facts) Stale(maxAge time.Duration) bool {
	return f.Collected.IsZero() || time.Since(f.Collected) > maxAge
}

// Validate reports facts that contradict themselves, which a check would misread.
func (f *Facts) Validate() error {
	var errs []error
	for _, r := range f.Repos {
		if r.Path == "" || r.Name == "" {
			errs = append(errs, errors.New("repo facts without a name or path"))
		}
		if r.Err == nil && r.Ahead < 0 {
			errs = append(errs, errors.New(r.Name+": negative commits ahead"))
		}
	}
	return errors.Join(errs...)
}

// GitCollector reads each task repo's worktree through git.
func GitCollector(f *Facts) {
	f.Repos = nil
	for _, tr := range f.Task.Repos {
		if tr.WorktreePath == nil {
			continue
		}
		wt, def := *tr.WorktreePath, f.Default[tr.RepoID]
		r := RepoFacts{Name: tr.RepoName, Branch: tr.Branch, Default: def, Path: wt, HasPR: tr.PRURL != nil}
		out, err := workspace.Git(wt, "--no-optional-locks", "status", "--porcelain=v2") // global option: goes before the subcommand
		if err != nil {
			r.Err = err
			f.Repos = append(f.Repos, r)
			continue
		}
		r.Dirty = out != ""
		if out, err := workspace.Git(wt, "rev-list", "--count", "origin/"+def+"..HEAD"); err == nil {
			r.Ahead, _ = strconv.Atoi(out)
		}
		if r.Ahead > 0 {
			out, err := workspace.Git(wt, "rev-list", "--count", "origin/"+tr.Branch+"..HEAD")
			r.Unpushed = err != nil || out != "0"
			r.HasChangeset = fileExists(filepath.Join(wt, ".changeset", "config.json"))
			r.NeedsChangeset = r.HasChangeset && needsChangeset(wt, "origin/"+def)
		}
		r.HasMD = fileExists(filepath.Join(wt, "CLAUDE.md"))
		f.Repos = append(f.Repos, r)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
