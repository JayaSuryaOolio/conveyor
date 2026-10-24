# 008: Workspace panel, terminal opener, CLI attach/open

**Wave 3** · depends on: 003, 005, 006 · branch `task/008-workspace-panel`

## Goal
The operator's main ask: from a task, see its worktrees and sessions, and open terminals or Claude sessions there.

## API
```go
package terminal
// Open fills the {cwd} and {cmd} placeholders and runs the template via `sh -c`. AppleScript-safe quoting for the default
// template (tech-spec §10): escape \ and " inside the AppleScript string, and shell-quote the cwd.
func Open(template, cwd, cmd string) error
func Render(template, cwd, cmd string) string // for --print and copy buttons
```
- `core.Service` gains `Actions(key string) ([]Action, error)`. Each action is `{Label, Repo, Cwd, Cmd string; Kind string}`. The kinds:

  | Kind | Command | When offered |
  |---|---|---|
  | `attach` | `claude.AttachCommand(bg_id)` | latest run active |
  | `resume` | `claude.ResumeCommand(session_id, settings)` | latest run has a session_id and isn't active |
  | `fresh` | `claude.FreshCommand(...)` with the brief path | always |
  | `shell` | empty command, just `cd` | always |
  | `editor` | the `editor_command` setting | always |

  `attach`, `resume` and `fresh` are offered once per task, in the primary cwd. `shell` and `editor` are offered per repo. Before computing actions, ensure the settings file exists and the brief is rendered.
- **Sessions list.** Also return the manual sessions found in `~/.claude/projects/<slug(worktree_path)>/*.jsonl`. The slug replaces every non-alphanumeric character with `-`, as in worktree-studio's `ListSessionsForCwd`. Return the id, mtime and first user prompt (truncated to 80 chars). Each gets a `resume` action.
- **CLI:**
  - `task attach KEY`: `syscall.Exec` of `claude attach <bg_id>`.
  - `task open KEY [--shell|--resume|--fresh|--editor] [--repo r] [--print]`: default `--shell` on the primary; `--print` prints the rendered command instead of opening it.
- **App: `WorkspacePanel` component in the Task page aside.**
  - Per repo: name, worktree path (click to copy), branch, PR link and state.
  - The action buttons, each with a ⧉ copy button that copies `cd <cwd> && <cmd>`.
  - A sessions list, run rows plus manual sessions.
  - Bound methods `Actions(key)`, `RunAction(key, kind, repo)` and `Sessions(key)`.
  - A "Workspace not created yet" state with a "Create workspace" button that calls `workspace.Ensure`.

## Tests
- `terminal.Render` quoting cases: spaces, a `'`, a `"`, and `$`.
- `Open` with a fake `osascript` records argv.
- The actions set across task states.
- Session discovery against a temp `~/.claude/projects` (inject the projects root).
- CLI `task open --print`.
