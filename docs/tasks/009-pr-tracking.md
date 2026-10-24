# 009: PR tracking: review → done

**Wave 2** · depends on: 002 · branch `task/009-pr-tracking`

## Goal
Make "done" mean that every PR of the task has merged (design §3).

## API
```go
package gh
var ErrNotFound = errors.New("gh CLI not found")
type PR struct{ URL, State string; Number int; ReviewDecision string; UpdatedAt string }
// FindPR runs `gh pr list --head <branch> --state all --json url,state,number,reviewDecision,updatedAt --limit 1` in repoPath.
// It returns (nil, nil) when there is no PR.
func FindPR(ctx context.Context, repoPath, branch string) (*PR, error)
// SyncTask updates pr_url and pr_state for every task repo, then:
// - task status review and at least one PR, all of them MERGED or CLOSED with at least one MERGED → done (system), plus an event
// - task status review and every PR CLOSED (none merged) → needs_you, plus a system comment "PR closed without merge"
// Repos without a PR are ignored in the "all merged" check, but at least one PR must exist.
func SyncTask(ctx context.Context, db *sql.DB, task store.Task) error
```
- CLI `conveyor task sync KEY` runs `SyncTask` on demand and prints the PR states.

## Tests
A fake `gh` script returns canned JSON per branch. Cover:
- no PR
- open → nothing changes
- merged → done
- two repos, one merged and one open → stays review
- all closed → needs_you
- `gh` missing → `ErrNotFound`
