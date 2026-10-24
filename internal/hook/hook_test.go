package hook

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

type env struct {
	db   *sql.DB
	home string
	task store.Task
}

func setup(t *testing.T, st store.Status) env {
	t.Helper()
	home := t.TempDir()
	db, err := store.Open(core.DBPath(home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r, err := store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := store.CreateTask(db, store.Task{Title: "t", Status: st}, []store.TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	return env{db, home, tk}
}

func (e env) cwd() string { return filepath.Join(core.WorkspacesDir(e.home), e.task.Key, "a") }

func (e env) fire(t *testing.T, event string, m map[string]any) error {
	t.Helper()
	if _, ok := m["cwd"]; !ok {
		m["cwd"] = e.cwd()
	}
	b, _ := json.Marshal(m)
	return Handle(e.db, e.home, event, b)
}

func (e env) run(t *testing.T, kind, status, session, prompt string) store.Run {
	t.Helper()
	var sp *string
	if session != "" {
		sp = &session
	}
	r, err := store.CreateRun(e.db, store.Run{TaskID: e.task.ID, Kind: kind, Mode: "fresh", Status: status, SessionID: sp, Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e env) status(t *testing.T) store.Task {
	t.Helper()
	tk, err := store.GetTask(e.db, e.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

const doneMsg = "All good.\n<conveyor>\nstatus: done\nsummary: s\ncriteria:\n- [x] AC1 — ok\nlearning: l1\nspawn: child\n</conveyor>"
const failMsg = "<conveyor>\nstatus: failed\nsummary: nope\n</conveyor>"

func TestPromptSetsSessionAndNeedsYouResumes(t *testing.T) {
	e := setup(t, store.StatusNeedsYou)
	r := e.run(t, "auto", "launching", "", "p")
	if err := e.fire(t, "UserPromptSubmit", map[string]any{"session_id": "S1"}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(e.db, r.ID)
	if got.SessionID == nil || *got.SessionID != "S1" || got.Status != "running" {
		t.Fatalf("%+v", got)
	}
	if e.status(t).Status != store.StatusRunning {
		t.Fatal("needs_you should become running")
	}
}

func TestStopDoneToReview(t *testing.T) {
	e := setup(t, store.StatusRunning)
	r := e.run(t, "auto", "running", "S1", "do it")
	tp := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(tp, []byte(`{"type":"user","message":{"content":"do it"}}`+"\n"+
		`{"type":"assistant","message":{"id":"m","usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":10,"cache_creation_input_tokens":1}}}`+"\n"), 0o644)
	if err := e.fire(t, "Stop", map[string]any{"session_id": "S1", "last_assistant_message": doneMsg, "transcript_path": tp}); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetRun(e.db, r.ID)
	if got.Status != "finished" || got.EndedAt == nil || *got.BlockStatus != "done" || *got.FinalMessage != "All good." ||
		*got.Criteria != "- [x] AC1 — ok" || got.CtxPeak != 16 || got.APICalls != 1 || got.TokensOut != 2 {
		t.Fatalf("%+v", got)
	}
	if e.status(t).Status != store.StatusReview {
		t.Fatal("want review")
	}
	cs, _ := store.ListComments(e.db, e.task.ID)
	if len(cs) != 1 || cs[0].Author != "agent" || cs[0].Body != "All good." {
		t.Fatalf("%+v", cs)
	}
	var n int
	var repo sql.NullInt64
	e.db.QueryRow(`SELECT COUNT(*), MAX(repo_id) FROM learnings WHERE status='proposed'`).Scan(&n, &repo)
	if n != 1 || !repo.Valid {
		t.Fatalf("learnings %d %v", n, repo)
	}
	evs, _ := store.ListEvents(e.db, e.task.ID)
	spawn := 0
	for _, ev := range evs {
		if ev.Kind == "spawn" && ev.Detail == "child" {
			spawn++
		}
	}
	if spawn != 1 {
		t.Fatal("want spawn event")
	}
	all, _ := store.ListTasks(e.db, store.StatusBacklog)
	if len(all) != 1 || all[0].Title != "child" || all[0].ParentID == nil || *all[0].ParentID != e.task.ID ||
		len(all[0].Repos) != 1 || all[0].Repos[0].RepoName != "a" || !all[0].Repos[0].IsPrimary {
		t.Fatalf("spawned child: %+v", all)
	}
}

func TestStopNoBlockNeedsYou(t *testing.T) {
	e := setup(t, store.StatusRunning)
	e.run(t, "auto", "running", "S1", "")
	if err := e.fire(t, "Stop", map[string]any{"session_id": "S1", "last_assistant_message": "question?"}); err != nil {
		t.Fatal(err)
	}
	if e.status(t).Status != store.StatusNeedsYou {
		t.Fatal("want needs_you")
	}
}

func TestStopFailedThenMax(t *testing.T) {
	e := setup(t, store.StatusRunning)
	store.SetSetting(e.db, "max_consecutive_failures", "2")
	e.run(t, "auto", "running", "S1", "")
	e.fire(t, "Stop", map[string]any{"session_id": "S1", "last_assistant_message": failMsg})
	tk := e.status(t)
	if tk.Status != store.StatusTodo || tk.ConsecutiveFailures != 1 {
		t.Fatalf("%+v", tk)
	}
	store.UpdateTaskStatus(e.db, tk.ID, store.StatusRunning, store.ActorSystem)
	e.run(t, "auto", "running", "S2", "")
	e.fire(t, "Stop", map[string]any{"session_id": "S2", "last_assistant_message": failMsg})
	tk = e.status(t)
	if tk.Status != store.StatusFailed || tk.ConsecutiveFailures != 2 {
		t.Fatalf("%+v", tk)
	}
}

func TestStopWithPendingCommentsToTodo(t *testing.T) {
	e := setup(t, store.StatusRunning)
	e.run(t, "auto", "running", "S1", "")
	store.AddComment(e.db, store.Comment{TaskID: e.task.ID, Author: store.AuthorHuman, Body: "also"})
	e.fire(t, "Stop", map[string]any{"session_id": "S1", "last_assistant_message": doneMsg})
	if e.status(t).Status != store.StatusTodo {
		t.Fatal("want todo")
	}
}

func TestInteractiveStopLeavesTask(t *testing.T) {
	e := setup(t, store.StatusReview)
	// no run yet: an interactive run is created on the fly
	if err := e.fire(t, "Stop", map[string]any{"session_id": "I1", "last_assistant_message": doneMsg}); err != nil {
		t.Fatal(err)
	}
	if e.status(t).Status != store.StatusReview {
		t.Fatal("status must not change")
	}
	r, err := store.RunBySession(e.db, "I1")
	if err != nil || r.Kind != "interactive" || r.Status != "finished" || r.FinalMessage == nil {
		t.Fatalf("%+v %v", r, err)
	}
	if cs, _ := store.ListComments(e.db, e.task.ID); len(cs) != 0 {
		t.Fatal("no comment for interactive")
	}
}

func TestResumeUsesNewestRunOfSession(t *testing.T) {
	e := setup(t, store.StatusRunning)
	e.run(t, "auto", "finished", "S1", "first")
	r2 := e.run(t, "auto", "running", "S1", "second")
	e.fire(t, "Stop", map[string]any{"session_id": "S1", "last_assistant_message": "q"})
	got, _ := store.GetRun(e.db, r2.ID)
	if got.Status != "finished" {
		t.Fatalf("%+v", got)
	}
}

func TestNotification(t *testing.T) {
	e := setup(t, store.StatusRunning)
	e.fire(t, "Notification", map[string]any{"message": "needs input", "notification_type": "idle"})
	evs, _ := store.ListEvents(e.db, e.task.ID)
	if len(evs) == 0 || evs[len(evs)-1].Kind != "notification" {
		t.Fatalf("%+v", evs)
	}
}

func TestOutsideWorkspacesIgnored(t *testing.T) {
	e := setup(t, store.StatusRunning)
	for _, cwd := range []string{"/somewhere/else", core.WorkspacesDir(e.home), ""} {
		if err := e.fire(t, "Stop", map[string]any{"cwd": cwd, "session_id": "S1", "last_assistant_message": doneMsg}); err != nil {
			t.Fatal(err)
		}
	}
	if rs, _ := store.ListRuns(e.db, e.task.ID); len(rs) != 0 {
		t.Fatal("no run expected")
	}
	if e.status(t).Status != store.StatusRunning {
		t.Fatal("unchanged")
	}
}

func TestMalformedJSON(t *testing.T) {
	e := setup(t, store.StatusRunning)
	if err := Handle(e.db, e.home, "Stop", []byte("{nope")); err == nil {
		t.Fatal("want error")
	}
}
