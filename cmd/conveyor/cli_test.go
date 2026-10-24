package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cvbin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "conveyor")
	if b, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic(string(b))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func cv(t *testing.T, home string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"--home", home}, args...)...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("conveyor %v: %v\n%s", args, err, b)
	}
	return string(b)
}

func TestCLIFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir()) // keep the real ~/.claude/agents out of `agent list`
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "i"}} {
		if b, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	name := filepath.Base(repo)
	if out := cv(t, home, "repo", "add", repo); !strings.Contains(out, "default branch main") {
		t.Fatal(out)
	}
	cv(t, home, "task", "new", "Do the thing", "--repo", name, "--todo")
	var tasks []struct {
		Key    string `json:"key"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(cv(t, home, "task", "list", "--json")), &tasks); err != nil || len(tasks) != 1 || tasks[0].Key != "CV-1" || tasks[0].Status != "todo" {
		t.Fatalf("list: %v %+v", err, tasks)
	}
	cv(t, home, "task", "comment", "CV-1", "hello")
	if out := cv(t, home, "task", "show", "CV-1", "--thread"); !strings.Contains(out, "human: hello") {
		t.Fatal(out)
	}
	cv(t, home, "config", "set", "max_parallel", "7")
	if out := cv(t, home, "config", "get", "max_parallel"); strings.TrimSpace(out) != "7" {
		t.Fatal(out)
	}
	cv(t, home, "task", "set", "CV-1", "--branch", "feat/x", "--pr", "https://gh/pr/1")
	if out := cv(t, home, "task", "show", "CV-1"); !strings.Contains(out, "branch feat/x") || !strings.Contains(out, "https://gh/pr/1") {
		t.Fatal(out)
	}
	cv(t, home, "task", "set", "CV-1", "--model", "opus")
	if out := cv(t, home, "task", "show", "CV-1", "--json"); !strings.Contains(out, `"model": "opus"`) {
		t.Fatal(out)
	}
	cv(t, home, "task", "set", "CV-1", "--pr", "")
	if out := cv(t, home, "task", "show", "CV-1"); !strings.Contains(out, "branch feat/x") || strings.Contains(out, "gh/pr") {
		t.Fatal(out)
	}
	if out := cv(t, home, "task", "new", "Sub", "--repo", name, "--parent", "CV-1"); !strings.HasPrefix(out, "CV-1A ") {
		t.Fatal(out)
	}
	cv(t, home, "task", "new", "Other", "--repo", name)
	cv(t, home, "task", "link", "CV-1A", "blocks", "CV-2")
	if out := cv(t, home, "task", "show", "CV-2"); !strings.Contains(out, "is blocked by: CV-1A") {
		t.Fatal(out)
	}
	if out := cv(t, home, "task", "show", "CV-1"); !strings.Contains(out, "child: CV-1A") {
		t.Fatal(out)
	}
	cv(t, home, "task", "unlink", "CV-1A", "blocks", "CV-2")
	if out := cv(t, home, "task", "show", "CV-2"); strings.Contains(out, "blocked") {
		t.Fatal(out)
	}
	cv(t, home, "agent", "add", "rev", "--desc", "Reviews", "--prompt", "Be strict", "--model", "opus", "--repo", name)
	if out := cv(t, home, "agent", "list", "--repo", name); !strings.Contains(out, "MODEL") || !strings.Contains(out, "opus") {
		t.Fatal(out)
	}
	cv(t, home, "agent", "model", "rev", "", "--repo", name)
	if out := cv(t, home, "agent", "list", "--repo", name); strings.Contains(out, "opus") {
		t.Fatal(out)
	}
	cv(t, home, "task", "cancel", "CV-1")
	if out := cv(t, home, "task", "list", "--status", "cancelled"); !strings.Contains(out, "CV-1") {
		t.Fatal(out)
	}
	// a non-repo path is rejected
	if b, err := exec.Command(bin, "--home", home, "repo", "add", t.TempDir()).CombinedOutput(); err == nil {
		t.Fatalf("expected failure: %s", b)
	}
}

func TestHookAlwaysExitsZero(t *testing.T) {
	home := t.TempDir()
	cmd := exec.Command(bin, "--home", home, "hook", "Stop")
	cmd.Stdin = strings.NewReader("{garbage")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hook failed: %v %s", err, out)
	}
	if b, err := os.ReadFile(filepath.Join(home, "logs", "hook.log")); err != nil || len(b) == 0 {
		t.Fatalf("log: %v %q", err, b)
	}
}

func TestAutomationCLI(t *testing.T) {
	home := t.TempDir()
	cv(t, home, "automation", "add", "--template", "pr-sweep")
	if out := cv(t, home, "automation", "list"); !strings.Contains(out, "pr-sweep") || !strings.Contains(out, "*/15 * * * *") {
		t.Fatal(out)
	}
	cv(t, home, "automation", "disable", "pr-sweep")
	if out := cv(t, home, "automation", "list"); !strings.Contains(out, "false") {
		t.Fatal(out)
	}
	if b, err := exec.Command(bin, "--home", home, "automation", "add", "bad", "--schedule", "nope", "--kind", "cli", "--arg", "{}").CombinedOutput(); err == nil {
		t.Fatalf("expected bad schedule to fail: %s", b)
	}
	if b, err := exec.Command(bin, "--home", home, "automation", "add", "bad", "--schedule", "* * * * *", "--kind", "cli", "--arg", "not json").CombinedOutput(); err == nil || !strings.Contains(string(b), "not valid JSON") {
		t.Fatalf("expected invalid --arg to fail: %s", b)
	}
	cv(t, home, "automation", "rm", "pr-sweep")
}
