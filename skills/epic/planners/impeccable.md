# Playbook: impeccable

Maps [pbakaus/impeccable](https://github.com/pbakaus/impeccable), a design skill for frontend work, onto a Conveyor task or epic. It runs as `/impeccable <command>` and reads `PRODUCT.md` and `DESIGN.md` from the repo. Load the `impeccable` skill if installed; command names vary by release, so use the ones it lists.

| impeccable | Conveyor |
|---|---|
| `document`: write `DESIGN.md` from existing code | a first task on a repo without one |
| `shape`: plan the UX and UI before code | the Approach (default step 1): the `approach.svg` shows the screens or components changed |
| build with the design context loaded | implement (default step 2), test-first |
| `audit`, `polish`, `typeset` and similar | the review pass (default step 3), before `status: done`; fix what is valid, at most two rounds |
| the local detector (anti-pattern rules) | a check before opening the PR; report what it flagged in the summary |

Use it for UI work only; for backend tasks pick another playbook.
