package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	cv(t, home, "repo", "setup", name, "direnv allow .")
	if out := cv(t, home, "repo", "setup", name); strings.TrimSpace(out) != "direnv allow ." {
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
	if b, err := exec.Command(bin, "--home", home, "task", "new", "NoDesc", "--repo", name, "--parent", "CV-1").CombinedOutput(); err == nil {
		t.Fatalf("sub-task without --desc was created: %s", b)
	}
	if out := cv(t, home, "task", "new", "Sub", "--repo", name, "--parent", "CV-1", "--desc", "d"); !strings.HasPrefix(out, "CV-1A ") {
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

// hook.log gets a line per Conveyor-session event (so a missing Stop shows) and none for foreign sessions.
func TestHookLogScope(t *testing.T) {
	home := t.TempDir()
	hook := func(event, cwd string) {
		cmd := exec.Command(bin, "--home", home, "hook", event)
		cmd.Stdin = strings.NewReader(`{"session_id":"s1","cwd":"` + cwd + `"}`)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hook failed: %v %s", err, out)
		}
	}
	hook("Stop", t.TempDir())
	if _, err := os.Stat(filepath.Join(home, "logs", "hook.log")); !os.IsNotExist(err) {
		t.Fatalf("foreign session logged: %v", err)
	}
	hook("Stop", filepath.Join(home, "workspaces", "CV-9", "repo"))
	b, _ := os.ReadFile(filepath.Join(home, "logs", "hook.log"))
	if !strings.Contains(string(b), "CV-9 [Stop] session=s1") {
		t.Fatalf("log: %q", b)
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

// Model names are not validated: agent list resolves aliases, shows the default for a blank, and keeps unknown names.
func TestCLIModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")
	if out := strings.Join(strings.Fields(cv(t, home, "models")), " "); !strings.Contains(out, "claude-fable-5-1 fable fable") || !strings.Contains(out, "(source: builtin)") {
		t.Fatal(out)
	}
	cv(t, home, "config", "set", "default_model", "sonnet")
	cv(t, home, "agent", "add", "a", "--desc", "d", "--prompt", "p", "--model", "opus")
	cv(t, home, "agent", "add", "b", "--desc", "d", "--prompt", "p")
	cv(t, home, "agent", "add", "c", "--desc", "d", "--prompt", "p", "--model", "claude-whatever-1")
	out := cv(t, home, "agent", "list")
	for _, want := range []string{"opus -> claude-opus-5-5", "(default: sonnet)", "claude-whatever-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

// cvFail runs conveyor expecting a non-zero exit and returns its output.
func cvFail(t *testing.T, home string, args ...string) string {
	t.Helper()
	b, err := exec.Command(bin, append([]string{"--home", home}, args...)...).CombinedOutput()
	if err == nil {
		t.Fatalf("conveyor %v: want failure\n%s", args, b)
	}
	return string(b)
}

func TestEpicCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "i"}} {
		if b, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	name := filepath.Base(repo)
	cv(t, home, "repo", "add", repo)

	desc := "## Acceptance criteria\n- [ ] AC1 first\n- [ ] AC2 second\n"
	cv(t, home, "task", "new", "Big thing", "--repo", name, "--epic", "--desc", desc)
	if out := cvFail(t, home, "task", "new", "Clash", "--repo", name, "--epic-name", "cv-1"); !strings.Contains(out, "taken") {
		t.Fatal(out) // default name is the lower-cased key
	}
	cv(t, home, "task", "new", "Build it", "--repo", name, "--parent", "CV-1", "--desc", "covers AC1", "--agent", "sse")
	cv(t, home, "task", "label", "CV-1A", "set", "epic_kind:build")

	// Checks: the child lacks epic:cv-1; AC2 is uncovered.
	out := cvFail(t, home, "epic", "check", "CV-1")
	for _, want := range []string{"CV-1A: missing label epic:cv-1", "AC2 is covered by no build task"} {
		if !strings.Contains(out, want) {
			t.Fatalf("check missing %q:\n%s", want, out)
		}
	}
	out = cvFail(t, home, "epic", "check", "CV-1", "--fix")
	if strings.Contains(out, "missing label") || !strings.Contains(out, "AC2") {
		t.Fatalf("--fix should repair labels only:\n%s", out)
	}
	cv(t, home, "task", "label", "CV-1A", "set", "epic_kind:research") // prefix swap keeps epic:cv-1
	if out := cv(t, home, "task", "show", "CV-1A"); !strings.Contains(out, "epic:cv-1") || !strings.Contains(out, "epic_kind:research") {
		t.Fatal(out)
	}

	// Status JSON, approve, restart.
	var st struct {
		Stage string
		Flow  *struct{ Attempt int }
		Tasks []struct{ Key string }
	}
	if err := json.Unmarshal([]byte(cv(t, home, "epic", "status", "CV-1", "--json")), &st); err != nil || st.Stage != "chart" || st.Flow != nil || len(st.Tasks) != 1 {
		t.Fatalf("status: %v %+v", err, st)
	}
	cv(t, home, "epic", "approve", "CV-1")
	cvFail(t, home, "epic", "approve", "CV-1") // only once
	if out := cv(t, home, "task", "show", "CV-1"); !strings.Contains(out, "epic_stage:decide") {
		t.Fatal(out)
	}
	cvFail(t, home, "epic", "restart", "CV-1") // reason required
	cv(t, home, "epic", "restart", "CV-1", "--reason", "wrong plan")
	if out := cv(t, home, "epic", "flows", "CV-1"); !strings.Contains(out, "flow 1  abandoned") || !strings.Contains(out, "flow 2  active") {
		t.Fatal(out)
	}
	if out := cv(t, home, "task", "show", "CV-1A"); !strings.Contains(out, "cancelled") {
		t.Fatal(out) // open sub-tasks are cancelled
	}
	if out := cv(t, home, "task", "graph", "CV-1A"); !strings.Contains(out, "CV-1  ") || !strings.Contains(out, "(child of CV-1)") {
		t.Fatal(out)
	}
}

// Hook, store and CLI actions of an epic member land in audit.jsonl; epic audit filters by flow and since.
func TestEpicAuditCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "i"}} {
		if b, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	name := filepath.Base(repo)
	cv(t, home, "repo", "add", repo)
	cv(t, home, "task", "new", "Big thing", "--repo", name, "--epic", "--desc", "## Acceptance criteria\n- [ ] AC1 x\n")
	cv(t, home, "task", "new", "Build it", "--repo", name, "--parent", "CV-1", "--desc", "AC1")
	cv(t, home, "task", "label", "CV-1A", "set", "epic:cv-1")
	cv(t, home, "epic", "approve", "CV-1") // flow 1

	cwd := filepath.Join(home, "workspaces", "CV-1A")
	os.MkdirAll(cwd, 0o755)
	hookEvent := func(event, payload string) string {
		c := exec.Command(bin, "--home", home, "hook", event)
		c.Stdin = strings.NewReader(payload)
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("hook %s: %v\n%s", event, err, b)
		}
		return string(b)
	}
	pay := func(extra string) string { return `{"session_id":"S1","cwd":"` + cwd + `"` + extra + `}` }
	hookEvent("UserPromptSubmit", pay(""))
	hookEvent("PreToolUse", pay(`,"tool_name":"Bash","tool_input":{"command":"git push origin HEAD"}`))
	if out := hookEvent("PreToolUse", pay(`,"tool_name":"Bash","tool_input":{"command":"gh pr merge 3"}`)); !strings.Contains(out, "deny") {
		t.Fatalf("merge not denied: %s", out)
	}
	hookEvent("Stop", pay(`,"last_assistant_message":"<conveyor>\nstatus: done\nsummary: s\n</conveyor>"`))
	cv(t, home, "epic", "log", "CV-1", "chose option B")
	cv(t, home, "epic", "restart", "CV-1", "--reason", "wrong plan") // flow 2

	events := func(args ...string) (ev []string) {
		for _, l := range strings.Split(strings.TrimSpace(cv(t, home, append([]string{"epic", "audit", "CV-1A", "--json"}, args...)...)), "\n") {
			var r map[string]any
			if err := json.Unmarshal([]byte(l), &r); err != nil || r["root"] != "CV-1" || r["epic"] != "cv-1" {
				t.Fatalf("bad record %q: %v", l, err)
			}
			ev = append(ev, r["event"].(string))
		}
		return
	}
	all := strings.Join(events(), " ")
	for _, want := range []string{"epic.run.start", "epic.command", "epic.merge_denied", "epic.run.end", "epic.status_block", "epic.log", "epic.flow.end", "epic.flow.start"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s in %s", want, all)
		}
	}
	if f1 := strings.Join(events("--flow", "1"), " "); !strings.Contains(f1, "epic.run.start") || strings.Count(f1, "epic.flow.start") != 1 {
		t.Errorf("flow 1: %s", f1)
	}
	if f2 := events("--flow", "2"); len(f2) != 1 || f2[0] != "epic.flow.start" {
		t.Errorf("flow 2: %v", f2)
	}
	if out := cv(t, home, "epic", "audit", "CV-1", "--since", "1h"); !strings.Contains(out, "chose option B") {
		t.Errorf("since 1h: %s", out)
	}
	time.Sleep(1100 * time.Millisecond)
	if out := strings.TrimSpace(cv(t, home, "epic", "audit", "CV-1", "--since", "1s")); out != "" {
		t.Errorf("since 1s should be empty: %s", out)
	}
}
