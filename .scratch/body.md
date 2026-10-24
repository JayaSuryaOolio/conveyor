Spec: docs/tasks/011-learnings.md

## Built
- Learning store queries and `internal/learning` (List, Accept, Reject, Edit, Apply, Personal).
- `core.SpawnChildren`, called from `hook.Handle` so spawn lines become backlog child tasks with the parent's repos.
- `conveyor learning list|accept|reject|edit|apply` CLI.
- Learnings page with a nav badge, and the bound methods behind it (bindings regenerated).

## Tested
gofmt, go vet, go test ./... pass. New tests cover state transitions, Apply grouping and description, and spawn children. `npm ci && npm run build` and `wails build` succeed. The GUI was not exercised by hand.

## Out-of-scope follow-ups
- Personal learnings stay `accepted` with no "mark pasted" action.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
