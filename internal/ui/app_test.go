package ui

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
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
	if err := a.AddComment(key, "hi", ""); err != nil {
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

func TestAutomationCRUDAndDescribeSchedule(t *testing.T) {
	a, _ := newApp(t)
	x := store.Automation{Name: "n", Schedule: "0 9 * * 1-5", ActionKind: "cli", ActionArg: `{"args":["list"]}`}
	if err := a.AddAutomation(x); err != nil {
		t.Fatal(err)
	}
	if a.AddAutomation(x) == nil {
		t.Fatal("duplicate name accepted")
	}
	if a.AddAutomation(store.Automation{Name: "bad", Schedule: "nope", ActionKind: "cli", ActionArg: "{}"}) == nil {
		t.Fatal("bad schedule accepted")
	}
	got, _ := store.GetAutomation(a.svc.DB, "n")
	res := "ok"
	got.LastResult = &res
	store.UpdateAutomation(a.svc.DB, got)
	x.Schedule, x.Precheck = "*/5 * * * *", "true"
	if err := a.UpdateAutomation(x); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetAutomation(a.svc.DB, "n")
	if got.Schedule != "*/5 * * * *" || got.Precheck != "true" || got.LastResult == nil || !got.Enabled {
		t.Fatalf("update: %+v", got)
	}
	x.Name = "missing"
	if a.UpdateAutomation(x) == nil {
		t.Fatal("update of missing automation succeeded")
	}
	if err := a.DeleteAutomation("n"); err != nil {
		t.Fatal(err)
	}
	if a.DeleteAutomation("n") == nil {
		t.Fatal("double delete succeeded")
	}
	if i := a.DescribeSchedule("0 9 * * 1-5"); !i.Valid || len(i.Next) != 3 || i.Description != "Every weekday at 09:00" {
		t.Fatalf("describe: %+v", i)
	}
	if i := a.DescribeSchedule("x"); i.Valid || i.Error == "" {
		t.Fatalf("describe invalid: %+v", i)
	}
}

func TestSetLabelSwapsPrefixOnly(t *testing.T) {
	a, _ := newApp(t)
	key, _ := a.CreateTask(core.NewTaskInput{Title: "k", Repos: []string{"a"}})
	for _, l := range []string{"epic:os", "epic_kind:build", "plain"} {
		a.AddLabel(key, l)
	}
	if err := a.SetLabel(key, "epic_kind", "research"); err != nil {
		t.Fatal(err)
	}
	tk, _ := store.GetTaskByKey(a.svc.DB, key)
	ls, _ := store.TaskLabels(a.svc.DB, tk.ID)
	if got := strings.Join(ls, ","); !strings.Contains(got, "epic_kind:research") || strings.Contains(got, "epic_kind:build") || !strings.Contains(got, "epic:os") || !strings.Contains(got, "plain") {
		t.Fatalf("labels: %v", ls)
	}
	if a.SetLabel(key, "epic_kind", "") == nil {
		t.Fatal("empty value accepted")
	}
}

func TestCreator(t *testing.T) {
	s := func(v string) *string { return &v }
	p := &store.TaskRef{Key: "CV-1A"}
	for _, c := range []struct {
		t      store.Task
		parent *store.TaskRef
		user   string
		want   string
	}{
		{store.Task{CreatedBy: s("human")}, nil, "", "human"},
		{store.Task{CreatedBy: s("human")}, nil, "Ann", "Ann"},
		{store.Task{CreatedBy: s("agent:CV-1A")}, p, "", "agent · CV-1A"},
		{store.Task{CreatedBy: s("automation:nightly:x")}, nil, "", "automation · nightly:x"},
		{store.Task{CreatedBy: s("jira")}, nil, "", "jira"},
		{store.Task{Key: "XY-1A1", Source: "manual"}, p, "", "agent · CV-1A"}, // legacy row: nested child, real parent
		{store.Task{Key: "XY-1A1", Source: "followup"}, p, "", "follow-up of CV-1A"},
		{store.Task{Key: "XY-1", Source: "jira"}, nil, "", "jira"},
		{store.Task{Key: "XY-1", Source: "manual"}, nil, "Ann", "Ann"},
	} {
		if got := creator(c.t, c.parent, c.user); got != c.want {
			t.Errorf("creator(%+v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestQueueStatusHoldsWhenSlotsFull(t *testing.T) {
	a, _ := newApp(t)
	store.SetSetting(a.svc.DB, "max_parallel", "1")
	run, _ := a.CreateTask(core.NewTaskInput{Title: "Runs", Repos: []string{"a"}, Start: true})
	wait, _ := a.CreateTask(core.NewTaskInput{Title: "Waits", Repos: []string{"a"}, Start: true})
	tk, _ := store.GetTaskByKey(a.svc.DB, run)
	store.CreateRun(a.svc.DB, store.Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "running"})
	q, err := a.QueueStatus()
	if err != nil || q.DebounceSecs != 20 {
		t.Fatalf("%+v %v", q, err)
	}
	for _, i := range q.Items {
		if i.Key == run && !i.Running || i.Key == wait && i.Held == "" {
			t.Fatalf("item %+v", i)
		}
	}
}

// AC6: the ring counts from the newest unconsumed human/system comment, and a heartbeat older than 3 ticks is stale.
func TestQueueStatusPendingAndStale(t *testing.T) {
	a, _ := newApp(t)
	key, _ := a.CreateTask(core.NewTaskInput{Title: "Queued", Repos: []string{"a"}, Start: true})
	tk, _ := store.GetTaskByKey(a.svc.DB, key)
	for i, c := range []struct{ author, at string }{
		{store.AuthorHuman, "2026-01-01T00:00:00Z"},
		{store.AuthorSystem, "2026-01-01T00:00:10Z"}, // newest pending: the debounce runs from here
		{"agent", "2026-01-01T00:00:20Z"},            // agents never queue a launch
	} {
		c2, err := store.AddComment(a.svc.DB, store.Comment{TaskID: tk.ID, Author: c.author, Body: strconv.Itoa(i)})
		if err != nil {
			t.Fatal(err)
		}
		a.svc.DB.Exec(`UPDATE comments SET created_at=? WHERE id=?`, c.at, c2.ID)
	}
	pending := func() string {
		q, err := a.QueueStatus()
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range q.Items {
			if i.Key == key {
				return i.Pending
			}
		}
		t.Fatalf("%s not queued: %+v", key, q)
		return ""
	}
	if got := pending(); got != "2026-01-01T00:00:10Z" {
		t.Fatalf("pending = %q, want the newest human/system comment", got)
	}

	for _, c := range []struct {
		ago   time.Duration
		stale bool
	}{{2 * time.Second, false}, {16 * time.Second, true}} { // tick 5s: stale past 15s
		store.RecordTick(a.svc.DB, time.Now().Add(-c.ago), 5)
		q, _ := a.QueueStatus()
		if q.Stale != c.stale || q.TickSecs != 5 || q.LastTickAt == "" {
			t.Fatalf("heartbeat %v ago: %+v, want stale=%v", c.ago, q, c.stale)
		}
	}
}

func TestConsumeIntents(t *testing.T) {
	a, _ := newApp(t)
	now := time.Now()
	add := func(verb string, age time.Duration) int64 {
		id, err := store.AddIntent(a.svc.DB, verb, []byte(`{"name":"logs"}`), now.Add(-age))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	stale, fresh := add("go", 11*time.Second), add("palette", time.Second)
	var got []int64
	consumeIntents(a.svc.DB, now, func(i store.Intent) { got = append(got, i.ID) })
	if len(got) != 1 || got[0] != fresh {
		t.Fatalf("emitted %v, want only %d", got, fresh)
	}
	if ok, _ := store.IntentConsumed(a.svc.DB, fresh); !ok {
		t.Fatal("fresh row not marked consumed")
	}
	if _, err := store.IntentConsumed(a.svc.DB, stale); err == nil {
		t.Fatal("stale row should be deleted")
	}
	got = nil
	consumeIntents(a.svc.DB, now, func(i store.Intent) { got = append(got, i.ID) })
	if len(got) != 0 {
		t.Fatalf("re-emitted %v", got)
	}
}

func TestSetViewWritesState(t *testing.T) {
	a, _ := newApp(t)
	a.svc.Home = t.TempDir()
	if err := a.SetView(core.UIView{Name: "epic", Key: "CV-1", Task: "CV-1B"}); err != nil {
		t.Fatal(err)
	}
	s, err := core.ReadUIState(a.svc.Home)
	if k, _ := s.CurrentTask(); err != nil || k != "CV-1B" {
		t.Fatalf("state %+v err %v", s, err)
	}
}
