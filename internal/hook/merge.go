package hook

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

// EpicMember reports whether cwd is inside the workspace of a task that belongs to an epic.
func EpicMember(db *sql.DB, home, cwd string) bool {
	key := workspace.KeyFromCwd(home, cwd)
	if key == "" {
		return false
	}
	t, err := store.GetTaskByKey(db, key)
	return err == nil && store.IsEpicMember(t)
}

// mergesPR reports whether any gh invocation in cmd merges a PR (`gh pr merge`, `gh api .../merge`).
// ponytail: argument scan, not a shell parser; `bash -c "gh pr merge"` wrappers and aliases slip through.
// Upgrade to a GitHub token without merge rights if agents route around it.
func mergesPR(cmd string) bool {
	for _, seg := range tokenize(cmd) {
		seg = stripEnv(seg)
		if len(seg) == 0 || filepath.Base(seg[0]) != "gh" {
			continue
		}
		args := seg[1:]
		for i, a := range args {
			if a != "pr" && a != "api" {
				continue
			}
			if w := firstArg(args[i+1:]); (a == "pr" && w == "merge") ||
				(a == "api" && strings.HasSuffix(strings.TrimRight(strings.SplitN(w, "?", 2)[0], "/"), "/merge")) {
				return true
			}
		}
	}
	return false
}

// firstArg is the first word that is not a flag or a -R/--repo value.
func firstArg(args []string) string {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-R" || a == "--repo":
			i++
		case !strings.HasPrefix(a, "-"):
			return a
		}
	}
	return ""
}

// AuditCommand records the conveyor, gh and git push commands of the run at cwd, and a denied merge.
// The audit writer drops runs outside an epic.
func AuditCommand(home string, raw []byte, denied bool) {
	var p struct {
		ToolName  string `json:"tool_name"`
		Cwd       string `json:"cwd"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(raw, &p) != nil || p.ToolName != "Bash" {
		return
	}
	cmd := p.ToolInput.Command
	key := workspace.KeyFromCwd(home, p.Cwd)
	if key == "" || !tracked(cmd) {
		return
	}
	store.Audit("epic.command", "task", key, "msg", "command", "command", cmd[:min(len(cmd), 300)])
	if denied && mergesPR(cmd) {
		store.Audit("epic.merge_denied", "task", key, "command", cmd[:min(len(cmd), 300)])
	}
}

// tracked reports whether any segment runs conveyor, gh or git push.
func tracked(cmd string) bool {
	for _, seg := range tokenize(cmd) {
		seg = stripEnv(seg)
		if len(seg) == 0 {
			continue
		}
		switch filepath.Base(seg[0]) {
		case "conveyor", "gh":
			return true
		case "git":
			for _, a := range seg[1:] {
				if a == "push" {
					return true
				}
			}
		}
	}
	return false
}
