# Conveyor — Progress

Master index of implementation tasks. The source of truth is `docs/tech-spec.md`; each task's spec is in
`docs/tasks/NNN-*.md`, and its outcome notes go in an **Outcome** section appended to that file.
Update this table when a task starts, finishes, or changes branch.

| # | Task | Wave | Depends on | Branch | Status | PR |
|---|------|------|------------|--------|--------|----|
| 000 | Repo scaffold (v1) | – | – | merged | Superseded by 002 | – |
| 001 | Data layer (v1) | – | – | merged | Superseded by 002 | – |
| 002 | [Store v2, core, CLI skeleton](../docs/tasks/002-store-core-cli.md) | 1 | – | `task/002-store-core-cli` | Todo | |
| 003 | [Workspace + worktrees](../docs/tasks/003-workspace.md) | 2 | 002 | `task/003-workspace` | Todo | |
| 004 | [Hooks, status block, transcript](../docs/tasks/004-hooks-statusblock-transcript.md) | 2 | 002 | `task/004-hooks` | Todo | |
| 005 | [Claude runner](../docs/tasks/005-claude-runner.md) | 2 | 002 | `task/005-claude-runner` | Todo | |
| 006 | [Desktop shell (Wails)](../docs/tasks/006-desktop-shell.md) | 2 | 002 | `task/006-desktop-shell` | Todo | |
| 009 | [PR tracking](../docs/tasks/009-pr-tracking.md) | 2 | 002 | `task/009-pr-tracking` | Todo | |
| 007 | [Dispatcher, brief, daemon](../docs/tasks/007-dispatcher.md) | 3 | 003 004 005 009 | `task/007-dispatcher` | Todo | |
| 008 | [Workspace panel, terminal](../docs/tasks/008-workspace-panel.md) | 3 | 003 005 006 | `task/008-workspace-panel` | Todo | |
| 010 | [Automations, Jira import](../docs/tasks/010-automations.md) | 4 | 007 | `task/010-automations` | Todo | |
| 011 | [Learnings](../docs/tasks/011-learnings.md) | 4 | 004 006 007 | `task/011-learnings` | Todo | |
| 012 | [E2E + README](../docs/tasks/012-e2e-and-docs.md) | 5 | all | `task/012-e2e` | Todo | |

## Conventions

See tech-spec §13. In short: one worktree, branch and PR per task, cut from up-to-date `main`; rebase before
opening the PR; a task's PR is merged before its dependents start.
