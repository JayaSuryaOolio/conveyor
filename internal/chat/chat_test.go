package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
	store.UpdateTaskStatus(db, tk.ID, store.StatusReview, store.ActorSystem) // filtered out
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
	tk, _ := store.CreateTask(db, store.Task{Title: "hello", Status: store.StatusTodo}, []store.TaskRepo{{RepoID: repo.ID, IsPrimary: true, Branch: "t"}})
	b := &bridge{&core.Service{DB: db}, t.Logf}
	if got := b.command("move " + tk.Key + " to done"); strings.Contains(got, "warning") {
		t.Fatal(got)
	}
	if got, _ := store.GetTaskByKey(db, tk.Key); got.Status != store.StatusDone {
		t.Fatalf("status %s", got.Status)
	}
}
