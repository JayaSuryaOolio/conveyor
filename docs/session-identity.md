# Run and session identity

A recurring failure, seen on CV-9 run 13, CV-46 run 148 and CV-56 run 183: **a resumed run never ends, and an extra "interactive" run appears for the same work.** This page explains why it happens, which invariants Conveyor relies on, and how to fix and prevent it. The step-by-step recipe is in [troubleshooting.md](troubleshooting.md#a-resumed-run-is-adopted-by-a-new-session-id).

## Three identifiers, one run

| Id | Set by | Meaning |
|---|---|---|
| `run.id` | Conveyor | One launch attempt of a task. What the UI shows as "Run N". |
| `run.bg_id` | `claude --bg` output, saved by the dispatcher | The background agent that was launched for this run. Stable for the agent's life. |
| `run.session_id` | The first hook that matches (or the reaper's adoption of a `launching` run) | The Claude conversation the hooks report. **Not stable across a resume.** |

Conveyor assumes `bg_id` and `session_id` name the same thing. That holds for a fresh launch. It does not hold for a resume.

## How hooks find their run

Hooks (`UserPromptSubmit`, `Stop`, `Notification`) carry only the `cwd` and a `session_id`. `internal/hook.resolveRun` maps them to a run in this order:

1. The newest run whose `session_id` equals the hook's (resumes are supposed to share an id, so newest wins).
2. A run still `launching` with no session id: adopt it and record the id.
3. Otherwise **create an `interactive` run**, on the assumption that a human started a session by hand in the worktree.

Step 3 is the trap. It is correct for a manual session and wrong for a background session Conveyor started itself but could not recognise.

## What goes wrong on resume

`claude --bg --resume <old-session>` can start the work as **a copy under a new session id** instead of continuing the old one (Claude Code behaviour, not ours). The sequence on CV-56:

```
run 183 (resume)  bg_id=4417568e  session_id=106b7dbf…   <- dispatcher saved the OLD id
agent 4417568e    sessionId=4417568e-5cde…               <- what the hooks report
hook Stop 4417568e  -> step 1 no match, step 2 no launching run, step 3 creates run 184 (interactive)
run 183           no hook ever matches -> stays "running", task stays "running"
```

Consequences:

- Run 183 never gets its `Stop`, so its summary, status block and the task move to review/done are lost.
- Run 184 receives the status block and shows `block_status: done`, but its status stays `running` (interactive runs end only when their session leaves the agents list).
- The task cannot be marked done by the run that did the work, and the UI shows a second, unexplained session.
- A reply to the human can still be correct; only Conveyor's bookkeeping is wrong. The agent is not at fault.

Before CV-58 the reaper (`dispatch.reap`) did not repair this. It only adopted a session id for runs that are `launching` with no id; a resumed run was already `running` with the old id, so it was skipped. The bug stayed latent until the session goes away, at which point run 183 is failed as "session vanished" and 184 is finished, which hides the real outcome.

An earlier variant of the same family (CV-9 run 13) was a resume woken without `--settings`, so no hook fired at all. That is prevented since CV-47. This page covers the case where hooks fire but name an id we do not know.

## Invariants to keep

1. **A run's `session_id` must be updated whenever its `bg_id` agent reports a different one.** `bg_id` is the stable key for runs Conveyor launched; `session_id` is a cache of what the hooks say.
2. **A hook for a session that belongs to a known `bg_id` must never create an interactive run.** Interactive runs are for sessions Conveyor did not launch.
3. **Resolve by the strongest identity first:** `session_id`, then `bg_id` (the agents list maps `bg_id` to its current `sessionId`), then adoption, then interactive.
4. **Never fix this by hand-editing `conveyor.db`.** Use the recipe, which replays the hook through the same code path a live one takes.

## Prevention (since CV-58)

- `hook.resolveRun` adopts the newest `launching` run even when its `session_id` is already set. A resume run starts with the old id as a guess; the first hook (`UserPromptSubmit`, fired while `claude --bg --resume` is still starting) replaces it with the id Claude really uses.
- `dispatch.reap` sets `session_id` from the agents list (`alive[bg_id].SessionID`) for every active non-interactive run whose id differs, so a hook that arrives after the run left `launching` still matches in step 1.
- The task sidebar keys each run's attach/resume action by run id (`attach:<run id>`, `resume:<run id>`), not by session id, which resumed runs share. Before, every run of one session got the newest run's action, so "attach" on an older run opened the latest one.

Tests: `TestResumeAsCopyAdoptsLaunchingRun` (hook), `TestReapFollowsResumedSessionID` (dispatch).

## Detecting it

Any one of these:

- A run with `mode=resume` whose `bg_id` is not the first 8 characters of its `session_id`.
- An `interactive/fresh` run created seconds after a `resume` launch in `conveyor.log` (`launch KEY: run N (resume) bg X`), with `session_id` starting with `X`.
- `hook.log` shows hooks for a session id that no run of the task lists as the `session_id` of a non-interactive run.
- The task stays `running` after the agent visibly finished.

`conveyor-doctor fix KEY` applies the recipe and checks these.
