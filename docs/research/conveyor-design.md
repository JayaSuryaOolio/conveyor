# Conveyor design direction (v2), from the research

Inputs:
- [landscape.md](landscape.md): kanban-code, multica, orca, paperclip, hermes, and Claude Code's native mechanisms
- [multica-oolio-98.md](multica-oolio-98.md): measured token costs of a real multica task
- worktree-studio's integration seams: `internal/claudehook`, `internal/spotlight`, `server.addr`, the `?terminal=` deep link
- the operator's stated wants (2026-10-24)

This doc proposes changes to `docs/kanban-board-prd.md` and `PLAN.md`. Neither is edited yet; see §11.

---

## 1. What we're optimising for

| Want | Where it came from | Design answer |
|---|---|---|
| Tasks run on their own, in a Jira-like view where comments trigger runs | multica | Task page with a comment thread. Comments are queued and **coalesced** into one run (§5) |
| Jump into an interactive shell or Claude session with the task's context | multica's biggest gap; kanban-code does it | Every run is an attachable interactive session. The **Workspace panel** on each task (§4) |
| See the task's worktree(s), ideally one per repo | operator, mid-research | Invariant: **1 task = 1 workspace folder; per repo: 1 worktree = 1 branch = at most 1 PR** (§2) |
| Unattended runs, safely | operator | Task runs use `bypassPermissions`, confined to a Conveyor-owned workspace folder. Interactive sessions keep normal permissions (§5.1) |
| Two modes: a board, plus a productive list | kanban-code (they're two *views* over one state model, not two run modes) | Board view and Focus list view (§3) |
| Tracking all the way to done | operator | Done = PR merged, detected via `gh`, not "agent said so" (§3) |
| Efficient tokens: a summary by default, full context only on demand | multica: 92K context for a 5-call revert run | Fresh session from a structured brief when the old one is big. Resume only when it is small (§6) |
| Uses the repo's own agents and skills, nothing registered | multica copies skills into `workdir/` and runs above the repo | `cwd = worktree root`, nothing injected into `CLAUDE.md` (§7) |
| Self-evolving | hermes | A learnings inbox that feeds repo-native files through normal PRs (§8) |
| Automations | orca | A cron schedule plus a precheck that creates tasks or comments (§9) |
| Simple, extensible | operator; paperclip as the counter-example | CLI `--json`, REST, per-launch Claude hooks, outbound event scripts. No plugin system (§10) |

Non-goals carried over from the PRD: multi-user, org charts and budgets (paperclip), our own agent loop or credentials (hermes), a skill/agent registry (multica).

---

## 2. Domain model (small)

```
Task ──1:1── Workspace folder  ~/.conveyor/workspaces/<KEY>/
  │             └──1:N── Repo checkout (repo, worktree_path=<folder>/<repo>, branch, pr_url, primary?)
  │
  ├──1:N── Comment   (author: human|agent|system, body, consumed_by_run)
  ├──1:N── Run       (trigger, session_id, started/ended, status, tokens_in/out/cache, ctx_peak)
  ├──1:N── Handoff   (one per finished run: summary, state, next, from the status block)
  └──1:N── Learning  (proposed → accepted|rejected, target)
```

- **Task keys** are human-readable: `CV-12`, or the Jira key when imported. The key names the worktree, the branch (`<type>/<KEY>-<slug>`) and the Claude session (`--name KEY`).
- **A task can span several repos.** Each repo gets one worktree in the task's workspace folder, all on the same branch name. One repo is **primary**: it is the session's cwd, and the others are passed with `--add-dir` plus `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1`. Verified in [spike-bg.md](spike-bg.md): every repo's `CLAUDE.md`, skills and agents load. Running from the parent folder (multica's layout) loads none of them.
- **At most one PR per repo per task, enforced.** Follow-up work that needs a second PR in the same repo becomes a child task; the agent may suggest it via `<spawn>` in its status block (§5.3). This removes the "two PRs on one task" confusion from OOLIO-98.
- **A session lineage, not a single session.** A task has many runs. Consecutive runs either resume the previous session or start fresh from the brief (§6). `runs.session_id` keeps every session reachable.
- `kanban_task_events` stays as the history log. The existing store package from Task 001 extends rather than restarts. Its status set changes (§3).

