# 000 — Repo scaffold

**Phase**: 0 (PLAN.md) · **Branch**: `project-setup` → merged to `main` at `ba2cae8` · **Status**: Done

## What

- `go.mod` at `github.com/JayaSuryaOolio/conveyor`, Go 1.25.
- `cmd/conveyor/main.go`: `conveyor serve [--port N] [--db path]`, default port `8420`,
  default DB path `~/.conveyor/conveyor.db`.
- `internal/store/store.go`: `Open(path)` opens SQLite via `modernc.org/sqlite` with
  `_pragma=busy_timeout(5000)` in the DSN from the start (PLAN.md explicitly calls out not
  inheriting worktree-studio's zero-busy-timeout gap), `SetMaxOpenConns(1)`.
- chi router with `GET /healthz` → `200 ok`.
- `.gitignore` for the built binary, `*.db`, `.conveyor/`, and future `node_modules`/`web/dist`.

## Checkpoint (verified)

Built the binary, ran `conveyor serve --port 8421 --db /tmp/conveyor-test.db`, confirmed
`GET /healthz` returns `200` and the DB file is created on disk.

## Deliberately not done here

`internal/dispatcher`, `internal/provider`, `internal/hooks`, `internal/tagparser`,
`internal/jira`, and `web/` from PLAN.md's Phase 0 layout are **not** stubbed out yet —
they get created with real content in the tasks that actually populate them, rather than as
empty placeholder packages.
