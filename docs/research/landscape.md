# Landscape: agent kanbans, dispatchers, and memory loops

Research for Conveyor (a local, single-operator kanban that dispatches tasks to real Claude Code
sessions on a subscription, with one git worktree per task). Researched 2026-10-24 from primary
sources: READMEs, docs, and source code via `gh api`, shallow clones, and `orca --help`. Marketing
copy was ignored. **UNVERIFIED** marks anything not confirmed in source or docs.

Repos were shallow-cloned for reading (outside this repo):
`/private/tmp/claude-502/-Users-jayasurya-work-conveyor/ad2c0db1-2b81-492b-b9d4-b8bd6139212e/scratchpad/src/{kanban-code,multica,orca}`.

---

## 0. TL;DR comparison

| | kanban-code | multica | orca | paperclip | hermes kanban | Claude Code native (`--bg` / agent view) |
|---|---|---|---|---|---|---|
| Shape | macOS app over your existing `~/.claude` sessions | Server, Postgres, and a local daemon. Issues assigned to agents | Electron IDE for worktrees, terminals, automations | "Company" of agents with org chart, budgets, heartbeats | SQLite board plus dispatcher that spawns hermes profiles | Supervisor process plus background sessions |
| Session driver | tmux plus interactive `claude` (TUI) | Headless `claude -p` with stream-json in and out | PTY terminal running the TUI agent | Headless `claude` per heartbeat | Own Python agent process | Interactive `claude` process, detached |
| Can you jump in? | Yes: embedded terminal or `tmux attach` | No terminal. You comment or steer through stream-json | Yes, it is the terminal | No (UNVERIFIED for any attach) | CLI or dashboard comments only | Yes: `claude attach <id>` |
| State detection | Claude hooks to `hook-events.jsonl`, then a reconciler | Daemon owns the process and reads the stream | Managed status hooks plus OSC title | Adapter owns the process | Heartbeats and PID reaping | `claude agents --json` (`state`/`status`/`waitingFor`) |
| Resume model | Resumes the same session in tmux | `--resume` keyed per (agent, issue), prior workdir reused | Optional reuse-session for automations | Session per (agent, taskKey), cwd-aware | Fresh worker. Context comes from comments and parent handoffs | Same session ID. Supervisor restarts it on reply or attach |
| Worktree | `claude --worktree <name>` | Per-task env dir plus `multica repo checkout` | First-class: managed worktree per run if chosen | cwd per agent | `worktree` workspace kind at `.worktrees/<id>/` | Auto-moves into `.claude/worktrees/` before the first edit |
| Agent/skill config | Repo-native (`~/.claude`) | Registered in multica, written into workdir per run | Repo-native (`.claude`) | Registered. Skills symlinked via `--add-dir` | Hermes profiles and skills | Repo-native |

**Biggest finding for Conveyor:** Claude Code now ships a native supervisor for background
sessions. It covers `claude --bg`, `claude agents --json`, `claude attach`, `claude logs`,
`claude stop`, `claude respawn`, `claude rm`, and it auto-isolates edits into a worktree. That
covers roughly half of what Conveyor's dispatcher would otherwise build (section 6.1). It is a
research preview, so treat the interface as unstable.

---

## 1. langwatch/kanban-code

- Repo: https://github.com/langwatch/kanban-code (Swift/SwiftUI, Apache-2.0, ~330 stars)
- Sources: `README.md`, `specs/**/*.feature` (Gherkin specs are the best documentation),
  `Sources/KanbanCodeCore/**`, `Scripts/hooks/hook.sh`

