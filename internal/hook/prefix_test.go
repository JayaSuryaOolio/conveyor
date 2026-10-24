package hook

import (
	"encoding/json"
	"testing"
)

const pre = "ai agent: "

func TestMissesPrefix(t *testing.T) {
	for _, c := range []struct {
		name, cmd string
		deny      bool
	}{
		{"pr comment ok", `gh pr comment 12 --body "ai agent: done"`, false},
		{"pr comment bad", `gh pr comment 12 --body "done"`, true},
		{"pr comment single quotes ok", `gh pr comment 12 --body 'ai agent: done'`, false},
		{"pr comment single quotes bad", `gh pr comment 12 --body 'done'`, true},
		{"short flag bad", `gh pr comment 12 -b "done"`, true},
		{"short flag ok", `gh pr comment 12 -b "ai agent: hi"`, false},
		{"equals form bad", `gh pr comment 12 --body="done"`, true},
		{"equals form ok", `gh issue comment 3 --body="ai agent: x"`, false},
		{"issue comment bad", `gh issue comment 3 -b nope`, true},
		{"pr review bad", `gh pr review 4 --approve --body "lgtm"`, true},
		{"pr review no body", `gh pr review 4 --approve`, false},
		{"repo flag first", `gh -R o/r pr comment 1 --body hi`, true},
		{"abs path gh", `/opt/homebrew/bin/gh pr comment 1 --body hi`, true},
		{"chained", `git push && gh pr comment 1 --body "hi"`, true},
		{"chained ok", `git push && gh pr comment 1 --body "ai agent: hi"`, false},
		{"env prefix", `GH_TOKEN=x gh pr comment 1 --body hi`, true},
		{"body-file allowed", `gh pr comment 1 --body-file notes.md`, false},
		{"heredoc allowed", "gh pr comment 1 --body \"$(cat <<'EOF'\nhello\nEOF\n)\"", false},
		{"var allowed", `gh pr comment 1 --body "$MSG"`, false},
		{"quoted ; stays in body", `gh pr comment 1 --body "ai agent: a; b"`, false},
		{"api -f bad", `gh api repos/o/r/issues/1/comments -f body="hi"`, true},
		{"api -f ok", `gh api repos/o/r/issues/1/comments -f body="ai agent: hi"`, false},
		{"api -F bad", `gh api repos/o/r/pulls/1/comments -F body='hi'`, true},
		{"api --field bad", `gh api repos/o/r/pulls/1/reviews --field body=hi`, true},
		{"api explicit POST bad", `gh api -X POST repos/o/r/issues/1/comments -f body=hi`, true},
		{"api reply bad", `gh api repos/o/r/pulls/1/comments/9/replies -f body=hi`, true},
		{"api file allowed", `gh api repos/o/r/issues/1/comments -F body=@msg.md`, false},
		{"api GET allowed", `gh api -X GET repos/o/r/issues/1/comments -f body=hi`, false},
		{"api other endpoint", `gh api repos/o/r/issues/1/labels -f body=hi`, false},
		{"api no body", `gh api repos/o/r/issues/1/comments`, false},
		{"unrelated", `ls -la && git status`, false},
		{"gh unrelated", `gh pr view 1 --json body`, false},
		{"echo mentions gh", `echo "gh pr comment --body hi"`, false},
	} {
		if got := missesPrefix(c.cmd, pre); got != c.deny {
			t.Errorf("%s: missesPrefix=%v want %v (%s)", c.name, got, c.deny, c.cmd)
		}
	}
}

func TestPreToolUse(t *testing.T) {
	in := func(tool, cmd string) []byte {
		b, _ := json.Marshal(map[string]any{"tool_name": tool, "tool_input": map[string]any{"command": cmd}})
		return b
	}
	p := func() string { return pre }
	if got := PreToolUse(in("Bash", "ls"), func() string { t.Error("prefix looked up for non-gh"); return pre }); got != nil {
		t.Errorf("allow should be silent, got %s", got)
	}
	if got := PreToolUse(in("Bash", "gh pr comment 1 --body hi"), func() string { return "" }); got != nil {
		t.Errorf("empty prefix disables the check, got %s", got)
	}
	if got := PreToolUse(in("Edit", "gh pr comment 1 --body hi"), p); got != nil {
		t.Errorf("non-Bash allowed, got %s", got)
	}
	if got := PreToolUse([]byte("{"), p); got != nil {
		t.Errorf("bad json allowed, got %s", got)
	}
	raw := PreToolUse(in("Bash", "gh pr comment 1 --body hi"), p)
	var m map[string]map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("deny not JSON: %s", raw)
	}
	h := m["hookSpecificOutput"]
	if h["hookEventName"] != "PreToolUse" || h["permissionDecision"] != "deny" || h["permissionDecisionReason"] == "" {
		t.Errorf("bad deny output: %s", raw)
	}
}
