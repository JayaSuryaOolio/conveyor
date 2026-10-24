package hook

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// PreToolUse decides a Bash PreToolUse hook call. It returns the JSON for Claude Code to deny a GitHub
// comment whose body lacks the pr_comment_prefix, and nil (allow, no output) for everything else.
// prefix is a func so the settings DB is only opened for commands that mention gh.
func PreToolUse(raw []byte, prefix func() string) []byte {
	var p struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ToolName != "Bash" || !strings.Contains(p.ToolInput.Command, "gh") {
		return nil
	}
	pre := prefix()
	if pre == "" || !missesPrefix(p.ToolInput.Command, pre) {
		return nil
	}
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": "Every comment or reply on a PR or issue must start with `" + pre + "`. Run the command again with the body beginning with `" + pre + "`.",
	}})
	return out
}

// missesPrefix reports whether any gh invocation in cmd posts an inspectable body without the prefix.
func missesPrefix(cmd, prefix string) bool {
	for _, seg := range tokenize(cmd) {
		for len(seg) > 0 && strings.Contains(seg[0], "=") && !strings.HasPrefix(seg[0], "-") { // FOO=bar gh ...
			seg = seg[1:]
		}
		if len(seg) == 0 || filepath.Base(seg[0]) != "gh" {
			continue
		}
		if b, ok := ghBody(seg[1:]); ok && inspectable(b) && !strings.HasPrefix(b, prefix) {
			return true
		}
	}
	return false
}

// inspectable is false for bodies built by the shell ($(cat <<EOF ...), $VAR) or read from a file.
// ponytail: those bodies are allowed and only covered by the brief rule; upgrade by parsing heredocs
// and --body-file contents if agents routinely slip through.
func inspectable(b string) bool {
	return !strings.HasPrefix(b, "$") && !strings.HasPrefix(b, "`") && !strings.HasPrefix(b, "@")
}

// ghBody extracts the comment body from the args of a gh command that posts one.
func ghBody(args []string) (string, bool) {
	for i := 0; i+1 < len(args); i++ {
		switch a, b := args[i], args[i+1]; {
		case a == "pr" && (b == "comment" || b == "review"), a == "issue" && b == "comment":
			return flagValue(args[i+2:], "-b", "--body")
		case a == "api":
			return apiBody(args[i+1:])
		}
	}
	return "", false
}

// flagValue finds `--body v`, `--body=v`, `-b v` or `-bv`.
func flagValue(args []string, short, long string) (string, bool) {
	for i, a := range args {
		switch {
		case a == short || a == long:
			if i+1 < len(args) {
				return args[i+1], true
			}
		case strings.HasPrefix(a, long+"="):
			return a[len(long)+1:], true
		case strings.HasPrefix(a, short) && !strings.HasPrefix(a, "--"):
			return a[len(short):], true
		}
	}
	return "", false
}

// apiBody handles `gh api <endpoint> [-X POST] -f body=...` for comment, reply and review endpoints.
func apiBody(args []string) (string, bool) {
	var endpoint, body string
	var found bool
	post := true // fields imply POST unless -X says otherwise
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		var field string
		switch {
		case a == "-X" || a == "--method":
			post = strings.EqualFold(next(), "POST")
		case strings.HasPrefix(a, "--method="):
			post = strings.EqualFold(a[9:], "POST")
		case a == "-f" || a == "-F" || a == "--field" || a == "--raw-field":
			field = next()
		case strings.HasPrefix(a, "--field="), strings.HasPrefix(a, "--raw-field="):
			field = a[strings.IndexByte(a, '=')+1:]
		case len(a) > 2 && (strings.HasPrefix(a, "-f") || strings.HasPrefix(a, "-F")) && a[2] != '-':
			field = a[2:]
		case !strings.HasPrefix(a, "-") && endpoint == "":
			endpoint = a
		}
		if v, ok := strings.CutPrefix(field, "body="); ok {
			body, found = v, true
		}
	}
	e, _, _ := strings.Cut(endpoint, "?")
	e = strings.TrimRight(e, "/")
	if !post || !found || !(strings.HasSuffix(e, "/comments") || strings.HasSuffix(e, "/reviews") || strings.HasSuffix(e, "/replies")) {
		return "", false
	}
	return body, true
}

// tokenize splits a shell command into segments (at ; & | newline) of quote-aware words.
// ponytail: not a shell parser (no nesting, expansion or heredoc bodies); good enough for gh one-liners.
func tokenize(s string) [][]string {
	var segs [][]string
	var cur []string
	var w strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			cur = append(cur, w.String())
			w.Reset()
			inWord = false
		}
	}
	endSeg := func() {
		flush()
		if len(cur) > 0 {
			segs = append(segs, cur)
			cur = nil
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			inWord = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				j = len(s) - i - 1
			}
			w.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			inWord = true
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					i++
				}
				w.WriteByte(s[i])
			}
		case c == '\\' && i+1 < len(s):
			inWord = true
			i++
			w.WriteByte(s[i])
		case c == ' ' || c == '\t':
			flush()
		case c == ';' || c == '&' || c == '|' || c == '\n':
			endSeg()
		default:
			inWord = true
			w.WriteByte(c)
		}
	}
	endSeg()
	return segs
}
