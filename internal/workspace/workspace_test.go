package workspace

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=t"}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

// mkRepo makes a repo with a bare origin (so origin/main exists).
func mkRepo(t *testing.T, db *sql.DB, root, name string) store.Repo {
	t.Helper()
	p, o := filepath.Join(root, name), filepath.Join(root, name+"-origin.git")
	os.MkdirAll(p, 0o755)
	run(t, p, "init", "-b", "main")
	run(t, p, "commit", "--allow-empty", "-m", "init")
	run(t, root, "init", "--bare", o)
	run(t, p, "remote", "add", "origin", o)
	run(t, p, "push", "origin", "main")
	r, err := store.AddRepo(db, store.Repo{Name: name, Path: p, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func setup(t *testing.T) (*sql.DB, string, store.Task) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a, b := mkRepo(t, db, root, "a"), mkRepo(t, db, root, "b")
	tk, err := store.CreateTask(db, store.Task{Title: "t", Status: store.StatusBacklog},
		[]store.TaskRepo{{RepoID: a.ID, IsPrimary: true, Branch: "x"}, {RepoID: b.ID, Branch: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	return db, filepath.Join(root, "home"), tk
}

func TestEnsureIdempotent(t *testing.T) {
	db, home, tk := setup(t)
	got, err := Ensure(db, home, tk)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got.Repos {
		if r.WorktreePath == nil || *r.WorktreePath != filepath.Join(Dir(home, tk.Key), r.RepoName) {
			t.Fatalf("bad path %v", r.WorktreePath)
		}
		br, _ := git(*r.WorktreePath, "rev-parse", "--abbrev-ref", "HEAD")
		if br != r.Branch {
			t.Fatalf("branch %q want %q", br, r.Branch)
		}
	}
	if PrimaryPath(got) == "" || len(OtherPaths(got)) != 1 {
		t.Fatal("primary/other paths wrong")
	}
	again, err := Ensure(db, home, got)
	if err != nil || PrimaryPath(again) != PrimaryPath(got) {
		t.Fatalf("second call: %v", err)
	}
}

func TestEnsureAdopts(t *testing.T) {
	db, home, tk := setup(t)
	if _, err := Ensure(db, home, tk); err != nil {
		t.Fatal(err)
	}
	fresh, _ := store.GetTask(db, tk.ID) // persisted
	for _, r := range fresh.Repos {
		if r.WorktreePath == nil {
			t.Fatal("not persisted")
		}
		r.WorktreePath = nil // simulate lost DB state, path still on disk
		store.UpdateTaskRepo(db, r)
	}
	fresh, _ = store.GetTask(db, tk.ID)
	got, err := Ensure(db, home, fresh)
	if err != nil || PrimaryPath(got) == "" {
		t.Fatalf("adopt: %v", err)
	}
}

func TestEnsureExistingBranch(t *testing.T) {
	db, home, tk := setup(t)
	a, _ := store.GetRepoByName(db, "a")
	run(t, a.Path, "branch", tk.Repos[0].Branch)
	if _, err := Ensure(db, home, tk); err != nil {
		t.Fatal(err)
	}
}

func TestKeyFromCwd(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(real, link)
	nested := filepath.Join(real, "workspaces", "CV-1", "a", "src")
	os.MkdirAll(nested, 0o755)
	for _, home := range []string{real, link} {
		if k := KeyFromCwd(home, nested); k != "CV-1" {
			t.Errorf("home %s: got %q", home, k)
		}
		if k := KeyFromCwd(home, filepath.Join(real, "workspaces", "CV-1")); k != "CV-1" {
			t.Errorf("ws root: got %q", k)
		}
		if k := KeyFromCwd(home, os.TempDir()); k != "" {
			t.Errorf("outside: got %q", k)
		}
		if k := KeyFromCwd(home, filepath.Join(real, "workspaces")); k != "" {
			t.Errorf("workspaces root: got %q", k)
		}
	}
}

func TestRemoveDirty(t *testing.T) {
	db, home, tk := setup(t)
	got, _ := Ensure(db, home, tk)
	os.WriteFile(filepath.Join(PrimaryPath(got), "f"), []byte("x"), 0o644)
	if err := Remove(db, home, got, false); !errors.Is(err, ErrDirty) {
		t.Fatalf("want ErrDirty, got %v", err)
	}
	if err := Remove(db, home, got, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Dir(home, tk.Key)); !os.IsNotExist(err) {
		t.Fatal("workspace dir remains")
	}
}
