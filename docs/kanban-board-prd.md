# PRD: Kanban Board with AI Task Dispatch

## 0. Recommendation up front

**You asked: if an existing tool already does this without token-metered API billing, use that instead of building. Verdict: build it — but not by reusing hermes-agent.**

hermes-agent's kanban system (public, documented at [dev.to](https://dev.to/_prshant01/hermes-agents-kanban-system-is-the-most-underrated-feature-in-open-source-ai-agents-3af6), also installed locally at `~/.hermes/hermes-agent`) does avoid per-token API billing, but not by running Claude Code sessions. It:

- Reimplements its own agent loop from scratch and calls `api.anthropic.com` directly via the `anthropic` Python SDK (`agent/transports/anthropic.py`, `agent/anthropic_adapter.py`).
- Gets subscription-covered (not metered) billing by **harvesting your Claude Code OAuth token** from `~/.claude/.credentials.json` or the macOS Keychain entry `"Claude Code-credentials"`, then **spoofing the Claude Code client**: it sends the `claude-code-20250219`/`oauth-2025-04-20` beta headers, a fake Claude-Code version user-agent (detected via `claude --version`), and prepends `"You are Claude Code, Anthropic's official CLI for Claude."` to its own system prompt (`agent/anthropic_adapter.py:342-382`).
- The *only* legitimate use of the real `claude` binary is `claude setup-token` (one-time login) and `claude --version` (to steal a version string to spoof) — never for actual task work.

This works today, but it's the wrong foundation: it's fragile to Anthropic tightening OAuth validation, it duplicates Claude Code's own harness instead of using it, and header-spoofing to make traffic look like an official client is exactly the kind of thing that gets revoked without notice. It is not something to build a team-facing internal tool on top of.

The clean path that gets you the same "no extra token cost, runs on the Team plan" property **without spoofing anything**: actually run the real `claude` CLI — either as an interactive session in a tmux pane (headful) or via `claude -p` (headless/print mode) — both officially supported invocation styles that consume your existing plan's usage exactly like a normal session would, no credential trickery involved. This PRD is built around that, using Claude Code's own **hooks** (Section 5) as the harness, rather than parsing subprocess stdout.

hermes-agent's kanban *board design* (SQLite state machine, worktree-per-task, atomic claim, typed block reasons, protocol-violation retries, circuit breakers) is genuinely well battle-tested, though, and this PRD borrows several of those patterns directly — credited inline below.

---

## 1. Context

**Problem**: Tasks (self-authored or pulled from a Jira filter) currently require a human to notice them, open a terminal, start a Claude Code session, and babysit it to completion. There's no board, no automatic dispatch, and no place to see what's in flight, blocked, or done.

**Why now**: worktree-studio already has most of the low-level machinery a *richer* version of this needs — tmux-backed session management, git-worktree-per-task isolation, and a REST API surface exposing worktree/terminal lifecycle. But the kanban product itself should not depend on worktree-studio existing at all: it's a genuinely separate product (its own repo), and worktree-studio is one optional, pluggable way to run a task's session — not a requirement to have a working board.

**Outcome**: A standalone kanban service, in its own repository (Conveyor), where a card can be created manually or synced from Jira, sits in `Todo`, gets picked up automatically (poll or manual trigger), and a real Claude Code session works it in an isolated git worktree. Claude Code's own hooks tell the kanban backend what happened, with a human-in-the-loop escape hatch for anything the AI can't decide alone. Whether that session runs as a plain background subprocess or as a watchable tmux pane via worktree-studio is a pluggable, optional choice (Section 3), not a hard dependency.

---

## 2. Goals / Non-goals

**Goals**
- Kanban ships as **its own repository and product**, fully functional with zero dependency on worktree-studio (or any other external tool) being installed. worktree-studio integration is an optional, explicitly-configured enhancement, never a requirement (Section 3).
- `localhost`-only, single operator — matches worktree-studio's existing security posture, no new auth surface.
- Tasks from two sources: an internal "New Task" modal, and a configured Jira filter URL synced via `acli`.
- A board with a deliberately small, meaningful status set (Section 4).
- Backend dispatch loop: polls every N seconds (default 30, configurable), plus an immediate re-trigger on specific events (Section 8) and a manual "Trigger now" button — triggered *from* the frontend, executed on the backend.
- Each dispatched task runs a real Claude Code session, in its own git worktree, in either **headless** (`claude -p`, works standalone, zero extra dependencies) or **headful** (interactive tmux pane you can watch/answer directly — only available when the worktree-studio provider is configured, Section 3) mode.
- Claude Code's own hooks (`Stop`, `UserPromptSubmit`, `Notification`) are the harness — the source of truth for "what state is this task in," not a stdout-parsing heuristic (Section 5). Hooks fire identically regardless of which provider launched the session, which is exactly what makes the provider pluggable without forking the state-machine logic.
- A `Needs Clarification` state with a UX that fits each mode: headful tasks are answered by typing in the live terminal; headless tasks are answered via a text box that triggers a resumed `-p` call.

