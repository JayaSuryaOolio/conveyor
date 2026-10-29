package core

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/brief"
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
		if _, err := s.AddComment(tk.Key, c.author, "hi", FollowUp); err != nil {
			t.Fatal(err)
		}
		got, _ := store.GetTask(s.DB, tk.ID)
		if got.Status != c.want {
			t.Errorf("%s + %s comment: %s want %s", c.from, c.author, got.Status, c.want)
		}
	}
}

func TestCommentAfterMergeStartsFollowUp(t *testing.T) {
	s := newSvc(t)
	tk, _ := s.CreateTask(NewTaskInput{Title: "readme", Description: "rewrite it", Repos: []string{"a"}})
	s.DB.Exec(`UPDATE tasks SET status=? WHERE id=?`, store.StatusReview, tk.ID)
	url, state := "https://github.com/o/r/pull/90", "MERGED"
	tr := tk.Repos[0]
	tr.PRURL, tr.PRState = &url, &state
	store.UpdateTaskRepo(s.DB, tr)
	if _, err := s.AddComment(tk.Key, store.AuthorHuman, "tone it down\nmore detail", FollowUp); err != nil {
		t.Fatal(err)
	}
	parent, _ := store.GetTask(s.DB, tk.ID)
	child, err := store.GetTaskByKey(s.DB, "CV-1A")
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != store.StatusDone || child.Status != store.StatusTodo || child.Title != "tone it down" ||
		child.Description != "tone it down\nmore detail" || *child.ParentID != tk.ID || child.Repos[0].Branch != "cv/CV-1A-tone-it-down" {
		t.Fatalf("parent %s, child %+v", parent.Status, child)
	}
	cs, _ := store.ListComments(s.DB, tk.ID)
	if last := cs[len(cs)-1]; last.Author != store.AuthorSystem || !strings.Contains(last.Body, "continues in CV-1A") {
		t.Fatalf("parent note %+v", last)
	}
	text, err := brief.Build(s.DB, child)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## Continues CV-1: readme") || !strings.Contains(text, "### Its description\n\nrewrite it") {
		t.Fatalf("brief lacks the parent's context:\n%s", text)
	}
}

func TestCommentAfterMergeAskOrNote(t *testing.T) {
	s := newSvc(t)
	for _, c := range []struct {
		after AfterMerge
		want  store.Status
		notes int
	}{{Ask, store.StatusTodo, 1}, {Note, store.StatusDone, 0}} {
		tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
		s.DB.Exec(`UPDATE tasks SET status=? WHERE id=?`, store.StatusDone, tk.ID)
		url, state := "https://github.com/o/r/pull/1", "MERGED"
		tr := tk.Repos[0]
		tr.PRURL, tr.PRState = &url, &state
		store.UpdateTaskRepo(s.DB, tr)
		if pr, _ := s.MergedPR(tk.Key); pr != url {
			t.Fatalf("MergedPR %q", pr)
		}
		s.AddComment(tk.Key, store.AuthorHuman, "why?", c.after)
		got, _ := store.GetTask(s.DB, tk.ID)
		rel, _ := store.ListRelations(s.DB, got)
		cs, _ := store.ListComments(s.DB, tk.ID)
		if got.Status != c.want || len(rel.Children) != 0 || len(cs) != 1+c.notes {
			t.Errorf("%q: status %s, %d children, %d comments", c.after, got.Status, len(rel.Children), len(cs))
		}
	}
}

