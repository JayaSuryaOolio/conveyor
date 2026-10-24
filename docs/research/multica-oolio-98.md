# Case study: multica run economics on OOLIO-98

Source data, collected 2026-10-24:
- `multica issue get|runs|usage|timeline OOLIO-98`
- the local Claude transcript, `~/.claude/projects/-Users-jayasurya-multica-workspaces-oolio-956527b6ae16-oolio-98-1a51c53eb92b-workdir/c4b83834-….jsonl` (4,439 lines, 11.6 MB)
- the task workspace, `~/multica_workspaces/oolio-956527b6ae16/oolio-98-1a51c53eb92b/`

## What happened

One issue ran 17 times over 4 days (15 runs completed, 2 cancelled). After the first implementation, every run was triggered by a short human comment, such as "revert this change", "sweep PR 1362 again" or "stack 1389 on 1362". All runs resumed **one** Claude session.

Multica-reported usage: 197K input, 2.9M output, **420M cache-read**, 13.3M cache-write tokens.

## Per-run numbers (from the transcript `usage` blocks)

| run start | API calls | ctx at first call | max ctx | compactions | trigger |
|---|---:|---:|---:|---:|---|
| 10-19 08:39 | 117 | 41K | 138K | 0 | initial |
| 10-22 11:28 | 98 | 140K | 162K | 1 | PR review sweep |
| 10-22 11:47 | 2 | 125K | 125K | 0 | "1374 is the PR now" |
| 10-23 04:19 | 114 | 135K | 166K | 1 | re-queued |
| 10-23 04:29 | 7 | 146K | 148K | 0 | "ignore last comment" |
| 10-23 04:31 | 32 | 149K | 165K | 1 | "stack 1389 on 1362" |
| 10-23 07:05 | 100 | 92K | 166K | 1 | "sweep 1362 again" |
| 10-23 08:17 | 3 | 78K | 78K | 0 | (superseded 24 s later) |
| 10-23 09:56 | 39 | 146K | 166K | 1 | "reason must be saved… none of your changes went into 1362" |
| 10-23 11:33 | 5 | 92K | 94K | 0 | "revert this change" |

Total context processed across 706 calls: **75M tokens**. A 5-call revert run starts at 92K tokens of context.

## Diagnosis

1. **The 41K baseline is paid before any work starts.** It consists of the system prompt, every MCP server's tools (figma, grafana, mobile, playwright, …), a 20.6 KB multica-generated `CLAUDE.md` (runtime rules, identity, workspace rules, an acli cheat-sheet) and skills copied into `workdir/.claude/skills`. A tight task needs perhaps 15–20K of that.
2. **cwd is `workdir/`, not the repo.** The repo is `workdir/accounts/`, so Claude Code's native discovery of the repo's `CLAUDE.md`, `.claude/skills`, `.claude/agents` and hooks doesn't load it as project config. Multica works around this with a prose instruction ("Always read claude commands and skills from <repo>/.claude"), and skills have to be registered with multica separately. This is the "multica doesn't read the repo's agents/skills" complaint.
3. **Resume carries everything; compaction is generic.** The session never resets. It grows to the auto-compact threshold, gets summarized by Claude Code's generic compactor, and grows again (5 compactions). The summary isn't task-structured, so facts like "which commit is on which PR" are lossy.
4. **The redundant re-read is the issue itself.** Every resumed run is told to run `multica issue get … --output json` first (the ~6 KB description), even though the session already holds it. The comment *delta* handling is good: "[NEW COMMENT] focus on this, delta is empty".
5. **One task ended up with two PRs** (#1362 and #1389). Commits landed on the wrong one, and the human had to say "offload 1389 to a different task". Nothing enforces one task = one branch = one PR.
6. **Rapid comments become separate runs.** At 08:17:27 and 08:17:51 two comments produced two runs, the first cancelled or superseded. Coalescing the queue would make that one run.
7. **No jump-in.** You can't open a terminal or an interactive `claude --resume` from the issue view, and you can't see which worktree the task used without digging through `~/multica_workspaces`.

## What a standalone session does differently (why it "feels better")

- cwd = repo root, so the repo's own `CLAUDE.md`, skills and agents load natively and nothing extra is injected.
- The human starts fresh when the context gets muddy (`/clear`, or a new session with a handoff note) instead of riding compaction forever.
- The human is in the loop at the moment of ambiguity, not 10 minutes later via a comment.

## Levers, ranked by token impact

1. **A fresh session from a structured brief when the session is big.** A brief of about 2–4K tokens replaces 90–150K of resumed context. Resume only while the session is small, because then it is cheap and keeps tool results warm.
2. **Run Claude at the repo or worktree root.** No injected CLAUDE.md, no copied skills; the baseline drops by the 20 KB file plus the skill copies.
3. **Scope MCP per launch** with `--strict-mcp-config --mcp-config <repo's .mcp.json>`. Every unused MCP server's tool schemas count toward the baseline.
4. **Coalesce queued comments** into one run.
5. **Use a cheaper model for mechanical runs** (PR comment sweep, rebase, revert).
