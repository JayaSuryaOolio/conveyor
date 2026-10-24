package claude

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeClaude installs a `claude` script on PATH that logs "cwd|env|argv" per call
// and prints out/<first-arg>.txt from the fake dir.
func fakeClaude(t *testing.T, outputs map[string]string) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "log")
	for k, v := range outputs {
		if err := os.WriteFile(filepath.Join(dir, "out-"+k), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/bash
d="$(dirname "$0")"
printf '%s|%s|' "$PWD" "$CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD" >> "$d/log"
printf '%s\x1f' "$@" >> "$d/log"
printf '\n' >> "$d/log"
k="$1"; [ "$k" = "--bg" ] && k="bg"; [ "$2" = "--resume" ] && k="resume"
cat "$d/out-$k" 2>/dev/null
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

type call struct {
	cwd, env string
	args     []string
}

func readLog(t *testing.T, p string) []call {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var cs []call
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		parts := strings.SplitN(l, "|", 3)
		cwd, _ := filepath.EvalSymlinks(parts[0])
		cs = append(cs, call{cwd, parts[1], strings.Split(strings.TrimSuffix(parts[2], "\x1f"), "\x1f")})
	}
	return cs
}

func TestArgs(t *testing.T) {
	s := LaunchSpec{Prompt: "do it", Name: "K-1", SettingsPath: "/s.json", AddDirs: []string{"/a", "/b"}}
	want := []string{"--bg", "do it", "--name", "K-1", "--settings", "/s.json", "--strict-mcp-config", "--add-dir", "/a", "/b"}
	if got := s.Args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	s.Bypass, s.MCPConfig, s.Model, s.Agent = true, "/m.json", "opus", "rev"
	want = []string{"--bg", "do it", "--name", "K-1", "--settings", "/s.json", "--permission-mode", "bypassPermissions",
		"--strict-mcp-config", "--mcp-config", "/m.json", "--model", "opus", "--agent", "rev", "--add-dir", "/a", "/b"}
	if got := s.Args(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestLaunch(t *testing.T) {
	log := fakeClaude(t, map[string]string{"bg": "backgrounded · abc123\n"})
	cwd := t.TempDir()
	cwdReal, _ := filepath.EvalSymlinks(cwd)
	id, err := Launch(context.Background(), LaunchSpec{Cwd: cwd, Prompt: "p", Name: "K-1", SettingsPath: "/s"})
	if err != nil || id != "abc123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	c := readLog(t, log)[0]
	if c.cwd != cwdReal || c.env != "1" || c.args[0] != "--bg" {
		t.Fatalf("bad call %+v", c)
	}
}

func TestLaunchUntrusted(t *testing.T) {
	fakeClaude(t, map[string]string{"bg": "Workspace not trusted: /x\n"})
	_, err := Launch(context.Background(), LaunchSpec{Cwd: t.TempDir(), Prompt: "p"})
	if !errors.Is(err, ErrUntrusted) || !strings.Contains(err.Error(), "/x") {
		t.Fatalf("err=%v", err)
	}
}

func TestNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Agents(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestResume(t *testing.T) {
	log := fakeClaude(t, map[string]string{"resume": "backgrounded · same\n"})
	id, err := Resume(context.Background(), t.TempDir(), "bg1", "sess1", "delta")
	if err != nil || id != "same" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	cs := readLog(t, log)
	if len(cs) != 2 || !reflect.DeepEqual(cs[0].args, []string{"stop", "bg1"}) ||
		!reflect.DeepEqual(cs[1].args, []string{"--bg", "--resume", "sess1", "delta"}) {
		t.Fatalf("calls %+v", cs)
	}
}

func TestResumeCopy(t *testing.T) {
	fakeClaude(t, map[string]string{"resume": "started a copy as newid\nbackgrounded · other\n"})
	id, err := Resume(context.Background(), t.TempDir(), "bg1", "s", "p")
	if err != nil || id != "newid" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestAgents(t *testing.T) {
	fakeClaude(t, map[string]string{"agents": `[{"id":"a1","name":"K-1","cwd":"/w","state":"working","status":"busy","sessionId":"s1","pid":42},{"name":"hand","cwd":"/x","sessionId":"s2"}]`})
	as, err := Agents(context.Background())
	if err != nil || len(as) != 2 || as[0].ID != "a1" || as[0].SessionID != "s1" || as[0].PID != 42 || as[1].ID != "" {
		t.Fatalf("as=%+v err=%v", as, err)
	}
}

func TestStopRemove(t *testing.T) {
	log := fakeClaude(t, nil)
	_ = Stop(context.Background(), "x")
	_ = Remove(context.Background(), "x")
	cs := readLog(t, log)
	if cs[0].args[0] != "stop" || cs[1].args[0] != "rm" {
		t.Fatalf("calls %+v", cs)
	}
}

func TestWriteSettings(t *testing.T) {
	home := filepath.Join(t.TempDir(), "my home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := WriteSettings(home, "/bin dir/conveyor")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	var m struct {
		Worktree struct{ BgIsolation string }
		Hooks    map[string][]struct {
			Hooks []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Worktree.BgIsolation != "none" || len(m.Hooks) != 3 {
		t.Fatalf("settings %s", b)
	}
	want := "'/bin dir/conveyor' --home '" + home + "' hook Stop"
	if got := m.Hooks["Stop"][0].Hooks[0].Command; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCommandStrings(t *testing.T) {
	if got := AttachCommand("a b"); got != "claude attach 'a b'" {
		t.Fatal(got)
	}
	if got := ResumeCommand("s 1", "/my dir/s.json"); got != "claude --resume 's 1' --settings '/my dir/s.json'" {
		t.Fatal(got)
	}
	got := FreshCommand("/my dir/s.json", "/my dir/brief.md", []string{"/a b", "/it's"})
	want := `claude --settings '/my dir/s.json' 'Read /my dir/brief.md first, then wait for my instructions.' --add-dir '/a b' --add-dir '/it'\''s'`
	if got != want {
		t.Fatalf("got %s", got)
	}
}
