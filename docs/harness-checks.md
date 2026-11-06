# Deterministic harness checks

A weaker model in a good harness beats a stronger model in a bad one. Anything the agent is told to
"remember to check" and that a script can check should be a script. This note ranks the candidates
by measured waste in recent runs and records which ones are built.

## Built: the Stop completion gate

`internal/hook/complete.go` (base from CV-84A; this task added the changeset and tests-after-edit checks). On the Stop of an `auto` run whose status block says `status: done`, the
hook checks the facts below. If any fail, it prints `{"decision":"block","reason":...}`: Claude
Code keeps the session going with the list as its next prompt, and the run stays `running`. Claude
Code sets `stop_hook_active` on the retry's Stop; that one is never checked again (no loops). Each
block records a `completion_check` event. A check that cannot run (PR refresh failed, git error, no
transcript) is skipped, never failed.

Per task repo with a worktree:

| Check | How | Only when |
|---|---|---|
| Worktree clean | `git status --porcelain` | always |
| Branch pushed | `HEAD == refs/remotes/origin/<branch>` | commits ahead of `origin/<default>` |
| PR exists | task repo has a PR URL after the Stop's PR refresh | commits ahead; not prototype/research tasks |
| Changeset added | `git diff --name-only base...HEAD` has a `.changeset/*.md` | repo has `.changeset/config.json` and the branch changes code files |
| Repo CLAUDE.md loaded | `InstructionsLoaded` hook event (recorded as an `instructions_loaded` event) since the run started, or a `Read` of it in the transcript | repo that was changed |

Per run (transcript):

| Check | How |
|---|---|
| Skills from the brief loaded | a `Skill` tool call per `skill:<name>` label, plus `epic` for epic tasks (`plugin:<name>` counts) |
| Tests ran after the last code edit | no `Edit`/`Write` of a code file after the last Bash command containing test/build/vet/lint/tsc/check/make |

## Measurements (last 120 auto sessions, 458 `done` runs in the DB, 2026-11-06)

| Signal | Count |
|---|---|
| Bash calls denied by the user-level RTK hook and retried | 810 in 113 of 120 sessions (~7 extra model turns per session) |
| `done` runs that edited code after their last test/build command | 22 of 102 |
| `done` runs that edited code and ran no test/build at all | 16 of 102 |
| `done` runs whose state says `tests: not run` | 222 of 458 |
| `done` runs with an unchecked criterion | 156 of 458 |
| Extra re-reads of a file already read in the session | 151 (136 files) |
| Exact duplicate Bash commands | 299 |
| Tool results over 20k chars | 20 |
| Runs without a status block | 71 of 567, nearly all deliberate questions to the human |
| `git push` to main/master | 0 in 521 sessions |

## Ranked candidates

| # | Check | Where | Cost | False-positive risk | Status |
|---|---|---|---|---|---|
| 1 | Stop RTK deny-and-retry: `~/.claude/rtk-shims/rtk-hook-deny-wrapper.sh` turns every RTK rewrite into a denied call plus a retry, the largest waste measured. Re-test whether Claude Code now applies `updatedInput`; if so, go back to plain `rtk hook claude`. Otherwise limit the wrapper to rewrites that save real output (git, test runners), not `cat`/`ls`/`sed` | user config, not this repo | none | none | for the human |
| 2 | Completion gate: clean, pushed, PR, CLAUDE.md, skills | Stop | 3-4 git calls, only on `done` | low: skipped when it can't run, one round only | built (CV-84A) |
| 3 | Tests after the last code edit | Stop (gate) | transcript scan, already read | medium: keyword heuristic; edits via `sed -i` are missed | built |
| 4 | Changeset present | Stop (gate) | one `git diff` | medium: changes that need no release; the message says to explain in the summary | built |
| 5 | Per-run waste metrics (hook denials, re-reads, duplicate commands, large results) saved on the run at Stop, shown by `task show --runs` and read by conveyor-doctor | Stop | transcript scan | none (measurement, never blocks) | built (CV-88A): runs `hook_denials`, `rereads`, `dup_bash`, `big_results` |
| 6 | Edits outside the task's worktrees | PreToolUse on Edit/Write (deny, allow the scratchpad, `/tmp`, `~/.claude`) | path prefix check | low | sub-task |
| 7 | Stuck loop: the same failing Bash command 3 times in a row | PreToolUse (Bash), state in a per-session file | small file read/write per call | low | sub-task |
| 8 | PR body has the repo template's sections | Stop (gate), reads `.github/pull_request_template.md` headings | 1 gh call | medium: templates vary | later, after 5 shows it matters |
| 9 | `done` with unchecked criteria, or `tests: not run` after code edits | Stop (gate) | none | high: partial work is often legitimate | not planned; 3 covers the useful part |
| 10 | Re-read of an unchanged large doc | PreToolUse on Read | stat + per-session state | medium: context compaction drops earlier reads | not planned until 5 measures it per run |
| 11 | Push to the default branch | PreToolUse (Bash) | regex | low | not planned: 0 occurrences, branch protection covers it |
| 12 | Missing status block | Stop | none | high: a missing block is how agents ask questions | not planned |
