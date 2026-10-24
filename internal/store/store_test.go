package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mkRepo(t *testing.T, db *sql.DB, name string) Repo {
	t.Helper()
	r, err := AddRepo(db, Repo{Name: name, Path: "/x/" + name, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mkTask(t *testing.T, db *sql.DB, st Status) Task {
	t.Helper()
	r, err := GetRepoByName(db, "a")
	if err != nil {
		r = mkRepo(t, db, "a")
	}
	tk, err := CreateTask(db, Task{Title: "t", Status: st}, []TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestTransitionTable(t *testing.T) {
	H, S := ActorHuman, ActorSystem
	valid := []struct {
		from, to Status
		actors   []Actor
	}{
		{StatusBacklog, StatusTodo, []Actor{H}},
		{StatusTodo, StatusBacklog, []Actor{H}},
		{StatusTodo, StatusRunning, []Actor{S}},
		{StatusRunning, StatusNeedsYou, []Actor{S}},
		{StatusRunning, StatusReview, []Actor{H, S}},
		{StatusRunning, StatusFailed, []Actor{S}},
		{StatusRunning, StatusTodo, []Actor{S}},
		{StatusNeedsYou, StatusRunning, []Actor{S}},
		{StatusNeedsYou, StatusTodo, []Actor{H, S}},
		{StatusNeedsYou, StatusDone, []Actor{H}},
		{StatusReview, StatusTodo, []Actor{H, S}},
		{StatusReview, StatusNeedsYou, []Actor{S}},
		{StatusReview, StatusDone, []Actor{H, S}},
		{StatusFailed, StatusTodo, []Actor{H, S}},
		{StatusDone, StatusTodo, []Actor{H, S}},
	}
	for _, v := range valid {
		for _, a := range []Actor{H, S} {
			want := false
			for _, ok := range v.actors {
				want = want || ok == a
			}
			if got := ValidateTransition(v.from, v.to, a) == nil; got != want {
				t.Errorf("%s->%s by %s: allowed=%v want %v", v.from, v.to, a, got, want)
			}
		}
	}
	// cancel: human only, from any non-terminal status
	for _, from := range AllStatuses {
		terminal := from == StatusDone || from == StatusCancelled
		if got := ValidateTransition(from, StatusCancelled, H) == nil; got == terminal {
			t.Errorf("cancel from %s by human: allowed=%v", from, got)
		}
		if ValidateTransition(from, StatusCancelled, S) == nil {
			t.Errorf("cancel from %s by system must fail", from)
		}
	}
	for _, e := range [][2]Status{{StatusBacklog, StatusRunning}, {StatusTodo, StatusDone}, {StatusDone, StatusReview}, {StatusCancelled, StatusTodo}, {StatusRunning, StatusBacklog}} {
		if ValidateTransition(e[0], e[1], H) == nil || ValidateTransition(e[0], e[1], S) == nil {
			t.Errorf("%s->%s must be invalid", e[0], e[1])
		}
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	db := openTest(t)
	tk := mkTask(t, db, StatusBacklog)
	if _, err := UpdateTaskStatus(db, tk.ID, StatusRunning, ActorSystem); err == nil {
		t.Fatal("backlog->running must fail")
	}
	got, err := UpdateTaskStatus(db, tk.ID, StatusTodo, ActorHuman)
	if err != nil || got.Status != StatusTodo {
		t.Fatalf("got %v %v", got.Status, err)
	}
	evs, _ := ListEvents(db, tk.ID)
	if len(evs) != 1 || evs[0].Kind != "status_changed" {
		t.Fatalf("events = %+v", evs)
	}
}

func TestCreateTaskKeysAndPrimary(t *testing.T) {
	db := openTest(t)
	a, b := mkRepo(t, db, "a"), mkRepo(t, db, "b")
	t1, err := CreateTask(db, Task{Title: "one"}, []TaskRepo{{RepoID: a.ID, IsPrimary: true, Branch: "one"}, {RepoID: b.ID, Branch: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	t2, _ := CreateTask(db, Task{Title: "two"}, []TaskRepo{{RepoID: a.ID, IsPrimary: true, Branch: "two"}})
	if t1.Key != "CV-1" || t2.Key != "CV-2" {
		t.Fatalf("keys %s %s", t1.Key, t2.Key)
	}
	if len(t1.Repos) != 2 || !t1.Repos[0].IsPrimary || t1.Repos[0].Branch != "cv/CV-1-one" {
		t.Fatalf("repos %+v", t1.Repos)
	}
	if _, err := CreateTask(db, Task{Title: "x"}, []TaskRepo{{RepoID: a.ID, Branch: "x"}}); err == nil {
		t.Fatal("0 primaries must fail")
	}
	if _, err := CreateTask(db, Task{Title: "x"}, []TaskRepo{{RepoID: a.ID, IsPrimary: true}, {RepoID: b.ID, IsPrimary: true}}); err == nil {
		t.Fatal("2 primaries must fail")
	}
	if _, err := GetTaskByKey(db, "CV-99"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("want ErrNoRows, got %v", err)
	}
	list, _ := ListTasks(db, StatusBacklog)
	if len(list) != 2 || list[0].Key != "CV-1" {
		t.Fatalf("list %+v", list)
	}
}

func TestCommentsConsume(t *testing.T) {
	db := openTest(t)
	tk := mkTask(t, db, StatusBacklog)
	h, _ := AddComment(db, Comment{TaskID: tk.ID, Author: AuthorHuman, Body: "h"})
	AddComment(db, Comment{TaskID: tk.ID, Author: AuthorAgent, Body: "a"})
	s, _ := AddComment(db, Comment{TaskID: tk.ID, Author: AuthorSystem, Body: "s"})
	u, _ := UnconsumedComments(db, tk.ID)
	if len(u) != 2 {
		t.Fatalf("unconsumed %d", len(u))
	}
	run, err := CreateRun(db, Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "launching"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ConsumeComments(db, []int64{h.ID, s.ID}, run.ID); err != nil {
		t.Fatal(err)
	}
	if u, _ = UnconsumedComments(db, tk.ID); len(u) != 0 {
		t.Fatalf("still unconsumed %d", len(u))
	}
	if all, _ := ListComments(db, tk.ID); len(all) != 3 {
		t.Fatalf("all %d", len(all))
	}
}

func TestUnconsumeComments(t *testing.T) {
	db := openTest(t)
	tk := mkTask(t, db, StatusBacklog)
	c, _ := AddComment(db, Comment{TaskID: tk.ID, Author: AuthorHuman, Body: "h"})
	r1, _ := CreateRun(db, Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "launching"})
	r2, _ := CreateRun(db, Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "launching"})
	other, _ := AddComment(db, Comment{TaskID: tk.ID, Author: AuthorHuman, Body: "o"})
	ConsumeComments(db, []int64{c.ID}, r1.ID)
	ConsumeComments(db, []int64{other.ID}, r2.ID)
	if err := UnconsumeComments(db, r1.ID); err != nil {
		t.Fatal(err)
	}
	u, _ := UnconsumedComments(db, tk.ID)
	if len(u) != 1 || u[0].ID != c.ID {
		t.Fatalf("unconsumed %+v, want only comment %d", u, c.ID)
	}
}

func TestRuns(t *testing.T) {
	db := openTest(t)
	tk := mkTask(t, db, StatusTodo)
	sid := "uuid-1"
	r1, _ := CreateRun(db, Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "launching"})
	r2, _ := CreateRun(db, Run{TaskID: tk.ID, Kind: "auto", Mode: "resume", Status: "running", SessionID: &sid})
	if l, _ := LatestRun(db, tk.ID); l.ID != r2.ID {
		t.Fatal("latest")
	}
	if g, err := RunBySession(db, sid); err != nil || g.ID != r2.ID {
		t.Fatal("by session", err)
	}
	r1.Status = "finished"
	if err := UpdateRun(db, r1); err != nil {
		t.Fatal(err)
	}
	if a, _ := ActiveRuns(db); len(a) != 1 || a[0].ID != r2.ID {
		t.Fatalf("active %+v", a)
	}
	if _, err := RunBySession(db, "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}

func TestSettingsDefaults(t *testing.T) {
	db := openTest(t)
	if GetSetting(db, "key_prefix") != "CV" || GetSetting(db, "max_parallel") != "3" {
		t.Fatal("defaults")
	}
	SetSetting(db, "max_parallel", "5")
	if GetSetting(db, "max_parallel") != "5" || ListSettings(db)["max_parallel"] != "5" || ListSettings(db)["key_prefix"] != "CV" {
		t.Fatal("override")
	}
}

// Two processes creating tasks at once must not fail with SQLITE_BUSY when
// the deferred read->write upgrade loses the race.
func TestCreateTaskConcurrentHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.db")
	var dbs [2]*sql.DB
	for i := range dbs {
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		dbs[i] = db
	}
	r := mkRepo(t, dbs[0], "a")
	const per = 20
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		keys = map[string]bool{}
	)
	for _, db := range dbs {
		for i := 0; i < per; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tk, err := CreateTask(db, Task{Title: "t"}, []TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "t"}})
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				keys[tk.Key] = true
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	if len(keys) != 2*per {
		t.Fatalf("got %d unique keys, want %d", len(keys), 2*per)
	}
}