### 1.1 The "two modes"
- The modes are **Kanban mode** and **list mode** (the screenshots call list mode "Productive
  Mode"). In code this is `BoardViewMode { kanban, list }` in `Sources/KanbanCode/BoardViewMode.swift`.
- They are only presentation (`specs/board/view-modes.feature`). Kanban shows columns. List
  groups cards by status in a left rail beside a large embedded terminal, with
  Terminal/History/Pull Request tabs. **Stage logic is identical in both modes.**
- A separate, unrelated pair is `RemoteLink.Mode { boxd, mutagen }` for remote execution.

### 1.2 Columns and transitions
The column enum lives in `Sources/KanbanCodeCore/Domain/Entities/KanbanCodeColumn.swift`.

| Column | Entered when (from `specs/board/card-lifecycle.feature`, `specs/sessions/activity-detection.feature`) |
|---|---|
| Backlog | Card created and not launched |
| In Progress | **Only a hook** can promote a card here (UserPromptSubmit or tool activity). Polling never promotes |
| Waiting (`requires_attention`) | A `Stop` hook, a permission `Notification`, or a plan approval. Notifications are deduped within 62 s |
| In Review | A PR is found for the branch **and** the session has been idle for more than 5 min. If Claude is still working, the card stays In Progress |
| Done | All PRs are merged, or the PR is closed. Worktree cleanup is offered from Done |
| All Sessions | Archive of every discovered session. Auto-archives after 1440 min unless a worktree or tmux session is linked |

- When review feedback is addressed, the card goes In Review → In Progress → In Review and skips Waiting.
- Running subagents keep a card In Progress, up to a 2 h staleness cap.
- `SessionStart` with `source=compact` does **not** reset state.
- Activity states are `actively_working`, `idle_waiting`, `needs_attention`, `ended` (>1 h), and `stale` (>24 h).
  A 5-minute timeout matches Claude's tool timeout.
- PR lookups are batched with `gh` per repo, and cross-repo PRs are supported.

### 1.3 Launch, attach, and state detection
- Launch (`specs/sessions/launching.feature`):
  - A confirm dialog shows an editable prompt built from a template (e.g. `/orchestrate ${prompt}`).
  - A "Create worktree" checkbox is persisted, and the dialog previews the command `claude --worktree issue-123 '<prompt>'`.
  - The sequence is: tmux session `issue-123` → `cd project` → `claude --worktree issue-123` → send the prompt.
- Linking: the launch records `tmuxLink`, then the `SessionStart` hook adds `sessionLink`. A launch only claims the session it started.
- Attach: an embedded SwiftTerm view attaches to the tmux session, and a "Copy tmux attach" button exists. **You can always drop into the real TUI.**
- Hooks (`specs/system/headless-runtime.feature`, `Scripts/hooks/hook.sh`):
  - The daemon installs hooks into `~/.claude/settings.json` idempotently. They point at `~/.kanban-code/hook.sh`.
  - That script is plain bash with no jq. It extracts `session_id`, `hook_event_name`, and
    `transcript_path` and appends a line to `~/.kanban-code/hook-events.jsonl`.
- Statusline: writes context usage to `~/.kanban-code/context/<sessionId>.json`.
- Reconciler, not poller: it matches session ID → branch name → project path. State is held in
  `~/.kanban-code/links.json` (the single source of truth), with an Elm-style reducer.
- Queued prompts (`specs/terminal/queued-prompts.feature`):
  - Prompts queued for a card are auto-sent with `tmux send-keys` when a `Stop` arrives.
  - The send is suppressed if a `UserPromptSubmit` follows quickly.
- Auto-compaction steering:
  - 500k tokens: a self-compact nudge is queued.
  - 700k tokens: a steer is pasted.
  - 750k tokens: Escape plus `/compact`.
- Discovery: reads every session in `~/.claude/projects/*.jsonl`, with BM25 search. Fork and
  checkpoint are done by editing session files directly.
- CLI: `kanban list|status|show|sessions|capture|send|interrupt|transcript|export --json`, meant to be driven by a master agent.
- Other features: Pushover notifications, sleep prevention, a Slack bridge, and multi-assistant support (codex, gemini).

### 1.4 Context and configuration
- No context assembly of its own. It sends the prompt (optionally templated) and relies on repo
  `CLAUDE.md` and skills. Settings live in `~/.kanban-code/settings.json`.

**Steal**
- Promote to In Progress only on a hook.
- Stop or Notification → Waiting.
- PR plus idle → In Review.
- PR merged → Done.
- tmux-backed TUI so the operator can always attach.
- A dumb append-only hook script, with a reconciler on the app side.
- Queued follow-up prompts delivered on Stop.

**Avoid**
- Rewriting Claude's session files to fork or checkpoint. Use `--fork-session` instead.
- Mutating global `~/.claude/settings.json`. Prefer `--settings <file>` per launch.
- Pixel-scraping or sending keys for control when a structured channel exists.

---

## 2. multica-ai/multica

- Repo: https://github.com/multica-ai/multica (Go and Next.js, Postgres 17, ~54k stars)
- Sources: `server/pkg/agent/claude.go`, `server/internal/daemon/{prompt.go,types.go}`,
  `server/internal/daemon/execenv/*`, `pkg/db/queries/agent.sql`, and docs

### 2.1 Model
- Architecture: web, desktop, and mobile clients talk to a Go API (Chi plus WebSocket) backed by
  Postgres. A **local daemon** connects over WebSocket and spawns agent CLIs (26 providers).
- Entities:
  - Workspace.
  - Issue, with comments and threads.
  - Agent: name, provider, runtime, instructions, skills, MCP, custom env and args.
  - Run (a row in `agent_task_queue`).
  - Squads, autopilots, chat, projects.
- Triggers are **explicit only**:
  - Assign an issue.
  - `@mention` in a comment.
  - Chat.
  - Autopilot (cron or webhook; "create issue" or "run only").
  - `/note` comments never wake an agent, and backlog issues wait.
- Run states:
  - `deferred`, `queued`, `dispatched` (fails if stuck for more than 5 min), `waiting_local_directory` (directory lock), `running`, `completed`, `failed`, `cancelled`.
  - Heartbeat every 15 s. A runtime is offline after about 3 min.
  - Auto-retry up to 2 times (3 for network errors). On failure, an in-progress issue returns to `todo`.
  - Concurrency: 20 per daemon, 6 per agent.
- Steering: a reply to a running agent is injected into the live run through `--input-format stream-json`.

### 2.2 How Claude is invoked (`server/pkg/agent/claude.go`, `buildClaudeArgs`)
- Base command:
  `claude -p --output-format stream-json --input-format stream-json --verbose --permission-mode bypassPermissions --disallowedTools AskUserQuestion`
- Optional flags: `--strict-mcp-config`, `--model`, `--effort`, `--max-turns`, `--resume <ResumeSessionID>`, and `--settings <daemon file>`.
- The system prompt is **deliberately not** passed through `--append-system-prompt`. The daemon
  writes a per-task `CLAUDE.md` into the workdir instead (comment cites MUL-5392).
- User custom args may not override `-p`, `--output-format`, `--input-format`, `--permission-mode`, `--mcp-config`, or `--effort`.

### 2.3 Context assembly per run (`server/internal/daemon/prompt.go`)
- Prompt structure: `BuildPrompt` = `buildPromptBody` + `perTurnContextBlocks`. Per-run values are
  appended **after the cached prefix** (MUL-5377), so `CLAUDE.md` stays byte-stable and the prompt cache survives.
- A cold assignment gives pointers, not data:
  - "Your assigned issue ID is X. Start by running `multica issue get X --output json`."
  - Then: `multica issue comment list X --roots-only --summary --compact --output json`.
  - Expand a thread only on demand with `--thread <id> --tail 30`. Bulk pulls are explicitly discouraged.
- Comment-triggered runs (`buildCommentPrompt`):
  - The prompt contains `[NEW COMMENT] ... Focus on THIS comment`, plus earlier comments coalesced since the last run.
  - If resumed (`PriorSessionID` is set and `PriorSessionResumeUnavailable` is false), it gives **delta hints** ("new comments since X").
  - Otherwise it gives a cold hint to rebuild context through the CLI.
  - Replies are routed per thread.
- Resume key (`pkg/db/queries/agent.sql` `GetLastTaskSession`):
  - Uses the newest terminal run for **(agent_id, issue_id)**.
  - Skips retired or "poisoned" sessions (context overflow, invalid request).
  - Triage runs and execution runs are kept separate.
  - `PriorWorkDir` is reused.
- Manual retry continues the prior session when safe. `multica issue rerun` forces a fresh session and workdir.

### 2.4 Exec environment and skills (`server/internal/daemon/execenv/`)
- Workdir: `~/multica_workspaces/{workspace}/{task}`. Repos are checked out on demand with `multica repo checkout` (`local_worktree.go`).
- Per-task files written into the workdir:
  - `{workDir}/CLAUDE.md`: a runtime brief with agent identity, instructions, workspace context, and CLI commands. Sections are gated by task kind (`runtime_config_sections.go`).
  - `{workDir}/.claude/skills/{name}/SKILL.md`: skills bound to the agent (`context.go`).
  - `.multica/project`: project resources.
- Skills: workspace skills are registered in multica and bound to agents. Repo `.claude/skills`
  stay repo-managed and are not registered. Runtime-local skills can be disabled per agent.
- Environment: `MULTICA_TOKEN` (task-scoped, 24 h), `MULTICA_TASK_ID`, `MULTICA_AGENT_ID`, `MULTICA_WORKSPACE_ID`, `MULTICA_SERVER_URL`.

### 2.5 Human-in-the-loop
- Everything goes through issue comments, @mentions, chat, or steering. There is **no attach to a
  terminal**, and `AskUserQuestion` is disallowed, so the agent must ask through a comment.

**Steal**
- Resume keyed per (agent, issue), skipping poisoned sessions.
- A stable per-task `CLAUDE.md` brief, with volatile per-run data placed after the cached prefix.
- Pointer prompts: "run `X get` and fetch more only on demand".
- New-comment focus plus delta hints when resumed.
- Explicit triggers only.
- Disallow `AskUserQuestion` in headless runs.
- Separate statuses for "dispatched" and "waiting for directory lock".

**Avoid**
- Postgres, a server, and multi-tenancy are overkill for one operator.
- Headless-only means no way to jump into a live TUI.
- Registered skills duplicate what repo-native `.claude/skills` already gives you.

---

## 3. stablyai/orca

- Repo: https://github.com/stablyai/orca (Electron/TS, MIT, ~104k stars). The local CLI is at `~/.local/bin/orca` and was used read-only.
- Sources: `orca --help`, `orca automations --help`, `orca automations show --id <id>`,
  `src/main/automations/headless-automation-agent-launch.ts`, and `docs/site/content/docs/**`

### 3.1 Concepts
- Orca is a worktree-centric IDE: repos → worktrees → terminals (PTYs) running agent TUIs.
- CLI surface:
  - `worktree create|set|ps`
  - `terminal create|read|send|wait --for exit|tui-idle`
  - `automations`, `search` (full text across agent sessions), `skills`, `linear`, `computer`, `browser`
  - An experimental `orchestration` layer (Run/Task/Dispatch/worker_done/decision gates; `cli/orchestration.mdx`)
- Worktree checkpoints (`cli/worktree-checkpoints.mdx`): agents set a free-text comment and a
  status with `orca worktree set --comment ... --workspace-status todo|in-progress|in-review|completed`.
  This is effectively a lightweight per-worktree kanban.

### 3.2 Automations
- Create command:
  `orca automations create --name --trigger <preset|cron|rrule> --prompt --provider claude|codex|... [--precheck <cmd>] [--repo|--workspace|--project --host] --workspace-mode existing|new-per-run --base-branch --timezone --missed-run-grace-minutes --reuse-session|--fresh-session [--extra-agent-args model/effort/--add-dir only]`
- Launch path (`headless-automation-agent-launch.ts`):
  - Automations **always launch a TUI agent in a PTY**; the comment says "Automations observe PTYs".
  - With `new_per_run`, Orca creates a managed worktree with `startupAgent` and `startupPrompt`. Otherwise it launches into the existing worktree.
  - `reuseSession` sends the prompt to the previous live session.
- Run record fields: `status` (e.g. `skipped_missed`), `sessionKind: terminal`, `outputSnapshot`, `usage`, `precheckResult`, `terminalSessionId`.
- Real example on this machine, "Daily change review":
  - rrule `FREQ=DAILY;BYHOUR=16;BYMINUTE=30`, `workspaceMode existing`, `reuseSession false`.
  - `missedRunPolicy run_once_within_grace` with 180 min grace.
- A precheck is a shell command that gates the run cheaply (no LLM tokens spent if nothing changed).

### 3.3 Hooks, state, and memory
- From `agents/hooks-memory.mdx`:
  - Orca reads the repo's `.claude`/`.codex` config and **leaves CLAUDE.md and AGENTS.md alone**.
  - Worktree setup hooks run things like `pnpm install`.
  - Managed "agent status hooks" report working/waiting/done (`orca agent hooks status|on|off`).
  - The hook endpoint is written to `{userData}/agent-hooks/endpoint.env`, so hooks survive restarts.
- From `agents/claude-code.mdx`: a statusline hook emits OSC title events that drive state dots. Accounts can be hot-swapped.
- From `agents/session-history.mdx`: Orca scans transcripts on disk and resumes with `claude --resume <id>` in a new terminal.
- **Learnings or memory capture: none found.** A grep of source and docs for learning, lessons, and memory capture found nothing relevant. UNVERIFIED-absent.

**Steal**
- Precheck commands before LLM runs.
- RRULE with a missed-run grace policy.
- Workspace mode `existing` vs `new-per-run`.
- Reuse-session vs fresh-session as a per-automation choice.
- `terminal wait --for tui-idle`.
- The hook endpoint stored in a file so hooks survive app restarts.
- Agent-set worktree comment and status as a cheap progress channel.

**Avoid**
- A full IDE and Electron surface.
- Observing PTY output as the state source when hooks and `claude agents --json` exist.

---

## 4. paperclipai/paperclip (brief)

- Repo: https://github.com/paperclipai/paperclip (TS, MIT, ~107k stars; Node 24.11; `npx paperclipai onboard`)
- Pitch: "If OpenClaw is an employee, Paperclip is the company."
- Four pillars: task manager, org chart, agent training, agentic OS.
- Systems:
  - Identity and access.
  - Org chart: roles, titles, reporting lines, budgets.
  - Tasks: atomic checkout, execution locks, goal ancestry.
  - Heartbeat execution: DB-backed wakeup queue with coalescing, budget checks, secret injection, skill loading.
  - Governance and approvals, budgets, routines, plugins, secrets, company export and import.
- Heartbeat flow (`docs/guides/agent-developer/how-agents-work.md`):
  - trigger → adapter → process → agent calls the Paperclip API → result capture → run record.
  - Env: `PAPERCLIP_AGENT_ID`, `PAPERCLIP_COMPANY_ID`, `PAPERCLIP_API_URL`, `PAPERCLIP_API_KEY` (JWT), `PAPERCLIP_RUN_ID`, `PAPERCLIP_TASK_ID`, `PAPERCLIP_WAKE_REASON` (`issue_assigned`, `issue_comment_mentioned`, ...), `PAPERCLIP_WAKE_COMMENT_ID`, `PAPERCLIP_APPROVAL_ID`.
  - Agent statuses: active, idle, running, error, paused, terminated.
- Claude adapter (`docs/adapters/claude-local.md`):
  - Config: `cwd`, `model`, `promptTemplate` with `{{agentId}}`-style variables, `maxTurnsPerRun` (300), `dangerouslySkipPermissions: true`.
  - The session ID persists across heartbeats, keyed by (agentId, taskKey) in `agentTaskSessions`. Resume is cwd-aware, and an unknown session falls back to a fresh one.
  - A poisoned `previous_message_id` is auto-rotated, validated before persisting, and cleared.
  - Skills are injected as a temp dir of symlinks passed with `--add-dir`.
- Why it is heavy: org and governance modeling, budgets and approvals, multi-company tenancy, a
  JWT API, and a plugin system. None of this is needed for one operator. The useful kernel is
  the same as multica's: a wake reason in the env plus resume per (agent, task).

**Steal**
- A `WAKE_REASON` and `WAKE_COMMENT_ID` env contract.
- Validate a session before persisting it, and auto-rotate a poisoned resume.
- Skills via `--add-dir` symlink dirs (no writes into the repo).

**Avoid**
- Orgs, budgets, approvals, and the company abstraction.

---

## 5. NousResearch/hermes-agent (memory and learning loop)

- Repo: https://github.com/NousResearch/hermes-agent (Python, MIT). Docs: `website/docs/user-guide/features/{memory,skills,curator,kanban}.md`
- README claim: "the only agent with a built-in learning loop". The pieces are agent-curated
  memory with nudges, autonomous skill creation and improvement, FTS5 session search, and Honcho user modeling.

### 5.1 Memory (`features/memory.md`)

| Store | Purpose | Cap |
|---|---|---|
| `~/.hermes/memories/MEMORY.md` | Agent notes: environment facts, conventions, lessons | 2,200 chars (~800 tokens) |
| `~/.hermes/memories/USER.md` | User profile and preferences | 1,375 chars (~500 tokens) |

- **Frozen snapshot:** memory is rendered into the system prompt once at session start, with a
  usage header (e.g. `[67% — 1,474/2,200 chars]`) and entries separated by `§`. Writes go to disk
  immediately but appear only in the **next** session. This is done to preserve the prefix cache.
- The `memory` tool supports add, patch, and remove with substring matching. There is no read
  action, because memory is already in the prompt. `replace` was removed because models used it to wipe multi-fact entries.
- No auto-compaction: a write over the cap errors, and the agent must consolidate in the same turn.
- Optional `write_approval: true` stages writes until `/memory approve`.
- Rule of thumb from the docs: put a recurring-task fact in a **skill** (loaded only when relevant), not in memory.
- "Memory needs session boundaries": the loop of forget → recall → search only works if sessions
  end. That argues for fresh sessions per task, with memory carried forward, not endless sessions.

### 5.2 Background review ("learning loop")
- After a turn, a **forked agent** reviews the conversation and may write memory or skills. Frequency is set by
  `memory.nudge_interval` and `skills.creation_nudge_interval`. The defaults are not stated in the doc page (UNVERIFIED).
- A same-model review keeps the system prompt, tools, and transcript byte-identical, so it is
  mostly prompt-cache reads.
- A cheaper model can be used (`auxiliary.background_review`); it then gets a digest (recent
  turns verbatim plus a summary of older ones). The docs report the same memory capture quality
  and near-identical skill capture, at roughly 3–5x lower cost.
- Cost controls: `background_review.enabled: false` and `max_input_tokens`. The docs warn it "can burn a meaningful share of total tokens".

### 5.3 Skills as procedural memory (`features/skills.md`)
- `skill_manage` actions: create, patch (old/new string), patch with full content, delete, write_file, remove_file.
- The system prompt asks the agent to save a skill when it:
  1. worked out a repeatable multi-step workflow,
  2. hit dead ends and then found the working path, or
  3. was corrected by the user.
- Content rule: "**lessons, not logs**". A pitfall is stated as a general rule plus one clause of
  *why*. No PR numbers, dates, or incident narration. An advisory linter flags
  `incident-log-shape`, `references-sprawl` (more than 60 files), and `oversized-body` (more than ~24k chars).
- Progressive disclosure:
  - L0 `skills_list` (name and description, about 3k tokens).
  - L1 `skill_view(name)`.
  - L2 `skill_view(name, path)`.
  - Skills added mid-session arrive as a one-time note on the next message, and the system prompt is updated at the next compaction (to protect the cache).

### 5.4 Curator (`features/curator.md`)
- Triggered by inactivity, not cron: at least `interval_hours` (168) since the last run and at least `min_idle_hours` (2) idle.
- Deterministic pass: unused for 14 days → `stale`; unused for 30 days → `.archive/`. Pinned skills and skills referenced by cron are exempt. Nothing is ever auto-deleted.
- Optional LLM consolidation into umbrella skills (`consolidate: false` by default; 50–100 API calls per sweep).
- Has backups and rollback, and a per-mutation ledger.

### 5.5 Hermes Kanban (`features/kanban.md`), the closest analogue to Conveyor
- Storage: SQLite at `~/.hermes/kanban.db`, one DB per board.
- Task statuses: `triage | todo | ready | running | blocked | review | done | archived`.
- Dependencies: parent → child links. The dispatcher promotes `todo` → `ready` when all parents are done.
- Dispatcher (default every 60 s):
  - Reclaims stale claims and crashed workers (PID gone).
  - Reaps workers still alive 2 min after completing, matched on PID plus spawn fingerprint.
  - Auto-blocks a task after `failure_limit` (2) consecutive spawn failures.
  - A stale timeout (4 h with no heartbeat in the last hour) requeues without counting as a failure.
- Workers get a `kanban_*` toolset (via the `HERMES_KANBAN_TASK` env), **not** a CLI. Tools:
  `kanban_show`, `kanban_complete(summary, metadata)`, `kanban_request_review`,
  `kanban_request_changes`, `kanban_block(kind=dependency|needs_input|capability|transient)`, `kanban_heartbeat`, and `kanban_comment`.
- Workspaces: `scratch` (deleted on done), `dir:<abs path>`, and `worktree` (`.worktrees/<id>/`, kept).
- Context handoff:
  - A worker reads `build_worker_context`: title, body, prior attempts, the comment thread, and
    `## Parent task results` (the parent's summary and metadata verbatim).
  - **Follow-up work is a new child card, not a reopened done card.** "Repo state tells the
    follow-up worker *what* the code looks like, but not *why*."
- PR completion contract: a card declared against a repo cannot complete until the required GitHub checks are green.
- Note: this machine has a `kanban-work` skill ("pick up and work a hermes kanban task"), so the user has likely used it (UNVERIFIED).

**Steal**
- Small, capped memory with a usage header, frozen per session.
- Lessons-not-logs skill rules, including the linter heuristics.
- A structured `complete(summary, metadata)` handoff.
- Block kinds that route to a human vs wait on a dependency.
- New child card for follow-ups, carrying the parent summary.
- Stale-claim and PID reaping.
- Spawn-failure auto-block.

**Avoid**
- A per-turn background review fork by default (token burn). Run review once per task at completion instead.
- A shared home dir written by multiple agents. The docs warn that compounding writes corrupt memory.

---

## 6. Claude Code native mechanisms (code.claude.com/docs)

Pages: `cli-reference`, `hooks`, `memory`, `sessions`, `headless`, `skills`, `context-window`,
`agent-view` (all under `https://code.claude.com/docs/en/`). Version gates are quoted where the docs give them.

### 6.1 Background sessions and agent view (research preview) — `agent-view.md`

| Command | Use for Conveyor |
|---|---|
| `claude --bg [--name N] [--agent A] "<prompt>"` | Dispatch. Prints a short id. Cannot be combined with `-p` |
| `claude --resume <uuid> --bg "<prompt>"` | Follow-up turn in the background on the same session (≥ v2.1.257). Otherwise it forks a copy and prints `note:` |
| `claude agents --json [--all] [--cwd P]` | **The supported state API.** Fields: `id`, `sessionId`, `name`, `cwd`, `kind`, `state` (`working`/`blocked`/`done`/`failed`/`stopped`), `status` (`busy`/`waiting`/`idle`), `waitingFor` (`permission prompt`, `input needed`, ...) |
| `claude attach <id>` | Operator jumps into the live TUI |
| `claude logs <id>` / `claude stop <id>` / `claude respawn <id>` / `claude rm <id>` | Tail output, stop, restart, delete (deletes the worktree only if that is safe) |

- A supervisor process hosts the sessions, so they survive closing the terminal and sleep.
  Idle sessions with no attached terminal are stopped after about 1 h and resume on reply or attach.
- **Auto worktree:** before the first edit, the session moves into `.claude/worktrees/` unless it
  is already in a linked worktree (`worktree.bgIsolation: "none"` disables this).
- On finish it commits and pushes, opens a draft PR "when the task calls for it", and never
  pushes to main or force-pushes. CLAUDE.md instructions override this. The `#N` PR label shows in agent view.
- `~/.claude/jobs/<id>/` is **not** a stable interface; use `claude agents --json`.
  `$CLAUDE_JOB_DIR/tmp` is the documented place for a session to write its own progress.
- Each session consumes subscription quota independently.
- Implication: Conveyor could dispatch by putting a worktree at `cwd` and running
  `claude --bg --name <task>`, then poll `claude agents --json` for state, use `claude attach`
  for HITL, and send follow-ups with `--resume <uuid> --bg`.
  - Risk: research preview, so the CLI surface may change.
  - Alternative: tmux plus TUI (kanban-code style), or `-p` stream-json (multica style).

### 6.2 Headless and session flags (`cli-reference.md`, `headless.md`, `sessions.md`)
- `-p` with `--output-format text|json|stream-json`, `--input-format stream-json` (multi-turn
  steering on stdin), `--include-partial-messages`, `--verbose`. The first stream event is `system/init` (model, tools, MCP, plugins).
- Session flags:
  - `--session-id <uuid>`: Conveyor can pick the ID itself, so no parsing is needed.
  - `--resume <id|name|.jsonl path>`, `--continue`.
  - `--fork-session`: new ID when resuming.
  - `--name/-n`.
  - `--no-session-persistence`.
  - `--continue` skips `-p` and SDK sessions unless you also pass `-p`.
- Run limits: `--max-turns` and `--max-budget-usd` (print mode only; the budget is a client-side estimate).
- Isolation and extra context:
  - `--worktree/-w <name>` → `<repo>/.claude/worktrees/<name>`. It accepts `#<PR>` or a PR URL. `--tmux` (requires `-w`) creates a tmux session.
  - `--add-dir` grants file access, and skills in that dir load, but most other `.claude/` config is not discovered.
  - `--settings <file|json>` gives per-launch overrides, a good way to install Conveyor's hooks without touching `~/.claude/settings.json`.
- Startup options:
  - `--bare` skips hooks, skills, CLAUDE.md, and memory. **It does not use the subscription login** and needs `ANTHROPIC_API_KEY`, so avoid it.
  - `--permission-mode default|acceptEdits|plan|auto|dontAsk|bypassPermissions`.
- System prompt:
  - `--append-system-prompt[-file]` and `--system-prompt[-file]`.
  - **The recorded snapshot:** the system prompt is built on the first request and reused on
    `--resume` and `--continue` until compaction. A different append text passed on resume only takes
    effect after compaction (`--system-prompt-snapshot off` changes that).
  - `__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__` (≥ v2.1.275) splits a custom prompt into cached and dynamic parts.
  - `--exclude-dynamic-system-prompt-sections` improves cache reuse.
- Transcripts:
  - Stored at `~/.claude/projects/<cwd with non-alphanumerics → '-'>/<session-id>.jsonl`, kept for 30 days by default (`cleanupPeriodDays`).
  - Resuming one session from two terminals without forking interleaves both into one transcript.

### 6.3 Hooks (`hooks.md`)
- Events: SessionStart, Setup, UserPromptSubmit, PreToolUse, PermissionRequest, PostToolUse,
  Notification, SubagentStart/Stop, TaskCreated/Completed, Stop, StopFailure, PreCompact,
  PostCompact, SessionEnd, WorktreeCreate/Remove, CwdChanged, FileChanged, InstructionsLoaded, and others.
- Handler types: `command`, `http` (POSTs JSON to a URL, a natural fit for a local Conveyor server), `mcp_tool`, `prompt`, `agent`.
- Common input: `session_id`, `transcript_path`, `cwd`, `permission_mode`, `hook_event_name`, and `agent_id` for subagents.
- Per-event details:

| Event | Details |
|---|---|
| SessionStart | Matchers `startup`/`resume`/`clear`/`compact`/`fork`. `hookSpecificOutput.additionalContext` is injected as a system reminder before the first prompt. `CLAUDE_ENV_FILE` lets the hook export env vars. Only `command` and `mcp_tool` handlers |
| Stop | Input: `stop_hook_active`, `last_assistant_message` (use this, not the transcript), `background_tasks`. `decision:"block"` + `reason`, or `additionalContext`, continues the session, subject to `stop_hook_active` and an 8-consecutive-continuation guard |
| Notification | Matchers include `permission_prompt`, `idle_prompt`, `agent_needs_input`, `agent_completed` |
| PreCompact | Gets `trigger` and `custom_instructions`. Can block. `matcher: manual\|auto` |
| PostCompact | Gets `compact_summary`, which is useful to persist as a task digest |
| SessionEnd | Reasons `clear`/`resume`/`logout`/`prompt_input_exit`/`other`. 1.5 s budget |
| UserPromptSubmit | `additionalContext` is added beside the prompt. Exit 2 blocks the prompt |

### 6.4 Memory, CLAUDE.md, and skills (`memory.md`, `skills.md`, `context-window.md`)
- CLAUDE.md files above the cwd load at launch. Subdirectory CLAUDE.md files load on demand.
  `.claude/rules/*.md` with a `paths:` frontmatter are path-scoped. `@imports` organize content but do not save tokens.
  Size guidance: under 200 lines per file.
- **Auto memory:**
  - Stored at `~/.claude/projects/<project>/memory/MEMORY.md` plus topic files. The first 200 lines or 25 KB load each session, and topic files are read on demand.
  - Claude writes it itself, with a reminder near the limits.
  - `<project>` is derived from the git repo, so **all worktrees of a repo share one memory dir**.
    That is good for Conveyor (lessons accumulate per repo) but means parallel tasks write to the same files.
  - Settings: `autoMemoryEnabled`, `autoMemoryDirectory`.
- Skills: `~/.claude/skills/<name>/SKILL.md` (personal), `.claude/skills/` (project, plus nested
  dirs), and `--add-dir` dirs. Custom commands are merged into skills.
  `disable-model-invocation: true` makes a skill user-only.
- What survives compaction:
  - Re-injected from disk: project-root CLAUDE.md, unscoped rules, auto memory, the plan file, skill descriptions (≥ v2.1.304).
  - Up to 5 recently touched files are re-read.
  - Invoked skill bodies are re-injected, capped at 5k tokens each and 25k in total.
  - Hook-added context is only summarized, but **SessionStart hooks with `compact` re-run** after compaction.

---

## 7. Design implications for Conveyor (synthesis)

### 7.1 Driver choice

| Option | Pros | Cons |
|---|---|---|
| A. `claude --bg` + `claude agents --json` + `claude attach` | Native state machine (`blocked`/`waitingFor`), attach, auto worktree, survives terminal close, subscription login | Research preview and unstable surface. Less control over flags on resume. `~/.claude/jobs` is not an API |
| B. tmux + TUI (`claude --worktree --session-id`) + hooks | Proven (kanban-code). Full TUI attach. Total control | Must build state from hooks. send-keys is fragile. Dedup logic needed |
| C. `claude -p --input-format/--output-format stream-json` | Structured events, steering through stdin, easy parsing (multica) | No TUI to jump into. Interactive approvals impossible, so it needs bypass or allowlists |

A hybrid is possible:
- Run B or A for each task.
- Pre-assign `--session-id` so Conveyor knows the transcript path up front.
- Install hooks per launch with `--settings` (an `http` hook to Conveyor), not globally.

### 7.2 Stage transitions (proven pattern)
- `todo → running` on the first SessionStart or UserPromptSubmit hook for that session (never on a poll).
- `running → needs_input` on Stop, a permission/idle Notification, or `state=blocked` from agents --json.
- `→ in_review` when a PR exists for the branch **and** the session is idle (kanban-code waits 5 min).
- `→ done` when the PR is merged or closed. Offer worktree cleanup.
- A review comment or follow-up sends the card back to running. Pick one policy:
  - Resume the same session (multica, kanban-code), or
  - Create a new child card that carries the parent summary (hermes).

### 7.3 Token-efficient context per run
1. A stable per-task brief that is byte-identical across resumes. The options:
   - Repo CLAUDE.md plus a SessionStart `additionalContext`.
   - The task file written into the worktree.
   - Not `--append-system-prompt`, since a resume freezes the first prompt anyway.
2. The first prompt holds pointers, not dumps: "task id X; run `conveyor task show X`".
3. Follow-ups use `--resume <session>` with only the delta, e.g. "[NEW COMMENT] ... focus on this".
   On a cold start (resume unavailable or poisoned), give the cold hint and let the agent pull what it needs.
4. Keep a resume map of (task) → last good session id. Skip sessions that died from context overflow or invalid requests.
5. Persist `compact_summary` from PostCompact, and `last_assistant_message` from Stop, as a cheap task digest for the board UI.

### 7.4 Learning loop without building one
- Use native auto memory, which is shared across the repo's worktrees. Add a single end-of-task step:
  - A Stop hook, or a final instruction in the brief, telling Claude to record lessons ("lessons, not logs")
    into a repo skill or `.claude/rules`, or into auto memory.
- Option: a once-per-task review run with `--resume <id> --fork-session -p` so the work session is not polluted.
- Prune skills like hermes's curator (stale → archive by last use) only if skills actually pile up. YAGNI until then.

### 7.5 What not to build
- No server, database cluster, org chart, budgets, or approvals (paperclip, multica).
- No registered skill store; repo-native `.claude/skills` is enough.
- No editing of Claude's `.jsonl` files to fork; use `--fork-session`.
- No global `~/.claude/settings.json` mutation; use per-launch `--settings`.
- No per-turn reflection forks (hermes warns they burn tokens).

### 7.6 Open questions / UNVERIFIED
- Whether `claude --bg` accepts `--settings` and `--session-id`. The docs say configuration flags
  carry over across restarts, but those two were not checked specifically.
- Whether `claude agents --json` reports sessions that were started with `-p`. Probably not, since `--bg` cannot be combined with `-p`.
- Hermes nudge interval defaults.
- Whether Paperclip offers any terminal attach.
- Version gates: this machine runs Claude Code 2.1.310 (`claude --version`), which satisfies every gate cited above (≥ v2.1.257 for `--resume --bg` in place, ≥ v2.1.304 for skill re-injection). That a feature works in practice was not tested.
