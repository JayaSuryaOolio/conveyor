package hook

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/statusblock"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/transcript"
)

// completionGaps lists what an auto run reporting status done still owes: per task repo a clean worktree,
// a pushed branch, a PR for commits ahead of the default branch and, for a changed repo, its CLAUDE.md
// loaded; plus a Skill call for each skill the brief lists. prSynced is false when the PR refresh failed,
// which skips the PR check rather than nag about a PR gh could not see. Runs once per stop: a session
// already continuing from this hook (stop_hook_active) finishes whatever it still lacks.
func completionGaps(db *sql.DB, task store.Task, run store.Run, p payload, prSynced bool) ([]string, error) {
	block, _, ok := statusblock.Parse(p.LastAssistantMessage)
	if run.Kind != "auto" || !ok || block.Status != "done" || p.StopHookActive {
		return nil, nil
	}
	task, err := store.GetTask(db, task.ID) // refreshPR may have just set pr_url
	if err != nil {
		return nil, err
	}
	repos, err := store.ListRepos(db)
	if err != nil {
		return nil, err
	}
	def := map[int64]string{}
	for _, r := range repos {
		def[r.ID] = r.DefaultBranch
	}
	var uses []transcript.ToolUse
	usesOK := false
	if p.TranscriptPath != "" {
		uses, err = transcript.ToolUses(p.TranscriptPath)
		usesOK = err == nil
	}
	events, err := store.ListEvents(db, task.ID)
	if err != nil {
		return nil, err
	}
	kind := store.LabelValue(task, store.PrefixKind)
	wantPR := prSynced && kind != "prototype" && kind != "research" // their kind rules say open no PR

	var gaps []string
	for _, tr := range task.Repos {
		if tr.WorktreePath == nil {
			continue
		}
		wt, name := *tr.WorktreePath, tr.RepoName
		dirty, err := git(wt, "status", "--porcelain")
		if err != nil {
			continue // not a worktree (any more): nothing to check
		}
		if dirty != "" {
			gaps = append(gaps, fmt.Sprintf("%s: the worktree has uncommitted changes; commit or discard them", name))
		}
		ahead := 0
		if out, err := git(wt, "rev-list", "--count", "origin/"+def[tr.RepoID]+"..HEAD"); err == nil {
			ahead, _ = strconv.Atoi(out)
		}
		if ahead > 0 {
			if out, err := git(wt, "rev-list", "--count", "origin/"+tr.Branch+"..HEAD"); err != nil || out != "0" {
				gaps = append(gaps, fmt.Sprintf("%s: branch %s has commits not pushed to origin; push it", name, tr.Branch))
			}
			if wantPR && tr.PRURL == nil {
				gaps = append(gaps, fmt.Sprintf("%s: branch %s is %d commit(s) ahead of %s but has no PR; open one", name, tr.Branch, ahead, def[tr.RepoID]))
			}
		}
		if ahead > 0 && fileExists(filepath.Join(wt, ".changeset", "config.json")) && needsChangeset(wt, "origin/"+def[tr.RepoID]) {
			gaps = append(gaps, fmt.Sprintf("%s: code changed but no .changeset/*.md was added; add one with the repo's changeset command, or say in the summary why it needs none", name))
		}
		md := filepath.Join(wt, "CLAUDE.md")
		if _, err := os.Stat(md); err == nil && (dirty != "" || ahead > 0) && !mdLoaded(md, run, events, uses) {
			gaps = append(gaps, fmt.Sprintf("%s: you changed this repo without loading its CLAUDE.md; read %s and check the work against it", name, md))
		}
	}
	if usesOK {
		if f := untestedEdit(uses); f != "" {
			gaps = append(gaps, fmt.Sprintf("you edited %s after the last test/build/lint command; run the repo's checks again and fix what fails", f))
		}
		for _, s := range briefSkills(task) {
			if !skillUsed(s, uses) {
				gaps = append(gaps, fmt.Sprintf("the brief lists skill %q but no Skill tool call loaded it; load it and follow it", s))
			}
		}
	}
	return gaps, nil
}

// mdLoaded: Claude Code reported the file through an InstructionsLoaded hook during this run, or the agent read it.
func mdLoaded(md string, run store.Run, events []store.Event, uses []transcript.ToolUse) bool {
	for _, e := range events {
		if e.Kind == "instructions_loaded" && e.CreatedAt >= run.StartedAt && filepath.Clean(e.Detail) == md {
			return true
		}
	}
	for _, u := range uses {
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
	out, err := git(wt, "diff", "--name-only", "--diff-filter=AM", base+"...HEAD")
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

// untestedEdit returns the last code file edited after the last check command, or "".
func untestedEdit(uses []transcript.ToolUse) string {
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

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// stopBlock is the Stop hook answer that keeps the session going with the gaps as its next prompt.
func stopBlock(gaps []string) string {
	reason := "Conveyor's completion check found work left before this run can finish:\n- " + strings.Join(gaps, "\n- ") +
		"\nFix these, then end with the status block again. If one cannot be fixed, say why and report status: failed."
	b, _ := json.Marshal(map[string]string{"decision": "block", "reason": reason})
	return string(b)
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
