package automation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func at(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func TestDue(t *testing.T) {
	last := "2026-01-01T10:00:00Z"
	a := store.Automation{Schedule: "*/15 * * * *", Enabled: true, LastRunAt: &last}
	for _, c := range []struct {
		name string
		mod  func(*store.Automation)
		now  string
		want bool
	}{
		{"before next slot", nil, "2026-01-01T10:14:59Z", false},
		{"at next slot", nil, "2026-01-01T10:15:00Z", true},
		{"missed window fires", nil, "2026-01-01T13:07:00Z", true},
		{"disabled", func(a *store.Automation) { a.Enabled = false }, "2026-01-01T13:07:00Z", false},
		{"bad schedule", func(a *store.Automation) { a.Schedule = "nope" }, "2026-01-01T13:07:00Z", false},
	} {
		x := a
		if c.mod != nil {
			c.mod(&x)
		}
		if got := Due(x, at(c.now)); got != c.want {
			t.Errorf("%s: Due=%v want %v", c.name, got, c.want)
		}
	}
	// a missed run fires once: after recording, it is not due again
	now := at("2026-01-01T13:07:00Z")
	ts := now.Format(time.RFC3339)
	a.LastRunAt = &ts
	if Due(a, now.Add(time.Minute)) {
		t.Error("fired twice for one missed window")
	}
}

// env builds a service with repo "a" and fake conveyor/gh binaries on PATH that log argv.
func env(t *testing.T) (*core.Service, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	bind := t.TempDir()
	log := filepath.Join(bind, "argv.log")
	script := func(name, body string) {
		if err := os.WriteFile(filepath.Join(bind, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script("conveyor", `echo "conveyor $@" >> "`+log+`"; echo cli-ok`)
	// gh reports new comments only for PRs whose URL ends in /1
	script("gh", `echo "gh $@" >> "`+log+`"; case "$3" in */1) echo 2;; *) echo 0;; esac`)
	t.Setenv("PATH", bind+":"+os.Getenv("PATH"))
	return &core.Service{DB: db, Home: t.TempDir(), Bin: filepath.Join(bind, "conveyor")}, log
}

func mk(t *testing.T, kind, arg, pre string) store.Automation {
	return store.Automation{Name: "x", Schedule: "* * * * *", Precheck: pre, ActionKind: kind, ActionArg: arg, Enabled: true}
}

func TestRunOneCreateTask(t *testing.T) {
	svc, _ := env(t)
	a := mk(t, "create_task", `{"title":"Fix","description":"saw {output}","repos":["a"],"agent":"reviewer","start":true}`, `echo boom`)
	res, err := RunOne(context.Background(), svc, a)
	if err != nil || res != "created CV-1" {
		t.Fatalf("%q %v", res, err)
	}
	tk, _ := store.GetTaskByKey(svc.DB, "CV-1")
	if tk.Description != "saw boom" || tk.Status != store.StatusTodo || tk.Agent == nil || *tk.Agent != "reviewer" {
		t.Fatalf("%+v", tk)
	}
	// failing precheck skips
	a.Precheck = "exit 1"
	if res, err := RunOne(context.Background(), svc, a); err != nil || !strings.HasPrefix(res, "skipped") {
		t.Fatalf("%q %v", res, err)
	}
	if ts, _ := store.ListTasks(svc.DB); len(ts) != 1 {
		t.Fatal("skipped run created a task")
	}
}

func TestRunOneCommentKey(t *testing.T) {
	svc, _ := env(t)
	svc.CreateTask(core.NewTaskInput{Title: "T", Repos: []string{"a"}})
	a := mk(t, "comment", `{"task_key":"CV-1","body":"hi {output}"}`, `printf yo`)
	if res, err := RunOne(context.Background(), svc, a); err != nil || res != "commented on CV-1" {
		t.Fatalf("%q %v", res, err)
	}
	tk, _ := store.GetTaskByKey(svc.DB, "CV-1")
	cs, _ := store.ListComments(svc.DB, tk.ID)
	if len(cs) != 1 || cs[0].Body != "hi yo" || cs[0].Author != store.AuthorSystem {
		t.Fatalf("%+v", cs)
	}
}

func TestPRSweepTemplate(t *testing.T) {
	svc, log := env(t)
	repo, _ := store.GetRepoByName(svc.DB, "a")
	for i, pr := range []string{"https://gh/o/r/pull/1", "https://gh/o/r/pull/2"} {
		pr := pr
		_ = i
		tk, err := store.CreateTask(svc.DB, store.Task{Title: "T", Status: store.StatusReview},
			[]store.TaskRepo{{RepoID: repo.ID, IsPrimary: true, Branch: "t", PRURL: &pr}})
		if err != nil {
			t.Fatal(err)
		}
		_ = tk
	}
	a := Templates["pr-sweep"]
	since := "2026-01-01T00:00:00Z"
	a.LastRunAt = &since
	res, err := RunOne(context.Background(), svc, a)
	if err != nil || res != "commented on CV-1" {
		t.Fatalf("%q %v", res, err)
	}
	t1, _ := store.GetTaskByKey(svc.DB, "CV-1")
	t2, _ := store.GetTaskByKey(svc.DB, "CV-2")
	if t1.Status != store.StatusTodo || t2.Status != store.StatusReview {
		t.Fatalf("statuses %s %s", t1.Status, t2.Status)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "gh pr view https://gh/o/r/pull/1") {
		t.Fatalf("gh argv: %s", b)
	}
}

func TestRunOneCLI(t *testing.T) {
	svc, log := env(t)
	a := mk(t, "cli", `{"args":["import","jira","--jql","x y"]}`, "")
	res, err := RunOne(context.Background(), svc, a)
	if err != nil || res != "cli-ok" {
		t.Fatalf("%q %v", res, err)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "conveyor --home "+svc.Home+" import jira --jql x y") {
		t.Fatalf("argv: %s", b)
	}
}

func TestTickRecordsRun(t *testing.T) {
	svc, _ := env(t)
	old := "2026-01-01T00:00:00Z"
	a := mk(t, "create_task", `{"title":"Tick","repos":["a"]}`, "")
	a.LastRunAt = &old
	store.CreateAutomation(svc.DB, a)
	now := at("2026-01-01T00:05:00Z")
	if err := Tick(context.Background(), svc, now); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetAutomation(svc.DB, "x")
	if *got.LastRunAt != now.Format(time.RFC3339) || *got.LastResult != "created CV-1" {
		t.Fatalf("%+v", got)
	}
	Tick(context.Background(), svc, now.Add(time.Second)) // not due again
	if ts, _ := store.ListTasks(svc.DB); len(ts) != 1 {
		t.Fatalf("%d tasks", len(ts))
	}
}

func TestOneShotFiresOnceIgnoringSchedule(t *testing.T) {
	svc, _ := env(t)
	r, _ := store.GetRepoByName(svc.DB, "a")
	tk, err := store.CreateTask(svc.DB, store.Task{Title: "t", Status: store.StatusReview}, []store.TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	a := store.Automation{Name: "shot:" + tk.Key, Schedule: "", ActionKind: "comment", TaskID: &tk.ID,
		ActionArg: `{"task_key":"` + tk.Key + `","body":"ping"}`}
	now := at("2026-01-01T00:05:00Z")
	if err := store.ScheduleOnce(svc.DB, a, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	Tick(context.Background(), svc, now) // not yet
	if cs, _ := store.ListComments(svc.DB, tk.ID); len(cs) != 0 {
		t.Fatal("fired early")
	}
	for _, d := range []time.Duration{time.Minute, 2 * time.Minute} {
		if err := Tick(context.Background(), svc, now.Add(d)); err != nil {
			t.Fatal(err)
		}
	}
	if cs, _ := store.ListComments(svc.DB, tk.ID); len(cs) != 1 {
		t.Fatalf("%d comments, want 1", len(cs))
	}
	if as, _ := store.ListAutomations(svc.DB); len(as) != 1 { // only the seeded prune-closed
		t.Fatalf("not deleted: %+v", as)
	}
}

func TestAddRejectsDuplicateName(t *testing.T) {
	svc, _ := env(t)
	a := mk(t, "cli", `{"args":["task","list"]}`, "")
	if err := Add(svc, a); err != nil {
		t.Fatal(err)
	}
	if err := Add(svc, a); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Add: %v, want already exists", err)
	}
}

func TestPruneClosed(t *testing.T) {
	svc, _ := env(t)
	repo, _ := store.GetRepoByName(svc.DB, "a")
	mk := func(status store.Status, age time.Duration) store.Task {
		tk, err := store.CreateTask(svc.DB, store.Task{Title: "T", Status: status}, []store.TaskRepo{{RepoID: repo.ID, IsPrimary: true, Branch: "t"}})
		if err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-age).UTC().Format(time.RFC3339)
		svc.DB.Exec(`UPDATE tasks SET updated_at=? WHERE id=?`, ts, tk.ID)
		store.SetSetting(svc.DB, "slack_thread:C:"+tk.Key, tk.Key)
		store.SetSetting(svc.DB, "slack_task:"+tk.Key, "C:"+tk.Key)
		return tk
	}
	day := 24 * time.Hour
	oldDone, oldCan := mk(store.StatusDone, 8*day), mk(store.StatusCancelled, 9*day)
	newDone, run, todo := mk(store.StatusDone, day), mk(store.StatusRunning, 30*day), mk(store.StatusTodo, 30*day)
	store.SetSetting(svc.DB, "slack_thread:C:orphan", "CV-999")
	store.SetSetting(svc.DB, "slack_task:CV-998", "C:x")

	a := Templates["prune-closed"]
	res, err := RunOne(context.Background(), svc, a)
	if err != nil || res != "pruned 2 tasks, 6 slack links" {
		t.Fatalf("%q %v", res, err)
	}
	for _, k := range []string{oldDone.Key, oldCan.Key} {
		if _, err := store.GetTaskByKey(svc.DB, k); err == nil {
			t.Errorf("%s kept", k)
		}
	}
	for _, tk := range []store.Task{newDone, run, todo} {
		if _, err := store.GetTaskByKey(svc.DB, tk.Key); err != nil {
			t.Errorf("%s pruned", tk.Key)
		}
		if store.GetSetting(svc.DB, "slack_task:"+tk.Key) == "" {
			t.Errorf("%s link pruned", tk.Key)
		}
	}
}

func TestPruneSeededAndDue(t *testing.T) {
	svc, _ := env(t)
	if err := Tick(context.Background(), svc, time.Now()); err != nil {
		t.Fatal(err)
	}
	a, err := store.GetAutomation(svc.DB, "prune-closed")
	if err != nil || a.LastResult == nil || !strings.HasPrefix(*a.LastResult, "pruned") {
		t.Fatalf("%+v %v", a, err)
	}
	store.DeleteAutomation(svc.DB, "prune-closed")
	Tick(context.Background(), svc, time.Now())
	if _, err := store.GetAutomation(svc.DB, "prune-closed"); err == nil {
		t.Error("reseeded after rm")
	}
}
