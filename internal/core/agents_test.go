package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestAgents(t *testing.T) {
	s := newSvc(t)
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	store.RemoveRepo(s.DB, "a")
	if _, err := store.AddRepo(s.DB, store.Repo{Name: "a", Path: repo, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAgent("rev-1", "Reviews: code", "Be strict.", "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAgent("me", "Mine", "Hi", "user"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"Bad Name", "d", "a"}, {"rev-1", "d", "a"}, {"x", "", "a"}, {"x", "d", "nope"}} {
		if s.AddAgent(bad[0], bad[1], "p", bad[2]) == nil {
			t.Errorf("%v should fail", bad)
		}
	}
	os.WriteFile(filepath.Join(repo, ".claude", "agents", "noname.md"), []byte("no frontmatter"), 0o644)
	got, err := s.ListAgents([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{{"rev-1", "Reviews: code", "a"}, {"me", "Mine", "user"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
	tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}, Agent: "rev-1"})
	if tk.Agent == nil || *tk.Agent != "rev-1" {
		t.Fatalf("%+v", tk.Agent)
	}
	if err := s.SetTaskAgent(tk.Key, ""); err != nil {
		t.Fatal(err)
	}
	if tk, _ = store.GetTaskByKey(s.DB, tk.Key); tk.Agent != nil {
		t.Fatal("agent not cleared")
	}
}
