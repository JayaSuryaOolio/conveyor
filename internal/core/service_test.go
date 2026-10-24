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
