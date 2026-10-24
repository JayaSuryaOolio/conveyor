package automation

import (
	"context"
	"encoding/json"
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
	old := Bin
	Bin = filepath.Join(bind, "conveyor")
	t.Cleanup(func() { Bin = old })
	return &core.Service{DB: db, Home: t.TempDir()}, log
}

func mk(t *testing.T, kind, arg, pre string) store.Automation {
	return store.Automation{Name: "x", Schedule: "* * * * *", Precheck: pre, ActionKind: kind, ActionArg: arg, Enabled: true}
}

func TestRunOneCreateTask(t *testing.T) {
	svc, _ := env(t)
	a := mk(t, "create_task", `{"title":"Fix","description":"saw {output}","repos":["a"],"start":true}`, `echo boom`)
	res, err := RunOne(context.Background(), svc, a)
	if err != nil || res != "created CV-1" {
		t.Fatalf("%q %v", res, err)
	}
	tk, _ := store.GetTaskByKey(svc.DB, "CV-1")
	if tk.Description != "saw boom" || tk.Status != store.StatusTodo {
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

func TestOpenPRReviewSweepTemplate(t *testing.T) {
	a := Templates["open-pr-review-sweep"]
	if err := Validate(a); err != nil {
		t.Fatal(err)
	}
	var act action
	if err := json.Unmarshal([]byte(a.ActionArg), &act); err != nil || !act.Start || !strings.Contains(act.Description, "reviewThreads") {
		t.Fatalf("bad arg %v %+v", err, act)
	}
}
