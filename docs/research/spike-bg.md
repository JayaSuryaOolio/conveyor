# Spike: `claude --bg` as Conveyor's runner

Claude Code 2.1.310, 2026-10-24.

**Fixture:**
- two throwaway repos, `alpha` and `beta`, each with its own `CLAUDE.md` code word, a skill and an agent
- a task workspace `ws/CV-1/{alpha,beta}`, made of `git worktree`s on branch `cv/CV-1`
- a Python HTTP receiver that logs hook POSTs
- every run used `--model haiku`

**Verdict:** `--bg` works as the runner, given two settings (below). The tmux fallback isn't needed.

## Answers

| Question | Answer | Evidence |
|---|---|---|
| Is the prompt delivered? | Yes, **if the prompt comes before variadic flags**. `--add-dir <dirs...>` swallows a trailing positional prompt as a directory: the session starts with an empty input box, `state: blocked`, and no hooks fire. | t1 (prompt last: nothing ran), t2 (prompt first: ran) |
| Do `--settings` HTTP hooks fire under `--bg`? | Yes: `UserPromptSubmit`, `Stop` and `Notification`. **`SessionStart` was never delivered** to our HTTP hook. | t2–t6 hooks.log |
| Does `Stop` carry `last_assistant_message`? | Yes. Other keys: `session_id`, `cwd`, `transcript_path`, `permission_mode`, `prompt_id`, `stop_hook_active`, `background_tasks`, `session_crons`, `effort`. | t2 |
| Is `--session-id` honoured? | **No.** The bg id is the first 8 hex of a new session UUID. | t1: passed `39cf62e4…`, got `e343dd9d…` |
| Does it isolate itself inside an existing linked worktree? | **Yes, by default.** On the first edit it created `repos/alpha/.claude/worktrees/cv-1-notes`, inside the *main* repo, and wrote alpha's file there, outside our workspace. `../beta` (from `--add-dir`) was written in place. That contradicts the docs' skip rule ("already inside a linked git worktree"), so it's likely a bug. | t2 |
| Can that isolation be turned off? | Yes: `{"worktree": {"bgIsolation": "none"}}` in the `--settings` file (documented in agent-view.md, under "How file edits are isolated"). It edited in place with no new worktree. The undocumented env `CLAUDE_CODE_HOST_WORKTREE=<cwd>` also works; don't depend on it. | t6b, t4 |
| `--bg --resume <sid> "<msg>"` **with flags** | Starts a **copy** under a new id. The context is carried (it answered from memory). The flags passed apply to the copy. | t3 |
| `--bg --resume <sid> "<msg>"` **without flags**, original stopped | Continues **the same id** with its saved options ("woke session ee0081a7 with its saved options (--name, --settings, --permission-mode, --strict-mcp-config, --add-dir, --model)"). Context and hooks are kept. | t6a |
| Same, but the original is still alive (idle) | Starts a copy *without* the saved options: the user's default hooks and permission mode apply, and our hooks don't fire. So **stop first, then resume**. | t5 |
| `claude agents --json [--all]` | It also lists the operator's *interactive* sessions (no `id`, with `name` and `cwd`), and filters with `--cwd <path>`. A finished bg run shows as `state: done, status: idle`; a waiting one shows as `blocked/idle`. | all |
| `claude logs <id>` | Raw ANSI TUI repaint, which is not machine-readable. Use the Stop hook plus `transcript_path` instead. | t6 |
| `claude rm <id>` | Refuses while a session-created worktree is dirty ("kept … worktree is still at …"). | cleanup |

## Multi-repo config loading (t1, `-p` runs)

| cwd | `CLAUDE.md` loaded | skills / agents seen | ctx baseline |
|---|---|---|---|
| `ws/CV-1` (parent of the worktrees; multica's layout) | none | none | ~29K |
| `ws/CV-1/alpha` + `--add-dir ../beta` | alpha only | alpha **and** beta | ~29K |
| same + `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1` | alpha and beta | alpha and beta | ~30K |

The baselines were measured with `--strict-mcp-config`.

## Workspace trust

`--bg` refuses an untrusted folder ("Workspace not trusted. Run `claude` in <dir> once…"). `-p` skips the check, which is how multica gets away with it.

**Trust rules:**
- A **linked worktree inherits trust from its main repo.**
- A **trusted parent folder does not** extend trust to a git worktree beneath it, though a plain subfolder does inherit it.

**Consequence:** repos the operator already uses with Claude need nothing. For a never-opened repo, Conveyor fails the launch with a clear "open it in Claude once" error. It does not write `~/.claude.json`.

## Resulting launch recipe

```
cd ~/.conveyor/workspaces/CV-12/<primary-repo>
CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1 \
claude --bg "<run prompt>" \
  --name CV-12 \
  --settings ~/.conveyor/claude-settings.json \   # HTTP hooks + worktree.bgIsolation: none
  --permission-mode bypassPermissions \
  --strict-mcp-config [--mcp-config .mcp.json] \
  [--model …] [--agent …] \
  --add-dir ../<repo2> ../<repo3>                 # variadic, so it must come after the prompt
```

- **Session id:** parse `backgrounded · <id>` from stdout. The full UUID arrives as `session_id` on the first `UserPromptSubmit` hook (map it by `cwd`).
- **Follow-up run (hot tier):** `claude stop <id>`, then `claude --bg --resume <uuid> "<delta>"` with **no flags**.
- **Fresh run (warm tier):** a new launch as above.
- **Start signal:** use `UserPromptSubmit`, not `SessionStart`.
