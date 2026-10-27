// Package workspace manages a task's workspace folder: one git worktree per repo.
package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ErrDirty is returned by Remove when a worktree has uncommitted changes.
var ErrDirty = errors.New("workspace has uncommitted changes")

// Dir returns <home>/workspaces/<KEY>.
func Dir(home, key string) string { return filepath.Join(home, "workspaces", key) }

// LearningsFile is <home>/repos/<repo>/LEARNINGS.md: a repo's accepted learnings, kept out of the repo itself.
func LearningsFile(home, repo string) string {
	return filepath.Join(home, "repos", repo, "LEARNINGS.md")
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, s)
	}
	return s, nil
}

func commonDir(dir string) string {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	return realPath(out)
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// Ensure creates <home>/workspaces/<KEY>/ and, for every task repo without a worktree_path,
// adds a worktree <ws>/<repo.name> on the task branch (see docs/tasks/003-workspace.md)
// and runs the repo's setup script in it. Idempotent. Persists worktree_path via
// store.UpdateTaskRepo only after setup succeeds, so a failed setup reruns next time.
func Ensure(db *sql.DB, home string, task store.Task) (store.Task, error) {
	ws := Dir(home, task.Key)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return task, fmt.Errorf("create workspace dir: %w", err)
	}
	task.Repos = append([]store.TaskRepo(nil), task.Repos...)
	for i := range task.Repos {
		tr := &task.Repos[i]
		if tr.WorktreePath != nil {
			continue
		}
		repo, err := store.GetRepoByName(db, tr.RepoName)
		if err != nil {
			return task, fmt.Errorf("repo %s: %w", tr.RepoName, err)
		}
		path := filepath.Join(ws, repo.Name)
		if _, err := os.Stat(path); err == nil {
			if err := checkAdoptable(repo, tr.Branch, path); err != nil {
				return task, err
			}
		} else if err := addWorktree(repo, tr.Branch, path); err != nil {
			return task, err
		}
		if err := runSetup(ws, repo, path); err != nil {
			return task, err
		}
		tr.WorktreePath = &path
		if err := store.UpdateTaskRepo(db, *tr); err != nil {
			return task, err
		}
	}
	return task, nil
}

// SetupTimeout bounds a repo setup script.
var SetupTimeout = 10 * time.Minute

// runSetup runs repo.SetupScript with bash in the worktree, stdin closed, output to
// <ws>/setup-<repo>.log. A failure returns the log's tail.
// ponytail: runs inside the dispatcher tick, so a slow script delays other launches; launch async if that bites.
func runSetup(ws string, repo store.Repo, path string) error {
	if strings.TrimSpace(repo.SetupScript) == "" {
		return nil
	}
	logPath := filepath.Join(ws, "setup-"+repo.Name+".log")
	ctx, cancel := context.WithTimeout(context.Background(), SetupTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", repo.SetupScript)
	cmd.Dir = path
	cmd.Env = append(os.Environ(), "CONVEYOR_REPO="+repo.Name, "CONVEYOR_REPO_PATH="+repo.Path)
	out, err := cmd.CombinedOutput()
	os.WriteFile(logPath, out, 0o644)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		err = fmt.Errorf("timed out after %s", SetupTimeout)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return fmt.Errorf("setup script for %s failed: %v (log %s):\n%s", repo.Name, err, logPath, strings.Join(lines, "\n"))
}

func checkAdoptable(repo store.Repo, branch, path string) error {
	if commonDir(path) == "" || commonDir(path) != commonDir(repo.Path) {
		return fmt.Errorf("%s exists but is not a worktree of %s", path, repo.Name)
	}
	if b, _ := git(path, "rev-parse", "--abbrev-ref", "HEAD"); b != branch {
		return fmt.Errorf("%s is on branch %q, want %q", path, b, branch)
	}
	return nil
}

func addWorktree(repo store.Repo, branch, path string) error {
	if _, err := git(repo.Path, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		_, err = git(repo.Path, "worktree", "add", path, branch)
		return err
	}
	if _, err := git(repo.Path, "fetch", "origin", repo.DefaultBranch); err != nil {
		log.Printf("workspace: %v", err)
	}
	base := repo.DefaultBranch
	if _, err := git(repo.Path, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+base); err == nil {
		base = "origin/" + base
	}
	_, err := git(repo.Path, "worktree", "add", path, "-b", branch, base)
	return err
}

// PrimaryPath returns the primary repo's worktree path, or "" if none.
func PrimaryPath(task store.Task) string {
	for _, r := range task.Repos {
		if r.IsPrimary && r.WorktreePath != nil {
			return *r.WorktreePath
		}
	}
	return ""
}

// OtherPaths returns the non-primary worktree paths (for --add-dir).
func OtherPaths(task store.Task) []string {
	var out []string
	for _, r := range task.Repos {
		if !r.IsPrimary && r.WorktreePath != nil {
			out = append(out, *r.WorktreePath)
		}
	}
	return out
}

// KeyFromCwd returns the task key if cwd is inside <home>/workspaces/<KEY>/..., else "".
func KeyFromCwd(home, cwd string) string {
	rel, err := filepath.Rel(realPath(filepath.Join(home, "workspaces")), realPath(cwd))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return strings.Split(rel, string(filepath.Separator))[0]
}

// Remove runs `git worktree remove` for each worktree, without --force unless force is set.
// The workspace folder is removed only when every worktree is gone. Dirty worktrees return ErrDirty.
func Remove(db *sql.DB, home string, task store.Task, force bool) error {
	if !force { // check all first so a dirty repo doesn't leave a half-removed workspace
		for _, tr := range task.Repos {
			if tr.WorktreePath == nil {
				continue
			}
			if _, err := os.Stat(*tr.WorktreePath); err != nil {
				continue
			}
			out, err := git(*tr.WorktreePath, "status", "--porcelain")
			if err != nil {
				return err
			}
			if out != "" {
				return fmt.Errorf("%s: %w", tr.RepoName, ErrDirty)
			}
		}
	}
	for _, tr := range task.Repos {
		if tr.WorktreePath == nil {
			continue
		}
		if _, err := os.Stat(*tr.WorktreePath); err == nil {
			repo, err := store.GetRepoByName(db, tr.RepoName)
			if err != nil {
				return fmt.Errorf("repo %s: %w", tr.RepoName, err)
			}
			args := []string{"worktree", "remove"}
			if force {
				args = append(args, "--force")
			}
			if _, err := git(repo.Path, append(args, *tr.WorktreePath)...); err != nil {
				return err
			}
		}
		tr.WorktreePath = nil
		if err := store.UpdateTaskRepo(db, tr); err != nil {
			return err
		}
	}
	os.Remove(Dir(home, task.Key)) // only succeeds when empty
	return nil
}