func TestMoveCancelDone(t *testing.T) {
	s := newSvc(t)
	tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
	if err := s.MarkDone(tk.Key, false); err == nil {
		t.Fatal("backlog->done must fail")
	}
	if err := s.Move(tk.Key, store.StatusTodo, false); err != nil {
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
		"move":    func(k string) error { return s.Move(k, store.StatusTodo, false) },
		"comment": func(k string) error { _, err := s.AddComment(k, store.AuthorHuman, "retry", FollowUp); return err },
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

func TestSubtaskDefaultsToParentAgent(t *testing.T) {
	s := newSvc(t)
	if _, err := s.CreateTask(NewTaskInput{Title: "p", Repos: []string{"a"}, Agent: "reviewer"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateTask(NewTaskInput{Title: "c", ParentKey: "CV-1"})
	if err != nil || c.Agent == nil || *c.Agent != "reviewer" {
		t.Fatalf("%+v %v", c.Agent, err)
	}
	e, err := s.CreateTask(NewTaskInput{Title: "e", ParentKey: "CV-1", Agent: "other"})
	if err != nil || e.Agent == nil || *e.Agent != "other" {
		t.Fatalf("%+v %v", e.Agent, err)
	}
	n, err := s.CreateTask(NewTaskInput{Title: "n", ParentKey: "CV-1", AgentSet: true})
	if err != nil || n.Agent != nil {
		t.Fatalf("cleared agent inherited: %+v %v", n.Agent, err)
	}
}

func TestSetTaskDescriptionNotesEditAfterRun(t *testing.T) {
	s := newSvc(t)
	tk, err := s.CreateTask(NewTaskInput{Title: "t", Description: "old\nline", Repos: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskDescription("CV-1", "draft"); err != nil {
		t.Fatal(err)
	}
	if cs, _ := store.ListComments(s.DB, tk.ID); len(cs) != 0 {
		t.Fatalf("noted an edit no run saw: %+v", cs)
	}
	if _, err := store.CreateRun(s.DB, store.Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "finished"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateTaskStatus(s.DB, tk.ID, store.StatusTodo, store.ActorHuman); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskDescription("CV-1", "new"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskDescription("CV-1", "new"); err != nil { // unchanged: no second note
		t.Fatal(err)
	}
	got, _ := store.GetTaskByKey(s.DB, "CV-1")
	cs, _ := store.UnconsumedComments(s.DB, tk.ID)
	if got.Description != "new" || len(cs) != 1 || cs[0].Author != store.AuthorSystem || !strings.Contains(cs[0].Body, "> draft") {
		t.Fatalf("%q %+v", got.Description, cs)
	}
}

func TestAdoptChildRefusesReparent(t *testing.T) {
	s := newSvc(t)
	for _, n := range []string{"p", "q", "c"} {
		if _, err := s.CreateTask(NewTaskInput{Title: n, Repos: []string{"a"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AdoptChild("CV-1", "CV-3"); err != nil {
		t.Fatal(err)
	}
	if err := s.AdoptChild("CV-1", "CV-3"); err != nil {
		t.Fatalf("same parent is a no-op: %v", err)
	}
	if err := s.AdoptChild("CV-2", "CV-3"); err == nil {
		t.Fatal("silently re-parented")
	}
	if err := s.SetParent("CV-3", "CV-2"); err != nil {
		t.Fatalf("explicit re-parent must work: %v", err)
	}
}

func TestStopRun(t *testing.T) {
	s := newSvc(t)
	tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
	if err := s.Stop(tk.Key); err == nil {
		t.Fatal("stop with no run must fail")
	}
	s.DB.Exec(`UPDATE tasks SET status=? WHERE id=?`, store.StatusRunning, tk.ID)
	// No bg id, so no `claude` call.
	r, _ := store.CreateRun(s.DB, store.Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "launching"})
	if err := s.Stop(tk.Key); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetTask(s.DB, tk.ID)
	run, _ := store.GetRun(s.DB, r.ID)
	if got.Status != store.StatusNeedsYou || run.Status != "failed" || run.EndedAt == nil {
		t.Fatalf("task %s run %s", got.Status, run.Status)
	}
}

func TestMoveToReviewEndsRunsOnConfirm(t *testing.T) {
	s := newSvc(t)
	tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
	s.DB.Exec(`UPDATE tasks SET status=? WHERE id=?`, store.StatusRunning, tk.ID)
	r, _ := store.CreateRun(s.DB, store.Run{TaskID: tk.ID, Kind: "auto", Mode: "fresh", Status: "running"})
	manual, _ := store.CreateRun(s.DB, store.Run{TaskID: tk.ID, Kind: "interactive", Mode: "fresh", Status: "running"})
	var ar *ActiveRunsError
	if err := s.Move(tk.Key, store.StatusReview, false); !errors.As(err, &ar) || ar.N != 1 {
		t.Fatalf("want ActiveRunsError{N:1}, got %v", err)
	}
	if got, _ := store.GetTask(s.DB, tk.ID); got.Status != store.StatusRunning {
		t.Fatalf("unconfirmed move moved the task to %s", got.Status)
	}
	if err := s.Move(tk.Key, store.StatusReview, true); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetTask(s.DB, tk.ID)
	run, _ := store.GetRun(s.DB, r.ID)
	m, _ := store.GetRun(s.DB, manual.ID)
	if got.Status != store.StatusReview || run.Status != "failed" || run.EndedAt == nil || m.Status != "running" {
		t.Fatalf("task %s run %s manual %s", got.Status, run.Status, m.Status)
	}
	// nothing left running: no confirmation needed
	if err := s.Move(tk.Key, store.StatusDone, false); err != nil {
		t.Fatal(err)
	}
}
