# Playbook: default

The simple loop every task follows when it has no `playbook:*` label: architect, implement, review. It is the brief's own rules, written as steps. For an epic root the stage loop in `SKILL.md` runs instead, with `wayfinder` as the planner.

1. **Architect.** On the first run, before any code, reply with an Approach and no status block: `approach.svg` (lanes for the layers touched, numbered flow arrows, new parts vs reused paths, open decisions as callouts) plus a few lines for new names, open decisions and risks. The task waits in `needs_you` until the human acks; record the approach as `decision:` lines.
2. **Implement.** Test-first: write the failing test, then the minimum code. Follow the repo's CLAUDE.md, skills, commands and agents over generic defaults. One PR per repo, never to the default branch, never merged by you.
3. **Review.** Before `status: done`, have a subagent that has not seen your reasoning review the diff against the brief and the repo's conventions (the repo's reviewer or architect agent if it has one), and one exercise the change when it has behaviour to run. Fix what is valid, at most two rounds, and put what it found in the summary. Skip the loop for review-comment fixes or small fixes to a nearly done task.
