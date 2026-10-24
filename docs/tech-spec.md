# Conveyor tech spec (v2)

This spec is the starting point for implementation. The design rationale lives in `docs/research/conveyor-design.md`, and the verified Claude Code behaviour in `docs/research/spike-bg.md`. Where this spec and the PRD (`docs/kanban-board-prd.md`) disagree, this spec wins.

Every task has its own spec in `docs/tasks/NNN-*.md`; `progress/progress.md` tracks their status. An implementer reads this file, then their task file, and nothing else is required.

---

## 1. What we're building

Conveyor is a local kanban that runs tasks through Claude Code background sessions:
- tasks live on a board, and each task has a Jira-like page with a comment thread
- a comment triggers the next run
- every run is a real Claude session you can attach to
- each task works in its own workspace folder, which holds one git worktree per repo

**Stack:**
- Go 1.25
- SQLite (`modernc.org/sqlite`, already a dependency)
- a desktop app on **Wails v2** with a React + TypeScript frontend
- a CLI covering every major operation

There is **no web server**: no chi, no REST, no HTTP hooks.

## 2. Process model

```
┌──────────────┐  ┌───────────────┐  ┌──────────────────────┐
│ Conveyor.app │  │ conveyor CLI  │  │ conveyor hook <Event>│  (spawned by Claude Code)
│ (Wails)      │  │ (one-shot)    │  │ (one-shot)           │
└──────┬───────┘  └──────┬────────┘  └─────────┬────────────┘
       │ dispatcher if lock held                │
       └───────────── ~/.conveyor/conveyor.db (SQLite, WAL) ─┘
                 ▲
       conveyor daemon (dispatcher, headless alternative to the app)
```

- **One database, many processes.** Every process opens the DB with `journal_mode=WAL` and `busy_timeout=5000`. There is no RPC between processes.
- **Dispatcher single-instance.** The dispatcher runs in whichever process (the app or `conveyor daemon`) holds an exclusive `flock` on `~/.conveyor/dispatcher.lock`. A process that can't get the lock runs UI or CLI only. The app retries the lock every 10 s, so quitting the daemon hands dispatching over to the app.
- **Change notification.** The app polls `PRAGMA data_version` every 700 ms and emits the Wails event `db:changed` when it changes. The frontend refetches the current view. There is no other event plumbing.
- **Claude hooks are command hooks.** Claude Code runs `conveyor --home <home> hook <Event>`, which reads the payload JSON on stdin and writes to the DB. It always exits 0 and logs errors to `~/.conveyor/logs/hook.log`.

## 3. Filesystem layout

`CONVEYOR_HOME` defaults to `~/.conveyor`. It can be overridden with the global flag `--home` or the env var `CONVEYOR_HOME`, and tests always override it.

```
$CONVEYOR_HOME/
  conveyor.db
  dispatcher.lock
  claude-settings.json         # generated; passed to every launch via --settings
  logs/hook.log, logs/dispatcher.log
  workspaces/<KEY>/
    BRIEF.md                   # rendered before every run (outside every repo; nothing to git-exclude)
    <repo-name>/               # git worktree of the registered repo, branch cv/<KEY>-<slug>
```

## 4. Repository layout

```
main.go, app.go, wails.json, frontend/     # Wails v2 desktop app (Wails default layout at repo root)
cmd/conveyor/                              # CLI: main.go + cmd_<group>.go per command group
internal/store/        schema, models, queries, status machine
internal/core/         domain operations shared by CLI and app (create task, comment, move, cancel…)
internal/workspace/    per-task git worktrees
internal/claude/       launch / resume / stop / attach / list background sessions; settings file
internal/hook/         Claude hook event → store updates
internal/statusblock/  parse the <conveyor> block
internal/transcript/   token usage from transcript JSONL
internal/brief/        render BRIEF.md and run prompts
internal/dispatch/     dispatcher loop
internal/gh/           PR lookup via the gh CLI
internal/automation/   cron + precheck + action
internal/learning/     learnings inbox → "apply learnings" tasks
internal/terminal/     open a terminal from a template
internal/ui/           Wails bindings (thin: calls core/store)
```

