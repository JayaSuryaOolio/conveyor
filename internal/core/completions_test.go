package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	labels := func(kind, q string, key string, repos []string) string {
		got, err := s.Complete(key, repos, kind, q)
		if err != nil {
			t.Fatal(err)
		}
		var l []string
		for _, g := range got {
			l = append(l, g.Label)
		}
		return strings.Join(l, ",")
	}
	if got := labels("@", "", "", []string{"a", "nope"}); got != ".claude/commands/db/reset.md,.claude/skills/deploy/SKILL.md,src/a.go" {
		t.Errorf("all files %q", got)
	}
	if got := labels("@", "A", "", []string{"a"}); got != "src/a.go,.claude/commands/db/reset.md,.claude/skills/deploy/SKILL.md" { // basename prefix before substring
		t.Errorf("ranked files %q", got)
	}
	if got := labels("/", "", "", []string{"a"}); got != "/db:reset,/deploy,/mine" { // repo deploy shadows the user's
		t.Errorf("commands %q", got)
	}
	if g, _ := s.Complete("", []string{"a"}, "/", "dep"); len(g) != 1 || g[0].Insert != "/deploy" || g[0].Hint != "Ship it" {
		t.Errorf("deploy %+v", g)
	}
	tk, _ := s.CreateTask(NewTaskInput{Title: "t", Repos: []string{"a"}})
	if got := labels("@", "a.go", tk.Key, nil); got != "src/a.go" {
		t.Errorf("by key %q", got)
	}
	for _, ref := range []string{"@../../etc/passwd", "@nope.go", "@../a/../../x"} {
		if err := s.OpenFile(tk.Key, ref, "Finder"); err == nil {
			t.Errorf("OpenFile %s: want error", ref)
		}
	}
	if err := s.OpenFile(tk.Key, "@src/a.go", "Terminal"); err == nil || !strings.Contains(err.Error(), "cannot open files") {
		t.Errorf("Terminal: %v", err)
	}
}
