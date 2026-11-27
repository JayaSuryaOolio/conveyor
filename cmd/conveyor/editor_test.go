package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEditor(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "workspaces", "CV-7", "Repo", "sub")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONVEYOR_HOOKS", "")
	t.Setenv("CLAUDECODE", "")
	t.Chdir(ws)
	if got := editor(home); got != "human" {
		t.Fatalf("plain terminal: %q", got)
	}
	t.Setenv("CONVEYOR_HOOKS", home)
	if got := editor(home); got != "agent:CV-7" {
		t.Fatalf("agent in workspace: %q", got)
	}
	t.Chdir(home)
	if got := editor(home); got != "agent" {
		t.Fatalf("agent outside a workspace: %q", got)
	}
}

func TestCommentAuthor(t *testing.T) {
	for _, c := range []struct{ hooks, as, want string }{
		{"", "", "human"}, {"/h", "", "agent"}, {"/h", "human", "human"}, {"", "agent", "agent"},
	} {
		t.Setenv("CONVEYOR_HOOKS", c.hooks)
		if got, err := commentAuthor(c.as); err != nil || got != c.want {
			t.Fatalf("hooks=%q as=%q: %q, %v", c.hooks, c.as, got, err)
		}
	}
	if _, err := commentAuthor("bot"); err == nil {
		t.Fatal("bad --as accepted")
	}
}