**Rules:**
- Packages depend downward only: `ui` and `cmd` → `core` / `dispatch` → leaf packages → `store`.
- Leaf packages never import `core`.
- External tools (`git`, `claude`, `gh`, `acli`, `osascript`) run via `os/exec`. Each is found with `exec.LookPath`, and a missing one returns a sentinel error.
- Every exec point goes through a package-level `var execCommand = exec.CommandContext`, so tests can swap in a fake. Tests use fake shell scripts on `PATH`, never the real `claude` or `gh`.

**`frontend/dist` and builds:**
- `frontend/dist/.gitkeep` is committed, so `//go:embed all:frontend/dist` compiles and `go test ./...` works without building the frontend.
- The real `dist` is gitignored except `.gitkeep`.

## 5. Data model

**Fresh schema.** The v1 tables `kanban_tasks` and `kanban_task_events` are dropped; there is no production data. Migrations follow the existing convention: `Migrate` runs `CREATE TABLE IF NOT EXISTS`, and later column additions are idempotent `migrateAddX` steps. Timestamps are RFC3339 UTC text, and IDs are `INTEGER PRIMARY KEY`.

```sql
CREATE TABLE repos (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,            -- short name, defaults to basename(path)
  path TEXT NOT NULL UNIQUE,            -- absolute path of the main clone
  default_branch TEXT NOT NULL,         -- detected at add time (origin/HEAD, else current branch)
  created_at TEXT NOT NULL
);

CREATE TABLE tasks (
  id INTEGER PRIMARY KEY,
  key TEXT NOT NULL UNIQUE,             -- "CV-12"; prefix from settings.key_prefix
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'backlog',
  priority INTEGER NOT NULL DEFAULT 0,  -- higher first
  model TEXT,                           -- optional --model override
  agent TEXT,                           -- optional --agent (repo agent name)
  source TEXT NOT NULL DEFAULT 'manual',-- manual | jira | automation | learning
  external_key TEXT UNIQUE,             -- e.g. Jira key
  external_url TEXT,
  parent_id INTEGER REFERENCES tasks(id),
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE task_repos (
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  repo_id INTEGER NOT NULL REFERENCES repos(id),
  is_primary INTEGER NOT NULL DEFAULT 0,  -- exactly one per task
  branch TEXT NOT NULL,                   -- cv/<KEY>-<slug>
  worktree_path TEXT,                     -- set once created
  pr_url TEXT,
  pr_state TEXT,                          -- OPEN | MERGED | CLOSED | NULL
  PRIMARY KEY (task_id, repo_id)
);

CREATE TABLE runs (
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,                     -- auto | interactive
  mode TEXT NOT NULL,                     -- fresh | resume
  bg_id TEXT,                             -- 8-hex id printed by `claude --bg`
  session_id TEXT,                        -- full UUID, learned from the first hook
  status TEXT NOT NULL,                   -- launching | running | finished | failed | stopped
  prompt TEXT NOT NULL DEFAULT '',
  block_status TEXT,                      -- done | failed | NULL (no block)
  summary TEXT, state TEXT, next TEXT, criteria TEXT,  -- from the status block
  final_message TEXT,                     -- last_assistant_message minus the block
  tokens_in INTEGER NOT NULL DEFAULT 0, tokens_out INTEGER NOT NULL DEFAULT 0,
  cache_read INTEGER NOT NULL DEFAULT 0, cache_write INTEGER NOT NULL DEFAULT 0,
  ctx_peak INTEGER NOT NULL DEFAULT 0,    -- max(input+cache_read+cache_write) of any single call
  api_calls INTEGER NOT NULL DEFAULT 0,
  error TEXT,
  started_at TEXT NOT NULL,
  ended_at TEXT
);
CREATE INDEX idx_runs_task ON runs(task_id);
CREATE INDEX idx_runs_session ON runs(session_id);

CREATE TABLE comments (
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  author TEXT NOT NULL,                   -- human | agent | system
  body TEXT NOT NULL,
  run_id INTEGER REFERENCES runs(id),     -- for agent comments: the run that produced it
  consumed_by_run_id INTEGER REFERENCES runs(id),  -- for human/system comments: the run that received it
  created_at TEXT NOT NULL
);
CREATE INDEX idx_comments_task ON comments(task_id);

CREATE TABLE events (
  id INTEGER PRIMARY KEY,
  task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,                     -- status_changed | run_started | run_finished | hook | error | ...
  detail TEXT NOT NULL DEFAULT '',        -- free text or JSON
  created_at TEXT NOT NULL
);

CREATE TABLE learnings (
  id INTEGER PRIMARY KEY,
  task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  run_id INTEGER REFERENCES runs(id) ON DELETE SET NULL,
  repo_id INTEGER REFERENCES repos(id),   -- NULL = personal (~/.claude/CLAUDE.md)
  body TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'proposed',-- proposed | accepted | rejected | applied
  target TEXT,                            -- relative file in the repo, e.g. CLAUDE.md
  applied_task_id INTEGER REFERENCES tasks(id),
  created_at TEXT NOT NULL
);

CREATE TABLE automations (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  schedule TEXT NOT NULL,                 -- 5-field cron
  precheck TEXT NOT NULL DEFAULT '',      -- shell; exit 0 = run the action; stdout passed to the action as {output}
  action_kind TEXT NOT NULL,              -- create_task | comment | cli
  action_arg TEXT NOT NULL,               -- JSON, shape depends on kind (task 010)
  enabled INTEGER NOT NULL DEFAULT 1,
  last_run_at TEXT,
  last_result TEXT
);

CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
```

