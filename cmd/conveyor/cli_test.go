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
	cv(t, home, "task", "cancel", "CV-1")
	if out := cv(t, home, "task", "list", "--status", "cancelled"); !strings.Contains(out, "CV-1") {
		t.Fatal(out)
	}
	// a non-repo path is rejected
	if b, err := exec.Command(bin, "--home", home, "repo", "add", t.TempDir()).CombinedOutput(); err == nil {
		t.Fatalf("expected failure: %s", b)
	}
}
