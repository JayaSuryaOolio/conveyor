<p align="center">
  <img src="build/appicon.png" alt="Conveyor" width="128">
</p>

<h1 align="center">Conveyor</h1>

<p align="center">
  <b>A kanban board that runs Claude Code for you.</b><br>
  Write a task, drop it in Todo, review the PR. Local, boring, fast.
</p>

<!-- Screenshots: drop PNGs in docs/screenshots/ (board.png, task-page.png) -->

---

## Why Conveyor

Most "software factories" want you to move in: a server, an account, a new agent runtime, their own way of registering skills. Conveyor doesn't. It's a desktop app and a CLI over one SQLite file on your machine, and every run is a **real Claude Code session** in a **real git worktree** of your repo.

- **Your repo stays in charge.** Runs load the repo's own `CLAUDE.md`, skills, commands and agents. Nothing to register or sync.
- **You can always jump in.** Attach to a running session, resume it in your terminal, or open a fresh one with the task's context.
- **Comments drive the work.** Like Jira: comment on a finished task and the next run picks it up. Comment while it runs and it's queued.
- **It finishes the job.** Conveyor follows each PR with `gh` and marks the task done when it merges.
- **It gets cheaper over time.** It decides how much context each run starts with, and learns from its own runs (see below).

Compared with kanban-code, multica, paperclip, orca and hermes, Conveyor keeps what we liked (real attachable sessions, worktree per task, repo-native config) and skips what we didn't (servers, headless-only agents, registries). The full comparison is in [`docs/research/landscape.md`](docs/research/landscape.md).

## The architecture advantage

![Conveyor architecture](docs/diagrams/architecture.svg)

**One task, one workspace.** Each task gets `~/.conveyor/workspaces/<KEY>/` with one git worktree per repo and a short `BRIEF.md`. Multi-repo tasks just work: the primary repo is the working directory, the rest come in with `--add-dir`, and there's at most one PR per repo.

**Two front ends, one core.** The Wails app and the `conveyor` CLI call the same `core.Service`, so anything you can click, an agent can script. Agents use the CLI themselves to split work into sub-tasks and links.

**Hooks, not scraping.** Claude Code's hooks call `conveyor hook`. When an agent stops, its status block moves the task to Review or Needs you. No log parsing, no guessing.

**Safe to leave alone.** Unattended runs bypass permission prompts, but the worktree is the boundary. Interactive sessions keep normal permissions.

## Prompt and token optimizations

The expensive part of an agent run is the context it starts with. Conveyor picks it per run.

**Resume when cheap, start fresh when not.** If the last session ended under `resume_threshold` (60K tokens), the next comment resumes it with only the new comments. Above that, a fresh session starts from the brief, roughly 15–20K tokens instead of 78–150K.

**A brief, not a history.** `BRIEF.md` is a few KB: description, acceptance criteria and their state, the last run's next step, one-line summaries of past runs, and your comments. The full thread is one command away if needed.

**Structured hand-offs.** Every run ends with a `status / summary / state / next / criteria / learning / spawn` block, which feeds the next brief. No agent re-reads a transcript to figure out where things stand.

**No duplicate context.** The primary repo's skills are already loaded by Claude Code, so the brief lists only the other repos' context. Runs use `--strict-mcp-config` with the repo's `.mcp.json`, so unrelated MCP servers stay out of the prompt.

**Measured, not guessed.** Every run records API calls, peak context and token counts, so you can tune from real numbers.

## It improves itself

- **Learnings.** Agents report `learning:` lines. Accept the good ones and Conveyor opens a PR to the repo's `CLAUDE.md` or skills.
- **Conveyor doctor.** Every Monday a task reviews the week's longest, shortest, failed and most-commented runs and proposes concrete prompt and settings fixes.
- **Automations.** Cron-scheduled tasks (e.g. sweeping PR review comments) are just more cards on the board.

More: [the showcase page](docs/showcase.md) (decisions and trade-offs) and the [user guide](docs/guide.md) (task states, hook flow, every feature).

---

## Quick start

Requires Go 1.25, `git`, the `claude` CLI, and `gh` for PR tracking.

```sh
scripts/install.sh                       # CLI → ~/.local/bin, Conveyor.app → /Applications
claude                                   # once per repo: accept the trust prompt, then exit
conveyor repo add ~/work/my-repo
conveyor task new "Fix the thing" --repo my-repo --desc "..." --todo
conveyor daemon                          # or just open the app
conveyor task comment CV-1 "also do X"   # send it back for another pass
```

`claude --bg` refuses untrusted folders, so do the trust step once per repo. A "workspace not trusted" launch failure means it was skipped. `CONVEYOR_HOME` (default `~/.conveyor`) holds the DB, logs and workspaces.

## For AI agents

Start with [`.claude/skills/conveyor/SKILL.md`](.claude/skills/conveyor/SKILL.md): commands, file locations, code map, troubleshooting.

| Need | Where |
|---|---|
| How to use every feature | [`docs/guide.md`](docs/guide.md) |
| Design and data model | [`docs/tech-spec.md`](docs/tech-spec.md) |
| Verified Claude Code `--bg` behaviour | [`docs/research/spike-bg.md`](docs/research/spike-bg.md) |
| Decisions and token strategy | [`docs/showcase.md`](docs/showcase.md) |
| Diagrams (sources are `.excalidraw`) | [`docs/diagrams/`](docs/diagrams/) |

**Tech:** Go, SQLite (`internal/store`), Wails + React/Mantine (`frontend/`, bindings in `internal/ui`), core logic in `internal/core`, CLI in `cmd/conveyor`, embedded terminals in `internal/term`. One dispatcher at a time (lock file in `CONVEYOR_HOME`), in the app or `conveyor daemon`, ticking every 2 seconds.

**CLI:** `conveyor <command>` with no args prints usage; list/show commands take `--json`.

```
agent  app  automation  config  daemon  hook  hooks  import  learning  repo  task
```

Integrations (terminal templates, external apps, global hooks, automations, Jira import) are covered in the [guide](docs/guide.md).

**Checks:** `gofmt -l .`, `go vet ./...`, `go test ./...`; frontend: `cd frontend && npm ci && npm run build`. End-to-end: `scripts/e2e.sh` against real `claude` (haiku) on two pre-trusted throwaway repos in `~/work/conveyor-e2e/` (override with `E2E_ROOT`).

## Roadmap

- A wrapped UI component layer so Mantine can be swapped in one place.
- A remote backend: dispatcher, DB and workspaces on a server, driven over an API.
- A ⌘K command palette for tasks, views and actions.