**Settings and their defaults:**

| key | default |
|---|---|
| `key_prefix` | `CV` |
| `max_parallel` | `3` |
| `resume_threshold` | `60000` |
| `comment_debounce_secs` | `20` |
| `max_consecutive_failures` | `3` |
| `default_model` | empty |
| `terminal_template` | see §10 |
| `editor_command` | `code {cwd}` |

The store reads them with `GetSetting(db, key)`, which falls back to the default.

## 6. Status machine

```
backlog ─► todo ─► running ─┬─► needs_you ─► todo (human comment) / running (human attached & typed)
                            ├─► review ─► done (all PRs merged, or human)
                            │      └─► todo (human comment / PR review sweep)
                            ├─► todo (Stop arrived while new comments were queued)
                            └─► failed ─► todo (human requeue)
any non-terminal ─► cancelled (human only)
```

| from → to | allowed actor |
|---|---|
| backlog → todo, todo → backlog | human |
| todo → running | system (dispatcher) |
| running → needs_you / review / failed / todo | system (hook, reaper) |
| needs_you → running | system (UserPromptSubmit from an attached human) |
| needs_you / review / failed / done → todo | human, or system on a comment (`core.AddComment`) |
| review → done | system (PR poller) or human |
| needs_you → done, running → review | human ("mark done" / manual override) |
| any except done/cancelled → cancelled | **human only** |

`store.ValidateTransition(from, to, actor)` encodes this table. `store.UpdateTaskStatus` applies a change with `UPDATE … WHERE id=? AND status=?` (optimistic concurrency, as Task 001 does) and inserts an `events` row with kind `status_changed`.

## 7. Claude integration (from the spike)

**Settings file**, generated by `claude.WriteSettings(home, conveyorBin)` before every launch, idempotently:

```json
{
  "worktree": { "bgIsolation": "none" },
  "hooks": {
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "'<bin>' --home '<home>' hook UserPromptSubmit" }] }],
    "Stop":             [{ "hooks": [{ "type": "command", "command": "'<bin>' --home '<home>' hook Stop" }] }],
    "Notification":     [{ "hooks": [{ "type": "command", "command": "'<bin>' --home '<home>' hook Notification" }] }]
  }
}
```

`<bin>` is `os.Executable()` of the CLI. The app locates the CLI via `exec.LookPath("conveyor")`, falling back to the bundled path.

**Fresh launch.** cwd is the primary repo's worktree. **The prompt goes before `--add-dir`**, because that flag is variadic and swallows a trailing prompt.

