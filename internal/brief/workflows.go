package brief

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

const (
	maxWorkflows = 30
	maxDescRunes = 100
)

// truncate cuts s to n runes (never mid-rune), marking the cut with an ellipsis.
func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// entry is one index line: "- <label>: <description>". Skills use name/description frontmatter.
func entry(file, kind, fallbackName string) (string, bool) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	fm, body := claude.Frontmatter(b)
	name, desc := fallbackName, oneLine(fm["description"])
	if kind == "skill" && fm["name"] != "" {
		name = fm["name"]
	}
	if strings.Trim(desc, "|>+-") == "" { // YAML block scalar marker, not a description
		desc = ""
	}
	if desc == "" {
		for _, l := range strings.Split(body, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				desc = oneLine(l)
				break
			}
		}
	}
	line := fmt.Sprintf("- %s `%s`", kind, name)
	if desc != "" {
		line += ": " + truncate(desc, maxDescRunes)
	}
	return line, true
}

// workflows indexes each task repo's .claude commands and skills (names and one-liners, no bodies).
func workflows(db *sql.DB, task store.Task) string {
	var b strings.Builder
	for _, r := range task.Repos {
		root := deref(r.WorktreePath)
		if st, err := os.Stat(root); root == "" || err != nil || !st.IsDir() {
			repo, err := store.GetRepoByName(db, r.RepoName)
			if err != nil {
				continue
			}
			root = repo.Path
		}
		var lines []string
		cmds, _ := filepath.Glob(filepath.Join(root, ".claude", "commands", "*.md"))
		for _, f := range cmds {
			if l, ok := entry(f, "command", "/"+strings.TrimSuffix(filepath.Base(f), ".md")); ok {
				lines = append(lines, l)
			}
		}
		skills, _ := filepath.Glob(filepath.Join(root, ".claude", "skills", "*", "SKILL.md"))
		for _, f := range skills {
			if l, ok := entry(f, "skill", filepath.Base(filepath.Dir(f))); ok {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			continue
		}
		if len(lines) > maxWorkflows {
			n := len(lines) - maxWorkflows
			lines = append(lines[:maxWorkflows], fmt.Sprintf("- (%d more omitted)", n))
		}
		fmt.Fprintf(&b, "%s:\n%s\n", r.RepoName, strings.Join(lines, "\n"))
	}
	return b.String()
}
