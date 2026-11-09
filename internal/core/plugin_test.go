package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
)

// One install puts hooks + skills in the conveyor@skills-dir plugin and migrates the hooks-only
// install away; uninstall removes it all; agy is listed as not supported.
func TestPlugin(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	s := &Service{Home: "/conveyor-home", Bin: "/bin/cv"}
	claudeDir := filepath.Join(h, ".claude")
	settings := filepath.Join(claudeDir, "settings.json")
	plugin := filepath.Join(claudeDir, "skills", "conveyor")
	oldEpic := filepath.Join(claudeDir, "skills", "epic")
	status := func(action, want string) {
		t.Helper()
		sts, err := s.Plugin("", action)
		if err != nil || len(sts) != 2 || sts[0].Tool != "claude" || sts[0].Status != want || sts[1].Status != "not supported" {
			t.Fatalf("%s: %+v, %v; want claude %q", action, sts, err, want)
		}
	}

	// The hooks-only install, as the previous version left it.
	os.MkdirAll(oldEpic, 0o755)
	os.WriteFile(settings, []byte(`{"model":"opus"}`), 0o644)
	if err := claude.SetUserHooks(settings, "/bin/cv", "/conveyor-home", true); err != nil {
		t.Fatal(err)
	}
	status("status", "outdated")

	status("install", "installed")
	for _, f := range []string{".claude-plugin/plugin.json", "hooks/hooks.json", "skills/epic/SKILL.md", "skills/epic/planners/wayfinder.md", "skills/conveyor/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(plugin, f)); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(plugin, "hooks", "hooks.json")); !strings.Contains(string(b), `'/bin/cv' --home '/conveyor-home' hook Stop`) {
		t.Fatal(string(b))
	}
	if b, _ := os.ReadFile(settings); strings.Contains(string(b), " hook ") || !strings.Contains(string(b), "opus") {
		t.Fatal("old hooks not migrated:", string(b))
	}
	if _, err := os.Stat(oldEpic); !os.IsNotExist(err) {
		t.Fatal("old epic skill left")
	}

	agent := filepath.Join(claudeDir, "agents", "epic-builder.md")
	if _, err := os.Stat(agent); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(agent, []byte("edited"), 0o644)
	status("install", "installed")
	if b, _ := os.ReadFile(agent); string(b) != "edited" {
		t.Fatal("install overwrote the epic-builder agent")
	}

	os.Remove(filepath.Join(plugin, "skills", "conveyor", "SKILL.md"))
	status("status", "outdated")
	s.Bin = "/new/cv" // a moved binary makes the hooks stale
	status("install", "installed")
	s.Bin = "/bin/cv"
	status("status", "outdated")

	status("uninstall", "not installed")
	if _, err := os.Stat(plugin); !os.IsNotExist(err) {
		t.Fatal("plugin left after uninstall")
	}
	if _, err := s.Plugin("vim", "status"); err == nil {
		t.Fatal("unknown tool accepted")
	}
}
