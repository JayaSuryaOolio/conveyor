# Conveyor — Progress

Master index of implementation tasks, sequenced per `PLAN.md`. Each task gets its own
`progress/NNN_<slug>.md` with its tech spec and outcome notes. Update this table whenever
a task starts, finishes, or moves branches — same living-doc discipline as `PLAN.md`.

| # | Task | Phase | Branch | Status | Notes |
|---|------|-------|--------|--------|-------|
| 000 | [Repo scaffold](000_repo-scaffold.md) | 0 | `project-setup` (merged to `main`) | Done | chi router, SQLite w/ busy_timeout, `/healthz` |
| 001 | [Data layer: kanban_tasks/kanban_task_events](001_data-layer.md) | 1 | `001-data-layer` | Done | Tables, migration convention, CRUD, status-transition guard |

## Conventions

- **Branch per task**, cut from `main`, merged back with `git merge --ff-only` (or a regular
  merge if history diverges) once its own checkpoint passes — no PR review, single-dev project.
- **Numbering** is sequential across the whole project, not per-phase, so task order in this
  table always matches build order.
- Tasks can be worked in parallel across separate worktrees once they don't depend on each
  other's in-flight code — check this table for what's `Done` before branching a dependent task.
