package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
)

// One install puts hooks + skills in the conveyor@skills-dir plugin and migrates the hooks-only
// install away; agy plugin installs hooks + skills + agents in ~/.gemini/config/plugins/conveyor;
// uninstall removes it all.
func TestPlugin(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	s := &Service{Home: "/conveyor-home", Bin: "/bin/cv"}
	claudeDir := filepath.Join(h, ".claude")
	settings := filepath.Join(claudeDir, "settings.json")
	claudePlugin := filepath.Join(claudeDir, "skills", "conveyor")
	agyPlugin := filepath.Join(h, ".gemini", "config", "plugins", "conveyor")
	oldEpic := filepath.Join(claudeDir, "skills", "epic")
	status := func(action, wantClaude, wantAgy string) {
		t.Helper()
		sts, err := s.Plugin("", action)
		if err != nil || len(sts) != 2 || sts[0].Tool != "claude" || sts[0].Status != wantClaude || sts[1].Tool != "agy" || sts[1].Status != wantAgy {
			t.Fatalf("%s: %+v, %v; want claude %q, agy %q", action, sts, err, wantClaude, wantAgy)
		}
	}

	// The hooks-only install, as the previous version left it.
	os.MkdirAll(oldEpic, 0o755)
	os.WriteFile(settings, []byte(`{"model":"opus"}`), 0o644)
	if err := claude.SetUserHooks(settings, "/bin/cv", "/conveyor-home", true); err != nil {
		t.Fatal(err)
	}
	status("status", "outdated", "not installed")

	status("install", "installed", "installed")
	for _, f := range []string{".claude-plugin/plugin.json", "hooks/hooks.json", "skills/epic/SKILL.md", "skills/epic/planners/wayfinder.md", "skills/conveyor/SKILL.md", "skills/conveyor-drift/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(claudePlugin, f)); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"plugin.json", "hooks.json", "skills/epic/SKILL.md", "skills/conveyor/SKILL.md", "skills/conveyor-drift/SKILL.md", "agents/sse.md", "agents/software-architect.md", "agents/senior-frontend-expert.md", "agents/peer-reviewer.md", "agents/automation-tester.md", "agents/ai-agents-expert.md", "agents/epic-builder.md"} {
		if _, err := os.Stat(filepath.Join(agyPlugin, f)); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(claudePlugin, "hooks", "hooks.json")); !strings.Contains(string(b), `'/bin/cv' --home '/conveyor-home' hook Stop`) {
		t.Fatal(string(b))
	}
	if b, _ := os.ReadFile(filepath.Join(agyPlugin, "hooks.json")); !strings.Contains(string(b), `'/bin/cv' --home '/conveyor-home' hook Stop`) {
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
	// Check seeded basic software engineering agents in Claude user agents dir
	for _, a := range []string{"sse.md", "software-architect.md", "senior-frontend-expert.md", "peer-reviewer.md", "automation-tester.md", "ai-agents-expert.md"} {
		if _, err := os.Stat(filepath.Join(claudeDir, "agents", a)); err != nil {
			t.Fatalf("missing seeded claude agent %s: %v", a, err)
		}
	}
	os.WriteFile(agent, []byte("edited"), 0o644)
	status("install", "installed", "installed")
	if b, _ := os.ReadFile(agent); string(b) != "edited" {
		t.Fatal("install overwrote the epic-builder agent")
	}

	os.Remove(filepath.Join(claudePlugin, "skills", "conveyor", "SKILL.md"))
	status("status", "outdated", "installed")
	s.Bin = "/new/cv" // a moved binary makes the hooks stale
	status("install", "installed", "installed")
	s.Bin = "/bin/cv"
	status("status", "outdated", "outdated")

	status("uninstall", "not installed", "not installed")
	if _, err := os.Stat(claudePlugin); !os.IsNotExist(err) {
		t.Fatal("claude plugin left after uninstall")
	}
	if _, err := os.Stat(agyPlugin); !os.IsNotExist(err) {
		t.Fatal("agy plugin left after uninstall")
	}
	if _, err := s.Plugin("vim", "status"); err == nil {
		t.Fatal("unknown tool accepted")
	}
}
