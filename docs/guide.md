# Conveyor user guide

Conveyor is a local kanban board. You describe a task, Conveyor starts a Claude Code session for it in its own git worktree, and the session reports back when it stops. You review the result, comment to send it back, or mark it done.

This guide covers day-to-day use. For setup see the [README](../README.md); for the design see [tech-spec.md](tech-spec.md). Where the two disagree with the code, the code is right.

## Concepts

- **Task.** A card with a key (`CV-12`, the prefix is the `key_prefix` setting), a title, a description, one or more repos, a status, a priority, an optional model and agent, and a comment thread.
- **Run.** One launch of Claude for a task. A run has its own status (`launching`, `running`, `finished`, `failed`), a Claude session id, token counts, and the summary the agent wrote. A task usually has several runs. Runs started by the dispatcher are `auto`; sessions you start yourself in a task worktree are `interactive`.
- **Workspace and worktree.** Each task gets a folder `~/.conveyor/workspaces/<KEY>/` with one git worktree per repo, `<KEY>/<repo>`, on a branch named after the task title. The folder also holds `BRIEF.md`, which the agent reads first. A worktree branches from `origin/<default branch>` after a fetch, or reuses the branch if it already exists.
- **Agent.** Optional. A named Claude Code agent (a prompt, optionally limited to one repo) that a task can run as. Its `model:` frontmatter line is the agent's default model: a task's own model wins, then the agent's, then `default_model`. `conveyor agent list|add|model`, or the Agents page.
- **Learning.** A lesson an agent proposes at the end of a run. You accept or reject it; accepted ones can be written into the repo's `CLAUDE.md` (or another file) by a follow-up task.
- **Automation.** A cron schedule with an optional shell check that creates a task, comments on tasks, or runs a CLI command.
- **App.** A named shell command that opens a task worktree in an editor, terminal or other program.

## The board and the task page

The sidebar has Board, Focus, Learnings, Automations, Repos, Apps, Agents, Logs and Settings. Board, Focus, Agents and Logs have the shortcuts `b`, `f`, `a` and `l`.

The board has a column per status: Backlog, Todo, Running, Needs you, Review, Failed (only shown when something has failed), Done and Cancelled. Done and Cancelled are collapsed and show the last 7 days. Focus is a single list of what wants your attention, in this order: needs_you, review, failed, running, todo.

