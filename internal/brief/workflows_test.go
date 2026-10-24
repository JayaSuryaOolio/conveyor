package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestRenderRepoWorkflows(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := filepath.Join(dir, "api")
	w := func(rel, body string) {
		p := filepath.Join(repo, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	w(".claude/commands/create-pr.md", "---\ndescription: Open a PR the repo way\n---\nSECRET-BODY-1\n")
	w(".claude/commands/check.md", "# Check\n\n"+strings.Repeat("é", 150)+"\nSECRET-BODY-2\n")
	w(".claude/skills/backend/SKILL.md", "---\nname: backend\ndescription: Backend conventions\n---\nSECRET-BODY-3\n")
	for i := 0; i < 30; i++ {
		w(fmt.Sprintf(".claude/commands/x%02d.md", i), "x\n")
	}
	w("CLAUDE.md", "# api\n")
	// The primary repo's context is loaded by Claude Code itself, so only api (not primary) is indexed.
	web := filepath.Join(dir, "web")
	os.MkdirAll(filepath.Join(web, ".claude", "commands"), 0o755)
	os.WriteFile(filepath.Join(web, ".claude", "commands", "web-only.md"), []byte("x\n"), 0o644)
	pr, _ := store.AddRepo(db, store.Repo{Name: "web", Path: web, DefaultBranch: "main"})
	r, _ := store.AddRepo(db, store.Repo{Name: "api", Path: repo, DefaultBranch: "main"})
	tk, err := store.CreateTask(db, store.Task{Title: "T", Status: store.StatusTodo}, []store.TaskRepo{{RepoID: pr.ID, IsPrimary: true, Branch: "b"}, {RepoID: r.ID, Branch: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	path, err := Render(db, dir, tk)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	for _, want := range []string{
		"## Other repos' context\n\n",
		"api:\n- Read `" + filepath.Join(repo, "CLAUDE.md") + "` before changing this repo\n",
		"- command `/check`: " + strings.Repeat("é", 99) + "…\n",
		"- command `/create-pr`: Open a PR the repo way\n",
		"- (4 more omitted)\n",
		"- The repo's CLAUDE.md, skills, commands and agents are the authority",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "web-only") {
		t.Errorf("primary repo indexed:\n%s", got)
	}
	if strings.Contains(got, "SECRET-BODY") {
		t.Errorf("bodies leaked:\n%s", got)
	}
}

func TestWorkflowsSkill(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".claude", "skills", "backend"), 0o755)
	os.WriteFile(filepath.Join(d, ".claude", "skills", "backend", "SKILL.md"), []byte("---\nname: backend\ndescription: Backend conventions\n---\nSECRET\n"), 0o644)
	os.MkdirAll(filepath.Join(d, ".claude", "agents"), 0o755)
	os.WriteFile(filepath.Join(d, ".claude", "agents", "reviewer.md"), []byte("---\nname: reviewer\ndescription: Reviews diffs\n---\nSECRET\n"), 0o644)
	got := workflows(nil, store.Task{Repos: []store.TaskRepo{{RepoName: "api", WorktreePath: &d}}})
	if want := "api:\n- skill `backend`: Backend conventions\n- agent `reviewer`: Reviews diffs\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
