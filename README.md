# Conveyor

A local kanban that runs tasks through Claude Code background sessions. Each task has a Jira-like page with a comment thread; a comment triggers the next run; every run is a real Claude session you can attach to; each task works in its own workspace folder holding one git worktree per repo.

Design: `docs/tech-spec.md`. Verified Claude Code behaviour: `docs/research/spike-bg.md`.

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

  automation manage automations (list|add|enable|disable|run|rm)
  config     list, get or set settings
  daemon     run the dispatcher in the foreground
  hook       handle a Claude Code hook event (internal)
  import     import tasks (jira --jql ... --repo a)
  learning   curate learnings (list|accept|reject|edit|apply)
  repo       manage registered repos (add|list|rm)
  task       manage tasks (new|list|show|comment|move|cancel|done|sync|brief|attach|open)
```

Run `conveyor <command>` with no arguments for per-command usage. Listing and show commands accept `--json`.

## Integrations

- **Terminal template.** `conveyor task open KEY --shell|--resume|--fresh|--editor` opens a terminal via the `terminal_template` setting (default: macOS Terminal through `osascript`; `{cwd}` and `{cmd}` are substituted). Set an iTerm, Ghostty or tmux template with `conveyor config set terminal_template '...'`. `--print` shows the command instead.
- **Automations.** Cron-scheduled actions with an optional shell precheck: `conveyor automation add --name n --schedule "0 9 * * 1-5" --precheck "..." --action create_task --arg '{...}'`. Actions: `create_task`, `comment`, `cli`.
- **Jira.** `conveyor import jira --jql "..." --repo a` (needs `acli`).

## End-to-end test

`scripts/e2e.sh` drives the full loop against the real `claude` (haiku) on two throwaway repos. The repos must already be trusted: by default they live in `~/work/conveyor-e2e/repo-a` and `repo-b` (override with `E2E_ROOT`); create them, run `claude` in each once by hand, then run the script.

## Development

`gofmt -l .`, `go vet ./...`, `go test ./...`; frontend: `cd frontend && npm ci && npm run build`.