**Non-goals (v1)**
- Multi-user auth, roles, or remote access.
- Concurrent multi-agent collaboration on one task (parent/child task DAGs) — out of scope.
- Two-way Jira sync (writing status back to Jira) — v1 is import-only.
- Cost/budget tracking — moot, this runs on a subscription plan (Section 0).
- Arbitrary external sources beyond one Jira filter URL.
- Any *silent* auto-detection of worktree-studio (or other integrations) that changes kanban's behavior based on unannounced machine state — the provider is always an explicit setting (Section 3).
- A generic "any worktree manager" plugin marketplace — v1 ships exactly two `WorktreeProvider` implementations (Section 3); add more only if a real second need shows up.

---

## 3. Product boundary: worktree-studio as an optional, pluggable provider

This is the central architectural question from your feedback, so it gets its own section: **how do two independent, separately-shipped programs connect when connecting is optional?**

The standard shape for this: the dependent program (kanban) defines a small internal interface for the one capability it actually needs from the other program, ships a **zero-dependency default implementation** of that interface, and treats the richer integration as just another implementation selected by **explicit configuration** — never silent auto-detection. This is how e.g. `git` treats optional `gh`/GPG/external-diff integrations, and how editors treat optional language servers: the tool works alone, and an integration is strictly additive, opted into on purpose.

### 3.1 The interface

```go
type WorktreeProvider interface {
    CreateWorktree(repo, name string) (path string, err error)
    // StartHeadful returns an attach URL if this provider supports a watchable/interactive
    // session. Providers that can't support this return ErrHeadfulUnsupported.
    StartHeadful(worktreePath, promptFile string) (attachURL string, err error)
    Cleanup(path string) error
}
```

Two implementations ship in v1:

