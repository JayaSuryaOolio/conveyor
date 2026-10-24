---
name: conveyor
description: Operate, troubleshoot and change Conveyor (this repo) - the local kanban that dispatches tasks to Claude Code background sessions. Use when inspecting tasks/runs, debugging why a task did or didn't run, reading its logs or DB, or editing its Go/Wails/React code.
---

# Conveyor: agent reference

Kept short on purpose. When this file and the code disagree, the code wins; fix this file in the same PR.
Long-form: `docs/guide.md` (behaviour), `docs/tech-spec.md` (original design, partly stale).

## Model in one paragraph

A **task** (`CV-12`; sub-tasks `CV-12A`, `CV-12A1`) has repos, a status, a comment thread and **runs**. The **dispatcher** (inside the app, or `conveyor daemon`; one per home via `dispatcher.lock`) ticks every 2s: reap vanished sessions, sync PRs, run automations, launch `todo` tasks (up to `max_parallel`, skipping ones with open `blocks` links). A launch makes `~/.conveyor/workspaces/<KEY>/<repo>` worktrees + `BRIEF.md` and runs `claude --bg ... --permission-mode bypassPermissions --settings <home>/claude-settings.json`. Claude Code **hooks** call `conveyor hook <Event>`; the `Stop` hook parses the `<conveyor>` status block and moves the task to `review`/`needs_you`. A human comment on a finished task requeues it (resume if context < `resume_threshold`, else fresh); on a running task it is queued for the next run. Once a PR of the task is merged, a comment instead starts a follow-up sub-task (`core.followUp`, source `followup`) whose brief carries the parent's context; `--ask` requeues it to answer without code changes, `--note` only comments (the app asks which).

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
conveyor task set KEY [--branch b] [--pr url] [--model m] [--desc d]   # --desc: backlog only
conveyor task link KEY blocks|relates OTHER     conveyor task unlink KEY kind OTHER
conveyor task parent KEY PARENT|--none
conveyor agent list|add|model         conveyor config list|get|set
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
| Daemon / hook logs | `~/.conveyor/logs/dispatcher.log`, `logs/hook.log` |
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
| UI (React + Mantine) | `frontend/src`: `App.tsx` nav, `views.tsx` board/focus/task page, `workspace.tsx` task sidebar, `relations.tsx`, `forms.tsx` dialogs + settings, `agents.tsx`, `automations.tsx`, `apps.tsx`, `logs.tsx`, `terminals.tsx` |

## Troubleshooting recipes

Known failure modes with confirm/fix steps: `docs/troubleshooting.md` (e.g. a run stuck in running after its session finished, an untracked second PR). Add to it when you fix a new one.

- **Task not launching:** is a dispatcher running (`dispatcher.lock`, app status dot green)? Task in `todo`? Open blockers (`task show KEY --json` -> relations)? `max_parallel` reached? Recent comment still in `comment_debounce_secs`? Then `grep KEY ~/.conveyor/conveyor.log`.
- **Launch fails:** read the system comment on the thread. "workspace not trusted" = run `claude` once in the main repo.
- **Stuck in running:** `claude agents --json --all` - is the bg session alive? Interactive runs are reaped (finished, task -> needs_you) once their session leaves that list. `logs/hook.log` for hook errors.
- **Wrong context for the agent:** `conveyor task brief KEY --preview`.
- **UI shows stale data:** UI polls `PRAGMA data_version`; check the app log for binding errors.

## Changing the code

- Tests: `go test ./...` (fast, uses fake `claude` via `execCommand`); `gofmt -l .`; `go vet ./...`.
- Frontend: `cd frontend && npm ci && npm run build` (tsc + vite).
- New/changed Go binding in `internal/ui`: `wails generate module`, then `git checkout -- frontend/wailsjs/runtime` (don't commit runtime churn).
- After `wails build`/`npm run build`: `git checkout -- frontend/dist/.gitkeep frontend/package.json.md5`.
- Run the UI live: `wails dev` (browser at http://localhost:34115). Install: `scripts/install.sh`.
- Keep every major action reachable from the CLI; the UI calls the same `core.Service`.
- Update `docs/guide.md` (and this file) when behaviour or commands change.