## 3. Status model and the finish line

```
backlog ──> todo ──> running ──┬──> needs_you ──(comment / attach)──> running
                               ├──> review ──(PR merged)──> done
                               │      └──(review comments / human comment)──> running
                               └──> failed ──(requeue)──> todo
cancelled: human only, from any state
```

| Status | Set by | Meaning |
|---|---|---|
| `backlog` | human, Jira import | Not ready. The old `draft` and `grooming` merged: one column, not two |
| `todo` | human, automation | Dispatcher may claim it |
| `running` | dispatcher on launch; `UserPromptSubmit` hook | A live session is working |
| `needs_you` | `Stop` hook with no status block, permission `Notification`, or a run that failed with budget left | The human is the blocker. Sort these first |
| `review` | status block `done` **and** a PR exists for the branch | The agent's part is over; the PR is the work item now |
| `done` | `gh pr view` reports `MERGED` (polled), or human closes a no-PR task | The finish line |
| `failed` | circuit breaker (N consecutive bad runs), missing resume target | Investigate. Shown in the "Needs you" column with a red badge |
| `cancelled` | human only | Also kills the session |

Acceptance criteria are a checklist in the description. The status block reports each one as met or unmet with evidence. The task page renders them as checkboxes, and `done` doesn't require them all ticked, but the card warns when any are unmet. This is the "complete to finish line" piece: it ties agent claims to evidence and ties *done* to a merged PR.

### Views (kanban-code's "two modes")
- **Board**: Backlog | Todo | Running | Needs you | Review | Done (last 7 days).
- **Focus**: a single list sorted by what needs the human (needs_you, then review with new PR comments, then failed, then running), each row with inline reply and "Open". This is the productive mode for working a queue.

## 4. Task page (Jira-like) and the Workspace panel

Left column: title, description, acceptance criteria, then the thread. Comments and agent replies are interleaved, with run separators like "Run 7 · resumed · 12 calls · 31K peak ctx · 2m". Right column:

