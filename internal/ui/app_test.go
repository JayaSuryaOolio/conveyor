package ui

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func newApp(t *testing.T) (*App, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	return &App{svc: &core.Service{DB: db}}, path
}

func TestListTasksAndGetTask(t *testing.T) {
	a, _ := newApp(t)
	key, err := a.CreateTask(core.NewTaskInput{Title: "One", Repos: []string{"a"}, Start: true})
	if err != nil {
		t.Fatal(err)
	}
	if cs, _ := a.ListTasks(); len(cs) != 1 || cs[0].LastSummary != "" || cs[0].Repos[0] != "a" || cs[0].Status != "todo" {
		t.Fatalf("no-run card: %+v", cs)
	}
	tk, _ := store.GetTaskByKey(a.svc.DB, key)
	sum := "did it"
	run, _ := store.CreateRun(a.svc.DB, store.Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "finished", CtxPeak: 31000})
	run.Summary = &sum
	store.UpdateRun(a.svc.DB, run)
	if err := a.AddComment(key, "hi"); err != nil {
		t.Fatal(err)
	}
	cs, _ := a.ListTasks()
	if cs[0].LastSummary != "did it" || cs[0].CtxPeak != 31000 {
		t.Fatalf("card: %+v", cs[0])
	}
	d, err := a.GetTask(key)
	if err != nil || d.Task.Key != key || len(d.Comments) != 1 || len(d.Runs) != 1 || len(d.Task.Repos) != 1 {
		t.Fatalf("detail: %+v %v", d, err)
	}
	if _, err := a.GetTask("NOPE-1"); err == nil {
		t.Fatal("want error")
	}
}

func TestPollEmitsOnExternalWrite(t *testing.T) {
	a, path := newApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var n atomic.Int32
	go pollDataVersion(ctx, path, 10*time.Millisecond, func() { n.Add(1) })
	time.Sleep(100 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatal("emitted without a write")
	}
	a.CreateTask(core.NewTaskInput{Title: "x", Repos: []string{"a"}})
	deadline := time.Now().Add(2 * time.Second)
	for n.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n.Load() == 0 {
		t.Fatal("no emit after write")
	}
}

func TestOpenLogRotates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "conveyor.log")
	os.WriteFile(p, make([]byte, maxLog+1), 0o644)
	f, err := openLog(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if fi, _ := os.Stat(p); fi.Size() != 0 {
		t.Fatalf("log not rotated: %d bytes", fi.Size())
	}
	if fi, err := os.Stat(p + ".1"); err != nil || fi.Size() != maxLog+1 {
		t.Fatalf("old log not kept: %v", err)
	}
}
