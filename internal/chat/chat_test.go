package chat

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

type fake struct{ got []Msg }

func (f *fake) Ready() bool                                   { return true }
func (f *fake) Send(m Msg) error                              { f.got = append(f.got, m); return nil }
func (f *fake) Listen(context.Context, func(In) string) error { return nil }

func TestNotifyHonoursStatusesAndToggle(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo, _ := store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	tk, err := store.CreateTask(db, store.Task{Title: "t", Status: store.StatusRunning}, []store.TaskRepo{{RepoID: repo.ID, IsPrimary: true, Branch: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	b := &bridge{&core.Service{DB: db}, t.Logf}
	f := &fake{}
	b.notify(f) // first call sets the cursor to "now"

	store.SetSetting(db, "chat_statuses", "needs_you")
	store.UpdateTaskStatus(db, tk.ID, store.StatusNeedsYou, store.ActorSystem)
	store.UpdateTaskStatus(db, tk.ID, store.StatusTodo, store.ActorHuman)
	store.UpdateTaskStatus(db, tk.ID, store.StatusRunning, store.ActorSystem)
	store.UpdateTaskStatus(db, tk.ID, store.StatusReview, store.ActorSystem)   // filtered out
	store.UpdateTaskStatus(db, tk.ID, store.StatusRunning, store.ActorSystem)  // first needs_you is now stale
	store.UpdateTaskStatus(db, tk.ID, store.StatusNeedsYou, store.ActorSystem) // current: sent
	b.notify(f)
	if len(f.got) != 1 || f.got[0].Status != "needs_you" {
		t.Fatalf("want one needs_you msg, got %+v", f.got)
	}

	b.notify(nil) // disabled: skip, and never replay on re-enable
	store.UpdateTaskStatus(db, tk.ID, store.StatusNeedsYou, store.ActorSystem)
	b.notify(nil)
	b.notify(f)
	if len(f.got) != 1 {
		t.Fatalf("replayed while disabled: %+v", f.got)
	}
}

type failing struct{ n int }

func (f *failing) Ready() bool                                   { return true }
func (f *failing) Send(Msg) error                                { f.n++; return errors.New("invalid_blocks") }
func (f *failing) Listen(context.Context, func(In) string) error { return nil }

// A send that keeps failing holds the cursor only until the event is maxRetry old (CV-98).
func TestNotifyDropsEventFailingPastMaxRetry(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo, _ := store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	tk, _ := store.CreateTask(db, store.Task{Title: "t", Status: store.StatusRunning}, []store.TaskRepo{{RepoID: repo.ID, IsPrimary: true, Branch: "t"}})
	b := &bridge{&core.Service{DB: db}, t.Logf}
	f := &failing{}
	b.notify(f)
	store.SetSetting(db, "chat_statuses", "review")
	store.UpdateTaskStatus(db, tk.ID, store.StatusReview, store.ActorSystem)
	b.notify(f)
	b.notify(f)
	if f.n != 2 || store.GetSetting(db, cursorKey) == strconv.FormatInt(b.maxEvent(), 10) {
		t.Fatalf("fresh failure should retry and hold the cursor: sends=%d", f.n)
	}
	db.Exec(`UPDATE events SET created_at=? WHERE kind='status_changed'`, time.Now().Add(-2*maxRetry).UTC().Format(time.RFC3339))
	b.notify(f)
	if got := store.GetSetting(db, cursorKey); got != strconv.FormatInt(b.maxEvent(), 10) {
		t.Fatalf("old failing event should be dropped, cursor=%s", got)
	}
}

func TestHelpCommand(t *testing.T) {
	b := &bridge{}
	for _, in := range []string{"help", "?", "bogus"} {
		if b.command(in) != Help {
			t.Fatalf("%q did not return help", in)
		}
	}
}

func TestShowCommand(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo, _ := store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	tk, _ := store.CreateTask(db, store.Task{Title: "hello", Status: store.StatusTodo}, []store.TaskRepo{{RepoID: repo.ID, IsPrimary: true, Branch: "t"}})
	b := &bridge{&core.Service{DB: db}, t.Logf}
	for _, in := range []string{"show " + tk.Key, tk.Key} {
		if got := b.command(in); !strings.Contains(got, "hello") || !strings.Contains(got, "a (") {
			t.Fatalf("%q: %s", in, got)
		}
	}
}

func TestMoveAcceptsTo(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo, _ := store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	tk, _ := store.CreateTask(db, store.Task{Title: "hello", Status: store.StatusReview}, []store.TaskRepo{{RepoID: repo.ID, IsPrimary: true, Branch: "t"}})
	b := &bridge{&core.Service{DB: db}, t.Logf}
	if got := b.command("move " + tk.Key + " to done"); strings.Contains(got, "warning") {
		t.Fatal(got)
	}
	if got, _ := store.GetTaskByKey(db, tk.Key); got.Status != store.StatusDone {
		t.Fatalf("status %s", got.Status)
	}
}

func TestRetryDelayBacksOffAndCaps(t *testing.T) {
	for fails, want := range map[int]time.Duration{0: 5 * time.Second, 1: 5 * time.Second, 2: 10 * time.Second, 7: 320 * time.Second, 8: 10 * time.Minute, 1000: 10 * time.Minute} {
		if got := retryDelay(fails); got != want {
			t.Fatalf("fails=%d: got %v want %v", fails, got, want)
		}
	}
}

func TestNewTaskFromForward(t *testing.T) {
	home := t.TempDir()
	db, err := store.Open(filepath.Join(home, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store.AddRepo(db, store.Repo{Name: "a", Path: "/x/a", DefaultBranch: "main"})
	b := &bridge{&core.Service{DB: db, Home: home}, t.Logf}
	png := []byte("\x89PNG\r\n\x1a\n")
	fwd := func() (string, []File) { return "> it crashes\n\n— Bob", []File{{"shot.png", png}} }
	if r := b.handle(In{Text: "new fix this crash", Fwd: fwd}); !strings.HasPrefix(r, "created ") {
		t.Fatal(r)
	}
	store.AddRepo(db, store.Repo{Name: "b", Path: "/x/b", DefaultBranch: "main"})
	if r := b.handle(In{Text: "new fix it"}); !strings.Contains(r, "Name a repo") {
		t.Fatalf("two repos need a name: %s", r)
	}
	if r := b.handle(In{Text: "new b", Fwd: fwd}); !strings.HasPrefix(r, "created ") {
		t.Fatal(r)
	}
	// phone keyboards: capitalised "New", repo in other case, smart quotes
	if r := b.handle(In{Text: "New B “bump sdk”"}); !strings.HasPrefix(r, "created ") || !strings.Contains(r, " bump sdk (") {
		t.Fatal(r)
	}
	// "/conveyor" typed as a forward's comment, where Slack doesn't run slash commands
	if r := b.handle(In{Text: "/conveyor new b “bump sdk”"}); !strings.HasPrefix(r, "created ") {
		t.Fatal(r)
	}
	ts, _ := store.ListTasks(db)
	if len(ts) != 4 {
		t.Fatalf("want 4 tasks, got %d", len(ts))
	}
	for _, tk := range ts {
		if tk.Title == "bump sdk" {
			if len(tk.Repos) == 0 || tk.Repos[0].RepoName != "b" {
				t.Fatalf("repo %v", tk.Repos)
			}
			continue
		}
		if !strings.Contains(tk.Description, "> it crashes") || !strings.Contains(tk.Description, "![shot.png]("+home) {
			t.Fatalf("description lacks quote or image:\n%s", tk.Description)
		}
	}
	if ts[0].Title != "fix this crash" && ts[1].Title != "fix this crash" {
		t.Fatalf("titles: %q %q", ts[0].Title, ts[1].Title)
	}
	if ts[0].Title != "it crashes" && ts[1].Title != "it crashes" {
		t.Fatal("empty comment should take the title from the forward")
	}
}

func TestThreadCmd(t *testing.T) {
	for in, want := range map[string]string{
		"move done":        "move CV-1 done",
		"Move to Done":     "move CV-1 done",
		"move CV-2 todo":   "move CV-2 todo",
		"show":             "show CV-1",
		"list":             "list",
		"move on please":   "",
		"move on":          "",
		"show me the diff": "",
		"use the v2 API":   "",
	} {
		if got := threadCmd("CV-1", in); got != want {
			t.Errorf("threadCmd(%q) = %q, want %q", in, got, want)
		}
	}
}
