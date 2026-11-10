package hook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/transcript"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// worktree gives the task repo a real clone of a bare origin, on the task branch, with CLAUDE.md on main.
func (e env) worktree(t *testing.T) (string, store.TaskRepo) {
	t.Helper()
	origin, wt := filepath.Join(t.TempDir(), "o.git"), e.cwd()
	gitT(t, ".", "init", "-q", "--bare", "-b", "main", origin)
	gitT(t, ".", "clone", "-q", origin, wt)
	gitT(t, wt, "config", "user.email", "t@t")
	gitT(t, wt, "config", "user.name", "t")
	gitT(t, wt, "checkout", "-q", "-b", "main")
	os.WriteFile(filepath.Join(wt, "CLAUDE.md"), []byte("rules\n"), 0o644)
	gitT(t, wt, "add", ".")
	gitT(t, wt, "commit", "-qm", "init")
	gitT(t, wt, "push", "-q", "origin", "main")
	tr := e.task.Repos[0]
	gitT(t, wt, "checkout", "-q", "-b", tr.Branch)
	tr.WorktreePath = &wt
	if err := store.UpdateTaskRepo(e.db, tr); err != nil {
		t.Fatal(err)
	}
	return wt, tr
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	return p
}

func TestStopCompletionCheckBlocksThenFinishes(t *testing.T) {
	e := setup(t, store.StatusRunning)
	if err := store.AddLabel(e.db, e.task.ID, "skill:tdd"); err != nil {
		t.Fatal(err)
	}
	wt, _ := e.worktree(t)
	os.WriteFile(filepath.Join(wt, "x.go"), []byte("package x\n"), 0o644)
	gitT(t, wt, "add", ".")
	gitT(t, wt, "commit", "-qm", "work")
	os.WriteFile(filepath.Join(wt, "dirty"), []byte("d"), 0o644)
	tp := writeTranscript(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{}}]}}`)
	r := e.run(t, "auto", "running", "S1", "p")

	out, err := e.fireOut(t, "Stop", map[string]any{"session_id": "S1", "last_assistant_message": "The answer.\n" + doneMsg, "transcript_path": tp})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"decision":"block"`, "uncommitted changes", "not pushed", "CLAUDE.md", `skill \"tdd\"`} {
		if !strings.Contains(out, want) {
			t.Errorf("block answer lacks %q:\n%s", want, out)
		}
	}
	if got, _ := store.GetRun(e.db, r.ID); got.Status != "running" {
		t.Fatalf("blocked stop finished the run: %s", got.Status)
	}
	if e.status(t).Status != store.StatusRunning {
		t.Fatal("blocked stop moved the task")
	}

	// The session continues from the hook; its next stop finishes whatever it still lacks.
	out, err = e.fireOut(t, "Stop", map[string]any{"session_id": "S1", "last_assistant_message": "Cleaned up.\n" + doneMsg, "transcript_path": tp, "stop_hook_active": true})
	if err != nil || out != "" {
		t.Fatalf("second stop: %q %v", out, err)
	}
	// The blocked stop's reply (often the real answer) is posted ahead of the fix-up message.
	if cs, _ := store.ListComments(e.db, e.task.ID); len(cs) != 1 || cs[0].Body != "The answer.\nAll good.\n\nCleaned up.\nAll good." {
		t.Fatalf("comments %+v", cs)
	}
	if e.status(t).Status != store.StatusReview {
		t.Fatalf("status %s, want review", e.status(t).Status)
	}
}

