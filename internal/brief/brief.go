// Package brief renders BRIEF.md, the hot-tier context an agent reads first, and the launch prompts.
package brief

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

const maxComments = 20

const protocol = `## Status block

End every final reply with this block. Omit it to ask the human a question instead.

` + "```" + `
<conveyor>
status: done | failed
summary: one paragraph
state: branch ..., PR #N, tests: pass|fail|not run
next: what remains
criteria:
- [x] AC1 - evidence
- [ ] AC2 - why not
learning: a lesson (repeatable line)
spawn: child task title (repeatable line)
</conveyor>
` + "```" + `

## Rules

- Work only inside the worktrees listed above.
- Open at most one PR per repo.
- Never push to the default branch.
- Prefer the repo's own commands and skills over ad-hoc steps. When one matches what you are doing (e.g. creating a PR, adding tests, writing a changeset), use it: run the slash command via the Skill tool, or Read the command file and follow it.

## Older history

Full thread: ` + "`conveyor task show %s --thread --json`" + `
`

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Render writes <ws>/BRIEF.md and returns its path.
func Render(db *sql.DB, home string, task store.Task) (string, error) {
	runs, err := store.ListRuns(db, task.ID)
	if err != nil {
		return "", err
	}
	comments, err := store.ListComments(db, task.ID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nKey: %s\n\n## Repos\n\n", task.Title, task.Key)
	for _, r := range task.Repos {
		pr := "none"
		if r.PRURL != nil {
			pr = *r.PRURL
		}
		fmt.Fprintf(&b, "- %s: %s (branch `%s`, PR: %s)\n", r.RepoName, deref(r.WorktreePath), r.Branch, pr)
	}
	desc := strings.TrimSpace(task.Description)
	if desc == "" {
		desc = "(none)"
	}
	fmt.Fprintf(&b, "\n## Description\n\n%s\n\n## Acceptance criteria\n\n", desc)
	criteria := "see description"
	for i := len(runs) - 1; i >= 0; i-- {
		if c := strings.TrimSpace(deref(runs[i].Criteria)); c != "" {
			criteria = c
			break
		}
	}
	fmt.Fprintf(&b, "%s\n", criteria)
	if w := workflows(db, task); w != "" {
		fmt.Fprintf(&b, "\n## Repo workflows\n\n%s", w)
	}
	if len(runs) > 0 {
		last := runs[len(runs)-1]
		fmt.Fprintf(&b, "\n## Latest run\n\nState: %s\nNext: %s\n", oneLine(deref(last.State)), oneLine(deref(last.Next)))
	}
	var sums []string
	for _, r := range runs {
		if s := oneLine(deref(r.Summary)); s != "" {
			sums = append(sums, fmt.Sprintf("- run %d: %s", r.ID, s))
		}
	}
	if len(sums) > 3 {
		sums = sums[len(sums)-3:]
	}
	if len(sums) > 0 {
		fmt.Fprintf(&b, "\n## Recent runs\n\n%s\n", strings.Join(sums, "\n"))
	}
	var human []string
	for _, c := range comments {
		if c.Author == store.AuthorHuman {
			human = append(human, "- "+firstLine(c.Body))
		}
	}
	if len(human) > maxComments {
		human = human[len(human)-maxComments:]
	}
	if len(human) > 0 {
		fmt.Fprintf(&b, "\n## Human comments\n\n%s\n", strings.Join(human, "\n"))
	}
	fmt.Fprintf(&b, "\n"+protocol, task.Key)

	dir := workspace.Dir(home, task.Key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create workspace dir: %w", err)
	}
	path := filepath.Join(dir, "BRIEF.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write brief: %w", err)
	}
	return path, nil
}

func deltaText(delta []store.Comment) string {
	lines := make([]string, len(delta))
	for i, c := range delta {
		lines[i] = fmt.Sprintf("- [%s] %s", c.Author, c.Body)
	}
	return strings.Join(lines, "\n")
}

// FreshPrompt is the first prompt of a fresh session.
func FreshPrompt(task store.Task, briefPath string, delta []store.Comment) string {
	p := fmt.Sprintf("You are working on Conveyor task %s: %s. Read %s first and follow it.", task.Key, task.Title, briefPath)
	if len(delta) > 0 {
		p += "\n" + deltaText(delta)
	}
	return p
}

// ResumePrompt is the prompt that resumes a hot session with new comments.
func ResumePrompt(task store.Task, briefPath string, delta []store.Comment) string {
	return fmt.Sprintf("New comments on %s:\n%s\n(The brief at %s is up to date.)", task.Key, deltaText(delta), briefPath)
}
