package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// The epic view reads stage, gate and per-card model/worktrees/skills from the graph.
func TestTaskGraphEpicView(t *testing.T) {
	s := newSvc(t)
	root, err := s.CreateTask(NewTaskInput{Title: "epic", Repos: []string{"a"}, Model: "opus"})
	if err != nil {
		t.Fatal(err)
	}
	store.AddLabel(s.DB, root.ID, "epic")
	store.AddLabel(s.DB, root.ID, "epic:os")
	kid, _ := s.CreateTask(NewTaskInput{Title: "kid", Repos: []string{"a"}, ParentKey: root.Key})
	if err := s.SetTaskSkills(kid.Key, []string{"tdd"}); err != nil {
		t.Fatal(err)
	}
	g, err := s.TaskGraph(kid.Key)
	if err != nil {
		t.Fatal(err)
	}
	if g.Stage != "chart" || !g.Gate || g.Name != "os" {
		t.Fatalf("stage/gate/name: %q %v %q", g.Stage, g.Gate, g.Name)
	}
	if g.Nodes[0].Model != "opus" || len(g.Nodes[0].Worktrees) != 0 {
		t.Fatalf("%+v", g.Nodes[0])
	}
	if _, err := store.ApproveEpic(s.DB, root.ID); err != nil {
		t.Fatal(err)
	}
	if g, _ = s.TaskGraph(root.Key); g.Gate || g.Stage != "decide" {
		t.Fatalf("after approve: %q %v", g.Stage, g.Gate)
	}
	lone, _ := s.CreateTask(NewTaskInput{Title: "lone", Repos: []string{"a"}})
	if g, _ = s.TaskGraph(lone.Key); g.Stage != "" || g.Gate {
		t.Fatalf("a plain task has no stage: %q %v", g.Stage, g.Gate)
	}
}

func TestListSkillsAndAgentSkills(t *testing.T) {
	s := newSvc(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".claude", "skills", "tdd"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "skills", "tdd", "SKILL.md"), []byte("---\nname: tdd\ndescription: d\n---\n"), 0o644)
	os.MkdirAll(filepath.Join(home, ".claude", "agents"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "agents", "r.md"), []byte("---\nname: r\ndescription: d\nskills: [tdd, epic]\n---\n"), 0o644)
	if got := s.ListSkills(nil); len(got) != 1 || got[0].Name != "tdd" {
		t.Fatalf("skills %+v", got)
	}
	ag, _ := s.ListAgents(nil)
	if len(ag) != 1 || !reflect.DeepEqual(ag[0].Skills, []string{"tdd", "epic"}) {
		t.Fatalf("agents %+v", ag)
	}
}
