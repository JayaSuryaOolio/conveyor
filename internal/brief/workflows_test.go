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
	r, _ := store.AddRepo(db, store.Repo{Name: "api", Path: repo, DefaultBranch: "main"})
	tk, err := store.CreateTask(db, store.Task{Title: "T", Status: store.StatusTodo}, []store.TaskRepo{{RepoID: r.ID, IsPrimary: true, Branch: "b"}})
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
		"## Acceptance criteria\n\nsee description\n\n## Repo workflows\n\napi:\n",
		"- command `/check`: " + strings.Repeat("é", 99) + "…\n",
		"- command `/create-pr`: Open a PR the repo way\n",
		"- (3 more omitted)\n",
		"- Prefer the repo's own commands and skills",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SECRET-BODY") {
		t.Errorf("bodies leaked:\n%s", got)
	}
}

func TestWorkflowsSkill(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".claude", "skills", "backend"), 0o755)
	os.WriteFile(filepath.Join(d, ".claude", "skills", "backend", "SKILL.md"), []byte("---\nname: backend\ndescription: Backend conventions\n---\nSECRET\n"), 0o644)
	got := workflows(nil, store.Task{Repos: []store.TaskRepo{{RepoName: "api", WorktreePath: &d}}})
	if want := "api:\n- skill `backend`: Backend conventions\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
