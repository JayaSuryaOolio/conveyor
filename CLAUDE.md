# Conveyor worktree setup

Facts about a fresh worktree of this repo (checked against `.gitignore`, `main.go`, `SKILL.md`). Full operating guide: `.claude/skills/conveyor/SKILL.md`.

- **`frontend/dist`**: `main.go` embeds `all:frontend/dist`. Only `.gitkeep` is tracked there, so `go build ./...` and `go test ./...` work in a fresh worktree; the real UI needs `cd frontend && npm ci && npm run build`. After a build, `git checkout -- frontend/dist/.gitkeep frontend/package.json.md5`.
- **`frontend/node_modules`**: absent in a fresh worktree. Run `npm ci` in `frontend/` before `tsc`, vite or `npm run build`.
- **`frontend/wailsjs`**: gitignored and generated, never committed or hand-edited. Missing in a fresh worktree; run `wails generate module` after a binding change in `internal/ui`, then `git checkout -- frontend/wailsjs/runtime` if it shows as modified.
- **`wails`**: installed at `~/go/bin/wails`, which may not be on `PATH`; call it by full path.
- **`origin/main`**: the local `main` branch can be stale. Compare and rebase against `origin/main` (`git fetch origin` first), not `main`.
- **graft**: only for tools bound to the repo root checkout (Tilt, an IDE). Skip it when tests run in the worktree. Usage and the `--stash-root-changes` flag are in the SKILL.md "Testing in the repo root (graft)" section; always `graft stop` afterwards.
