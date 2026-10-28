---
name: conveyor-doctor
description: Review a sample of recent Conveyor task runs (longest, shortest, failed, most-commented) and Conveyor's logs, judge whether the prompts, brief and repo CLAUDE.md gave agents what they needed without waste, and propose concrete fixes. Use for the weekly Conveyor review task, or when asked to audit Conveyor runs, prompts or token use, or to fix a stuck Conveyor task ("conveyor-doctor fix [KEY]").
---

# Conveyor doctor

You audit how well Conveyor's context serves its agents and turn what you find into small, reviewable changes. You are usually running as a Conveyor task yourself (the weekly "Conveyor doctor" task); leave that task out of the sample.

Read-only on everything except: your own task's worktree (the Conveyor repo), new backlog tasks you create, and your final report. Never edit `~/.conveyor/conveyor.db`, settings, or another repo directly.

## Fix mode

When asked to fix (`fix KEY`, or `fix` for every task), skip the review below. For each task, or each task in running, needs_you or review:
1. Read `docs/troubleshooting.md` in the Conveyor repo and run each recipe's **Confirm** steps.
2. Apply the **Fix** of every recipe that matches. Fixes go through `conveyor` commands (`conveyor hook ...`, `conveyor task set/sync/move`), never by editing the DB; that is the one exception to the read-only rule above. A resume run stuck `running` while an extra `interactive` run exists is the session-id drift in `docs/session-identity.md`: replay Stop under the resume run's own session id (see its recipe), not the new one. `conveyor task stop KEY` ends a live background session that will not end on its own; it fails the run and moves the task to needs_you.
3. Report per task: what was wrong, the commands you ran, and the state afterwards. A stuck state no recipe covers: describe it and propose a recipe; do not improvise a fix.

## 1. Pick the sample (6-10 runs from the last 7 days)

```
conveyor task list --json                         # all tasks
conveyor task show KEY --runs --thread --json     # runs: started_at, ended_at, status, block_status, error,
                                                  # api_calls, ctx_peak, tokens_in/out, cache_read, mode, kind, summary
```

Take, without duplicates:
- the 2 longest runs (wall time) and the 2 with the highest `ctx_peak`;
- the 2 shortest finished runs (did they finish fast, or bail early?);
- every failed run, and runs whose task went to `needs_you` (cap 3);
- the 2 tasks with the most human comments (the human had to steer: the context was missing something).

If there are fewer than 3 runs in the window, write a short report saying so and stop.

## 2. Read each run

- What the agent was given: `conveyor task brief KEY --preview` (brief + first prompt), and the run's `prompt` field (resume prompts).
- What it did: the transcript at `~/.claude/projects/<cwd with / replaced by ->/<session_id>.jsonl` (cwd is the primary worktree under `~/.conveyor/workspaces/KEY/`), or `conveyor task log KEY`. Skim; look for the turns listed below.
- The thread: human comments after the run show what it got wrong or missed.

For each run, note:
- **Outcome**: done / failed / needs_you, and whether the human agreed (later comments, PR merged?).
- **Waste**: re-reading files the brief already summarised, exploring to find things the brief or CLAUDE.md could have named, repeated failing commands, long tool outputs, `ctx_peak` far above what the change needed.
- **Missing context**: questions the agent asked, wrong assumptions, repo conventions it broke (tests, branch names, PR format), skills/commands it should have used but didn't.
- **Unused context**: brief sections or rules the agent never needed across the sample (candidates to cut).

## 3. Check the logs (recent only)

`~/.conveyor/conveyor.log` (+ `conveyor.log.1`), `~/.conveyor/logs/dispatcher.log`, `~/.conveyor/logs/hook.log`: errors and warnings from the last 7 days, grouped by message. Launch failures, hook errors and reaps explain bad runs that the transcript can't. Match what you find against `docs/troubleshooting.md` in the Conveyor repo: a known failure gets its recipe applied or cited, not a new diagnosis.

## 4. Decide what to change

Where each kind of fix belongs:

| Finding | Fix | How |
|---|---|---|
| Brief/prompt wording, a rule every task needs, a section nobody uses | Conveyor's defaults in `internal/brief/brief.go` (`defaultRules`, `DefaultFresh`, `DefaultResume`, the status block) | Edit in your worktree, refresh the golden file with `go test ./internal/brief -update`, open a PR |
| A rule only this user wants | `brief_extra_rules`, `prompt_fresh`, `prompt_resume`, `resume_threshold` settings | Put the exact `conveyor config set ...` command in the report; do not run it |
| A convention of one repo the agent kept missing | That repo's `CLAUDE.md`, a repo skill or agent | `conveyor task new "CLAUDE.md: <what>" --repo <repo> --desc "<the exact text to add and the runs that show why>"` (backlog; the human starts it) |
| A lesson about the Conveyor repo itself | `learning:` line in your status block | It shows up on the Learnings page |
| A Conveyor bug (dispatch, hooks, reaping) | Code fix | Small fix + test in the same PR, or a backlog task if it is not small |
| A new failure mode with a manual fix | `docs/troubleshooting.md` | Add a recipe (symptom, confirm, fix) in the same PR |

Rules for proposals:
- Every change cites the runs that motivate it (task key + run number). One run is an anecdote; prefer patterns seen in 2+ runs.
- Prefer deleting or tightening text over adding it. Each line in the brief is paid for on every launch.
- Keep the PR small: at most 3 prompt changes per week, so their effect can be seen in next week's sample.
- Before proposing a change, check last week's report (the previous "Conveyor doctor" task's thread and PR) and say whether its changes helped.

## 5. Report

Final reply (it becomes the task's summary): a table of sampled runs (key, run, outcome, duration, ctx_peak, one-line verdict), then findings, then what you changed (PR link), the settings commands you suggest, and the backlog tasks you created. End with the Conveyor status block.
