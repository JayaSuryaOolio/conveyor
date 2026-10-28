---
name: conveyor
description: Operate, troubleshoot and change Conveyor (this repo) - the local kanban that dispatches tasks to Claude Code background sessions. Use when inspecting tasks/runs, debugging why a task did or didn't run, reading its logs or DB, or editing its Go/Wails/React code.
---

# Conveyor: agent reference

Kept short on purpose. When this file and the code disagree, the code wins; fix this file in the same PR.
Long-form: `docs/guide.md` (behaviour), `docs/tech-spec.md` (original design, partly stale).

## Model in one paragraph

A **task** (`CV-12`; sub-tasks `CV-12A`, `CV-12A1`) has repos, a status, a comment thread and **runs**. The **dispatcher** (inside the app, or `conveyor daemon`; one per home via `dispatcher.lock`) ticks every 2s: reap vanished sessions, sync PRs, run automations, launch `todo` tasks (up to `max_parallel`, skipping ones with open `blocks` links). A launch makes `~/.conveyor/workspaces/<KEY>/<repo>` worktrees (running the repo's `setup_script`, log `setup-<repo>.log`) + `BRIEF.md` and runs `claude --bg ... --permission-mode bypassPermissions --settings <home>/claude-settings.json`. Claude Code **hooks** call `conveyor hook <Event>`; the `Stop` hook parses the `<conveyor>` status block and moves the task to `review`/`needs_you`. A human comment on a finished task requeues it (resume if context < `resume_threshold`, else fresh); on a running task it is queued for the next run. Once a PR of the task is merged, a comment instead starts a follow-up sub-task (`core.followUp`, source `followup`) whose brief carries the parent's context; `--ask` requeues it to answer without code changes, `--note` only comments (the app asks which).

Statuses: backlog, todo, running, needs_you, review, failed, done, cancelled. Allowed moves: `internal/store/status.go`.
Model precedence: task `model` > agent file `model:` > `default_model` setting.

## Commands you'll use

```
conveyor [--home dir] <cmd>          # no args = usage; list/show take --json
conveyor task list [--status todo,running] --json
conveyor task show KEY --thread --runs [--json]
conveyor task brief KEY --preview     # what the agent is told
conveyor task log KEY [--follow]      # live session output
conveyor task sessions KEY [--json]   # Claude session ids (runs + manual)
conveyor app open NAME KEY            # opener, or quick action (--output) printing its output
conveyor task new "t" --repo r [--desc ..] [--todo] [--parent KEY] (--repo/--agent optional with --parent: default to the parent's) [--model m] [--agent a]
conveyor task comment KEY "text" [--ask|--note]   conveyor task move KEY <status>
conveyor task stop KEY                # end the running bg session -> needs_you
conveyor task set KEY [--branch b] [--pr url] [--model m] [--desc d]   # --desc: backlog only
conveyor task link KEY blocks|relates OTHER     conveyor task unlink KEY kind OTHER
conveyor task parent KEY PARENT|--none
conveyor task label KEY add|rm NAME     # labels: many per task (labels + task_labels tables), filter chips on the Board
conveyor agent list|add|model         conveyor config list|get|set
conveyor repo add|list|rm|setup <name> [script|-]
conveyor models [--json]            # model list (API with ANTHROPIC_API_KEY, else built-in); names are not validated, a bad model fails the session -> needs_you
conveyor state export [FILE] | import FILE [--repo A=name]   # settings/apps/automations/user agents, repos as placeholders
conveyor hooks status|install|uninstall
conveyor daemon                       # dispatcher in the foreground
```

Use a throwaway home for experiments: `conveyor --home $(mktemp -d) ...`. Never point tests at `~/.conveyor` or write to the real `~/.claude/agents`.

## Where things live

| What | Path |
|---|---|
| DB (sqlite) | `~/.conveyor/conveyor.db` - `sqlite3 ~/.conveyor/conveyor.db '.tables'` |
| App log | `~/.conveyor/conveyor.log` (+ `.1`) |
| Dispatcher / hook logs | `~/.conveyor/logs/dispatcher.log` (launch/reap/errors, app or daemon), `logs/hook.log` (one line per non-PreToolUse hook event of a Conveyor session, + errors) |
| Task workspace | `~/.conveyor/workspaces/<KEY>/{<repo>/,BRIEF.md,attachments/}` |
| Session hooks file | `~/.conveyor/claude-settings.json` (rewritten each launch) |
| Session transcripts | `~/.claude/projects/...` (token counts come from here) |

## Code map

| Area | Path |
|---|---|
| CLI entry, one file per command | `cmd/conveyor/cmd_*.go`, tests `cli_test.go` |
| Domain ops shared by CLI + app | `internal/core` |
| Schema, migrations, queries, status rules, links | `internal/store` (`schema.go`, `status.go`, `links.go`) |
| Dispatch loop, reap, launch, model choice | `internal/dispatch` |
| Hook events -> store | `internal/hook`; status block parser `internal/statusblock` |
| `claude` CLI wrapper, settings/hooks files | `internal/claude` |
| BRIEF.md + launch prompts (golden test) | `internal/brief` (`testdata/brief.golden`) |
| Worktrees | `internal/workspace`; PRs `internal/gh`; terminals `internal/term`, `internal/terminal` |
| Learnings / automations / Jira | `internal/learning`, `internal/automation`, `internal/jira` |
| Wails bindings (thin) | `internal/ui/app.go`; app entry `main.go` |
| UI (React + Mantine) | `frontend/src`: `App.tsx` nav, `views.tsx` board/focus/task page, `workspace.tsx` task sidebar, `relations.tsx`, `forms.tsx` dialogs + settings (incl. Repos tab), `agents.tsx`, `automations.tsx`, `apps.tsx`, `logs.tsx`, `terminals.tsx` |

## Troubleshooting recipes

Known failure modes with confirm/fix steps: `docs/troubleshooting.md` (e.g. a run stuck in running after its session finished, an untracked second PR, a resumed run adopted by a new session id). Run/session id architecture and invariants: `docs/session-identity.md`. Add to it when you fix a new one.

- **Task not launching:** is a dispatcher running (`dispatcher.lock`, app status dot green)? Task in `todo`? Open blockers (`task show KEY --json` -> relations)? `max_parallel` reached? Recent comment still in `comment_debounce_secs`? Then `grep KEY ~/.conveyor/conveyor.log`.
- **Launch fails:** read the system comment on the thread. "workspace not trusted" = run `claude` once in the main repo.
- **Stuck in running:** `claude agents --json --all` - is the bg session alive? Interactive runs are reaped (finished, task -> needs_you) once their session leaves that list. `logs/hook.log` for hook errors.
- **Wrong context for the agent:** `conveyor task brief KEY --preview`.
- **UI shows stale data:** UI polls `PRAGMA data_version`; check the app log for binding errors.

## Changing the code

- Tests: `go test ./...` (fast, uses fake `claude` via `execCommand`); `gofmt -l .`; `go vet ./...`.
- Frontend: `cd frontend && npm ci && npm run build` (tsc + vite). A fresh worktree has no `frontend/node_modules`, so run `npm ci` before `tsc` or vite.
- New/changed Go binding in `internal/ui`: `wails generate module`, then `git checkout -- frontend/wailsjs/runtime` (don't commit runtime churn). `wails dev` hot-reloads frontend changes but not Go: restart it after a binding change.
- After `wails build`/`npm run build`: `git checkout -- frontend/dist/.gitkeep frontend/package.json.md5`.
- Run the UI live: `wails dev` (browser at http://localhost:34115). Install: `scripts/install.sh`.
- UI on a throwaway home: `CONVEYOR_HOME=<tmp> wails dev -devserver localhost:<port>`.
- Frontend PRs: add a Playwright screenshot to the PR description (`scripts/screenshot/`, mocks the Wails bindings with a Proxy on `window.go.ui.App`, no Go backend needed). If the latest playwright wants a newer chromium than installed, pass the cached one (e.g. chromium-1243 or the headless shell) via `executablePath`. Don't commit the image: GitHub has no CLI upload for PR images, so ask the human to drag it into the description, or link it by commit SHA and remove the file in a later commit.
- App commands (`apps`): `{key}` is substituted already shell-quoted, so never put it inside double quotes or the quotes show in the toast. Never open a user-supplied file with a configured app's command (`open -a Terminal file.sh` runs it); limit file opening to known editors and `open -R`.
- Bot review comments (cubic, coderabbit) start with HTML comments and tags; strip them before showing a comment in a toast.
- Sub-tasks: always set `--desc`/`--desc-file` at creation, or the agent has no context. A `spawn:` line in the status block only gives the sub-task the parent's summary/state/next; when it needs real context use `conveyor task new --parent KEY --desc ...`.
- Keep every major action reachable from the CLI; the UI calls the same `core.Service`.
- Update `docs/guide.md` (and this file) when behaviour or commands change.
