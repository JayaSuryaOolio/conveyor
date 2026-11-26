# Loop engineering

How Conveyor gets more than one agent to look at a task (architect, engineer, tester, reviewer) before the human does. A working document from CV-228, written 2026-11-26; improve it as the runs teach us.

## The problem

A task has one agent and its runs go straight from brief to code to PR. Nothing puts a second, independent agent in front of the diff, so the human and the PR reviewers do that job one round at a time.

## What exists today

- **Agent is per task, not per run.** `tasks.agent` is read at every launch (`dispatch.spec` in `internal/dispatch/dispatch.go`); `runs` has no agent column. Changing the agent between runs works, but only from the app (`SetTaskAgent`): there is no `task set --agent`.
- **Loops Conveyor already runs:**
  - the Stop completion gate (`internal/hook/complete.go`), which blocks a `status: done` once and feeds the gaps back;
  - the PR review check, which requeues the task on bot or human feedback;
  - the epic `verify` stage (`docs/epic-builder.md`);
  - sub-tasks with `--agent`, `blocks` links and `task message --reply`.
- **Most tasks have no agent:** 145 of the 248 created in the 30 days before 2026-11-26. A "team" a task must be assigned to would sit unused; a default in the brief reaches every task.

## Would a loop have helped?

A skim of the first 230 characters of each human comment on the five most-steered tasks of those 30 days. A rough read, not a measurement.

| Task | Human comments | What they were | Would a role loop have helped? |
|---|---|---|---|
| CV-117B | 13 | DDD layering and module ownership corrections | Yes: an architect reviewer on the first diff would catch about half |
| CV-209C1 | 9 | "Overcomplicated", relayed reviewer requests | Partly: 2-3 comments |
| CV-225A | 10 | UI bugs the human found from screenshots | Yes: a tester driving the UI would catch 2-3 |
| CV-181 | 20 | Requirement discovery and data questions | Mostly no: the human's domain knowledge |
| CV-174E | 20 | "Run the test", token and env updates | No: operational |

A loop addresses roughly a quarter to a third of the steering, in two places: an independent review of the first diff against the repo's conventions, and actually exercising the result. `docs/research/cv-117b-117i-study.md` covers the rest (approach agreed up front, durable decisions).

## What we do now: the loop inside one run

Decided on CV-228: a single run spawns subagents for the loop. The brief carries one rule (`loopRule` in `internal/brief/brief.go`), and the run decides whether its stage calls for it.

The recommended loop, before `status: done`:

1. **Review.** A subagent that has not seen the run's reasoning reads the diff against the brief and the repo's conventions. Use the repo's reviewer or architect agent when it has one, else a general one.
2. **Exercise.** When the change has behaviour to run, a subagent runs it: the tests, the UI.
3. **Fix** what is valid. At most two rounds.
4. **Report** what the loop found in the summary.

No loop when the run:

- only addresses PR review comments (the reviewer is already in the loop);
- makes a small fix to a nearly done task;
- changes no code.

The architect's turn up front is the first-run Approach step, which already exists.

Known weaknesses:

- The engineer reports on its own review: it chooses which findings are valid.
- The exchange is not in the task thread, only in the run's summary and transcript.
- Nothing enforces it. The Stop gate does not check that a loop ran.
- A long tester pass spends the run's own wall time.
- "Nearly done" is the run's own judgement; a resumed run that writes real code can talk itself out of the loop.
- The Stop gate's untested-edit check (`harness.UntestedEdit`) reads the run's own tool calls, so after a loop fix the run itself must rerun the tests; a tester subagent's run may not count.
- The rule is written in Claude Code terms (subagents, repo agents) but goes into every runner's brief, `agy` included.

## How we will know

`conveyor-doctor` compares, before and after the rule:

- human comments per task, and how many are shape or correctness corrections a reviewer could have made;
- PR review rounds per task;
- whether summaries name loop findings, and whether runs skip the loop when the rule says to.

## Later, if the in-run loop is too weak

- **Enforce it.** The Stop gate checks that a reviewer subagent ran when the run changed code, the way it checks that the brief's skills were used.
- **Choose the roles per task.** A `review:<agent>` label, like `skill:<name>`.
- **Per-run agents.** A `runs.agent` column and a `loop:<a>,<b>` label: after an engineer run ends done, the dispatcher launches a fresh run under the reviewer agent, whose reply is posted as a comment that requeues the engineer, with a round cap. This puts the back-and-forth in the thread. It costs a brief per role, resume rules so a reviewer session is never resumed as the engineer, and a thread that shows who spoke.
- **Teams.** An ordered list of roles, which the `loop:` label value already expresses. Not an entity until per-run agents exist.

Reassigning a task between agents, Jira style, adds nothing by itself: agents keep no memory between runs, so a reassignment is a fresh run with a different prompt. The board holds up; the agent loop sits between `running` and `review`.
