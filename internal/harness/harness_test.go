package harness

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/transcript"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, wt, file string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(filepath.Join(wt, file)), 0o755)
	os.WriteFile(filepath.Join(wt, file), []byte("x"), 0o644)
}

// clone gives a worktree of a bare origin, on branch b, with CLAUDE.md on main.
func clone(t *testing.T, b string) string {
	t.Helper()
	origin, wt := filepath.Join(t.TempDir(), "o.git"), filepath.Join(t.TempDir(), "w")
	git(t, ".", "init", "-q", "--bare", "-b", "main", origin)
	git(t, ".", "clone", "-q", origin, wt)
	git(t, wt, "config", "user.email", "t@t")
	git(t, wt, "config", "user.name", "t")
	git(t, wt, "checkout", "-q", "-b", "main")
	write(t, wt, "CLAUDE.md")
	git(t, wt, "add", ".")
	git(t, wt, "commit", "-qm", "init")
	git(t, wt, "push", "-q", "origin", "main")
	git(t, wt, "checkout", "-q", "-b", b)
	return wt
}

func facts(wt string, labels ...string) *Facts {
	return &Facts{
		Task:    store.Task{Labels: labels, Repos: []store.TaskRepo{{RepoID: 1, RepoName: "r", Branch: "feat", WorktreePath: &wt}}},
		Default: map[int64]string{1: "main"},
		WantPR:  true,
	}
}

func msgs(gaps []Gap) string {
	var s []string
	for _, g := range gaps {
		s = append(s, g.Check+": "+g.Msg)
	}
	return strings.Join(s, "\n")
}

func TestChecksGitFacts(t *testing.T) {
	wt := clone(t, "feat")
	write(t, wt, "x.go")
	git(t, wt, "add", ".")
	git(t, wt, "commit", "-qm", "work")
	write(t, wt, "dirty")
	f := facts(wt)
	f.Refresh()
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	out := msgs(Check(f))
	for _, want := range []string{"dirty: r: the worktree has uncommitted", "unpushed: r: branch feat", "no-pr: r: branch feat is 1 commit(s) ahead of main", "claude-md: r: you changed this repo"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// Clean, pushed, PR open, CLAUDE.md read: nothing owed.
	os.Remove(filepath.Join(wt, "dirty"))
	git(t, wt, "push", "-q", "-u", "origin", "feat")
	url := "u"
	f.Task.Repos[0].PRURL = &url
	f.Uses = []transcript.ToolUse{{Name: "Read", FilePath: filepath.Join(wt, "CLAUDE.md")}}
	f.Refresh()
	if g := Check(f); len(g) != 0 {
		t.Fatalf("gaps: %s", msgs(g))
	}
}

func TestChangesetGap(t *testing.T) {
	wt := clone(t, "feat")
	git(t, wt, "checkout", "-q", "main")
	write(t, wt, ".changeset/config.json")
	git(t, wt, "add", ".")
	git(t, wt, "commit", "-qm", "cfg")
	git(t, wt, "push", "-q", "origin", "main")
	git(t, wt, "checkout", "-q", "feat")
	git(t, wt, "merge", "-q", "main")
	write(t, wt, "a.ts")
	git(t, wt, "add", ".")
	git(t, wt, "commit", "-qm", "a")
	git(t, wt, "push", "-q", "-u", "origin", "feat")
	url := "u"
	f := facts(wt)
	f.Task.Repos[0].PRURL = &url
	f.Uses = []transcript.ToolUse{{Name: "Read", FilePath: filepath.Join(wt, "CLAUDE.md")}}
	f.Refresh()
	if out := msgs(Check(f)); !strings.Contains(out, "changeset: r: code changed but no .changeset") {
		t.Fatalf("want changeset gap:\n%s", out)
	}
	write(t, wt, ".changeset/brave-cats.md")
	git(t, wt, "add", ".")
	git(t, wt, "commit", "-qm", "cs")
	git(t, wt, "push", "-q", "origin", "feat")
	f.Refresh()
	if g := Check(f); len(g) != 0 {
		t.Fatalf("gaps: %s", msgs(g))
	}
}

func TestTranscriptChecks(t *testing.T) {
	edit, test := transcript.ToolUse{Name: "Edit", FilePath: "/w/a.go"}, transcript.ToolUse{Name: "Bash", Command: "go test ./..."}
	f := &Facts{Task: store.Task{Labels: []string{"skill:tdd"}}, Uses: []transcript.ToolUse{test, edit}}
	out := msgs(Check(f))
	if !strings.Contains(out, "untested-edit: you edited /w/a.go") || !strings.Contains(out, `skills: the brief lists skill "tdd"`) {
		t.Fatalf("got:\n%s", out)
	}
	f.Uses = []transcript.ToolUse{{Name: "Skill", Skill: "mattpocock-skills:tdd"}, edit, test}
	if g := Check(f); len(g) != 0 {
		t.Fatalf("gaps: %s", msgs(g))
	}
	// An unreadable transcript skips the checks that need it.
	f.Uses, f.UsesErr = []transcript.ToolUse{edit}, errors.New("read")
	if g := Check(f); len(g) != 0 {
		t.Fatalf("errored input checked: %s", msgs(g))
	}
}

func TestNotAWorktreeSkipped(t *testing.T) {
	f := facts(filepath.Join(t.TempDir(), "gone"))
	f.Refresh()
	if g := Check(f); len(g) != 0 {
		t.Fatalf("gaps: %s", msgs(g))
	}
}

func TestStaleAndValidate(t *testing.T) {
	f := &Facts{}
	if !f.Stale(time.Minute) {
		t.Fatal("never collected must be stale")
	}
	f.Refresh()
	if f.Stale(time.Minute) {
		t.Fatal("fresh facts are stale")
	}
	f.Collected = time.Now().Add(-time.Hour)
	if !f.Stale(30 * time.Second) {
		t.Fatal("old facts not stale")
	}
	f.Repos = []RepoFacts{{Name: "r"}} // no path
	if f.Validate() == nil {
		t.Fatal("repo without a path is inconsistent")
	}
}
