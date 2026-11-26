# Playbook: superpowers

Maps [obra/superpowers](https://github.com/obra/superpowers) onto a Conveyor task or epic. Its skills form a pipeline: brainstorming, writing-plans, then subagent-driven-development. Load those skills if installed; this file says where each step lands.

| superpowers | Conveyor |
|---|---|
| `brainstorming`: questions, alternatives, design in sections, saved design doc | the Approach (default step 1). On an epic: the `chart` stage grilling, design doc linked from the root description |
| `writing-plans`: tasks naming spec, files, failing test, passing test, stop point | on an epic: `epic_kind:build` sub-tasks (one repo, AC ids, spec link in `--desc`), ordered with `conveyor task link A blocks B`. On a single task: a checklist in the Approach |
| `subagent-driven-development`: fresh subagent per task, spec-compliance review then code-quality review | a build task's review loop (default step 3), run in that order. Do not dispatch a subagent for work that is its own Conveyor sub-task |
| `executing-plans`: batches with human checkpoints | `needs_you` between batches: end the run without a status block to ask |
| `test-driven-development`, `verification-before-completion` | the build rule: failing test first, run the tests before `status: done` |

Not mapped: `using-git-worktrees` and `finishing-a-development-branch`. Conveyor creates the worktree and the human merges; never merge or push to the default branch.
