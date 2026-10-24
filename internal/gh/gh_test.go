package gh

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// fakeGH installs a gh script returning $FAKE/<branch with / as _>.json, or [].
func fakeGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
while [ $# -gt 0 ]; do [ "$1" = --head ] && b=$2; shift; done
f="$FAKE/$(echo "$b" | tr / _).json"
if [ -f "$f" ]; then cat "$f"; else echo '[]'; fi
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("FAKE", dir)
	return dir
}

func setPR(t *testing.T, dir, branch, state string) {
	t.Helper()
	body := `[{"url":"https://x/pr/` + branch + `","state":"` + state + `","number":1,"reviewDecision":"","updatedAt":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(branch, "/", "_")+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTask(t *testing.T, nrepos int) (*sql.DB, store.Task) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var trs []store.TaskRepo
	for i := 0; i < nrepos; i++ {
		r, err := store.AddRepo(db, store.Repo{Name: string(rune('a' + i)), Path: t.TempDir(), DefaultBranch: "main"})
		if err != nil {
			t.Fatal(err)
		}
		trs = append(trs, store.TaskRepo{RepoID: r.ID, IsPrimary: i == 0, Branch: "s"})
	}
	tk, err := store.CreateTask(db, store.Task{Title: "t", Status: store.StatusReview}, trs)
	if err != nil {
		t.Fatal(err)
	}
	return db, tk
}

func sync(t *testing.T, db *sql.DB, tk store.Task) store.Task {
	t.Helper()
	if err := SyncTask(context.Background(), db, tk); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTask(db, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNoPR(t *testing.T) {
	fakeGH(t)
	db, tk := newTask(t, 1)
	pr, err := FindPR(context.Background(), t.TempDir(), tk.Repos[0].Branch)
	if pr != nil || err != nil {
		t.Fatalf("got %v %v", pr, err)
	}
	if got := sync(t, db, tk); got.Status != store.StatusReview || got.Repos[0].PRURL != nil {
		t.Fatalf("%+v", got)
	}
}

func TestOpenNothingChanges(t *testing.T) {
	dir := fakeGH(t)
	db, tk := newTask(t, 1)
	setPR(t, dir, tk.Repos[0].Branch, "OPEN")
	got := sync(t, db, tk)
	if got.Status != store.StatusReview || got.Repos[0].PRState == nil || *got.Repos[0].PRState != "OPEN" {
		t.Fatalf("%+v", got)
	}
}

func TestMergedDone(t *testing.T) {
	dir := fakeGH(t)
	db, tk := newTask(t, 1)
	setPR(t, dir, tk.Repos[0].Branch, "MERGED")
	if got := sync(t, db, tk); got.Status != store.StatusDone {
		t.Fatalf("status %s", got.Status)
	}
	evs, _ := store.ListEvents(db, tk.ID)
	found := false
	for _, e := range evs {
		found = found || e.Kind == "pr_merged"
	}
	if !found {
		t.Fatal("no pr_merged event")
	}
}

func TestMergedAndOpenStaysReview(t *testing.T) {
	dir := fakeGH(t)
	db, tk := newTask(t, 2)
	setPR(t, dir, tk.Repos[0].Branch, "MERGED")
	setPR(t, dir, tk.Repos[1].Branch, "OPEN")
	if got := sync(t, db, tk); got.Status != store.StatusReview {
		t.Fatalf("status %s", got.Status)
	}
}

func TestAllClosedNeedsYou(t *testing.T) {
	dir := fakeGH(t)
	db, tk := newTask(t, 2)
	setPR(t, dir, tk.Repos[0].Branch, "CLOSED")
	setPR(t, dir, tk.Repos[1].Branch, "CLOSED")
	if got := sync(t, db, tk); got.Status != store.StatusNeedsYou {
		t.Fatalf("status %s", got.Status)
	}
	cs, _ := store.ListComments(db, tk.ID)
	if len(cs) != 1 || cs[0].Author != store.AuthorSystem || cs[0].Body != "PR closed without merge" {
		t.Fatalf("%+v", cs)
	}
}

func TestMissingGH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := FindPR(context.Background(), t.TempDir(), "b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}