```
env CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1 \
claude --bg "<prompt>" --name <KEY> --settings <home>/claude-settings.json \
  --permission-mode bypassPermissions --strict-mcp-config [--mcp-config <wt>/.mcp.json if it exists] \
  [--model <m>] [--agent <a>] [--add-dir <other worktrees...>]
```

- Parse `backgrounded · <bg_id>` from stdout.
- `--session-id` is ignored by `--bg`, so don't pass it.
- If stdout contains `Workspace not trusted`, return `claude.ErrUntrusted` and show the hint "open <repo path> in Claude once to trust it".

**Resume (hot tier).** Run `claude stop <bg_id>` (ignore errors), then:

```
claude --bg --resume <session_id> "<delta prompt>"
```

Pass **no other flags**. That keeps the same id, its saved options and the hooks; any flag forks a copy instead.

**Other operations:**
- **Liveness:** `claude agents --json --all`, matching the run's `bg_id` against the returned `id`.
- **Stop:** `claude stop <bg_id>`.
- **Remove:** `claude rm <bg_id>`, on task done or cancel.
- **Attach:** `claude attach <bg_id>`, in a terminal.

**Interactive sessions** (Workspace panel "Resume in terminal" / "New session from brief"):
- They run in a terminal with `--settings` (for hooks) but **without** `--permission-mode bypassPermissions`.
- They create a `runs` row of `kind=interactive`.

## 8. Hooks → state (`internal/hook`)

`conveyor hook <Event>`: parse the stdin JSON, then process it.

1. **Resolve the task.** Take `cwd` relative to `<home>/workspaces/`; its first path segment is the task key. If cwd isn't under the workspaces folder, ignore the event and exit 0.
2. **Resolve the run.**
   - Look up the run by `session_id`.
   - Otherwise take the newest run of that task with `session_id IS NULL` and status `launching`, and set its `session_id`.
   - Otherwise, for an interactive session started by hand in the workspace, create a run with `kind=interactive, mode=fresh, status=running`.

| Event | Effect |
|---|---|
| `UserPromptSubmit` | run → `running`. If the task is `needs_you` and the run is the task's latest, task → `running` (a human attached and typed). |
| `Stop`, `kind=auto` | 1. Parse `last_assistant_message` with `statusblock.Parse`.<br>2. Fill the run's block fields and `final_message`; add usage from `transcript.Usage(transcript_path)`; run → `finished`, `ended_at`.<br>3. Insert an `agent` comment holding `final_message` (with the parsed fields kept on the run), and a `learnings` row per `learning:` line.<br>4. Move the task: unconsumed human/system comments exist → `todo`; else block `done` → `review` (with `consecutive_failures=0`); block `failed` → increment `consecutive_failures`, then `failed` if it reaches the max, else `todo`; no block → `needs_you`. |
| `Stop`, `kind=interactive` | Record usage and `final_message` only. Never change the task status. |
| `Notification` | Insert an `events` row of kind `notification` only. |

A hook processes in under 2 s, needs no network, and never blocks Claude.

## 9. Status block

The protocol is described to the agent in `BRIEF.md` (task 007):

```
<conveyor>
status: done | failed
summary: one paragraph
state: branch …, PR #N, tests: pass|fail|not run
next: what remains
criteria:
- [x] AC1 — evidence
- [ ] AC2 — why not
learning: a lesson (repeatable line)
spawn: child task title (repeatable line)
</conveyor>
```

Parsing rules:
- Take the **last** block in the message.
- Keys are case-insensitive and unknown keys are ignored.
- `criteria` runs to the next key.
- An invalid `status`, or a block without `status`, means no block.

## 10. Terminal opener (`internal/terminal`)

`terminal.Open(template, cwd, cmd string) error` replaces `{cwd}` and `{cmd}` with shell-quoted values and runs the result through `sh -c`. The default template targets macOS Terminal:

```
osascript -e 'tell application "Terminal" to do script "cd {cwd} && {cmd}"' -e 'tell application "Terminal" to activate'
```

(Quoting inside AppleScript is handled by the package; tests cover paths with spaces and quotes.) Users can set an iTerm, Ghostty or tmux template in settings, and every action also has a "copy command" button.

## 11. CLI (`cmd/conveyor`)