**Workspace panel** (the operator's mid-research ask):
- repo, worktree path, branch, PR link and state, and spotlight state if that integration is on
- sessions in this worktree: the `runs` list, plus any manual sessions found by scanning `~/.claude/projects/<slug(worktree)>/*.jsonl` (same trick as worktree-studio's `ListSessionsForCwd`), so hand-started sessions also show up
- actions:
  - **Attach**: the live session, `claude attach <id>` (§5.1)
  - **Resume in terminal**: `cd <wt> && claude --resume <session_id>`
  - **New session from brief**: `cd <wt> && claude "Read .conveyor/brief.md, then wait for my instructions."`
  - **Shell**: `cd <wt>`
  - **Open in editor**: `code <wt>` or `$EDITOR`

  Each action runs through the configured terminal opener (§10) and also has a copy-command button.

**Rule while a human is attached:** the dispatcher won't start an automated run for that task; comments queue up. The `UserPromptSubmit` and `Stop` hooks still fire, so the card stays correct while you drive.

## 5. Runs

### 5.1 Substrate: interactive Claude, detached
Every run is a real interactive Claude Code session that can be attached at any time. The PRD's creation-time headless/headful split goes away.

- **`claude --bg`**, verified by the spike ([spike-bg.md](spike-bg.md)):

  ```
  cd ~/.conveyor/workspaces/CV-12/<primary>
  CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1 claude --bg "<run prompt>" \
    --name CV-12 --settings ~/.conveyor/claude-settings.json \
    --permission-mode bypassPermissions --strict-mcp-config \
    [--agent <repo agent>] [--model ...] --add-dir ../<other repos>
  ```

  - The prompt goes **before** `--add-dir`. That flag is variadic and swallows a trailing prompt.
  - `--session-id` is ignored. Take the id from stdout (`backgrounded · <id>`) and the full UUID from the first hook.
  - The settings file must contain `"worktree": {"bgIsolation": "none"}`. Otherwise `--bg` escapes the workspace into `<main repo>/.claude/worktrees/` on its first edit.
  - Attach with `claude attach`, stop with `claude stop`. Liveness comes from `claude agents --json`; state comes from **our hooks**.
- **Permissions.** Task runs use `bypassPermissions`. Safety comes from the boundary: every checkout lives under `~/.conveyor/workspaces/<KEY>/`, never in the operator's main clones. Interactive sessions from the Workspace panel ("Resume in terminal", "New session from brief") launch with normal permissions. Note that `claude attach` joins the live run *in* bypass mode.
- **Trust.** `--bg` refuses untrusted folders. A worktree inherits trust from its main repo, so this only bites repos never opened in Claude. Fail the launch with "open <repo> in Claude once". Never write `~/.claude.json`.
- `claude -p` is kept only for automations' cheap side jobs (§9), never for task runs, because a `-p` run can't be joined live.

### 5.2 Hooks are per-launch, not global
Pass `--settings ~/.conveyor/claude-settings.json` containing HTTP hooks (`Stop`, `UserPromptSubmit`, `Notification`; `SessionStart` doesn't fire under `--bg`) that post to `http://localhost:<port>/hooks`, with the port read from `~/.conveyor/server.addr`. This replaces PRD §5.1's global `~/.claude/settings.json` installer:
- the merge and clobber risk goes away
- Conveyor never observes the operator's unrelated sessions

Manual sessions started from the Workspace panel go through the same launcher, so they're tracked too. Hook handlers always return 200 (worktree-studio's rule). The receiver maps `session_id` first and `cwd` prefix second.

### 5.3 Status block (replaces PRD §5.3's tag)
The final message of a finished run ends with:

```
<conveyor>
status: done | failed            # omit the whole block to ask the human a question
summary: one paragraph
state: branch X @ abc123, PR #N (draft|ready), tests: pass|fail|not run
next: what remains, if anything
criteria: [x] AC1 — evidence; [ ] AC2 — why not
learning: optional, 0..n lines (§8)
spawn: optional child-task titles
</conveyor>
```

"No block means needs_you" is kept from the PRD. The `Stop` hook payload carries `last_assistant_message`, so there's no transcript parsing on the fast path. `summary`, `state` and `next` become the run's **Handoff** row, which feeds the brief.

### 5.4 Comment → run
- A human comment on a task that isn't running → enqueue a run. Comments that arrive while a run is queued or in flight are **coalesced**: the next run gets all unconsumed comments.
- A 20 s debounce absorbs the "second comment 24 s later" pattern seen on OOLIO-98.
- A comment on a running task is shown as "queued" and delivered on the next `Stop`. We never inject keystrokes mid-turn.
- Agent output lands in the thread as an `agent` comment: the run's final message minus the block, with the block rendered as a card.

## 6. Context strategy (the token lever)

Three tiers. Only the first two are ever injected.

| Tier | What | Size | Used |
|---|---|---|---|
| Hot | The previous Claude session | its current context | resume, if `ctx_peak` of the last run < **60K** (operator-confirmed, configurable) and the last run ended cleanly. Mechanics: `claude stop <id>`, then `claude --bg --resume <uuid> "<delta>"` with **no flags**, which keeps the same id and saved options. Passing flags, or resuming a still-live session, forks a copy instead |
| Warm | `.conveyor/brief.md` in the worktree, rendered by Conveyor | ~2–4K | fresh session otherwise; always kept current |
| Cold | full thread, all handoffs, old transcripts | unbounded | on demand: `conveyor task show CV-12 --thread --json`, transcript paths in the brief |

**brief.md** (regenerated before every run; excluded via `.git/info/exclude`):
- title, description, acceptance criteria with current check state
- decisions: human comments the operator pinned, plus every comment's first line, newest last, capped
- current state and next, from the last Handoff
- the last 3 handoff summaries, one line each
- the protocol (§5.3)
- a pointer to the Cold-tier command

**Run prompt** stays tiny and pointer-shaped, like multica's but without the redundant re-fetch:
- resume: `New comments on CV-12:\n<delta>\n(brief updated at .conveyor/brief.md if needed)`
- fresh: `You're continuing task CV-12. Read .conveyor/brief.md first. New comments:\n<delta>`

**Why 60K.** On OOLIO-98, small follow-ups started at 78–150K of resumed context. A fresh session pays about 15–20K baseline plus a few K of brief, then re-reads only the files it touches. Below about 60K, resuming is cheaper and keeps warm file context. Expose `ctx_peak` per run on the card so the threshold can be tuned from real data.

**Baseline trim:**
- Launch with `--strict-mcp-config --mcp-config <wt>/.mcp.json` when the repo has one. That drops every global MCP server the task doesn't need.
- Optional per-task `--model` (a "sweep/revert" profile on a cheaper model).

## 7. Agents and skills: repo-native, zero registration

- `cwd = worktree root` → Claude Code loads the repo's `CLAUDE.md`, `.claude/skills`, `.claude/agents`, `.claude/settings.json` and `.mcp.json` natively.
- A task's "agent" is optional and just `--agent <name>`, picked from `.claude/agents/*.md` in that repo, with the list read live for the dropdown. No copies, no registry.
- Conveyor's own instructions live only in the brief and the run prompt, never in `CLAUDE.md`.

## 8. Self-evolving: learnings inbox → repo-native memory

hermes's loop works because memory is small, capped and curated. Ours stays in Claude Code's native places so a standalone session benefits too.

1. **Capture.** `learning:` lines in the status block, prompted with hermes's rule: "lessons, not logs: a correction you received, a dead end and the working path, a repeatable workflow". There's no per-turn reflection fork; hermes's own docs flag that one as a token sink.
2. **Inbox.** A Learnings page lists proposals with their source task and run. Actions are accept (choose a target), edit or reject.
3. **Apply through the conveyor itself.** Accepted learnings for a repo batch into an auto-created task, "Apply learnings to <repo>", which edits `CLAUDE.md`, a skill, or `.claude/agents/*` and opens a PR. Personal learnings (cross-repo) go to `~/.claude/CLAUDE.md`.

   The human reviews the PR, so memory never drifts silently, and the mechanism needs no new machinery.
4. **Curate.** Optional automation (§9): "monthly: review CLAUDE.md and skills for stale or duplicate rules, propose a PR".

## 9. Automations (orca-style, minimal)

```
automation: name, schedule (cron), precheck (shell; exit 0 = run), action
action = create_task(template) | comment(task selector, text) | cli(command)
```

- **Precheck** spends zero tokens until there's work (orca's best idea). Example: `gh pr view $PR --json reviews,comments` → new unresolved review threads since the last sweep.
- Built-in examples:
  - **PR review sweep**: for tasks in `review`, a precheck finds new review comments, then posts the system comment "Sweep new PR review comments" → run. This is the exact loop the operator did by hand 6+ times on OOLIO-98.
  - **Jira import**: replaces the PRD's bespoke sync ticker with an automation whose action is `cli: conveyor import jira --jql ...`.
  - **Learnings curation** (§8).
- Missed-run policy: run once on startup if it was missed, no backfill.

## 10. Integration surface (worktree-studio style: boring and explicit)

| Seam | Shape | Notes |
|---|---|---|
| CLI | `conveyor task new/show/list/comment/attach/cancel`, `conveyor import jira`, `conveyor automation run`, all `--json` | thin HTTP client to the running server, which it finds via `~/.conveyor/server.addr` (the worktree-studio pattern) |
| REST | same routes the UI uses | what worktree-studio's optional kanban view proxies to |
| Claude hooks (inbound) | per-launch `--settings` HTTP hooks | §5.2 |
| Event scripts (outbound) | `~/.conveyor/hooks/on-<event>` executables, JSON on stdin, fire-and-forget, 5 s timeout | events: `task.status_changed`, `run.finished`, `comment.created`, `learning.proposed`. Use for notifications, Jira write-back, Slack. The git-hooks model, no plugin API |
| Terminal opener | setting `open_terminal: "<template with {cwd} {cmd}>"` | defaults: `osascript` for iTerm/Terminal. worktree-studio: `worktree-studio terminal create … --initial-command {cmd}`, then open the `?terminal=` deep link. Ghostty or tmux: user-supplied template |
| Worktree provider | `local` (git worktree add) or `worktree-studio` | unchanged from the PRD. The headful/headless capability split is gone |
| External tools | `gh`, `acli`, `spotlight`, `claude`, `tmux`, each found via `LookPath` plus a fallback, failing with a sentinel error | worktree-studio's `internal/spotlight` shape |

Integrations are explicit settings with an on-demand "Test" button and never auto-detected (kept from the PRD).

## 11. Delta against the current PRD / PLAN

| PRD/PLAN today | Proposed |
|---|---|
| `mode` headless/headful fixed at creation; headful needs worktree-studio | removed: every run is attachable (`claude --bg`); worktree-studio becomes just a terminal opener and worktree provider |
| global `~/.claude/settings.json` hook installer (§5.1) | per-launch `--settings` HTTP hooks |
| statuses: draft, grooming, todo, in_progress, needs_clarification, done, failed, cancelled, archived | backlog, todo, running, needs_you, review, done, failed, cancelled (archive becomes a flag or a sweep) |
| done = agent's tag | done = PR merged; the agent's `done` lands in `review` |
| answer textbox → `claude --resume -p` | comment thread → coalesced run → resume or fresh-from-brief by context size |
| `<task-status>` tag | `<conveyor>` block with state, next, criteria, learning, spawn |
| Jira sync ticker | an automation |
| tables: tasks, events | + comments, runs, handoffs, learnings, automations |
| Remote Control flag | dropped for v1. `claude --bg` sessions plus `claude attach` cover local jump-in; revisit later |

Task 001's `status.go` transition guard needs the new status set. The rest of the store work carries over.

## 12. Build order

0. ~~Spike~~: done, see [spike-bg.md](spike-bg.md).
1. Store: new status set, plus the comments, runs and handoffs tables. CLI `task new/list/show/comment`. Board and Focus views. Manual moves only.
2. Runner and hooks: launch, Stop/UserPromptSubmit/Notification → status, the status-block parser (highest-value unit tests), brief rendering, comment coalescing, the resume-vs-fresh rule, the circuit breaker, reap via `claude agents --json`.
3. Task page and Workspace panel: attach, resume, new-from-brief, shell, editor. Run stats from transcript usage.
4. Review → done via `gh`. PR link discovery from the branch.
5. Automations (cron + precheck): the PR review sweep and Jira import.
6. Learnings inbox, then "apply learnings" tasks.
7. worktree-studio provider and terminal opener, then its optional kanban view (that plan's Part B).

## 13. Decisions (2026-10-24)

- Resume threshold: 60K, confirmed. Still tune it from `ctx_peak` data.
- Task runs: `bypassPermissions`, confined to `~/.conveyor/workspaces/<KEY>/`. Interactive sessions: normal permissions.
- A task can span several repos: one worktree per repo in the workspace folder, one primary repo as cwd, at most one PR per repo (§2).
