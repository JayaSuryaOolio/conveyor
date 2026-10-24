# Conveyor

A local kanban that runs tasks through Claude Code background sessions. Each task has a Jira-like page with a comment thread; a comment triggers the next run; every run is a real Claude session you can attach to; each task works in its own workspace folder holding one git worktree per repo.

Design: `docs/tech-spec.md`. Verified Claude Code behaviour: `docs/research/spike-bg.md`. How to use it: [`docs/guide.md`](docs/guide.md).

## Architecture

![Conveyor architecture](docs/diagrams/architecture.svg)

- **App and CLI.** The Wails desktop app (React UI, Go bindings in `internal/ui`) and the `conveyor` CLI (`cmd/conveyor`) are two front ends over the same `core.Service` (`internal/core`). Both read and write one SQLite database (`internal/store`) in `CONVEYOR_HOME`. The UI polls `PRAGMA data_version` and refreshes when the CLI or a hook changes the data.
- **Dispatcher.** One dispatcher runs at a time (a lock file in the home directory). It lives in the app, or in `conveyor daemon` when the app is closed. Every 2 seconds it reaps dead runs, launches `todo` tasks within `max_parallel`, and now and then syncs PRs and runs automations.
- **Workspaces and sessions.** A launch creates `~/.conveyor/workspaces/<KEY>/<repo>` (a git worktree per repo), writes `BRIEF.md`, and starts `claude --bg --settings claude-settings.json`. A comment on a finished task resumes the same session if its context is small, otherwise a fresh one starts.
- **Hooks.** The settings file points Claude Code's `UserPromptSubmit`, `Stop`, `Notification` and `PreToolUse` hooks at `conveyor hook <Event>`. That is how a run reports its result and how a task moves to `review` or `needs_you`.
- **Around the edges.** `gh` tracks PRs and finishes a task when they merge. Learnings, automations and apps are small modules on top of `core`. The app also embeds terminals (`internal/term`).

The task states and the hook flow have their own diagrams in the [guide](docs/guide.md). The diagram sources are `.excalidraw` files in `docs/diagrams/`.

## Install

Requires Go 1.25, `git`, and the `claude` CLI (plus `gh` for PR tracking).

```
scripts/install.sh             # CLI to ~/.local/bin (BIN_DIR), Conveyor.app to /Applications (APP_DIR)
```

It runs `go install ./cmd/conveyor` and `wails build` (installing Wails if missing).

## First run

1. **Trust your repos (once).** `claude --bg` refuses untrusted folders. Run `claude` in each repo you will register, accept the trust prompt, and exit. A linked worktree inherits trust from its main repo; a trusted parent folder does not extend to worktrees. If a launch fails with "workspace not trusted", this is why.
2. Add repos: `conveyor repo add ~/work/my-repo`
3. Create a task: `conveyor task new "Fix the thing" --repo my-repo --desc "..." --todo`
4. Start dispatching: open the app, or run `conveyor daemon` (only one dispatcher runs at a time; the app takes over when the daemon quits).
5. Watch it: `conveyor task show CV-1 --thread --runs`, or the board in the app. Comment to send it back: `conveyor task comment CV-1 "also do X"`.

`CONVEYOR_HOME` (default `~/.conveyor`) holds the DB, logs and workspaces; override with `--home` (before the subcommand) or the env var.

## CLI reference

```
usage: conveyor [--home dir] <command> [args]

  app        open worktrees in external apps (list|presets|add|rm|open)
  automation manage automations (list|add|enable|disable|run|rm)
  config     list, get or set settings
  daemon     run the dispatcher in the foreground
  hook       handle a Claude Code hook event (internal)
  hooks      Claude Code hooks in ~/.claude/settings.json (status|install|uninstall)
  import     import tasks (jira --jql ... --repo a)
  learning   curate learnings (list|accept|reject|edit|apply)
  repo       manage registered repos (add|list|rm)
  task       manage tasks (new|list|show|comment|move|cancel|done|sync|brief|attach|open)
```

Run `conveyor <command>` with no arguments for per-command usage. Listing and show commands accept `--json`.

## Integrations

- **Terminal template.** `conveyor task open KEY --shell|--resume|--fresh|--editor` opens a terminal via the `terminal_template` setting (default: macOS Terminal through `osascript`; `{cwd}` and `{cmd}` are substituted). Set an iTerm, Ghostty or tmux template with `conveyor config set terminal_template '...'`. `--print` shows the command instead.
- **Apps.** Open a task's worktree in any app: an app is a name plus a shell command where `{cwd}` becomes the quoted worktree path. Installed editors, terminals, git clients, Finder and the `spotlight` CLI are offered as presets. Manage them on the Apps page or with `conveyor app presets`, `conveyor app add Cursor` (preset), `conveyor app add Zed --command 'zed {cwd}'`, `conveyor app open Zed CV-12 [--repo r] [--print]` (or a directory instead of a key), `conveyor app rm Zed`. The task page offers them under "Open in…" on each repo.
- **Agent hooks.** Conveyor-launched sessions always report through `--settings`. `conveyor hooks install` also adds the same hook commands (UserPromptSubmit, Stop, Notification) to `~/.claude/settings.json`, so a `claude` you start yourself in a task worktree (from an app or any terminal) shows up as an interactive run. Sessions outside Conveyor workspaces are ignored, and identical commands run once per event. `conveyor hooks status|uninstall`; the old file is kept as `settings.json.conveyor.bak`.
- **Automations.** Cron-scheduled actions with an optional shell precheck: `conveyor automation add --name n --schedule "0 9 * * 1-5" --precheck "..." --action create_task --arg '{...}'`. Actions: `create_task`, `comment`, `cli`.
- **Jira.** `conveyor import jira --jql "..." --repo a` (needs `acli`).

## End-to-end test

`scripts/e2e.sh` drives the full loop against the real `claude` (haiku) on two throwaway repos. The repos must already be trusted: by default they live in `~/work/conveyor-e2e/repo-a` and `repo-b` (override with `E2E_ROOT`); create them, run `claude` in each once by hand, then run the script.

## TODO

- Command palette (⌘K): jump to any task, view or action, and run commands (new task, move, comment, open terminal), more robust than the sidebar search.

## Development

`gofmt -l .`, `go vet ./...`, `go test ./...`; frontend: `cd frontend && npm ci && npm run build`.
