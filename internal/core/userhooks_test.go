package core

import (
	"os"
	"path/filepath"
	"testing"
)

// Installing the hooks installs the epic skill beside them; uninstalling removes both.
func TestUserHooksInstallEpicSkill(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	s := &Service{Home: "/conveyor-home", Bin: "/bin/cv"}
	skill := filepath.Join(h, ".claude", "skills", "epic")
	for _, c := range []struct{ action, want string }{{"status", "not installed"}, {"install", "installed"}, {"status", "installed"}} {
		if got, err := s.UserHooks(c.action); err != nil || got != c.want {
			t.Fatalf("%s = %q, %v; want %q", c.action, got, err, c.want)
		}
	}
	for _, f := range []string{"SKILL.md", "planners/wayfinder.md"} {
		if _, err := os.Stat(filepath.Join(skill, f)); err != nil {
			t.Fatal(err)
		}
	}
	agent := filepath.Join(h, ".claude", "agents", "epic-builder.md")
	if _, err := os.Stat(agent); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(agent, []byte("edited"), 0o644)
	s.UserHooks("install")
	if b, _ := os.ReadFile(agent); string(b) != "edited" {
		t.Fatal("install overwrote the epic-builder agent")
	}
	os.RemoveAll(skill)
	if got, _ := s.UserHooks("status"); got != "outdated" {
		t.Fatalf("hooks without the skill: %q", got)
	}
	if got, err := s.UserHooks("uninstall"); err != nil || got != "not installed" {
		t.Fatal(got, err)
	}
	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Fatal("skill left after uninstall")
	}
}
