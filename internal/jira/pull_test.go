package jira

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// fakeAcli puts an acli on PATH that logs each jql to <dir>/argv and answers with <dir>/<parent key>.json.
// A file named "fail" makes it exit 1 like an unauthenticated acli; keys listed in <dir>/mine are assigned to the user.
func fakeAcli(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$5" >> ` + dir + `/argv
[ -f ` + dir + `/fail ] && { echo "not logged in" >&2; exit 1; }
case "$5" in "key = "*"assignee = currentUser()")
k=${5#key = }; k=${k%% *}
grep -qx "$k" ` + dir + `/mine 2>/dev/null && echo "[{\"key\":\"$k\"}]" || echo '[]'; exit 0;;
esac
k=${5#parent = }; k=${k%% *}
cat ` + dir + `/$k.json 2>/dev/null || echo '[]'
`
	os.WriteFile(filepath.Join(dir, "acli"), []byte(script), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func respond(t *testing.T, dir, parent, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, parent+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pullEnv returns a service with repos a and b and an epic "e" (in repo a) sourced from OS-1.
func pullEnv(t *testing.T, defaultRepo string) (*core.Service, store.Task) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	store.AddRepo(db, store.Repo{Name: "b", Path: "/x/b", DefaultBranch: "main"})
	svc := &core.Service{DB: db, Home: t.TempDir()}
	epic, err := svc.CreateTask(core.NewTaskInput{Title: "Epic", Repos: []string{"a"}, Epic: true, EpicName: "e"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetJiraSource(db, store.JiraSource{EpicID: epic.ID, JiraKey: "OS-1", DefaultRepo: defaultRepo}); err != nil {
		t.Fatal(err)
	}
	return svc, epic
}

func byExt(t *testing.T, svc *core.Service, key string) store.Task {
	t.Helper()
	tk, err := store.TaskByExternalKey(svc.DB, key)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return tk
}

func hasLabel(tk store.Task, l string) bool {
	for _, x := range tk.Labels {
		if x == l {
			return true
		}
	}
	return false
}

func TestPull(t *testing.T) {
	dir := fakeAcli(t)
	svc, epic := pullEnv(t, "a")
	respond(t, dir, "OS-1", `[
{"key":"OS-10","fields":{"summary":"Ten","labels":["repo:b"]}},
{"key":"OS-11","fields":{"summary":"Eleven","labels":[]}},
{"key":"OS-12","fields":{"summary":"Twelve","labels":["repo:nope"]}}]`)
	respond(t, dir, "OS-10", `[{"key":"OS-100","fields":{"summary":"Sub","labels":[]}}]`)

	keys, err := Pull(context.Background(), svc, epic.Key)
	if err != nil || len(keys) != 4 {
		t.Fatalf("%v %v", keys, err)
	}
	ten, sub := byExt(t, svc, "OS-10"), byExt(t, svc, "OS-100")
	if ten.Status != store.StatusBacklog || ten.Source != "jira" || ten.Repos[0].RepoName != "b" ||
		ten.ParentID == nil || *ten.ParentID != epic.ID || !hasLabel(ten, "epic:e") || hasLabel(ten, LabelNeedRepo) {
		t.Fatalf("OS-10: %+v", ten)
	}
	if sub.ParentID == nil || *sub.ParentID != ten.ID || sub.Repos[0].RepoName != "a" || !hasLabel(sub, "epic:e") || hasLabel(sub, LabelNeedRepo) {
		t.Fatalf("OS-100: %+v", sub)
	}
	for _, k := range []string{"OS-11", "OS-12"} { // no label / unknown repo -> default repo
		if tk := byExt(t, svc, k); tk.Repos[0].RepoName != "a" || hasLabel(tk, LabelNeedRepo) {
			t.Fatalf("%s: %+v", k, tk)
		}
	}
	src, _ := store.GetJiraSource(svc.DB, epic.ID)
	if src.LastSync == nil || src.LastError != "" {
		t.Fatalf("%+v", src)
	}

	// Re-pull: nothing new; a not-started task takes the Jira edit, a running one does not.
	respond(t, dir, "OS-1", `[
{"key":"OS-10","fields":{"summary":"Ten renamed","description":"new body","labels":["repo:b"]}},
{"key":"OS-11","fields":{"summary":"Eleven renamed","labels":[]}}]`)
	svc.DB.Exec(`UPDATE tasks SET status='running' WHERE external_key='OS-10'`)
	if keys, err = Pull(context.Background(), svc, epic.Key); err != nil || len(keys) != 0 {
		t.Fatalf("re-pull: %v %v", keys, err)
	}
	if tk := byExt(t, svc, "OS-11"); tk.Title != "Eleven renamed" {
		t.Fatalf("not updated: %+v", tk)
	}
	if tk := byExt(t, svc, "OS-10"); tk.Title != "Ten" {
		t.Fatalf("running task overwritten: %+v", tk)
	}
	if ts, _ := store.ListTasks(svc.DB); len(ts) != 5 { // epic + 4
		t.Fatalf("%d tasks", len(ts))
	}
	argv, _ := os.ReadFile(filepath.Join(dir, "argv"))
	lines := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if lines[0] != "parent = OS-1" || !strings.HasPrefix(lines[len(lines)-1], "parent = OS-1") || !strings.Contains(string(argv), "AND updated >= -") {
		t.Fatalf("jql: %q", argv)
	}
}

func TestPullFlagsTaskWithoutRepo(t *testing.T) {
	dir := fakeAcli(t)
	svc, epic := pullEnv(t, "")
	respond(t, dir, "OS-1", `[{"key":"OS-10","fields":{"summary":"Ten","labels":[]}}]`)
	if _, err := Pull(context.Background(), svc, epic.Key); err != nil {
		t.Fatal(err)
	}
	if tk := byExt(t, svc, "OS-10"); !hasLabel(tk, LabelNeedRepo) {
		t.Fatalf("not flagged: %+v", tk)
	}
}

func TestPullErrors(t *testing.T) {
	dir := fakeAcli(t)
	svc, epic := pullEnv(t, "a")
	os.WriteFile(filepath.Join(dir, "fail"), nil, 0o644)
	if _, err := Pull(context.Background(), svc, epic.Key); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("unauthenticated: %v", err)
	}
	src, _ := store.GetJiraSource(svc.DB, epic.ID)
	if src.LastSync != nil || !strings.Contains(src.LastError, "not logged in") {
		t.Fatalf("%+v", src)
	}

	t.Setenv("PATH", t.TempDir()) // no acli at all
	if _, err := Pull(context.Background(), svc, epic.Key); err == nil || !strings.Contains(err.Error(), "acli not found") {
		t.Fatalf("missing: %v", err)
	}
	if src, _ = store.GetJiraSource(svc.DB, epic.ID); !strings.Contains(src.LastError, "acli not found") {
		t.Fatalf("%+v", src)
	}
}
