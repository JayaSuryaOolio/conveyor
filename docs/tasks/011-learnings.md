# 011: Learnings inbox → "apply learnings" tasks

**Wave 4** · depends on: 004, 006, 007 · branch `task/011-learnings`

## Goal
The self-evolving loop (design §8): runs propose learnings, the human curates them, and a Conveyor task applies them through a normal PR.

## API
```go
package learning
func List(db *sql.DB, status ...string) ([]store.Learning, error)
func Accept(db *sql.DB, id int64, target string) error   // the default target is CLAUDE.md
func Reject(db *sql.DB, id int64) error
func Edit(db *sql.DB, id int64, body string) error
// Apply creates one task per repo: "Apply learnings to <repo>". Its description lists every accepted learning for that
// repo with its target file and source task key, plus the instruction: "Edit the target files so each lesson is captured
// concisely and isn't duplicated; open one PR." The task is created in status todo with source=learning, and the learnings
// are marked applied with applied_task_id. Personal learnings (repo_id NULL) get a task on no repo: skip them, and print
// them for the human to paste into ~/.claude/CLAUDE.md. Returns the created task keys.
func Apply(svc *core.Service, repo string) ([]string, error)
```
- Store queries for learnings.
- **Spawn lines.** Task 004 records them as `spawn` events. Turn them into backlog child tasks, with the same repos as the parent and `parent_id` set, when the run finishes. Do this in the hook path via a `core.SpawnChildren(task, titles)` helper, and update `hook.Handle` to call it instead of only recording the event.
- **CLI:** `conveyor learning list|accept|reject|edit|apply`.
- **App:**
  - A Learnings page: proposed items grouped by repo, with Accept (with a target field), Edit and Reject, and an "Apply to <repo>" button.
  - A count badge in the nav.

## Tests
- The state transitions.
- `Apply` groups by repo and creates the task with the right description.
- Spawn creates child tasks with inherited repos.

## Outcome
Built:
- `store/learnings.go` queries; `internal/learning` (List, Accept, Reject, Edit, Apply, plus Personal for the paste-into-~/.claude/CLAUDE.md list).
- `core.SpawnChildren(db, parent, titles)`; `hook.Handle` now creates backlog children (same repos/primary, `parent_id` set) besides recording the `spawn` event.
- `conveyor learning list|accept|reject|edit|apply`; Learnings page and nav count badge (proposed items); bound methods `Learnings`, `AcceptLearning`, `RejectLearning`, `EditLearning`, `ApplyLearnings`.

Deviations:
- `SpawnChildren` takes `db` as its first argument (the hook has a DB, not a Service).
- Added `Source` to `core.NewTaskInput` so Apply can set `source=learning`.
- `Apply(svc, "")` applies every repo; a repo name limits it to that repo. Applied learnings are immutable.
- The "Apply to <repo>" button shows only when the repo has accepted learnings.

Manual checks: not done in the running app (no GUI session); `wails build` succeeds.

Follow-ups: personal learnings stay `accepted` forever (no "mark pasted" action).
