package agy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestExtractConversationID(t *testing.T) {
	out := `Some header
Conversation ID: 12345678-1234-1234-1234-123456789abc
Model: gemini-pro`
	if got := ExtractConversationID(out); got != "12345678-1234-1234-1234-123456789abc" {
		t.Errorf("got %q", got)
	}
	out2 := "conversation: abcdef01-2345-6789-abcd-ef0123456789"
	if got := ExtractConversationID(out2); got != "abcdef01-2345-6789-abcd-ef0123456789" {
		t.Errorf("got %q", got)
	}
	if got := ExtractConversationID("no conversation here"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestWriteSettings(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteSettings(dir, "/usr/local/bin/conveyor")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "agy-settings.json") {
		t.Errorf("unexpected path: %s", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Error("settings file is empty")
	}
}

func TestFreshAndResumeCommand(t *testing.T) {
	f := FreshCommand("/path/to/agy-settings.json", "/path/to/BRIEF.md", []string{"/extra/repo"})
	if !strings.Contains(f, "agy --settings '/path/to/agy-settings.json' --prompt-interactive") {
		t.Errorf("unexpected fresh command: %s", f)
	}
	if !strings.Contains(f, "--add-dir '/extra/repo'") {
		t.Errorf("fresh command missing add-dir: %s", f)
	}

	r := ResumeCommand("conv-uuid-123", "/path/to/agy-settings.json", []string{"/extra/repo"})
	if !strings.Contains(r, "agy --settings '/path/to/agy-settings.json' --conversation 'conv-uuid-123'") {
		t.Errorf("unexpected resume command: %s", r)
	}
	if !strings.Contains(r, "--add-dir '/extra/repo'") {
		t.Errorf("resume command missing add-dir: %s", r)
	}
}

func TestLaunchSpecArgs(t *testing.T) {
	s := LaunchSpec{
		Prompt:       "do something",
		SettingsPath: "/path/to/agy-settings.json",
		Bypass:       true,
		Model:        "gemini-pro",
		Agent:        "test-agent",
		AddDirs:      []string{"/dir1", "/dir2"},
	}
	args := s.args()
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"--print", "do something",
		"--settings /path/to/agy-settings.json",
		"--dangerously-skip-permissions",
		"--model gemini-pro",
		"--agent test-agent",
		"--add-dir /dir1",
		"--add-dir /dir2",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("args %q does not contain %q", joined, expected)
		}
	}
}

// mockExec records the args of the next agy start and runs `true` instead.
func mockExec(t *testing.T) *[]string {
	t.Helper()
	old := execCommand
	t.Cleanup(func() { execCommand = old })
	var got []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		got = args
		return exec.Command("true")
	}
	return &got
}

func TestMockLaunch(t *testing.T) {
	mockExec(t)
	home := t.TempDir()
	pid, _, err := Launch(home, LaunchSpec{
		Cwd:          t.TempDir(),
		Name:         "CV-1",
		Prompt:       "test",
		SettingsPath: filepath.Join(home, "agy-settings.json"),
		Bypass:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pid == "" {
		t.Fatal("expected non-empty pid")
	}
}

// A resume appends to the task's own log (the one the reaper reads) and keeps model and agent.
func TestResumeLogPathAndArgs(t *testing.T) {
	got := mockExec(t)
	home := t.TempDir()
	log := BgLogPath(home, "CV-1")
	if err := os.MkdirAll(filepath.Dir(log), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, []byte("first run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, err := Resume(home, "999999999", "conv-1", LaunchSpec{
		Cwd: t.TempDir(), Name: "CV-1", Prompt: "go on", Model: "gemini-pro", Agent: "sse", Bypass: true,
	})
	if err != nil || pid == "" {
		t.Fatalf("pid %q err %v", pid, err)
	}
	if b, _ := os.ReadFile(log); string(b) != "first run\n" {
		t.Errorf("resume truncated or missed %s: %q", log, b)
	}
	joined := strings.Join(*got, " ")
	for _, want := range []string{"--conversation conv-1", "--print go on", "--model gemini-pro", "--agent sse", "--dangerously-skip-permissions"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

// agy runs in its own process group, so signals to Conveyor's group on quit do not reach it.
func TestStartDetached(t *testing.T) {
	old := execCommand
	t.Cleanup(func() { execCommand = old })
	execCommand = func(string, ...string) *exec.Cmd { return exec.Command("sleep", "30") }
	pid, _, err := Launch(t.TempDir(), LaunchSpec{Cwd: t.TempDir(), Name: "CV-1"})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(pid)
	defer syscall.Kill(n, syscall.SIGKILL)
	if g, err := syscall.Getpgid(n); err != nil || g != n || g == syscall.Getpgrp() {
		t.Errorf("pgid %d (err %v), want own group %d", g, err, n)
	}
}