The task page shows the description, the comment thread (yours, the agent's final replies, and system notes), the runs with their summaries and acceptance criteria, PR links per repo, and buttons to open a terminal, an editor or an app. Comments can include pasted images; they are saved under `~/.conveyor/workspaces/<KEY>/attachments`.

### Statuses

![Task lifecycle](diagrams/task-lifecycle.svg)

- **backlog**: parked. Nothing runs.
- **todo**: waiting for the dispatcher.
- **running**: a session is working on it.
- **needs_you**: the agent stopped without a final status block (it asked you something), or its session disappeared.
- **review**: the agent said it is done. If there is a PR, Conveyor watches it.
- **failed**: repeated failures. Comment to retry.
- **done**: finished, either by you or because every PR merged.
- **cancelled**: only you can cancel, from any status except done.

Moving a task to done or cancelled also stops and removes its Claude background sessions.

## How dispatch works

One dispatcher runs per Conveyor home. It is part of the app, and `conveyor daemon` runs it in the foreground when the app is closed. A lock file (`dispatcher.lock`) keeps it to one; the app tries to take over every 10 seconds, so closing the daemon hands over to a running app.

Every 2 seconds the dispatcher:

1. Reaps runs. If a run's background session is gone from `claude agents`, after a 30 second grace, the run is marked failed ("session vanished") and the task goes to needs_you.
2. Every 60 seconds, checks PRs of tasks in review with `gh` (see below).
3. Every 30 seconds, runs due automations.
4. Launches `todo` tasks, highest priority first, up to `max_parallel` (default 3) at once. A task with new comments waits `comment_debounce_secs` (default 20) after the last one, so you can write several comments before it starts.

To launch, it creates the worktrees, writes `claude-settings.json` and `BRIEF.md`, creates a run, marks the comments as consumed, moves the task to running and starts Claude:

```
claude --bg <prompt> --name <KEY> --settings <home>/claude-settings.json \
  --permission-mode bypassPermissions --strict-mcp-config [--model m] [--agent a] [--add-dir other-repos]
```

If the launch itself fails, the failure counter goes up and the task returns to todo. After `max_consecutive_failures` (default 3) in a row the task becomes failed with a system comment. Conveyor needs the repo to be trusted by Claude; the comment says so when that is the cause.

The agent works in the worktree and ends its last message with a status block:

```
<conveyor>
status: done | failed
summary: one paragraph
state: branch ..., PR #N, tests: pass|fail|not run
next: what remains
criteria:
- [x] AC1 - evidence
learning: a lesson
spawn: title of a follow-up task
</conveyor>
```

Without a block the agent is treated as asking you a question. `BRIEF.md` explains this protocol to the agent, along with the task, repos, latest runs and your comments. The `brief_extra_rules` setting adds your own rules to it.

PR comments: the agent is told to start every PR or issue comment with `pr_comment_prefix` (default `ai agent: `), and a hook blocks `gh` comment commands that lack it.

## Task relations

Tasks can be related, as in Jira. Conveyor only stores the relations; an agent (for example an "engineering manager" that writes a spec and splits it) creates the tasks and hands work off itself, using the CLI that `BRIEF.md` describes.

- **Parent and sub-tasks**: `conveyor task new "..." --repo r --parent CV-1`. A sub-task may use a different repo than its parent. A sibling is just another child of the same parent. Child keys extend the parent's: CV-1A, CV-1B ... CV-1Z, CV-1AA, CV-1AB ... Grandchildren switch to digits so keys stay unambiguous: CV-1A1, CV-1A2 ..., then CV-1A1A. Top-level numbering (CV-2, CV-3 ...) ignores them.
- **Links**: `blocks`, `relates` and `duplicates`, shown from the other side as "is blocked by", "relates to" and "is duplicated by". A todo task that is blocked by a task not yet done or cancelled is not launched; the task page says why.

```
conveyor task link CV-1A blocks CV-1B
conveyor task unlink CV-1A blocks CV-1B
```

`conveyor task show` lists the parent, sub-tasks and links. The task page has a Relations section in the sidebar with the same, plus add and remove for links; board cards show the parent key.

## Comments, resume and fresh sessions

A comment from you, or a system note, on a task in needs_you, review, failed or done puts it back in todo, and the dispatcher launches it again. A comment on a running task is queued (marked "queued" in the thread) and sent with the next run: when the run stops or its session vanishes, the task goes to todo instead of review or needs_you. A comment on a backlog or todo task just waits.

When the dispatcher launches a task that already has runs, it chooses between:

- **Resume**: stop the old background session and run `claude --bg --resume <session>` with only the new comments as the prompt. Used when the latest auto run finished, its session is known, and its peak context was below `resume_threshold` (default 60000 tokens).
- **Fresh**: a new session that reads `BRIEF.md`, which carries the summaries of recent runs and your comments. Used otherwise.

## Interactive sessions and terminals

The app has embedded terminals on the task page: a shell in the worktree, or one of the actions below. Output survives switching away (a 256 KB replay buffer), and terminals are closed when the app quits. From the CLI, `conveyor task open KEY` opens an external terminal instead, through the `terminal_template` setting.

Actions on a task:

- **attach**: `claude attach` to the live background session of an active run. `conveyor task attach KEY`.
- **resume**: `claude --resume <session> --settings <home>/claude-settings.json` on the latest session.
- **fresh**: a new interactive `claude` that is told to read `BRIEF.md` and wait for your instructions.
- **shell**: a login shell in the worktree.
- **editor**: the `editor_command` setting (default `code {cwd}`).

Sessions you started by hand in a worktree also appear on the task page, found by scanning `~/.claude/projects` for that worktree path.

With the Claude Code hooks installed (below), those manual sessions are also recorded as interactive runs, and replying to a task in needs_you moves it back to running.

## Hooks

![Hook and event flow](diagrams/hook-flow.svg)

Sessions report back through Claude Code hooks, which run `conveyor hook <Event>`. Conveyor-launched sessions get them through `--settings`. For sessions you start yourself, run:

```
conveyor hooks install      # status | install | uninstall
```

This adds `UserPromptSubmit`, `Stop` and `Notification` to `~/.claude/settings.json` (the old file is kept as `settings.json.conveyor.bak`). The PR prefix hook is not installed there, so it never affects your other Claude sessions. Hooks ignore any session whose directory is not under `~/.conveyor/workspaces/<KEY>`. The Apps page in the app shows the same status with an install button.

## Learnings

At the end of a run the agent can add `learning:` lines. Each becomes a proposed learning on the Learnings page, linked to its task and repo.

- **Accept** only marks the learning as accepted in Conveyor's database and sets a target file (default `CLAUDE.md`). Nothing is written to your repo yet.
- **Apply** (per repo) creates one todo task, "Apply learnings to <repo>", whose description lists the accepted learnings with their target files. Its agent edits those files and opens a PR like any other task. The learnings are marked applied. If an identical task is already pending, it is reused.
- **Personal** learnings (no repo) are not applied by a task. The page shows them for you to paste into `~/.claude/CLAUDE.md` yourself.
- **Reject** and **edit** work on proposed ones.

```
conveyor learning list [--status s]
conveyor learning accept ID [--target f]
conveyor learning apply [--repo r]
```

## Automations

An automation has a name, a standard 5-field cron schedule, an optional shell precheck, a kind and a JSON argument. The dispatcher checks every 30 seconds. After downtime a missed run fires once. The precheck runs with `sh -c` (60 second limit); a non-zero exit skips the run, and its output (up to 8 KB) can be used as `{output}` in the arguments. The last run time and result are shown on the page.

Kinds:

- `create_task`: `{title, description, repos, start}`.
- `comment`: `{task_key | task_status, body}`. With `task_status` it comments on every task in that status, running the precheck once per task with `CONVEYOR_TASK_KEY`, `CONVEYOR_TASK_BRANCH` and `CONVEYOR_TASK_PR` set.
- `cli`: `{args}`, run as a `conveyor` command.

There is one built-in template, `pr-sweep` (every 15 minutes, comments on tasks in review that have new PR reviews):

```
conveyor automation add --template pr-sweep
conveyor automation add NAME --schedule "0 9 * * 1-5" --kind create_task --arg '{"title":"Daily check","repos":["my-repo"],"start":true}' [--precheck CMD]
conveyor automation list | enable NAME | disable NAME | run NAME | rm NAME
```

## Apps

An app is a name plus a shell command in which `{cwd}` is replaced by the quoted worktree path. The Apps page offers presets detected on your Mac (editors, terminals, git clients, Finder). Apps show up as a row of icons on each repo of the task page; hover for the name. A launched icon flashes a check or a cross, and Spotlight shows a green dot while it runs for that worktree (click again to stop it).

```
conveyor app presets
conveyor app add Cursor                        # preset
conveyor app add Zed --command 'zed {cwd}'
conveyor app open Zed CV-12 [--repo r] [--print]
```

## Pull requests

For tasks in review, every 60 seconds Conveyor runs `gh pr list --head <branch>` in each repo and stores the PR URL and state. If every PR is merged or closed and at least one merged, the task moves to done. If every PR was closed without merging, a system comment is added and the task goes to needs_you. `conveyor task sync KEY` does this once, on demand. You can also attach a PR by hand: `conveyor task set KEY --repo r --pr URL`.

## Settings

Settings are in the app (Settings page) or `conveyor config list|get|set`.

- **General**: `key_prefix` (CV), `default_model`.
- **Dispatch**: `max_parallel` (3), `resume_threshold` (60000), `comment_debounce_secs` (20), `max_consecutive_failures` (3).
- **External terminal**: `terminal_template`, the command for external terminals (`{cwd}` and `{cmd}` are substituted; default uses macOS Terminal).
- **Prompts**: `pr_comment_prefix`, `brief_extra_rules`, and `prompt_fresh` / `prompt_resume` to replace the launch prompts (placeholders `{key}`, `{title}`, `{brief}`, `{comments}`). Empty means the built-in prompt. The page also shows the status block protocol the agent is given.

## CLI cheat-sheet

Global flag: `conveyor [--home dir] <command>`. Running a command with no arguments prints its usage. List and show commands take `--json`.

```
conveyor repo add <path> [--name n]       conveyor repo list | rm <name>

conveyor task new "<title>" --repo a[,b] [--primary r] [--desc t | --desc-file f]
                  [--todo] [--model m] [--agent a] [--parent KEY] [--priority n]
conveyor task list [--status a,b]         conveyor task show KEY [--thread] [--runs]
conveyor task comment KEY <text|->        conveyor task move KEY <status>
conveyor task done KEY                    conveyor task cancel KEY
conveyor task set KEY [--repo r] [--branch b] [--pr url] [--model m]
conveyor task link KEY blocks|relates|duplicates OTHER    conveyor task unlink KEY kind OTHER
conveyor task sync KEY                    conveyor task brief KEY [--preview]
conveyor task log KEY [--follow]          conveyor task attach KEY
conveyor task open KEY [--shell|--resume|--fresh|--editor] [--repo r] [--print]

conveyor agent list [--repo r]            conveyor agent add <name> --desc d (--prompt p | --prompt-file f) [--model m] [--repo r]
conveyor agent model <name> <model|""> [--repo r]
conveyor learning list|accept|reject|edit|apply
conveyor automation list|add|enable|disable|run|rm
conveyor app list|presets|add|rm|open
conveyor hooks status|install|uninstall
conveyor config list | get <key> | set <key> <value>
conveyor import jira --jql "..." --repo a      # needs acli
conveyor daemon                                # run the dispatcher in the foreground
```

## Files on disk

`~/.conveyor` (or `CONVEYOR_HOME`, or `--home`):

```
conveyor.db                 tasks, runs, comments, events, learnings, automations, settings, apps
conveyor.log                app log (rotated at 5 MB to conveyor.log.1)
claude-settings.json        hooks and options passed to launched sessions, rewritten at each launch
dispatcher.lock             held by whichever process is dispatching
logs/dispatcher.log         output of `conveyor daemon`
logs/hook.log               errors from `conveyor hook`
workspaces/<KEY>/<repo>/    git worktrees
workspaces/<KEY>/BRIEF.md   the agent's brief
workspaces/<KEY>/attachments/   pasted images
```

Outside it, Conveyor reads `~/.claude/projects` (session transcripts), `~/.claude/agents`, and edits `~/.claude/settings.json` only through `conveyor hooks install`.

## Troubleshooting

- **Logs.** The Logs page in the app shows `~/.conveyor/conveyor.log`; from a terminal use `tail -f ~/.conveyor/conveyor.log`. If you run `conveyor daemon`, its log is `logs/dispatcher.log`. Hook problems are in `logs/hook.log`.
- **Nothing starts.** Check that a dispatcher is running (the app, or `conveyor daemon`), the task is in todo, and fewer than `max_parallel` runs are active. A task you just commented on waits `comment_debounce_secs`.
- **"workspace not trusted".** Run `claude` once in the main repo and accept the trust prompt. Worktrees inherit it.
- **Task is failed.** Read the system comment on the thread, fix the cause, then comment to retry.
- **Task stays in running after a manual session.** Runs of kind interactive are not reaped and do not move the task on stop. A human can move a running task only to review (or cancel it): `conveyor task move KEY review`.
- **Hooks do not fire for a manual session.** `conveyor hooks status`; the session must run inside `~/.conveyor/workspaces/<KEY>/...`.
- **A comment on a running task seems ignored.** It is delivered when the run stops.
