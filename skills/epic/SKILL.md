---
name: epic
description: Run a Conveyor epic or one of its steps - the stage loop (chart, gate, decide, spec, build, verify, done), the checklist and the epic commands. Use when your brief says "Load the epic skill", when the task has an `epic` or `epic:<name>` label, or when asked to plan or drive an epic in Conveyor.
---

# Epic

An epic is a root task labelled `epic` and `epic:<name>`; every task in its tree carries `epic:<name>` too. The design is `docs/epic-builder.md` in the Conveyor repo. This skill is installed and removed with Conveyor's hooks (`conveyor hooks install`).

## Never merge

No agent merges a PR, in any form, ever. The human always merges. (`gh pr merge` is denied by the hook in epic runs; do not work around it.)

## State

You remember nothing between runs. **Start every run with `conveyor epic status ROOT --json`** (ROOT is the root key from your brief). If it shows no flow while the stage is `decide` or later, tell the human: `conveyor epic restart ROOT` starts flow 1. That is the state: flow, stage, tasks, PRs, AC coverage and the recent audit tail. STATE.md in the workspace is only a scratchpad.

Then run `conveyor epic check ROOT` and fix what it prints (`--fix` repairs labels only). Run it again before your status block.

## Which run are you?

- **Root run** (your task has the bare `epic` label): you drive the stages below.
- **Child run** (you have `epic:<name>` but not `epic`): do your step by the `Kind` rule in your brief, record the outcome where the rule says, end with the status block. Do not change stage labels or other tasks.

## Stage loop (root run)

The stage is the root's `epic_stage:<stage>` label; none means `chart`. Move it with `conveyor task label ROOT set epic_stage:<next>`, never by adding a second one. The planner adapter (`planners/<name>.md`, from `epic_planner:<name>`, default `wayfinder`) says how to write the map and create tickets.

| Stage | Do | Move on when |
|---|---|---|
| `chart` | Grill the human on the destination and the acceptance criteria. Write the map into the root's description (`conveyor task set ROOT --desc ...`): Destination, `## Acceptance criteria` as AC1, AC2, ..., Decisions so far, Not yet specified, Out of scope. Create the decision tickets (`epic_kind:grilling`, `prototype` or `research`) and their `blocks` links. Ask with `AskUserQuestion`: Conveyor's hook posts the questions to the root's thread as an agent comment. | Map and AC written. Then the **gate**. |
| gate | End the run in `needs_you` with the plan summarised in the status block (omit the block to ask). The human approves with the Approve button, `approve` comment or `conveyor epic approve ROOT`, which moves the stage to `decide`. With `epic_gate:off` there is no stop: set `decide` yourself. | Approved |
| `decide` | Move decision tickets to `todo`; the dispatcher runs the frontier. Read each resolution and add tickets it calls for. | No open grilling, prototype or research child |
| `spec` | Write the spec (link it from the root). Create `epic_kind:build` sub-tasks: exactly one repo, `repo:<name>`, an agent, the AC ids they cover in the description, a link to the spec, `skill:<name>` labels if useful, `blocks` links for order. Move them to `todo`. | Build tickets exist |
| `build` | Nothing to do while children run; you are woken when the last open child settles. Unstick failed or `needs_you` children by commenting on them. | Every build child done or cancelled |
| `verify` | Check every AC against the merged work. Tick an AC only with evidence (PR, test or commit). Record the evidence with `conveyor epic log ROOT "<AC: PR/test/commit>"`. Gaps become new build tickets and the stage goes back to `build`. | All AC met |
| `done` | `conveyor task move ROOT done`. | |

You do not run while any child is running or in `todo`; Conveyor wakes the root with a system comment naming its stage. Never restart a flow: only the human does (`conveyor epic restart`).

## Kinds

| Kind | Human? | Output |
|---|---|---|
| `grilling` | yes, ask and wait in `needs_you` | a decision line on the root's Decisions so far; no PR |
| `prototype` | yes, the human looks and reacts | a throwaway branch or notes; never a PR to merge |
| `research` | no | findings in the status block and on the root; no PR |
| `build` | only review and merge | one PR, test-first, citing AC ids |

## Checklist

Run at the start and before the status block. `conveyor epic check ROOT` does the mechanical items.

1. **Epic label.** Every task in the tree has `epic:<name>`; no task outside it does.
2. **Repo labels.** Each task's `repo:*` labels equal its repos. A build task has exactly one repo.
3. **Acceptance criteria.** The root's `## Acceptance criteria` is a numbered checklist (AC1, AC2, ...). Every AC is cited by at least one build task, and is ticked only with evidence.
4. **PRs.** Every build task in `review` or `done` has a PR on the flow (`conveyor epic prs ROOT [--commits]`; `conveyor epic search ROOT <regex>` finds where something changed).
5. **Sessions.** Recall why something was done with `conveyor epic sessions ROOT [--grep <text>]` instead of asking the human again.
6. **Kinds.** Every ticket you add mid-run, including follow-ups, has an `epic_kind:<kind>` and its AC ids. Children spawned by a member's `spawn:` lines join the epic automatically (`epic:<name>`) but have no `epic_kind`: on wake, reconcile each one. Assign `epic_kind` and AC ids, update the spec and AC map, add `blocks` links, and move it to `todo` if it should auto-start.
7. **Order and agents.** No `epic_kind:build` task in `todo` lacks an agent, a repo or AC ids.
8. **Audit.** Hooks record stage changes, tasks, labels, gates and restarts. Add the reason for a decision with `conveyor epic log ROOT "<why>"`.

## Commands

```
conveyor epic status ROOT [--flow N] [--json]   # the state; start here
conveyor epic check ROOT [--fix]
conveyor epic prs ROOT [--commits] [--json]
conveyor epic search ROOT <regex>
conveyor epic sessions ROOT [--grep <text>]
conveyor epic flows ROOT
conveyor epic log ROOT "<why>"
conveyor task graph ROOT [--json]
conveyor task label KEY set <prefix>:<value>     # replaces only that prefix
conveyor task label KEY add|rm NAME
conveyor task new "title" --parent KEY --repo R --desc "..." [--agent A] [--todo]
conveyor task link A blocks B
conveyor task set KEY --desc "..."
conveyor task move KEY todo|done
```
