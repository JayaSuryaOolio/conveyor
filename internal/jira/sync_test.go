package jira

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func argvLines(dir string) int {
	b, _ := os.ReadFile(dir + "/argv")
	return strings.Count(string(b), "\n")
}

// AC4: the poller syncs due sources only, skips done ones, and backs off a failing source.
func TestPollerTick(t *testing.T) {
	dir := fakeAcli(t)
	svc, epic := pullEnv(t, "a")
	respond(t, dir, "OS-1", `[{"key":"OS-2","fields":{"summary":"one"}}]`)
	p := &Poller{}
	now := time.Now()

	p.Tick(context.Background(), svc, now) // never synced: due
	if _, err := store.TaskByExternalKey(svc.DB, "OS-2"); err != nil {
		t.Fatalf("not pulled: %v", err)
	}
	n := argvLines(dir)
	p.Tick(context.Background(), svc, now.Add(time.Minute)) // inside the 300s interval
	if argvLines(dir) != n {
		t.Fatal("synced before the interval elapsed")
	}
	p.Tick(context.Background(), svc, now.Add(6*time.Minute))
	if argvLines(dir) == n {
		t.Fatal("due source not synced")
	}

	n = argvLines(dir)
	svc.DB.Exec(`UPDATE jira_sources SET done=1`)
	p.Tick(context.Background(), svc, now.Add(time.Hour))
	if argvLines(dir) != n {
		t.Fatal("done source synced")
	}

	// A failing source records last_error and is retried per interval, not per tick.
	svc.DB.Exec(`UPDATE jira_sources SET done=0, last_sync=NULL`)
	os.WriteFile(dir+"/fail", nil, 0o644)
	p = &Poller{}
	p.Tick(context.Background(), svc, now)
	n = argvLines(dir)
	p.Tick(context.Background(), svc, now.Add(30*time.Second))
	if argvLines(dir) != n {
		t.Fatal("failing source retried every tick")
	}
	if src, _ := store.GetJiraSource(svc.DB, epic.ID); src.LastError == "" {
		t.Fatal("last_error not recorded")
	}
}

// AC1: sources attach and detach by epic key.
func TestSourceByKey(t *testing.T) {
	dir := fakeAcli(t)
	os.WriteFile(dir+"/mine", []byte("OS-9\n"), 0o644)
	svc, epic := pullEnv(t, "a")
	if err := ClearSource(svc, epic.Key); err != nil {
		t.Fatal(err)
	}
	if s, err := GetSource(svc, epic.Key); err != nil || s != nil {
		t.Fatalf("want nil source, got %v %v", s, err)
	}
	if err := SetSource(svc, epic.Key, store.JiraSource{JiraKey: "OS-9", DefaultRepo: "b", IntervalSec: 60}); err != nil {
		t.Fatal(err)
	}
	if s, _ := GetSource(svc, epic.Key); s == nil || s.JiraKey != "OS-9" || s.IntervalSec != 60 {
		t.Fatalf("got %+v", s)
	}
	if err := SetSource(svc, epic.Key, store.JiraSource{JiraKey: "bad"}); err == nil {
		t.Fatal("invalid key accepted")
	}
	if err := SetSource(svc, epic.Key, store.JiraSource{JiraKey: "OS-9", StatusMap: "nope"}); err == nil {
		t.Fatal("invalid status map accepted")
	}
	if err := SetSource(svc, "NOPE-1", store.JiraSource{JiraKey: "OS-9"}); err == nil {
		t.Fatal("unknown epic accepted")
	}
}

// AC10: only an epic assigned to the acli user attaches; others and acli failures store nothing.
func TestSourceAssignedOnly(t *testing.T) {
	dir := fakeAcli(t)
	os.WriteFile(dir+"/mine", []byte("OS-9\n"), 0o644)
	svc, epic := pullEnv(t, "a")
	ClearSource(svc, epic.Key)

	err := SetSource(svc, epic.Key, store.JiraSource{JiraKey: "OS-8"}) // only child tasks assigned: not in the search
	if err == nil || !strings.Contains(err.Error(), "not assigned") {
		t.Fatalf("unassigned epic accepted: %v", err)
	}
	if s, _ := GetSource(svc, epic.Key); s != nil {
		t.Fatalf("source stored: %+v", s)
	}
	if b, _ := os.ReadFile(dir + "/argv"); !strings.Contains(string(b), "key = OS-8 AND assignee = currentUser()") {
		t.Fatalf("jql: %s", b)
	}

	os.WriteFile(dir+"/fail", nil, 0o644)
	if err := SetSource(svc, epic.Key, store.JiraSource{JiraKey: "OS-9"}); err == nil || !strings.Contains(err.Error(), "acli failed") {
		t.Fatalf("acli failure: %v", err)
	}
	if s, _ := GetSource(svc, epic.Key); s != nil {
		t.Fatalf("source stored: %+v", s)
	}
	os.Remove(dir + "/fail")
	if err := SetSource(svc, epic.Key, store.JiraSource{JiraKey: "OS-9"}); err != nil {
		t.Fatal(err)
	}
}
