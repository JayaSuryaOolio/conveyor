# 001 — Data layer: `kanban_tasks` / `kanban_task_events`

**Phase**: 1, step 1 (PLAN.md) · **Branch**: `001-data-layer` · **Status**: Done

Implements PRD Section 7 (schema) and Section 4 (status model + transition legality), and
PLAN.md's Phase 1 step 1: "tables + migration convention... CRUD functions: create, get,
list, update-status (with the allowed-transition guard from PRD Section 4 — cancelled/
archived human-only, enforced here not just in the API layer)."

## Schema

Exactly the DDL from PRD Section 7, copied verbatim: `kanban_tasks` (+ the unique partial
index on `external_key`) and `kanban_task_events`. No deviation — this is the one section of
the PRD that's already fully specified down to column names.

## Migration convention

`internal/store/schema.go`: `Migrate(db *sql.DB) error` runs `CREATE TABLE IF NOT EXISTS`
for both tables in a single transaction, mirroring worktree-studio's
`CREATE TABLE IF NOT EXISTS` + `migrateAddX` shape (PLAN.md Phase 1 step 1). No `migrateAddX`
functions exist yet since there's no prior schema version to migrate from — the seam is just
established: future column additions get their own `migrateAddX(db *sql.DB) error`, called
from `Migrate` after the base `CREATE TABLE`s, each independently idempotent (checked via
`PRAGMA table_info` before `ALTER TABLE ... ADD COLUMN`).

## Status transitions

`internal/store/status.go` encodes the state diagram (PRD Section 4) as a directed edge set,
`map[Status]map[Status]bool`, resolved from the section's prose (not the ASCII diagram, which
has a couple of ambiguous arrows) plus the "who moves it here" column:

| From | To | Trigger (informational only, not enforced by edge table) |
|---|---|---|
| `draft` | `grooming` | Human |
| `grooming` | `todo` | Human |
| `todo` | `in_progress` | Dispatcher claim |
| `in_progress` | `needs_clarification` | Stop hook, no tag |
| `in_progress` | `done` | Stop hook, tag=done |
| `in_progress` | `failed` | Dispatcher reap/circuit-breaker |
| `needs_clarification` | `in_progress` | UserPromptSubmit hook |
| `needs_clarification` | `failed` | Reap (session found dead) |
| `failed` | `todo` | Human requeue |
| `done` \| `failed` \| `cancelled` | `archived` | Human, or automatic sweep |
| `draft` \| `grooming` \| `todo` \| `in_progress` \| `needs_clarification` | `cancelled` | Human only |

**Actor enforcement**: the edge table above is actor-agnostic (it only says which edges
*exist*); PLAN.md specifically calls out that `cancelled`/`archived` must be human-only *at
the store layer*, not just the API layer. So `ValidateTransition` takes an explicit
`Actor` (`ActorHuman` / `ActorSystem`) and adds one extra rule on top of the edge table:
entering `cancelled` requires `ActorHuman` — the PRD is explicit this is "Human only — never
AI" with no sweep exception. Entering `archived` allows either actor, since the PRD's own
retention sweep is a legitimate automatic path there. Every other edge is actor-agnostic for
now; nothing in the PRD asks for finer-grained enforcement (e.g. "only the dispatcher, not a
human, may move todo→in_progress") and adding it would be speculative.

`UpdateTaskStatus` does the legality check in Go, then applies it with a guarded
`UPDATE ... SET status = ? WHERE id = ? AND status = ?` (checking `RowsAffected`) so a
concurrent status change loses the race cleanly instead of silently overwriting — the same
optimistic-concurrency shape the dispatcher's later atomic claim (PLAN Phase 1 step 6) will
reuse for `todo → in_progress`.

## CRUD surface (`internal/store/tasks.go`)

- `CreateTask(db, Task) (Task, error)` — generates `id` (uuid), stamps `created_at`/
  `updated_at`, inserts, defaults `status='draft'`, `mode='headless'` per the DDL.
- `GetTask(db, id) (Task, error)`
- `ListTasks(db) ([]Task, error)` — all tasks, ordered by `created_at`; filtering by status is
  a frontend/API concern (Section 11's `Board.tsx` groups client-side), not the store's.
- `UpdateTaskStatus(db, id, newStatus, actor) (Task, error)` — the guarded transition above.
- `RecordEvent(db, taskID, event, detail) error` — insert into `kanban_task_events`. Not
  auto-fired by `UpdateTaskStatus` in this task; callers (dispatcher, hook receiver — later
  tasks) call both explicitly, since not every event in the PRD's `event` enum
  (`claimed`, `stop_tagged`, ...) corresponds 1:1 with a status change.

## Explicitly deferred

- No API layer yet (PLAN Phase 1 step 2) — this task is store-only.
- No `mode`/provider-capability validation (headful requires `wtsProvider`) — that's a
  request-time check the API layer owns once it exists, not a store concern.
- No retention-sweep implementation — Phase 2.

## Verification

`internal/store/*_test.go` against an in-memory SQLite DB (`file::memory:`... actually `:memory:`
DSN, bypassing `Open`'s pragma since no concurrent access exists in tests):
- Migrate is idempotent (calling it twice doesn't error).
- CRUD round-trip: create → get → list contains it.
- Transition guard: a table-driven test over every edge in the state diagram (valid → no
  error) and a sample of invalid edges (e.g. `done → in_progress`, `draft → done`,
  `cancelled → todo`) → error.
- `cancelled` via `ActorSystem` is rejected even though `cancelled` is a legal edge from that
  source status for a human — proves the actor guard is enforced independent of edge
  legality.

## Outcome

Implemented as specified: `schema.go` (Migrate), `status.go` (edge table + actor guard),
`tasks.go` (CRUD + RecordEvent). 12 tests, all passing — transition table-driven tests, CRUD
round-trip, `sql.ErrNoRows` on missing get, illegal-transition rejection leaves status
unchanged, cancelled rejects a system actor, and the partial unique index on `external_key`
(dedupes Jira imports, allows unlimited manual tasks with no external_key).