- **`localProvider`** (the default): shells directly to `git worktree add`/`git worktree remove` — the same primitive worktree-studio itself wraps, just called independently, no dependency on worktree-studio existing. `StartHeadful` always returns `ErrHeadfulUnsupported` — this provider is headless-only, since "a persistent, watchable, browser-attachable terminal pane" is a real feature worktree-studio provides and kanban has no reason to reimplement standalone. Headless mode (`claude -p`, Section 5.4) needs nothing from this provider beyond a worktree path — kanban execs `claude -p` itself as a subprocess directly in that path.
- **`wtsProvider`**: backed by worktree-studio (transport in Section 3.3). Supports both `StartHeadful` (real tmux pane, attach URL into worktree-studio's own web UI) and headless.

A single setting (`worktree_provider: "local" | "worktree-studio"`, defaulting to `"local"`) selects which is active. **This is the only place the integration is decided** — never inferred from "oh, a `worktree-studio` binary happens to be on PATH." Switching the setting to `"worktree-studio"` triggers a one-time **"Test connection"** action (PATH lookup + a smoke-test call, Section 3.3) with a clear pass/fail result — not a background probe that runs on every startup and silently changes behavior.

This cleanly answers "how do you handle the optional case": there is no branch in kanban's core logic for "is worktree-studio present" — there's exactly one `WorktreeProvider` interface, the dispatcher and hook-harness code call it polymorphically, and the *only* place providers differ is `StartHeadful` either working or explicitly declining. The board UI reflects that directly: if the active provider is `localProvider`, the "headful" option simply isn't offered when creating a task — not a broken button, an absent one.

### 3.2 Why the capability boundary is headful vs. headless, specifically

This isn't an arbitrary on/off switch — it maps onto a real functional difference. Headless mode (`git worktree` + `claude -p`) needs only `git` and `claude` on `PATH`; kanban can do all of it unassisted. Headful mode needs a persistent tmux session plus a browser-attachable pty relay — that's worktree-studio's actual product, and duplicating it inside kanban would be pure waste. So the "optional integration" line falls exactly where the genuine capability gap is, rather than being a blanket "works better if X is installed" fuzziness.

### 3.3 `wtsProvider` transport: CLI subcommands, not direct SQLite access

Kanban is in its own repo, so `wtsProvider` talks to worktree-studio as an external black box. Two ways to do that were considered:

- **Direct SQLite access** (kanban process opening `~/.worktree-studio/studio.db` itself): rejected. `internal/store/store.go:33-58` opens the DB with no WAL mode and no `busy_timeout`, relying entirely on the running server's own `SetMaxOpenConns(1)` to serialize its writes against itself. A second process writing to the same file concurrently risks an immediate `SQLITE_BUSY` with no retry — there's no safety net to inherit.
- **CLI subcommands added to worktree-studio, each a thin loopback-HTTP wrapper**: worktree-studio already has a precedent for CLI-mode subcommands dispatched via a plain `os.Args` switch in `main()`, ahead of server startup (`cmd/worktree-studio/main.go:61-63`, `hooks.go:24-37`, today just `install-hooks`/`uninstall-hooks`). Add to that:
  ```
  worktree-studio worktree create --repo <repoID> --name <name> [--source-branch <b>] --json
  worktree-studio worktree list --repo <repoID> --json
  worktree-studio worktree archive --id <worktreeID>
  worktree-studio terminal create --worktree <worktreeID> --initial-command '<cmd>' --json
  worktree-studio terminal close --id <terminalID>
  ```
  Unlike `install-hooks` (which is DB-free and genuinely standalone), these **require the server to already be running** — each one is just an HTTP call to the server's own existing endpoints (confirmed: `POST /api/repos/{repoID}/worktrees/` takes `{name, source_branch}` → full `Worktree` row, fully synchronous; `POST .../terminals/` takes `{tab_label, initial_command, claude_session_id, claude_session_title}` → full `TerminalSession` row, also synchronous — both already exist, no new business logic needed, just a CLI entrypoint for them) over `http://localhost:<port>`, port discovered via a small file the server writes at startup (alongside its existing `~/.worktree-studio/server.log` convention). If the server isn't reachable, the subcommand fails fast with a clear stderr message — no direct-DB fallback.

  This mirrors, inverted, the exact "shell out, parse stdout JSON, non-zero exit on error" contract worktree-studio's own `internal/gh`/`internal/spotlight` already expect from external tools — worktree-studio becomes the callee here instead of the caller. Kanban's `wtsProvider` is then a thin `os/exec` wrapper around the `worktree-studio` binary, same shape as `internal/gh` is inside worktree-studio itself.

  **`StartHeadful`'s return value** (the attach URL) is just a deep link into worktree-studio's own web UI (e.g. `http://<wts-host>/repo/<repoId>/worktree/<worktreeId>?terminal=<terminalId>`) — a plain hyperlink the human's browser navigates to. This was always going to be true regardless of CLI-vs-HTTP transport underneath: live terminal interaction is inherently a browser-facing, websocket-backed feature of worktree-studio's own UI, not something `wtsProvider`'s Go code mediates.

- **"Test connection"** (Section 3.1) = `exec.LookPath("worktree-studio")` + one `worktree-studio worktree list --repo <configured-repo> --json` smoke call. Pass/fail surfaced directly in the settings UI, run only when the setting is changed or the user explicitly re-tests — never polled silently in the background.

---

## 4. Status model

| Status | Meaning | Who moves it here |
|---|---|---|
| `draft` | Freshly created, not yet fleshed out. Never touched by the dispatcher. | Human |
| `grooming` | Human is actively refining scope/description before it's AI-ready. | Human |
| `todo` | Ready for pickup. Dispatcher scans this column. | Human, Jira sync, or "human answered clarification" |
| `in_progress` | An active Claude Code session (either provider, either mode) is working it. | Dispatcher (on claim), or a `UserPromptSubmit` hook event (human just answered) |
| `needs_clarification` | Claude stopped and is waiting on the human — no valid status tag in its last message. | Detected via `Stop` hook (Section 5) |
| `done` | Claude's last message included a valid `<task-status>` tag saying so. | Detected via `Stop` hook |
| `failed` | Process crashed/timed out/exhausted its retry budget, or repeatedly failed to emit a parseable tag. | Dispatcher (reap/circuit-breaker) |
| `cancelled` | Human decided not to pursue it. | Human only — never AI |
| `archived` | Terminal housekeeping state, hidden from the board by default. | Human, or an automatic sweep after N days in `done`/`cancelled`/`failed` |

`failed` is kept distinct from `needs_clarification` (a question for a human vs. a crash/timeout a human should investigate), and `cancelled`/`archived` are human-only transitions enforced at the API layer — both justified by hermes-agent's own hard-learned incidents (Section 12).

```
draft ──> grooming ──> todo ──> in_progress ──┬──> done ──> archived
  │                      ▲          │  ▲       ├──> needs_clarification ──(human answers)──> in_progress
  │                      │          │  └───────┘         (same task, no re-claim — see 5.2)
  │                      │          │
  └──────────────────────┘          └──────────┴──> failed ──(human requeues)──> todo
                                                └──> cancelled (human only, any state) ──> archived
```

Answering a clarification does **not** route back through `todo`/re-dispatch — the same session (same worktree, same Claude Code session id) just receives the next message, and a `UserPromptSubmit` hook flips the card straight back to `in_progress`.

---

## 5. The harness: Claude Code hooks, not stdout-parsing

Hooks fire **identically regardless of which `WorktreeProvider` launched the session** — a headless `claude -p` subprocess kanban exec'd itself, and a headful `claude` session running in a worktree-studio-managed tmux pane, both go through the same Claude Code lifecycle and fire the same hook events. This is precisely what makes the provider abstraction in Section 3 possible without forking the state-machine logic: the dispatcher and hook-harness code don't know or care which provider is active, only the harness below.

### 5.1 Hooks kanban installs

Kanban installs its own hook scripts into `~/.claude/settings.json`, independent of any hooks worktree-studio itself installs (`session-tracking`/`session-context`, per `internal/claudehook`). Confirmed safe to coexist: worktree-studio's installer never overwrites the `hooks` section wholesale — for each event it appends its own matcher-group entry, identified purely by its own script's exact path (`internal/claudehook/install.go`'s `findOurEntry`), and only ever removes that one entry on uninstall. Kanban's installer must follow the identical discipline: read `~/.claude/settings.json` as a generic map, append new matcher-group entries to `hooks.Stop`/`hooks.UserPromptSubmit`/`hooks.Notification` (worktree-studio registers neither `Stop` nor `UserPromptSubmit` today), never replace an array or the `hooks` key wholesale, and only remove entries matching kanban's own script path.

| Hook | Fires when | Kanban's reaction |
|---|---|---|
| `UserPromptSubmit` | A new user message is submitted — whether typed live in an interactive pane, or delivered via a resumed `-p` call | Resolve `cwd` → task (same prefix-match trick worktree-studio's `FindWorktreeByPath` uses, against kanban's own `worktree_path` column). If task is `needs_clarification`, flip to `in_progress` immediately. |
| `Stop` | Claude finishes producing a response and the turn ends (fires after *every* turn) | Resolve `cwd` → task, read the session transcript for the just-completed assistant message, look for a `<task-status>` tag (Section 5.3). |
| `Notification` | Claude is idle waiting on input, or blocked on a permission prompt | Secondary signal for `needs_clarification`; the only signal for a permission-prompt block in headful mode, which `Stop` wouldn't otherwise catch. |

### 5.2 The rule that eliminates the "multiple questions" problem

On every `Stop` event: read the last assistant message.

- **Tag present, `status=done`**: task → `done`.
- **Tag present, `status=failed`**: task → `failed`.
- **No tag at all**: task → `needs_clarification`, card shows the raw last message as its "latest question." A `Stop` without the tag means, by definition, Claude is waiting on the human for something — genuine question or not. Every round-trip is handled identically: card shows `needs_clarification` → human responds (live keystrokes, or the board's answer box) → `UserPromptSubmit` fires → `in_progress` → eventually another `Stop`, tagged or not, repeats the cycle.

There is no multi-question state machine to build. Five questions in a row is just five iterations of the same two-hook cycle.

### 5.3 The status tag and prompt delivery

```
When you are done working on this for now, end your final message with exactly one block:

<task-status>
<status>done|failed</status>
<summary>one paragraph — what you did, or why you can't continue</summary>
</task-status>

Only emit this when you have truly finished or truly cannot proceed further without
being killed/timed out. If you need to ask the human something and will continue once
they answer, just ask normally — do not emit this block, and do not worry about
"ending correctly": stopping without this block is the expected way to wait for an answer.
```

The AI no longer needs to know about a "needs-clarification" status value at all — silence correctly means that (Section 5.2).

**Prompt delivery, either mode**: write the prompt to a file in the worktree (e.g. `.conveyor/prompt.md`) rather than inlining arbitrary human-written text as a raw CLI argument — sidesteps shell-escaping fragility entirely, and gives a durable, inspectable copy of exactly what was asked. Headful: `initial_command` = `claude "$(cat .conveyor/prompt.md)"`. Headless: kanban execs `claude -p --output-format json --permission-mode acceptEdits` with the same file as stdin.

**Permission mode, either way**: must pre-approve tool use. Headful: a blocked permission prompt just looks like another `needs_clarification` — harmless, the human approves in-pane. Headless: fatal without pre-approval — no pane exists to approve it in, it hangs until the subprocess timeout kills it.

### 5.4 Headless (standalone default) vs. headful (requires the worktree-studio provider)

- **Headless**: works with `localProvider` and needs nothing beyond `git`+`claude`. `needs_clarification` shows an inline answer textbox; submitting execs `claude --resume <session_id> -p "<answer>" --permission-mode acceptEdits` in the worktree.
- **Headful**: only offered when `wtsProvider` is active and connected (Section 3). `needs_clarification` shows an "Open terminal" deep link into worktree-studio's own terminal-attach UI instead of a textbox — no answer-relay logic to build at all, the human just types.

Store a `mode` column per task, validated server-side against the currently active provider's capability (creating a headful task while `localProvider` is active is a 400, not a silently-ignored request).

### 5.5 Mobile/browser access for headful sessions (Remote Control)

Headful sessions are persistent local processes, which means Claude Code's real **Remote Control** feature (`/remote-control` interactively, shorthand `/rc`) applies directly: it lets a running, local Claude Code session be viewed and controlled from claude.ai/code or the Claude mobile app, while the session itself keeps running locally against the worktree's filesystem. This is **not applicable to headless mode** — a `claude -p` process exits after one response, so there's nothing left for a phone to connect to once it's done; this isn't a gap in the design, it's a hard limitation of what Remote Control is (per Claude Code's own docs: "if you close the terminal... the session goes offline").

Rather than trying to inject `/rc` as simulated keystrokes into a freshly-started interactive pane, use the documented non-interactive equivalent: the headful `initial_command` becomes

```
claude --remote-control "<task title>" "$(cat .conveyor/prompt.md)"
```

No interactive pairing step is needed per-connection — the CLI flag alone makes the session connectable, and reconnecting from a phone or browser later doesn't require re-pairing. Two prerequisites worth checking before relying on this, not things kanban can control:

- **Team/Enterprise plans have Remote Control off by default** — an org Owner must enable it in Claude Code's admin settings first, and it's blocked entirely under a Zero Data Retention policy.
- Requires a full-scope `/login` session (rejects limited/API-key auth) — consistent with the rest of this design already assuming subscription login, not API keys.

Prefer the per-invocation `--remote-control` flag over the global `remoteControlAtStartup` settings.json toggle — the global toggle would make *every* interactive Claude Code session on the machine remote-controllable, including the human's own unrelated everyday usage, not just kanban-dispatched tasks. Scoping it to kanban's own `initial_command` keeps the effect limited to what kanban actually launches.

Whether kanban's own UI can capture and surface the resulting per-session URL/QR directly on the card (rather than the human finding it via the Claude mobile app's own session list) depends on exactly how/where Claude Code emits that URL at startup — worth a quick manual spike before committing to it; treat it as a nice-to-have for Section 13's phase 2, not a hard requirement of shipping headful mode.

**Scope**: Remote Control is a **per-task, creation-time-only toggle** (`remote_control_enabled`, Section 7), shown in `NewTaskModal` only when mode=headful. There is no runtime "enable later" API — if a task was created without it and the human decides mid-task they want mobile access, the documented escape hatch is to attach to the task's terminal (already surfaced via the "Open terminal" link) and type `/rc` themselves. Kanban deliberately doesn't build a toggle-on-a-running-task endpoint for this; the terminal already provides it, and duplicating it would just be two paths to the same effect.

### 5.6 Keeping the card in sync with the live session

Hooks are a **push** mechanism and can be silently dropped — if kanban's hook receiver is unreachable for the instant a `Stop`/`UserPromptSubmit` fires (a kanban restart/deploy, a transient error), that event is simply lost; Claude Code doesn't retry it, and nothing else notices on its own. Since headful sessions are genuinely live, freely-interactive processes (typed into locally, over Remote Control, or both), the card's DB row can drift from the session's actual state in more ways than "the tag parser got it wrong" — the process can also just die out from under a card that still thinks it's active.

Two concrete consequences, both real gaps if left unhandled:

- **A `needs_clarification` task whose session died** (human closed the terminal, killed the tmux session, machine issue) has no live process left, but nothing currently notices — it would sit in `needs_clarification` forever pointing at nothing. The dispatcher's reap step (Section 8) must check liveness for `needs_clarification` tasks, not only `in_progress` ones.
- **Cancelling a card doesn't, by itself, stop the underlying work.** A human clicking Cancel on an `in_progress`/`needs_clarification` task must also terminate the live session — kill the tmux session for headful (a new `worktree-studio terminal close --id <id>` CLI subcommand, added to Section 3.3's list) or kill the subprocess kanban owns for headless. Otherwise "cancelled" on the board and "still running" in reality diverge the instant the button is clicked — precisely the kind of drift to avoid.

The deeper fix: don't treat hooks as the *only* source of truth. Add a periodic **reconciliation** pass (same ticker as the dispatch loop, or its own) that, for every non-terminal task, reads the session's own transcript file directly — the same technique `internal/claudehook.SessionTitle` already uses to read `~/.claude/projects/*/<session-id>.jsonl` — and independently checks process/tmux liveness, regardless of whether a hook fired recently. Hooks remain the fast path (near-instant card updates); reconciliation is the bound on staleness, guaranteeing the board converges to the true state even when a push event is dropped, rather than depending on push delivery being perfect.

For headless mode's answer-and-continue path specifically, centralize it behind one small helper rather than re-implementing the `--resume` invocation ad hoc in both the dispatcher and the answer-submit API handler — call it once, consistently, with one important rule: **if `--resume <session_id>` itself fails** (session file missing/corrupted, Claude Code reports "session not found"), the task goes straight to `failed` with that error attached. It must never silently fall back to starting a fresh, unrelated session under the same task ID — that would look like a normal continuation while actually discarding all prior context, which is a worse failure than surfacing it plainly.

---

## 6. Architecture

```
conveyor/                  (its own repository)
  cmd/conveyor/main.go      entrypoint: own chi router, own port, own SQLite DB
  internal/store/           kanban_tasks, kanban_task_events (Section 7)
  internal/dispatcher/       poll ticker + dispatchNow channel (Section 8), claim logic, circuit breaker
  internal/provider/         WorktreeProvider interface (Section 3) + localProvider + wtsProvider impls
  internal/hooks/            kanban's own settings.json installer (merge discipline, Section 5.1) +
                             HTTP receiver for Stop/UserPromptSubmit/Notification payloads
  internal/jira/             thin os/exec wrapper around `acli`, same shape as worktree-studio's internal/gh
  web/                       Board.tsx, Column.tsx, Card.tsx, NewTaskModal.tsx, useKanbanBoard.ts,
                             SettingsPage.tsx (provider setting + Test Connection)
```

`localProvider` shells directly to `git`; `wtsProvider` shells to the `worktree-studio` CLI (Section 3.3). Neither is imported from worktree-studio's source — kanban never depends on worktree-studio's Go packages or its `studio.db`.

### 7. Data model

```sql
CREATE TABLE kanban_tasks (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT,
  status TEXT NOT NULL DEFAULT 'draft',
  mode TEXT NOT NULL DEFAULT 'headless',      -- 'headless' | 'headful' (headful requires wtsProvider)
  remote_control_enabled INTEGER NOT NULL DEFAULT 0, -- headful-only, set at creation, no runtime toggle
  source TEXT NOT NULL,                       -- 'manual' | 'jira'
  external_key TEXT,                          -- Jira issue key; UNIQUE when source='jira'
  external_url TEXT,
  worktree_path TEXT,                          -- resolved absolute path, cached locally for hook cwd-matching
  wts_worktree_id TEXT,                        -- only set when provider='worktree-studio'
  wts_terminal_id TEXT,                        -- only set for headful tasks, for the "Open terminal" deep link
  claude_session_id TEXT,                      -- for --resume in headless mode
  claim_lock TEXT,
  claimed_at TEXT,
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  latest_message TEXT,                         -- last assistant message shown on the card (tagged or not)
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_kanban_tasks_external_key ON kanban_tasks(external_key) WHERE external_key IS NOT NULL;

CREATE TABLE kanban_task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL REFERENCES kanban_tasks(id) ON DELETE CASCADE,
  event TEXT NOT NULL,          -- 'claimed' | 'stop_tagged' | 'stop_untagged' | 'user_prompt_submit' |
                                 -- 'notification' | 'crashed' | 'timed_out'
  detail TEXT,
  created_at TEXT NOT NULL
);
```

`kanban_tasks` is current state, `kanban_task_events` is history — never derive one from the other (same discipline worktree-studio already applies to its own store + audit log).

### 8. Dispatch loop

```go
dispatchNow := make(chan struct{}, 1)
go func() {
    ticker := time.NewTicker(pollInterval) // default 30s
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
        case <-dispatchNow:
        }
        srv.DispatchReadyTasks()
    }
}()
```

`dispatchNow` is signaled by the "Trigger now" API call (frontend-initiated, backend-executed) and a Jira sync that imports new `todo` tasks. Answering a clarification does **not** signal this — it's handled entirely by the `UserPromptSubmit` hook (Section 5.2).

`DispatchReadyTasks`:
1. Reap: any `in_progress` **or `needs_clarification`** task whose process/tmux session is dead, or whose `in_progress` run exceeded max-runtime → `failed`, increment `consecutive_failures`. (Checking `needs_clarification` too, not just `in_progress`, is what catches a human having killed the terminal instead of answering — see Section 5.6.)
2. For each `todo` task, up to `maxConcurrent` (default 2–3): atomically claim it, call `provider.CreateWorktree`, write `.conveyor/prompt.md`, then start it via the active provider — `provider.StartHeadful` (worktree-studio only) or a direct local `claude -p` exec (either provider, since headless needs nothing from the provider beyond the worktree path).
3. Circuit breaker: `consecutive_failures` past budget (default 3) → `failed` instead of retrying again.
4. **Reconciliation** (Section 5.6): for every non-terminal task, independently re-derive status from the session's own transcript file + liveness check, regardless of recent hook activity — the correctness backstop under hook delivery, not a replacement for it.

**Cancel** (human-only, any non-terminal state): in addition to setting `status='cancelled'`, terminates the live session — `worktree-studio terminal close` for headful, killing the owned subprocess for headless. A cancelled card must not leave work silently running (Section 5.6).

---

## 9. Jira integration

- One Jira filter URL/JQL, synced on its own ticker, decoupled from the dispatch poll interval.
- `internal/jira`, same shape as worktree-studio's `internal/gh` — thin `acli` wrapper; verify `acli`'s actual JSON output shape with a manual spike before writing the parser against it.
- Idempotent dedupe on `external_key` (unique index) — never dedupe on poll timestamp.
- New issues land in `todo` directly (configurable to `draft` for a grooming pass).
- Respect Jira Cloud's rate-limit dimensions — a single 30s-or-slower poll of one filter is trivial against them; avoid frequent status write-back (out of scope for v1 anyway).

---

## 10. Human-in-the-loop UX summary

- **Headless `needs_clarification`** (always available, no external dependency): card shows latest message + inline answer textbox. Submitting execs a resumed `-p` call locally.
- **Headful `needs_clarification`** (only when `wtsProvider` active): card shows latest message + "Open terminal" deep link into worktree-studio's terminal-attach UI. Human types the answer directly; `UserPromptSubmit` flips the card the moment they hit enter — no relay logic.
- **Don't let it rot silently**, either way: kanban runs its own small in-memory attention tracker (a pattern to replicate, not import, from worktree-studio's `internal/attention` — a second process can't share that in-memory state across the process boundary) + a desktop notification, so a `needs_clarification` card surfaces the same way worktree-studio's "Claude needs your attention" badge does today.

---

## 11. Frontend

- `Board.tsx`: columns per non-hidden status; Cancelled/Archived hidden behind a toggle. No drag-and-drop in v1 — the state machine moves cards; the only human-initiated moves are Draft→Grooming→Todo and Cancel.
- `NewTaskModal.tsx`: title, description, mode picker (headful option only rendered when the active provider is `worktree-studio` and its last "Test connection" passed), and — only when headful is selected — a "Enable Remote Control" checkbox (Section 5.5). Creation-time only; no equivalent toggle exists on an existing card.
- `SettingsPage.tsx`: `worktree_provider` setting (`local` default / `worktree-studio`) + "Test connection" button surfacing pass/fail.
- `Card.tsx`: title, source badge, status accent, and for headful `in_progress`/`needs_clarification`, the "Open terminal" deep link.
- `useKanbanBoard.ts`: a `StatusScheduler`-style instance (same touch/idle/visibility-aware polling idiom worktree-studio's `RepoContext` already uses) polling `GET /api/tasks` every 5–10s.
- "Trigger now": `POST /api/trigger`, then an immediate `refreshNow()`.

---

## 12. Pitfalls and mitigations

| Pitfall | Source | Mitigation in this design |
|---|---|---|
| Silent-completion / hallucinated "done" | Cited across multiple agent-kanban postmortems | Status is explicit and hook-derived, not inferred from exit code. |
| Model narrates instead of emitting the status tag | hermes-agent `agent/kanban_stop.py` (GLM/Qwen families cited) | Not a failure mode here — Section 5.2's "no tag = needs_clarification" makes an untagged stop a valid, expected state. |
| Runaway retries on an unfixable task | hermes-agent `DEFAULT_SPAWN_FAILURE_LIMIT` | Circuit breaker → `failed` after a bounded budget. |
| Hung/blocked-on-permission-prompt subprocess | Claude Code headless-mode docs | Headful: harmless (`needs_clarification`, human approves in-pane). Headless: fatal without `--permission-mode` pre-approval. |
| Orphaned/zombie `in_progress` tasks after a crash or restart | hermes-agent's zombie-reap pattern | Dispatch-loop reap step checks liveness every tick. |
| Two independent tools clobbering each other's `~/.claude/settings.json` hooks | Verified risk once a second tool (kanban) installs hooks alongside worktree-studio's | Append-by-own-path / remove-by-own-path merge discipline (Section 5.1), copied from worktree-studio's own implementation. |
| A second process writing worktree-studio's SQLite file directly | `internal/store` has no WAL/`busy_timeout`, single-connection design | `wtsProvider` never touches `studio.db` — only calls new CLI subcommands that proxy through the already-running server's own connection (Section 3.3). |
| Silent auto-detection of an optional integration surprising the user | General "optional dependency" design lesson (Section 3) | Provider is an explicit setting with an on-demand "Test connection" action, never a background probe. |
| Shell-escaping a long/arbitrary task description via a CLI arg | General fragility of inlining free text into a shell command | Write the prompt to a file, invoke `claude "$(cat .conveyor/prompt.md)"` instead (Section 5.3). |
| Duplicate cards from re-polling Jira | General Jira/webhook-vs-polling guidance | Idempotent dedupe on `external_key`. |
| Clarification card rotting with no visibility | Thin coverage in public sources, treated as a real named gap | Kanban's own attention-style pubsub + desktop notification (Section 10). |
| AI silently cancelling/archiving its own task | Extrapolated from "don't trust self-reported terminal states" | `cancelled`/`archived` are human-only, enforced at the API layer. |
| Worktrees/sessions never cleaned up | hermes-agent's `hermes kanban gc`; worktree-studio's own 60-day archive sweep | Reuse the same retention-sweep idea for `done`/`cancelled`/`failed` tasks past a threshold. |
| Assuming Remote Control "just works" on a Team plan | Confirmed via Claude Code docs: off by default for Team/Enterprise, admin-gated, blocked under Zero Data Retention | Treat as a checked prerequisite (Section 5.5), not something kanban enables programmatically — verify org settings before depending on it. |
| Card status silently drifting from the live session's real state | Hooks are push-only and can be dropped (kanban restart/transient failure mid-event) — nothing else notices on its own | Periodic transcript-read + liveness reconciliation, independent of hook delivery (Section 5.6) — hooks are the fast path, reconciliation is the guarantee. |
| A `needs_clarification` card pointing at an already-dead session | Human closed the terminal / killed the tmux session instead of answering | Reap sweep checks liveness for `needs_clarification`, not only `in_progress` (Section 5.6, Section 8). |
| Cancelling a card while the real session keeps running | Cancel previously only updated the DB row, no propagation to the live process | Cancel now also kills the session (`terminal close` for headful, subprocess kill for headless) (Section 8). |
| Headless `--resume` silently starting a fresh session instead of failing loudly when the prior session is missing/corrupted | Would look like a normal continuation while actually discarding all context | Centralized resume helper treats a failed `--resume` as an explicit `failed` state, never a silent fresh start (Section 5.6). |

---

## 13. Phased implementation plan

1. **Kanban skeleton + `localProvider` + headless mode** — deliberately provable with **zero worktree-studio involvement**: own binary, own DB/migrations, own chi API, `git worktree` shellouts, `claude -p` dispatch, kanban's own hook installer/receiver implementing Section 5.1–5.2's state machine end to end. Manual create → dispatch → `needs_clarification` (answered via textbox) → `in_progress` → `done`, entirely standalone.
2. **`wtsProvider` + headful mode**: add the new CLI subcommands to worktree-studio (Section 3.3), build `wtsProvider`, "Test connection" flow, "Open terminal" deep link. Prove the same end-to-end loop, this time answered live in a tmux pane.
3. **Jira sync**: `internal/jira` (`acli` wrapper), sync ticker, idempotent import.
4. **Polish**: attention-style pubsub for `needs_clarification`, retention/archive sweep, circuit breaker tuning, settings (poll interval, max concurrency, per-mode timeouts).

## 14. Verification

- Unit tests for the `Stop`-hook transcript parser (tagged done/failed, untagged, malformed tag, fenced tag).
- **Standalone check**: with worktree-studio not installed/running at all, confirm kanban's full headless loop works end-to-end and the headful option is simply absent from the UI, never a broken/erroring button.
- Manual end-to-end, headful: create a task, confirm a real worktree + terminal via the new CLI subcommands, watch it stop untagged (`needs_clarification`), type an answer directly in the pane, confirm the card flips to `in_progress` immediately, reaches `done`.
- Confirm hook coexistence: install kanban's hooks alongside worktree-studio's existing ones, inspect `~/.claude/settings.json` to confirm both tools' entries are present and neither was overwritten; uninstall kanban's and confirm worktree-studio's entries are untouched.
- Manual Jira spike: run the intended `acli` command by hand first to confirm output shape before writing the parser against it.
- Force a task to fail repeatedly (e.g. a broken worktree) and confirm it lands in `failed` after the configured budget rather than looping forever.
