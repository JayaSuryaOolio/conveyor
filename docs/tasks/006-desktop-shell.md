# 006: Desktop app shell (Wails v2): Board, Focus, Task page

**Wave 2** · depends on: 002 · branch `task/006-desktop-shell`

## Goal
A working Wails v2 app (React + TS) over `internal/core` and `internal/store`, with the Board, Focus and Task page (thread, comment, move) and the New task dialog, plus live refresh. The Workspace panel is a placeholder `<aside>`, filled in by task 008.

## Do
- Scaffold with `wails init -n conveyor -t react-ts` in a temp dir, then move `main.go`, `app.go`, `wails.json`, `frontend/` and `build/` to the repo root. Keep the existing `cmd/` and `internal/` layout.
  - `wails.json`: `"name": "Conveyor"`, `"outputfilename": "Conveyor"`.
  - Commit `frontend/dist/.gitkeep`, and add `frontend/dist/*` with `!frontend/dist/.gitkeep` to `.gitignore`, plus `frontend/node_modules`, `build/bin`.
- Root `main.go` and `app.go` stay thin and delegate to **`internal/ui`**.
  - `ui.App` opens the DB from the home directory (tech-spec §3; the env override `CONVEYOR_HOME` is honoured) and builds a `core.Service`.
  - It starts the `data_version` poller (700 ms) that emits `db:changed`.
  - Leave a clearly marked `startDispatcher(ctx)` hook point that is a no-op until task 007.
- `ui.App` bound methods (JSON-friendly structs):
  ```go
  ListTasks() ([]TaskCard, error)        // card: key, title, status, priority, repos []string, lastSummary, ctxPeak, updatedAt
  GetTask(key string) (TaskDetail, error) // task + repos + comments + runs (newest last)
  CreateTask(in core.NewTaskInput) (string, error)
  AddComment(key, body string) error
  Move(key, status string) error
  Cancel(key string) error
  ListRepos() ([]store.Repo, error)
  AddRepo(path, name string) error       // same validation as the CLI; move that logic into core.AddRepo and use it from both
  GetSettings() (map[string]string, error); SetSetting(k, v string) error
  ```
- **Frontend.** Keep it simple and fast to use, with no UI library.
  - **Board:** columns per tech-spec §12, cards and a status dropdown. Done shows only the last 7 days.
  - **Focus:** an ordered list with an inline comment box per row.
  - **Task page:** description, criteria checklist from the latest run, and the thread with run separators ("Run N · mode · calls · ctx peak · duration"), with agent comments rendered as markdown-ish preformatted text. Includes a comment box and status controls.
  - **New task** dialog with repo multi-select and primary choice.
  - **Repos** page (list and add) and **Settings** page (key/value editor).
  - Keyboard shortcuts: `n` new task, `/` focus search, `g b` board, `g f` focus.
  - Refetch on `EventsOn("db:changed")`.
  - Light and dark via `prefers-color-scheme` and CSS variables.

## Tests and checks
- Go: `internal/ui` unit tests for `ListTasks` and `GetTask` shaping, and the poller emitting on an external write (inject an emit func).
- `cd frontend && npm ci && npm run build` passes; `wails build` passes on macOS.
- `go test ./...` passes from the repo root, with the embed working against `.gitkeep`.
- Manual smoke: run `wails dev` against a temp `CONVEYOR_HOME` seeded via the CLI. Record what you checked in the Outcome.

## Outcome

**Built**
- Wails v2 shell at the repo root (`main.go`, `app.go`, `wails.json`, `frontend/`, `build/`), both thin over `internal/ui`. `frontend/dist/.gitkeep` is committed and the rest of `dist` is gitignored. `frontend/wailsjs` (generated bindings) is committed.
- `internal/ui`: `App` with all bound methods from the spec, the 700 ms `PRAGMA data_version` poller emitting `db:changed` (it uses its own DB connection, so the app's own writes also fire), and a no-op `startDispatcher(ctx)` hook for task 007.
- `core.Service.AddRepo` (moved from `cmd/conveyor`, which now calls it); the CLI behaviour is unchanged.
- Frontend (React + TS, no UI lib): Board, Focus, Task page (criteria checklist, thread with run separators, comment box, status controls, placeholder `<aside>` for task 008), New task dialog, Repos, Settings, shortcuts `n`, `/`, `g b`, `g f`, and refetch on `db:changed`.

**Tests**: `internal/ui` tests for `ListTasks`/`GetTask` shaping and for the poller emitting after a write. `gofmt -l`, `go vet ./...`, `go test ./...`, `npm ci && npm run build` and `wails build` pass.

**Deviations**
- The Board adds a Failed column when a failed task exists (spec §12 lists no such column, so failed tasks would otherwise be invisible there).
- `NewTaskInput` has no json tags, so the TS side uses its Go field names (`Title`, `Repos` ...).
- The Cancel button is on the Task page only.

**Manual checks**: with a temp `CONVEYOR_HOME` seeded via the CLI (repo + task + a comment added while running), the built `Conveyor.app` launched and ran without errors or crashes. I used `wails build` and the binary rather than `wails dev`, and I did not visually inspect the UI, so layout, shortcuts and live refresh in the window are untested by eye.

**Follow-ups**: none beyond the above; the 008 Workspace panel and 007 dispatcher plug into the placeholders.