func TestCompletionGapsSatisfied(t *testing.T) {
	e := setup(t, store.StatusRunning)
	store.AddLabel(e.db, e.task.ID, "skill:tdd")
	wt, tr := e.worktree(t)
	os.WriteFile(filepath.Join(wt, "x.go"), []byte("package x\n"), 0o644)
	gitT(t, wt, "add", ".")
	gitT(t, wt, "commit", "-qm", "work")
	gitT(t, wt, "push", "-q", "-u", "origin", tr.Branch)
	r := e.run(t, "auto", "running", "S1", "p")
	md := filepath.Join(wt, "CLAUDE.md")
	tp := writeTranscript(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"mattpocock-skills:tdd"}}]}}`)
	p := payload{LastAssistantMessage: doneMsg, TranscriptPath: tp}

	// PR missing and CLAUDE.md not loaded yet.
	gaps, err := completionGaps(e.db, e.status(t), r, p, true)
	if err != nil || len(gaps) != 2 || !strings.Contains(gaps[0], "no PR") || !strings.Contains(gaps[1], "CLAUDE.md") {
		t.Fatalf("gaps %q %v", gaps, err)
	}
	if _, err := e.fireOut(t, "InstructionsLoaded", map[string]any{"session_id": "S1", "file_path": md}); err != nil {
		t.Fatal(err)
	}
	url := "https://github.com/o/a/pull/1"
	tr.PRURL = &url
	store.UpdateTaskRepo(e.db, tr)
	if gaps, err := completionGaps(e.db, e.status(t), r, p, true); err != nil || len(gaps) != 0 {
		t.Fatalf("gaps %q %v", gaps, err)
	}
	// A failed run is not checked.
	p.LastAssistantMessage = failMsg
	os.WriteFile(filepath.Join(wt, "dirty"), []byte("d"), 0o644)
	if gaps, _ := completionGaps(e.db, e.status(t), r, p, true); len(gaps) != 0 {
		t.Fatalf("failed run checked: %q", gaps)
	}
}

func TestUntestedEdit(t *testing.T) {
	edit, doc := transcript.ToolUse{Name: "Edit", FilePath: "/w/a.go"}, transcript.ToolUse{Name: "Write", FilePath: "/w/README.md"}
	test, ls := transcript.ToolUse{Name: "Bash", Command: "go test ./..."}, transcript.ToolUse{Name: "Bash", Command: "ls"}
	for _, c := range []struct {
		uses []transcript.ToolUse
		want string
	}{
		{[]transcript.ToolUse{edit, test}, ""},
		{[]transcript.ToolUse{test, edit, ls}, "/w/a.go"},
		{[]transcript.ToolUse{test, doc}, ""},
		{nil, ""},
	} {
		if got := untestedEdit(c.uses); got != c.want {
			t.Errorf("%v: got %q want %q", c.uses, got, c.want)
		}
	}
}

func TestCompletionGapsChangeset(t *testing.T) {
	e := setup(t, store.StatusRunning)
	wt, tr := e.worktree(t)
	url := "https://github.com/o/a/pull/1"
	tr.PRURL = &url
	store.UpdateTaskRepo(e.db, tr)
	commit := func(file string) {
		os.MkdirAll(filepath.Dir(filepath.Join(wt, file)), 0o755)
		os.WriteFile(filepath.Join(wt, file), []byte("x"), 0o644)
		gitT(t, wt, "add", ".")
		gitT(t, wt, "commit", "-qm", file)
	}
	gitT(t, wt, "checkout", "-q", "main")
	commit(".changeset/config.json")
	gitT(t, wt, "push", "-q", "origin", "main")
	gitT(t, wt, "checkout", "-q", tr.Branch)
	gitT(t, wt, "merge", "-q", "main")
	commit("a.ts")
	gitT(t, wt, "push", "-q", "-u", "origin", tr.Branch)
	r := e.run(t, "auto", "running", "S1", "p")
	md := filepath.Join(wt, "CLAUDE.md")
	tp := writeTranscript(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"`+md+`"}}]}}`)
	p := payload{LastAssistantMessage: doneMsg, TranscriptPath: tp}
	if gaps, _ := completionGaps(e.db, e.status(t), r, p, true); len(gaps) != 1 || !strings.Contains(gaps[0], "no .changeset") {
		t.Fatalf("want changeset gap: %q", gaps)
	}
	commit(".changeset/brave-cats.md")
	gitT(t, wt, "push", "-q", "origin", tr.Branch)
	if gaps, _ := completionGaps(e.db, e.status(t), r, p, true); len(gaps) != 0 {
		t.Fatalf("gaps %q", gaps)
	}
}
