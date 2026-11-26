# Playbook: pstack

Maps pstack ([michael-denyer/pstack-claude](https://github.com/michael-denyer/pstack-claude), a port of Lauren Tan's skill stack) onto a Conveyor task or epic. Its entry point is `poteto-mode`, which picks one of its playbooks (Feature, Bug fix, Refactoring, Multi-phase plan, Investigation, Prototype and others) and runs the skills it names. Load `poteto-mode` if installed; this file says where its steps land.

| pstack | Conveyor |
|---|---|
| `poteto-mode` picks the playbook | the task's description picks it; state the choice in the Approach |
| `architect`: types, signatures and module shape before code | the Approach (default step 1) |
| `interrogate`, `thermo-nuclear-review`, `thermos` | the review loop (default step 3); fix what is valid, at most two rounds |
| `tdd` | failing test first (the build rule) |
| `deslop`, `no-comments`, `unslop` | a pass over your own diff before the review loop |
| Multi-phase plan | an epic: phases become `epic_kind:build` sub-tasks with `blocks` links |
| Investigation, `how`, `why` | `epic_kind:research`: findings in the status block, no PR |
| `babysit`, `fix-ci`, `fix-merge-conflicts`, `get-pr-comments` | Conveyor's own PR-watch runs and review-comment wakes; do not poll |
| `make-pr-easy-to-review`, Opening a PR | the repo's create-pr skill |
| Shipping, Autopilot, `arena`, `swarm` | not supported: no agent merges, and parallel work is a sub-task per repo |

Playbook step files (`playbooks/<name>.md` upstream) are read from the installed plugin, not copied here.
