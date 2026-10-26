# Conveyor: a kanban that runs Claude Code for you

Write a task, move it to Todo, and Conveyor runs it in a real Claude Code session inside its own git worktree. Comment on the task and the next run picks up your comment. It lives on your machine: a desktop app and a CLI over one SQLite file.

<!-- Screenshots: drop PNGs in docs/screenshots/ with these names. -->
![Board](screenshots/board.png)

## How it works

![Architecture](diagrams/architecture.svg)

- **One task, one workspace.** `~/.conveyor/workspaces/<KEY>/` holds one git worktree per repo the task touches, plus `BRIEF.md`. The agent works there and nowhere else.
- **Real sessions, not a wrapper.** Each run is `claude --bg` in the task's worktree. You can attach to it, resume it in your terminal, or open a fresh interactive session with the task's context from the task page.
- **Hooks report back.** Claude Code's hooks call `conveyor hook`. When the agent stops, its status block moves the task to Review or Needs you.
- **Tracked to the finish line.** Conveyor follows each repo's PR with `gh` and marks the task done when its PRs merge.

![Task lifecycle](diagrams/task-lifecycle.svg)

_Not yet shown: reviving a cancelled task to backlog or todo._

![Task page](screenshots/task-page.png)

## Decisions

| Decision | Why |
|---|---|
| Runs bypass permissions, confined to the task's workspace | Unattended runs can't stop at prompts. The worktree is the safety boundary. Interactive sessions keep normal permissions. |
| Agents, skills and commands come from the repo | Nothing to register or sync. The task's worktree is a normal checkout, so Claude Code loads the repo's `CLAUDE.md` and `.claude/` as usual. Picking an agent just passes `--agent`. |
| Repo context outranks Conveyor's | The brief says the repo's CLAUDE.md, skills, commands and agents decide how to work. Conveyor only owns the workspace, PR and status block rules. |
| One task, many repos | One worktree per repo and at most one PR per repo. The primary repo is the working directory and the others are attached with `--add-dir`. |
| Comments drive runs, Jira style | A comment on a finished task starts the next run. A comment on a running task is queued for the next one. A short debounce lets you send several before it starts. |
| Everything has a CLI | `conveyor task new/comment/move/link ...`. Agents use the same CLI to split work into sub-tasks and links. |
| Local and boring | Go, SQLite and Wails. No server, no account. The UI and CLI call the same `core.Service`, so the UI refreshes when the CLI or a hook changes data. |

## Token-efficient runs

The thing that costs tokens is how much context each run starts with. Conveyor decides that per run:

- **Resume when it's cheap, start fresh when it isn't.** If the last run ended cleanly below `resume_threshold` (60K tokens of context), the next comment resumes that session with only the new comments as the prompt. Above that, a fresh session starts from the brief. On the real task that motivated this, small follow-ups were resuming at 78–150K tokens of context. A fresh session costs about 15–20K plus the brief.
- **A summary instead of the whole history.** `BRIEF.md` is a few KB: the description, the acceptance criteria and their current state, the last run's state and next step, one-line summaries of recent runs, and your comments. The full thread is one command away (`conveyor task show KEY --thread --json`) if the agent needs it.
- **A structured status block.** Every run ends with `status / summary / state / next / criteria / learning / spawn`. That feeds the next brief, so the next agent doesn't re-read the transcript to find out where things stand.
- **No duplicate context.** The primary repo's skills and commands are already in Claude Code's context, so the brief doesn't list them again. It lists only the other repos' context, which `--add-dir` doesn't load.
- **Fewer MCP servers.** Runs use `--strict-mcp-config` with the repo's `.mcp.json`, so global MCP servers the task doesn't need stay out of the prompt.
- **Context use per run.** Every run records API calls, peak context and token counts, so you can tune the threshold from real numbers.

## It improves itself

- **Learnings.** Agents report `learning:` lines. You accept or reject them on the Learnings page, and accepted ones are applied as a normal task that edits the repo's `CLAUDE.md` or skills through a PR you review.
- **Conveyor doctor.** Every Monday an automation starts a task that samples the week's longest, shortest, failed and most-commented runs. It reads their briefs, transcripts and threads and proposes small changes to the prompts, settings or a repo's `CLAUDE.md`, citing the runs behind each one.
- **Automations.** Cron-scheduled tasks, such as sweeping PR review comments, are themselves just tasks on the board.

![Hook flow](diagrams/hook-flow.svg)

## More

- [User guide](guide.md): how everything works
- [Tech spec](tech-spec.md): the original design
- [Research](research/landscape.md): the tools we looked at (kanban-code, multica, paperclip, orca, hermes-agent) and what we took from each
