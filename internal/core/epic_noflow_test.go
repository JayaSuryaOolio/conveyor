package core

import (
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// CV-90G: an epic that reached build without `epic approve` gets a flow, and check and restart work.
func TestEpicWithNoFlow(t *testing.T) {
	s := newSvc(t)
	e, err := s.CreateTask(NewTaskInput{Title: "e", Repos: []string{"a"}, Epic: true, EpicName: "nf",
		Description: "## Acceptance criteria\n- AC1 works"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateTask(NewTaskInput{Title: "build AC1", Repos: []string{"a"}, ParentKey: e.Key, Agent: "x", AgentSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLabel(b.Key, store.PrefixKind+"build"); err != nil {
		t.Fatal(err)
	}
	b, _ = store.GetTask(s.DB, b.ID)
	url := "https://github.com/o/r/pull/1"
	r := b.Repos[0]
	r.PRURL = &url
	if err := store.UpdateTaskRepo(s.DB, r); err != nil {
		t.Fatal(err)
	}
	s.DB.Exec(`UPDATE tasks SET status=? WHERE id=?`, store.StatusDone, b.ID)
	// the CV-90 state: stage build written straight to the labels, no epic_flows row
	e, _ = store.GetTask(s.DB, e.ID)
	if err := store.SetPrefixedLabel(s.DB, e.ID, store.PrefixStage+"build"); err != nil {
		t.Fatal(err)
	}
	if f, _ := store.ActiveFlow(s.DB, e.ID); f != nil {
		t.Fatal("setup: flow already exists")
	}

	out, err := s.EpicCheck(e.Key, true)
	if err != nil || len(out) != 0 {
		t.Fatalf("check: %v %q", err, out)
	}
	if f, _ := store.ActiveFlow(s.DB, e.ID); f == nil || f.Attempt != 1 {
		t.Fatalf("flow not created: %+v", f)
	}
	v, _ := s.EpicStatus(e.Key, 0)
	if len(v.PRs) != 1 || v.PRs[0].PRURL == nil || *v.PRs[0].PRURL != url {
		t.Fatalf("prs: %+v", v.PRs)
	}

	// restart with no active flow starts the next one instead of failing
	s.DB.Exec(`DELETE FROM epic_flows WHERE epic_id=?`, e.ID)
	if err := s.EpicRestart(e.Key, "fresh"); err != nil {
		t.Fatal(err)
	}
	if f, _ := store.ActiveFlow(s.DB, e.ID); f == nil || f.Attempt != 1 {
		t.Fatalf("restart flow: %+v", f)
	}
}

// An epic root defaults to the epic-builder preset once hooks seeded it; --agent still wins.
func TestEpicRootDefaultAgent(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	s := newSvc(t)
	agent := func(in NewTaskInput) string {
		in.Repos, in.Epic = []string{"a"}, true
		e, err := s.CreateTask(in)
		if err != nil {
			t.Fatal(err)
		}
		if e.Agent == nil {
			return ""
		}
		return *e.Agent
	}
	if got := agent(NewTaskInput{Title: "e0", EpicName: "e0"}); got != "" {
		t.Fatalf("no preset installed: agent %q", got)
	}
	if _, err := s.UserHooks("install"); err != nil {
		t.Fatal(err)
	}
	if got := agent(NewTaskInput{Title: "e1", EpicName: "e1"}); got != "epic-builder" {
		t.Fatalf("agent %q", got)
	}
	if got := agent(NewTaskInput{Title: "e2", EpicName: "e2", Agent: "sse"}); got != "sse" {
		t.Fatalf("explicit agent: %q", got)
	}
}
