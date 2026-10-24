package core

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, n := range []string{"a", "b"} {
		store.AddRepo(db, store.Repo{Name: n, Path: "/x/" + n, DefaultBranch: "main"})
	}
	return &Service{DB: db}
}

func TestCreateTask(t *testing.T) {
	s := newSvc(t)
	tk, err := s.CreateTask(NewTaskInput{Title: "Fix the Login Bug!", Repos: []string{"a", "b"}, Primary: "b", Start: true})
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != store.StatusTodo || tk.Repos[0].RepoName != "b" || tk.Repos[0].Branch != "cv/CV-1-fix-the-login-bug" {
		t.Fatalf("%+v", tk)
	}
	for _, in := range []NewTaskInput{
		{Title: "x"},
		{Title: "x", Repos: []string{"nope"}},
		{Title: "x", Repos: []string{"a"}, Primary: "b"},
	} {
		if _, err := s.CreateTask(in); err == nil {
			t.Errorf("%+v should fail", in)
		}
	}
	long, _ := s.CreateTask(NewTaskInput{Title: strings.Repeat("word ", 20), Repos: []string{"a"}})
	if got := strings.TrimPrefix(long.Repos[0].Branch, "cv/CV-2-"); len(got) > 40 || strings.HasSuffix(got, "-") {
		t.Fatalf("slug %q", got)
	}
}

func TestAddCommentTransitions(t *testing.T) {
	s := newSvc(t)
	cases := []struct {
		from, want store.Status
		author     string
	}{
		{store.StatusNeedsYou, store.StatusTodo, store.AuthorHuman},
		{store.StatusReview, store.StatusTodo, store.AuthorHuman},
		{store.StatusFailed, store.StatusTodo, store.AuthorSystem},
		{store.StatusDone, store.StatusTodo, store.AuthorSystem},
		{store.StatusBacklog, store.StatusBacklog, store.AuthorHuman},
		{store.StatusTodo, store.StatusTodo, store.AuthorHuman},
		{store.StatusRunning, store.StatusRunning, store.AuthorHuman},
		{store.StatusReview, store.StatusReview, store.AuthorAgent},
	}
	for _, c := range cases {
		tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
		// force the status directly; the table is tested in store
		s.DB.Exec(`UPDATE tasks SET status=? WHERE id=?`, c.from, tk.ID)
		if _, err := s.AddComment(tk.Key, c.author, "hi"); err != nil {
			t.Fatal(err)
		}
		got, _ := store.GetTask(s.DB, tk.ID)
		if got.Status != c.want {
			t.Errorf("%s + %s comment: %s want %s", c.from, c.author, got.Status, c.want)
		}
	}
}

func TestMoveCancelDone(t *testing.T) {
	s := newSvc(t)
	tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
	if err := s.MarkDone(tk.Key); err == nil {
		t.Fatal("backlog->done must fail")
	}
	if err := s.Move(tk.Key, store.StatusTodo); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(tk.Key); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(tk.Key); err == nil {
		t.Fatal("cancel twice must fail")
	}
}

func TestSetTaskRepo(t *testing.T) {
	s := newSvc(t)
	tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a", "b"}})
	get := func() store.TaskRepo { g, _ := store.GetTask(s.DB, tk.ID); return g.Repos[0] }
	if err := s.SetTaskRepo(tk.Key, "a", "feat/x", "https://gh/pr/1"); err != nil {
		t.Fatal(err)
	}
	if r := get(); r.Branch != "feat/x" || deref(r.PRURL) != "https://gh/pr/1" {
		t.Fatalf("%+v", r)
	}
	// an unchanged URL keeps its state; a new one drops it
	st := "OPEN"
	r := get()
	r.PRState = &st
	store.UpdateTaskRepo(s.DB, r)
	s.SetTaskRepo(tk.Key, "a", "feat/x", "https://gh/pr/1")
	if deref(get().PRState) != "OPEN" {
		t.Fatal("state lost for same url")
	}
	s.SetTaskRepo(tk.Key, "a", "feat/x", "https://gh/pr/2")
	if get().PRState != nil {
		t.Fatal("stale state kept")
	}
	if err := s.SetTaskRepo(tk.Key, "a", "feat/x", ""); err != nil || get().PRURL != nil {
		t.Fatalf("clear: %v %+v", err, get())
	}
	for name, args := range map[string][3]string{
		"empty branch": {"a", " ", ""},
		"unknown repo": {"zzz", "x", ""},
	} {
		if err := s.SetTaskRepo(tk.Key, args[0], args[1], args[2]); err == nil {
			t.Errorf("%s should fail", name)
		}
	}
	if err := s.SetTaskRepo("CV-99", "a", "x", ""); err == nil {
		t.Error("unknown task should fail")
	}
	// once the worktree exists the branch is frozen, the PR is not
	wt := "/tmp/wt"
	r = get()
	r.WorktreePath = &wt
	store.UpdateTaskRepo(s.DB, r)
	if err := s.SetTaskRepo(tk.Key, "a", "other", ""); err == nil {
		t.Fatal("branch change with worktree should fail")
	}
	if err := s.SetTaskRepo(tk.Key, "a", "feat/x", "https://gh/pr/3"); err != nil || deref(get().PRURL) != "https://gh/pr/3" {
		t.Fatalf("pr with worktree: %v", err)
	}
}

func TestHumanRequeueResetsFailures(t *testing.T) {
	s := newSvc(t)
	for name, requeue := range map[string]func(key string) error{
		"move":    func(k string) error { return s.Move(k, store.StatusTodo) },
		"comment": func(k string) error { _, err := s.AddComment(k, store.AuthorHuman, "retry"); return err },
	} {
		tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
		s.DB.Exec(`UPDATE tasks SET status='failed', consecutive_failures=3 WHERE id=?`, tk.ID)
		if err := requeue(tk.Key); err != nil {
			t.Fatal(err)
		}
		got, _ := store.GetTask(s.DB, tk.ID)
		if got.Status != store.StatusTodo || got.ConsecutiveFailures != 0 {
			t.Errorf("%s: %s/%d, want todo/0", name, got.Status, got.ConsecutiveFailures)
		}
	}
}

func TestSubtaskDefaultsToParentRepos(t *testing.T) {
	s := newSvc(t)
	if _, err := s.CreateTask(NewTaskInput{Title: "p", Repos: []string{"a", "b"}, Primary: "b"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateTask(NewTaskInput{Title: "c", ParentKey: "CV-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Repos) != 2 || c.Repos[0].RepoName != "b" || !c.Repos[0].IsPrimary {
		t.Fatalf("%+v", c.Repos)
	}
	e, err := s.CreateTask(NewTaskInput{Title: "e", ParentKey: "CV-1", Repos: []string{"a"}})
	if err != nil || len(e.Repos) != 1 || e.Repos[0].RepoName != "a" {
		t.Fatalf("%+v %v", e.Repos, err)
	}
}

func TestSetTaskDescriptionBacklogOnly(t *testing.T) {
	s := newSvc(t)
	if _, err := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskDescription("CV-1", "new"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.GetTaskByKey(s.DB, "CV-1"); got.Description != "new" {
		t.Fatalf("%q", got.Description)
	}
	if _, err := s.CreateTask(NewTaskInput{Title: "u", Repos: []string{"a"}, Start: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskDescription("CV-2", "x"); err == nil {
		t.Fatal("edited a todo task")
	}
}
