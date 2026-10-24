# 003: Workspace folders and per-repo worktrees

**Wave 2** · depends on: 002 · branch `task/003-workspace`

## Goal
`internal/workspace` creates and removes a task's workspace folder, which holds one git worktree per repo (tech-spec §3).

## API
```go
package workspace

// Ensure creates <home>/workspaces/<KEY>/ and, for every task repo without a worktree_path,
// runs `git -C <repo.path> worktree add <ws>/<repo.name> -b <branch> <base>`.
// <base> is `origin/<default_branch>` if that ref exists (after `git fetch origin <default_branch>`,
// whose failure is logged, not fatal), else `<default_branch>`.
// If the branch already exists, use `git worktree add <path> <branch>` without -b.
// If the path already exists and is a worktree of that repo on that branch, adopt it.
// Persists worktree_path via store.UpdateTaskRepo. Idempotent.
func Ensure(db *sql.DB, home string, task store.Task) (store.Task, error)

// Dir returns <home>/workspaces/<KEY>.
func Dir(home, key string) string

// PrimaryPath returns the primary repo's worktree path, and OtherPaths the rest (for --add-dir).
func PrimaryPath(task store.Task) string
func OtherPaths(task store.Task) []string

// KeyFromCwd returns the task key if cwd is inside <home>/workspaces/<KEY>/..., else "".
// Resolve symlinks on both sides (macOS /tmp → /private/tmp).
func KeyFromCwd(home, cwd string) string

// Remove runs `git worktree remove` for each worktree, without --force unless force is set.
// It removes the workspace folder only when every worktree is gone. Dirty worktrees return ErrDirty.
func Remove(db *sql.DB, home string, task store.Task, force bool) error

var ErrDirty = errors.New("workspace has uncommitted changes")
```

## Notes
- Use the real `git` in tests: temp repos created with `git init`, plus a bare "origin" to exercise the `origin/<branch>` path.
- Don't touch `~/.claude.json` and don't do trust handling. That's the launch error path in task 005.

## Tests
- `Ensure` with two repos creates both worktrees on the right branch and is idempotent on a second call.
- Adopting existing paths works.
- `KeyFromCwd` handles nested dirs, a non-workspace path and a symlinked home.
- `Remove` on a dirty tree returns `ErrDirty`; with `force` it succeeds.
