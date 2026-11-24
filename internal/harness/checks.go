package harness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/transcript"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

// Check runs every check over f, per task repo first and then the transcript ones. A check whose
// input errored (unreadable worktree or transcript) is skipped rather than guessed at.
func Check(f *Facts) []Gap {
	var gaps []Gap
	add := func(check, format string, a ...any) { gaps = append(gaps, Gap{check, fmt.Sprintf(format, a...)}) }
	for _, r := range f.Repos {
		if r.Err != nil {
			continue
		}
		if r.Dirty {
			add("dirty", "%s: the worktree has uncommitted changes; commit or discard them", r.Name)
		}
		if r.Ahead > 0 {
			if r.Unpushed {
				add("unpushed", "%s: branch %s has commits not pushed to origin; push it", r.Name, r.Branch)
			}
			if f.WantPR && !r.HasPR {
				add("no-pr", "%s: branch %s is %d commit(s) ahead of %s but has no PR; open one", r.Name, r.Branch, r.Ahead, r.Default)
			}
		}
		if r.NeedsChangeset {
			add("changeset", "%s: code changed but no .changeset/*.md was added; add one with the repo's changeset command, or say in the summary why it needs none", r.Name)
		}
		if md := filepath.Join(r.Path, "CLAUDE.md"); r.HasMD && (r.Dirty || r.Ahead > 0) && !mdLoaded(md, f) {
			add("claude-md", "%s: you changed this repo without loading its CLAUDE.md; read %s and check the work against it", r.Name, md)
		}
	}
	if f.UsesErr == nil {
		if file := UntestedEdit(f.Uses); file != "" {
			add("untested-edit", "you edited %s after the last test/build/lint command; run the repo's checks again and fix what fails", file)
		}
		for _, s := range briefSkills(f.Task) {
			if !skillUsed(s, f.Uses) {
				add("skills", "the brief lists skill %q but no Skill tool call loaded it; load it and follow it", s)
			}
		}
	}
	return gaps
}

// mdLoaded: Claude Code reported the file through an InstructionsLoaded hook during this run, or the agent read it.
func mdLoaded(md string, f *Facts) bool {
	for _, e := range f.Events {
		if e.Kind == "instructions_loaded" && e.CreatedAt >= f.RunStart && filepath.Clean(e.Detail) == md {
			return true
		}
	}
	for _, u := range f.Uses {
		if u.Name == "Read" && filepath.Clean(u.FilePath) == md {
			return true
		}
	}
	return false
}

// briefSkills mirrors brief.labelLines: skill:<name> labels, plus the epic skill for an epic task.
func briefSkills(task store.Task) []string {
	var out []string
	if store.EpicName(task) != "" {
		out = append(out, "epic")
	}
	for _, l := range task.Labels {
		if s, ok := strings.CutPrefix(l, "skill:"); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func skillUsed(s string, uses []transcript.ToolUse) bool {
	for _, u := range uses {
		if u.Name == "Skill" && (u.Skill == s || strings.HasSuffix(u.Skill, ":"+s)) {
			return true
		}
	}
	return false
}

// needsChangeset: the branch changes code files but adds no changeset.
func needsChangeset(wt, base string) bool {
	out, err := workspace.Git(wt, "diff", "--name-only", "--diff-filter=AM", base+"...HEAD")
	if err != nil {
		return false
	}
	code := false
	for _, f := range strings.Split(out, "\n") {
		if strings.HasPrefix(f, ".changeset/") && strings.HasSuffix(f, ".md") {
			return false
		}
		code = code || codeExt[filepath.Ext(f)]
	}
	return code
}

var codeExt = map[string]bool{".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".py": true, ".rs": true, ".java": true, ".kt": true, ".swift": true, ".rb": true, ".cs": true, ".sql": true}

// checkCmd matches a Bash command that plausibly ran the repo's tests, build or linters.
// ponytail: keyword heuristic, not the repo's real test command; read it from repo config if it misfires.
func checkCmd(c string) bool {
	for _, k := range []string{"test", "build", "vet", "lint", "tsc", "check", "make"} {
		if strings.Contains(c, k) {
			return true
		}
	}
	return false
}

// UntestedEdit returns the last code file edited after the last check command, or "".
func UntestedEdit(uses []transcript.ToolUse) string {
	last := ""
	for _, u := range uses {
		switch {
		case u.Name == "Bash" && checkCmd(u.Command):
			last = ""
		case (u.Name == "Edit" || u.Name == "Write" || u.Name == "MultiEdit") && codeExt[filepath.Ext(u.FilePath)]:
			last = u.FilePath
		}
	}
	return last
}
