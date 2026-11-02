package claude

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestSkill(t *testing.T) {
	src := fstest.MapFS{"SKILL.md": {Data: []byte("v1")}, "planners/wayfinder.md": {Data: []byte("w")}}
	dir := filepath.Join(t.TempDir(), "skills", "epic")
	status := func(hooks, want string) {
		t.Helper()
		if got := SkillStatus(hooks, src, dir); got != want {
			t.Fatalf("SkillStatus(%q) = %q, want %q", hooks, got, want)
		}
	}
	status("not installed", "not installed")
	status("installed", "outdated") // hooks without the skill

	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "stale.md"), []byte("old"), 0o644)
	if err := SetSkill(src, dir, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "planners", "wayfinder.md")); string(b) != "w" {
		t.Fatalf("planner %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.md")); !os.IsNotExist(err) {
		t.Fatal("install keeps files the skill no longer ships")
	}
	status("installed", "installed")
	status("not installed", "outdated") // skill left behind
	status("outdated", "outdated")

	src["SKILL.md"] = &fstest.MapFile{Data: []byte("v2")}
	status("installed", "outdated") // a newer Conveyor ships a newer skill

	if err := SetSkill(src, dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("uninstall left the skill")
	}
	status("not installed", "not installed")
}
