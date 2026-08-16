# Implementation plan: Conveyor

Conveyor is the standalone kanban/auto-dispatch product from `docs/kanban-board-prd.md` — a separate repository, own binary, own DB, own frontend. This plan sequences its build so that **phase 1 is fully provable with zero worktree-studio involvement** (per the PRD's Section 3 "optional provider" design), and phase 2 is additive on top of that, built against a stubbed contract so it doesn't block on the worktree-studio-side integration plan landing first (see `worktree-studio-conveyor-integration-plan.md` in the worktree-studio repo's `docs/`).

Keep this file updated at the end of each phase — the same living-doc discipline worktree-studio uses for its own `PLAN.md`/`PROGRESS.md`.

Every section below references the PRD section it implements — read that first for the *why*; this is the *build order*.

---

## Phase 0 — Repo scaffold

- New repo, Go module `conveyor` (or your org's module path).
- Layout (PRD Section 6):
  ```
  cmd/conveyor/main.go
  internal/store/           kanban_tasks, kanban_task_events (PRD Section 7)
  internal/dispatcher/      poll ticker + dispatchNow channel, claim, circuit breaker, reap, reconciliation
  internal/provider/        WorktreeProvider interface + local.go + wts.go (PRD Section 3)
  internal/hooks/           settings.json installer + Stop/UserPromptSubmit/Notification receiver
  internal/tagparser/       <task-status> extraction (PRD Section 5.3)
  internal/jira/            acli wrapper (phase 2)
  web/                      Vite + React + TS, same stack choice as worktree-studio
  ```
- SQLite via `modernc.org/sqlite` (pure-Go, no cgo) — same driver choice as worktree-studio, for the same reason (no cgo toolchain dependency). Unlike worktree-studio's `store.Open`, **set a `_pragma=busy_timeout(5000)` DSN param from the start** — Conveyor's dispatcher, hook receiver, and API handlers all touch the same DB file from one process, but there's no reason to inherit worktree-studio's zero-busy-timeout gap when starting fresh.
- `go.mod` deps to bring in early: `chi/v5`, `modernc.org/sqlite`. Hold off on a websocket library — nothing in the design needs server push (Section 11 uses polling throughout); add one later only if that changes.

**Checkpoint**: `conveyor serve` starts, opens an empty DB, chi router responds on a health endpoint. Nothing else yet.

---

## Phase 1 — Skeleton, `localProvider`, headless mode (PRD Sections 4, 5, 7, 8, 11 — local path only)

Build order within the phase (each step should be runnable/testable before moving to the next):

1. **Data layer**: `kanban_tasks`/`kanban_task_events` tables + migration convention (`CREATE TABLE IF NOT EXISTS` + `migrateAddX`, mirroring worktree-studio's `internal/store/store.go` shape). CRUD functions: create, get, list, update-status (with the allowed-transition guard from PRD Section 4 — `cancelled`/`archived` human-only, enforced here not just in the API layer).
2. **REST API**: task CRUD, manual status transitions (`draft`→`grooming`→`todo`, `cancel`), `POST /api/trigger`. No dispatch logic yet — prove the board works as a plain manual task tracker first. **This API is the entire integration surface** for worktree-studio's own kanban view (PRD Section 3, `docs/worktree-studio-conveyor-integration-plan.md` Part B) — worktree-studio reads it live through its own backend proxy, no separate "external API" needed, no CORS work required here (worktree-studio's proxy calls this server-to-server, never from a browser). Keep the response shape exactly matching the internal row (Section 7) — both frontends consume it directly.
3. **Frontend skeleton**: `Board.tsx`/`Column.tsx`/`Card.tsx`/`NewTaskModal.tsx` (title/description/mode picker — grey out "headful" unconditionally in this phase, no provider exists yet to enable it), `useKanbanBoard.ts` polling `GET /api/tasks` every 5-10s. Manual drag-free moves via buttons only (PRD Section 11).
   - **Checkpoint**: create/view/move tasks by hand in a browser. No AI involved yet.
4. **`localProvider`** (`internal/provider/local.go`): `CreateWorktree` shells `git worktree add -b <branch> <path> <start-point>` (same primitive worktree-studio wraps, called independently — no shared code). `StartHeadful` returns `ErrHeadfulUnsupported` unconditionally. `Cleanup` shells `git worktree remove`.
5. **Prompt delivery**: on claim, write `.conveyor/prompt.md` into the new worktree containing the task's title+description+the status-tag instruction block verbatim from PRD Section 5.3.
6. **Headless dispatch**: `internal/dispatcher` — the `dispatchNow` channel + ticker (default 30s) from PRD Section 8; atomic claim (`UPDATE ... WHERE status='todo'`, check `RowsAffected`); spawn `claude -p --output-format json --permission-mode acceptEdits < .conveyor/prompt.md` as a tracked subprocess (store PID, start time).
7. **Hook installer** (`internal/hooks/install.go`): read `~/.claude/settings.json` as a generic map, append matcher-group entries to `hooks.Stop`/`hooks.UserPromptSubmit`/`hooks.Notification`, identified by Conveyor's own script's absolute path — copy the merge/remove-by-own-path discipline directly from worktree-studio's `internal/claudehook/install.go` (`findOurEntry`, never touch an array wholesale). Generated hook scripts POST the hook's stdin JSON to Conveyor's own receiver endpoint.
8. **Hook receiver + state machine** (PRD Section 5.1-5.2): resolve `cwd` → task via prefix-match against `worktree_path` (same trick as worktree-studio's `FindWorktreeByPath`). On `UserPromptSubmit`: `needs_clarification`→`in_progress`. On `Stop`: read the transcript's last assistant message (`internal/tagparser`), tagged `done`/`failed` → terminal state; untagged → `needs_clarification` + store `latest_message`.
9. **Headless answer path**: `needs_clarification` card shows an inline textbox; submit → the centralized resume helper (PRD Section 5.6) execs `claude --resume <session_id> -p "<answer>" --permission-mode acceptEdits`; **a failed `--resume` call is an explicit `failed` transition, never a silent fresh session**.
10. **Reap + reconciliation** (PRD Sections 5.6, 8): reap checks liveness for both `in_progress` *and* `needs_clarification` (PID check for headless), marks `failed` past `max_runtime`/dead-process; circuit breaker on `consecutive_failures` (default budget 3); reconciliation pass re-derives status from the transcript file directly on every dispatch tick, independent of whether a hook fired recently.
11. **Cancel kills the process**: cancelling an `in_progress`/`needs_clarification` headless task kills the owned subprocess, not just the DB row (PRD Section 8).
12. **Tag parser tests**: tagged done/failed, untagged, malformed tag, fenced-in-code-block tag — this is the single highest-leverage unit-testable piece (PRD Section 14).

**Phase 1 checkpoint (this is the real milestone — fully standalone)**: create a task by hand, let the dispatcher pick it up, confirm a real worktree + `claude -p` subprocess ran, watch it reach `needs_clarification` (untagged stop), answer via the textbox, confirm immediate `in_progress` (via `UserPromptSubmit`, not a 30s wait), and eventually `done`. Force a broken worktree and confirm the circuit breaker lands it on `failed` rather than retrying forever. All of this with worktree-studio not installed or running anywhere.

---

## Phase 2 — `wtsProvider`, Jira, Remote Control, polish (PRD Sections 3, 5.5, 9, 10)

This phase is built **against the documented CLI contract** (PRD Section 3.3's exact subcommand shapes), not against a running worktree-studio — use a tiny stub binary on `PATH` during development that honors the same `--json`/exit-code contract, so `wtsProvider`'s logic and error handling are fully unit/integration-tested before the worktree-studio-side plan ships anything real. Swapping the stub for the real `worktree-studio` binary at the end of that other plan should require no code changes here — only a config/PATH change.

1. **`wtsProvider`** (`internal/provider/wts.go`): `os/exec` wrapper around the `worktree-studio` binary — `CreateWorktree` → `worktree-studio worktree create --repo <id> --name <name> --json`; `StartHeadful` → `worktree-studio terminal create --worktree <id> --initial-command '<cmd>' --json`, returning the attach-URL deep link; `Cleanup`/cancel-path → `worktree-studio terminal close --id <id>` then `worktree-studio worktree archive --id <id>`.
2. **Settings page**: `worktree_provider` setting (`local` default / `worktree-studio`), a **"Test connection"** action (`exec.LookPath("worktree-studio")` + one `worktree-studio worktree list --json` smoke call) run only on demand — never a background probe (PRD Section 3.1, Section 12's "silent auto-detection" pitfall).
3. **Headful mode unlocked in the UI**: `NewTaskModal`'s mode picker only offers "headful" when the active provider is `worktree-studio` and its last Test Connection passed; the "Enable Remote Control" checkbox (PRD Section 5.5) appears only alongside it, creation-time-only, no runtime toggle.
4. **Remote Control wiring**: headful `initial_command` becomes `claude --remote-control "<task title>" "$(cat .conveyor/prompt.md)"` when the checkbox was set, plain `claude "$(cat .conveyor/prompt.md)"` otherwise.
5. **Attention-style pubsub**: a small in-memory tracker + desktop notification for `needs_clarification` cards (PRD Section 10) — a from-scratch, much smaller analog of worktree-studio's `internal/attention`, not a shared import.
6. **Jira sync** (`internal/jira`): thin `acli` wrapper mirroring worktree-studio's `internal/gh` shape — spike the actual `acli` output format by hand first, then write the parser against confirmed output, not assumed. Idempotent dedupe on `external_key` (unique index), sync on its own ticker decoupled from the dispatch poll.
7. **Retention sweep**: `done`/`cancelled`/`failed` tasks past a configurable age → `archived`, mirroring worktree-studio's 60-day archived-worktree sweep pattern.
8. **Config surface**: poll interval, max concurrency, per-mode timeouts — plain settings, no need for anything fancier.

**Phase 2 checkpoint (still stub-based)**: the same full headful loop as the phase-1 headless checkpoint, but driven through `wtsProvider` against the stub CLI — task dispatch → worktree/terminal creation calls issued correctly → simulated hook events → `needs_clarification`/`done`. This proves Conveyor's own logic end-to-end; the *real* worktree-studio integration test happens in the other plan, once its CLI subcommands exist for real.

---

## What this plan deliberately defers to the worktree-studio-side plan

- Nothing in `wtsProvider`'s contract changes based on what worktree-studio's implementation looks like internally — only the CLI subcommand shapes in PRD Section 3.7 matter, and those are already fixed. If the worktree-studio-side plan needs to deviate from that contract for a good reason, come back and adjust `wtsProvider` then, not preemptively.
- The "Open terminal" link's exact URL shape (`?terminal=<id>` query param) is only usable once that other plan's deep-link support ships — Conveyor can construct and store the URL from phase 2 onward regardless, it just won't do anything useful in a browser until then.
- **worktree-studio's own kanban view (PRD Section 3.2–3.4) requires zero work here beyond keeping the REST API's response shape stable.** It is not a second implementation Conveyor builds or maintains — it's an independent frontend in the other repo reading this one's API live. Don't add anything Conveyor-specific to accommodate it (no CORS headers, no special "external" response mode) — the contract is just "the API works," which phase 1 already delivers.
