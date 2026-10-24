package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestCompletions(t *testing.T) {
	s := newSvc(t)
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	w := func(p, body string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(repo, "src", "a.go"), "package a")
	w(filepath.Join(repo, ".claude", "skills", "deploy", "SKILL.md"), "---\nname: deploy\ndescription: Ship it\n---\n")
	w(filepath.Join(repo, ".claude", "commands", "db", "reset.md"), "---\ndescription: Reset\n---\n")
	w(filepath.Join(home, ".claude", "skills", "deploy", "SKILL.md"), "---\nname: deploy\ndescription: user copy\n---\n")
	w(filepath.Join(home, ".claude", "commands", "mine.md"), "hi")
	if out, err := exec.Command("sh", "-c", "cd "+repo+" && git init -q && git add -A").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	store.RemoveRepo(s.DB, "a")
	if _, err := store.AddRepo(s.DB, store.Repo{Name: "a", Path: repo, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Completions("", []string{"a", "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 3 || got.Files[2] != "src/a.go" {
		t.Errorf("files %v", got.Files)
	}
	want := []Command{{"db:reset", "Reset"}, {"deploy", "Ship it"}, {"mine", ""}}
	if len(got.Commands) != 3 || got.Commands[0] != want[0] || got.Commands[1] != want[1] || got.Commands[2] != want[2] {
		t.Errorf("commands %+v", got.Commands)
	}
	tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
	if got, err = s.Completions(tk.Key, nil); err != nil || len(got.Files) != 3 {
		t.Errorf("by key: %v %v", got.Files, err)
	}
}
