# Harness ledger

One entry per problem the harness tries to fix, with every attempt (merged, failed, reverted). conveyor-doctor reads
all of it before proposing a change, so a fix already tried is not proposed again. Seeded from
`docs/harness-checks.md` (CV-84A, CV-88) and `docs/learnings-review.md` (CV-81A); measurements are from those notes.

Attempt columns:

- **PR**: PR number or task key. A closed or reverted PR stays, with Observed `failed: <why>`.
- **Reason**: what the change should fix, in one line.
- **Predicted runs**: the effect expected on runs, written before merge (a run column or outcome, with a direction).
  Seed rows predate the ledger and say `not recorded`.
- **Observed**: the measured result, filled in by conveyor-doctor after enough runs. `pending` until then.
- **Model, Date**: the model that wrote the attempt and the date it merged or failed.

Judge an attempt by held-out outcomes, not by the run it was written from: merged without rework, human steering
comments, requeue rate. Add a row for your own PR in the PR itself (prediction) and fill Observed later.

## Agents ship incomplete work (dirty tree, unpushed, no PR, no changeset, CLAUDE.md or skills unread)

Measured 2026-11-06: 22 of 102 `done` runs edited code after their last test or build; 16 of 102 ran none.

| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-84A | Stop completion gate: clean, pushed, PR, changeset, CLAUDE.md, skills | not recorded | pending | unknown | 2026-11 |
| CV-112D1 (#355) | Move the gate into `internal/harness` (Facts, pure checks) so new checks land in one package | none (refactor); no change in gate blocks | pending | unknown | 2026-11 |
| CV-112D4 | Per-repo `check_command`: tests-after-edit counts only Bash calls containing it, so `go build` or `ls build` no longer clears a stale edit | fewer `done` runs with code edited after their last real test run in repos that set it; more untested-edit blocks at first, then fewer | pending | Sonnet 5.5 | 2026-11 |

## Wasted turns and context (denied hooks, re-reads, duplicate commands, huge results)

Measured: 810 RTK-denied Bash calls in 113 of 120 sessions; 151 re-reads; 299 duplicate Bash commands; 20 results over 20k chars.

| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-88A | Save `hook_denials`, `rereads`, `dup_bash`, `big_results` per run (measurement only) | not recorded | pending | unknown | 2026-11 |

Open, not attempted: stop the user-level RTK deny-and-retry wrapper (largest waste; user config, for the human).
Stuck-loop check (same failing Bash 3 times) and re-read of an unchanged large doc are not planned until the metrics
above show them per run.

## Agents write outside their worktrees

| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-88B | `permissions.blockWritesOutsideWorkingDirectories` plus `additionalDirectories` in launch settings | not recorded | pending | unknown | 2026-11 |

## Learnings pipeline lets junk in (A1-A5)

Snapshot 2026-11-04: 312 rows, 214 still `proposed`, about 40% one-task recaps, 55 duplicates of applied facts.

| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-81A | Analysis only: reject junk in bulk; stricter `learning:` brief line; drop `none`; Conveyor `CLAUDE.md` setup block | not recorded | pending | unknown | 2026-11 |

Proposed in CV-81A, not yet attempted here: parser drop of `none`/`n/a`, bulk reject by first 40 chars, auto-reject
of the duplicate classes. Fix the source (put the fact where agents read it) instead of fuzzy dedupe.

## Red CI and merge conflicts not reaching the agent

| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-112D3 | Review check lists failing CI checks in its system comment (deduped per head sha) and stores them in `epic_flow_prs.checks` | fewer human "CI is red" steering comments; requeue after red CI without a human comment | pending | Sonnet 5.5 | 2026-11 |

## Missing skill labels

| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-112D5 | `launchTodo` parks a task in needs_you, with a system comment naming the skill, when a `skill:*` label does not resolve | no runs that start without the skill the brief tells them to load; no gate block for a skill that was never loadable | pending | Sonnet 5.5 | 2026-11 |

## Rejected ideas (do not re-propose without new evidence)

- `done` with unchecked criteria or `tests: not run`: partial work is often legitimate; the tests-after-edit check covers the useful part.
- Missing status block as a failure: it is how agents ask the human a question.
- Push-to-default-branch check: 0 in 521 sessions, branch protection covers it.

## A failed run is invisible to the next one

A retry's brief showed run summaries only, so the cause of the previous failure was not in its context.

| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-112D6 | `brief.history()` shows each run's `block_status` and clipped `error` | fewer repeat failures on retried tasks (second run fails for the same reason); requeue rate on `failed` tasks down | pending | Sonnet 5.5 | 2026-11 |

## Model chosen without evidence (no per-run model, no difficulty grade)

| PR | Reason | Predicted runs | Observed | Model | Date |
|---|---|---|---|---|---|
| CV-112D13 | Record `runs.model` at launch; `spec` grades build children easy/medium/hard and sets `--model` through `conveyor harness grade`; doctor compares outcomes by (runner, model) | easy-graded children on sonnet (claude) fail or requeue no more often than medium ones; `hard` on opus needs fewer runs per task than `hard` on sonnet did; `runs.model` is set on every new auto run | pending | Sonnet 5.5 | 2026-11 |