**Conventions:**
- stdlib `flag` only.
- The global flag `--home` must come before the subcommand.
- Every listing or show command accepts `--json`. Errors go to stderr with exit 1.
- Each command group lives in its own file (`cmd_task.go`, `cmd_repo.go` …) and registers itself through `func init() { register("task", runTask, "manage tasks") }`. `main.go` then never conflicts between parallel tasks.

```
conveyor repo add <path> [--name n] | repo list | repo rm <name>
conveyor task new <title> --repo a[,b] [--primary a] [--desc s | --desc-file f] [--todo] [--model m] [--agent a] [--parent KEY] [--priority n]
conveyor task list [--status s[,s]] | task show KEY [--thread] [--runs]
conveyor task comment KEY <text|->     task move KEY <status>     task cancel KEY     task done KEY
conveyor task set KEY [--repo r] [--branch b] [--pr url]   (branch only before the worktree exists; --pr "" clears)
conveyor task attach KEY               (execs `claude attach <bg_id>` in this terminal)
conveyor task open KEY [--shell|--resume|--fresh|--editor] [--repo r] [--print]
conveyor task brief KEY                (prints the rendered brief)
conveyor daemon                        (dispatcher in the foreground; exits if the lock is held)
conveyor hook <Event>                  (internal; called by Claude Code)
conveyor learning list | accept ID [--target f] | reject ID | apply --repo r
conveyor automation list | add --name n --schedule "…" [--precheck "…"] --action kind --arg json | enable/disable/run NAME | rm NAME
conveyor import jira --jql "…" [--repo a]
conveyor config list | get k | set k v
```

## 12. Desktop app (Wails v2, React + TS)

**Frontend:** plain CSS with variables, light and dark. No component library; React Router or simple state for views.

**Bindings:** `internal/ui.App` methods return plain structs. The frontend refetches on `db:changed`.

**Views:**
- **Board.** Columns Backlog, Todo, Running, Needs you, Review, Done (last 7 days). A card shows:
  - the key and title
  - repo chips
  - the last run summary (one line)
  - a `ctx_peak` badge and a status-change dropdown
- **Focus.** One list ordered needs_you → review → failed → running → todo. Each row has an inline comment box and an "Open" button.
- **Task page.**
  - Left: title, description, an acceptance-criteria checklist (from the last run's `criteria`), and the thread. The thread interleaves comments with run separators: "Run 7 · resume · 12 calls · 31K peak · 2m".
  - Right: the **Workspace panel** (task 008) and run history.
  - A comment box.
- **New task** dialog: title, description, repo multi-select with a primary choice, model, agent, a "start now" (todo) toggle.
- **Repos, Learnings, Automations, Settings** pages.

## 13. Conventions for implementers

- **Branch and worktree per task.** From the latest `origin/main`:

  ```
  git worktree add ../conveyor-wt/<NNN> -b task/<NNN>-<slug> origin/main
  ```

  Work there only, never in the main checkout.
- **Before the PR:**
  1. `gofmt -l .` is empty, and `go vet ./...` and `go test ./...` pass.
  2. If you touched the frontend: `cd frontend && npm ci && npm run build`, then `wails build -skipbindings` or `go build .` succeeds.
  3. Rebase on `origin/main` and re-run the checks.
- **PR:** title `Task NNN: <title>`. The body holds the spec link, what was done, deviations, and test evidence. Update `progress/progress.md` (your row) and append an "Outcome" section to your task file in the same PR.
- **Tests:**
  - Package-level unit tests against a temp `CONVEYOR_HOME` and an in-memory or temp-file DB.
  - Fake `claude`, `gh`, `git` remote and `osascript` are shell scripts on a temp `PATH`; record their argv to a file and assert on it.
  - No network and no real Claude in unit tests.
- **Style:**
  - Match the existing code: small files, doc comments on exported identifiers, errors wrapped with `fmt.Errorf("…: %w", err)`.
  - No new dependencies beyond Wails and its frontend toolchain, unless the task spec names one.
- **Scope:** do only your task. Note anything else you find under "Follow-ups" in your task file's Outcome.
