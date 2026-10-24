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

const statusBlock = `## Status block

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
`

const defaultRules = `- Work only inside the worktrees listed above.
- Open at most one PR per repo.
- Never push to the default branch.
- Prefer the repo's own commands and skills over ad-hoc steps. When one matches what you are doing (e.g. creating a PR, adding tests, writing a changeset), use it: run the slash command via the Skill tool, or Read the command file and follow it.
`

// Built-in prompt templates; empty prompt_fresh / prompt_resume settings use these.
const (
	DefaultFresh  = "You are working on Conveyor task {key}: {title}. Read {brief} first and follow it.\n{comments}"
	DefaultResume = "New comments on {key}:\n{comments}\n(The brief at {brief} is up to date.)"
)

// rules is the Rules section body: the defaults, the PR comment prefix rule, then (if extra) the user's extra rules.
func rules(db *sql.DB, extra bool) string {
	r := defaultRules
	if p := store.GetSetting(db, "pr_comment_prefix"); p != "" {
		r += fmt.Sprintf("- Start every comment or reply you post on a PR or issue with `%s`.\n", p)
	}
	if x := strings.TrimSpace(store.GetSetting(db, "brief_extra_rules")); extra && x != "" {
		r += x + "\n"
	}
	return r
}

// Protocol is the status-block protocol and the built-in rules (extra rules excluded), for display.
func Protocol(db *sql.DB) string { return statusBlock + "\n## Rules\n\n" + rules(db, false) }

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
	text, err := Build(db, task)
	if err != nil {
		return "", err
	}
	dir := workspace.Dir(home, task.Key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create workspace dir: %w", err)
	}
	path := filepath.Join(dir, "BRIEF.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("write brief: %w", err)
	}
	return path, nil
}

// Path is where Render writes the task's brief.
func Path(home string, task store.Task) string {
	return filepath.Join(workspace.Dir(home, task.Key), "BRIEF.md")
}

// Build renders the brief text without writing anything.
func Build(db *sql.DB, task store.Task) (string, error) {
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
	fmt.Fprintf(&b, "\n%s\n## Rules\n\n%s\n## Older history\n\nFull thread: `conveyor task show %s --thread --json`\n", statusBlock, rules(db, true), task.Key)
	return b.String(), nil
}

func deltaText(delta []store.Comment) string {
	lines := make([]string, len(delta))
	for i, c := range delta {
		lines[i] = fmt.Sprintf("- [%s] %s", c.Author, c.Body)
	}
	return strings.Join(lines, "\n")
}

func fill(tmpl, def string, task store.Task, briefPath string, delta []store.Comment) string {
	if strings.TrimSpace(tmpl) == "" {
		tmpl = def
	}
	return strings.TrimSpace(strings.NewReplacer("{key}", task.Key, "{title}", task.Title, "{brief}", briefPath, "{comments}", deltaText(delta)).Replace(tmpl))
}

// FreshPrompt is the first prompt of a fresh session; tmpl is the prompt_fresh setting.
func FreshPrompt(task store.Task, briefPath string, delta []store.Comment, tmpl string) string {
	return fill(tmpl, DefaultFresh, task, briefPath, delta)
}

// ResumePrompt is the prompt that resumes a hot session with new comments; tmpl is the prompt_resume setting.
func ResumePrompt(task store.Task, briefPath string, delta []store.Comment, tmpl string) string {
	return fill(tmpl, DefaultResume, task, briefPath, delta)
}

// Preview renders the brief and the fresh prompt as a launch would, writing nothing.
func Preview(db *sql.DB, home string, task store.Task) (text, prompt string, err error) {
	if text, err = Build(db, task); err != nil {
		return "", "", err
	}
	delta, err := store.UnconsumedComments(db, task.ID)
	if err != nil {
		return "", "", err
	}
	return text, FreshPrompt(task, Path(home, task), delta, store.GetSetting(db, "prompt_fresh")), nil
}
