package core

import (
	"os"
	"path/filepath"
	"reflect"
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
	if err := s.AddAgent("rev-1", "Reviews: code", "Be strict.", "a", "opus"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAgent("me", "Mine", "Hi", "user", ""); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"Bad Name", "d", "a"}, {"rev-1", "d", "a"}, {"x", "", "a"}, {"x", "d", "nope"}} {
		if s.AddAgent(bad[0], bad[1], "p", bad[2], "") == nil {
			t.Errorf("%v should fail", bad)
		}
	}
	os.WriteFile(filepath.Join(repo, ".claude", "agents", "noname.md"), []byte("no frontmatter"), 0o644)
	got, err := s.ListAgents([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{{Name: "rev-1", Description: "Reviews: code", Scope: "a", Model: "opus", Path: filepath.Join(repo, ".claude", "agents", "rev-1.md"), Skills: []string{}}, {Name: "me", Description: "Mine", Scope: "user", Path: filepath.Join(home, ".claude", "agents", "me.md"), Skills: []string{}}}
	if len(got) != 2 || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if err := s.SetAgentModel("user", "me", "haiku"); err != nil || AgentModel("me") != "haiku" || AgentModel("rev-1", repo) != "opus" {
		t.Fatalf("set model: %v", err)
	}
	if s.SetAgentModel("user", "nope", "x") == nil {
		t.Fatal("missing agent should fail")
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

func TestSaveAgent(t *testing.T) {
	s := newSvc(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := s.AddAgent("me", "Mine", "Hi", "user", ""); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(home, ".claude", "agents", "me.md")
	mirror := filepath.Join(home, ".gemini", "config", "agents", "me.md")
	os.MkdirAll(filepath.Dir(mirror), 0o755)
	os.WriteFile(mirror, []byte("old"), 0o644)
	const next = "---\nname: me\ndescription: New\n---\nBye\n"
	if err := s.SaveAgent(f, next); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{f, mirror} {
		if b, _ := os.ReadFile(p); string(b) != next {
			t.Errorf("%s = %q", p, b)
		}
	}
	if got, err := s.ReadAgent(f); err != nil || got != next {
		t.Fatalf("read: %q %v", got, err)
	}
	other := filepath.Join(home, "x.md")
	os.WriteFile(other, []byte(next), 0o644)
	if s.SaveAgent(other, next) == nil || s.SaveAgent(filepath.Join(home, ".claude", "agents", "..", "..", "x.md"), next) == nil || s.SaveAgent(f, "no name") == nil {
		t.Fatal("bad save should fail")
	}
}

func TestSetFrontmatterModel(t *testing.T) {
	for _, c := range []struct{ in, model, want string }{
		{"---\nname: a\n---\nbody\nmodel: x\n", "opus", "---\nname: a\nmodel: opus\n---\nbody\nmodel: x\n"},
		{"---\nname: a\nmodel: haiku\ndescription: d\n---\nb", "opus", "---\nname: a\nmodel: opus\ndescription: d\n---\nb"},
		{"---\nname: a\nmodel: haiku\ndescription: d\n---\nb", "", "---\nname: a\ndescription: d\n---\nb"},
		{"---\nname: a\n---\nb", "", "---\nname: a\n---\nb"},
	} {
		got, err := setFrontmatterModel([]byte(c.in), c.model)
		if err != nil || string(got) != c.want {
			t.Errorf("%q -> %q, %v", c.in, got, err)
		}
	}
	if _, err := setFrontmatterModel([]byte("no fm"), "x"); err == nil {
		t.Error("want error")
	}
}
