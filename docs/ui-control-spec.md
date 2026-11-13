# Driving Conveyor from outside (CV-153)

Status: spec, not built. Replaces the "assistant orb" idea in CV-153 with the option the task leaned towards: Conveyor stays dumb and exposes commands; an external tool (voice app such as OpenWhispr, Raycast, Hammerspoon, a shell alias) does the parsing and any AI.

## 1. Decision

No orb, text box or AI inside Conveyor. Three small additions to the CLI cover what the orb would have done:

| Need | What exists today | Add |
|---|---|---|
| Move status, comment, stop, link, run app actions | `conveyor task move/comment/stop`, `conveyor app run/open` | nothing |
| "Check PR" button | binding `CheckReviews` only (UI) | `conveyor task check-pr KEY` |
| Navigate the open app | nothing: the CLI cannot reach the UI | `conveyor ui go/palette/current` (§3) |
| "on the task I'm looking at" | nothing: the CLI cannot see the UI | the `@` key token (§4) |
| Let a tool know what it can do | `app list` (app-level only) | `conveyor actions KEY --json` (§5) |

Why not the orb: an in-app orb needs its own text UI, a parser, a Claude session lifecycle, and later a second input path for voice. An external tool needs all of that anyway for voice, so building it in Conveyor would mean building it twice. The CLI is already the API that agents, automations and app manifests use.

## 2. Transport: CLI to running app

The app already polls `PRAGMA data_version` every 700 ms (`internal/ui/app.go` `pollDataVersion`) and emits `db:changed`. A CLI write is therefore already seen by the app within 700 ms. Reuse it:

```sql
CREATE TABLE ui_intents (
  id          INTEGER PRIMARY KEY,
  verb        TEXT NOT NULL,          -- go | palette
  args        TEXT NOT NULL,          -- JSON, e.g. {"name":"task","key":"CV-128"}
  created_at  TEXT NOT NULL,
  consumed_at TEXT
);
```

- **CLI** (`conveyor ui go ...`): validates the target first (task exists, view name known; a bad key fails here with exit 1), inserts one row, then polls the row every 100 ms for up to 3 s. `consumed_at` set: exit 0. Timeout: delete the row, print `Conveyor app is not running`, exit 3.
- **App** (Go, inside the existing poll callback, before emitting `db:changed`): `UPDATE ui_intents SET consumed_at=now WHERE consumed_at IS NULL AND created_at > now-10s RETURNING ...`, then for each row: `runtime.WindowUnminimise` + `runtime.WindowShow`, then `EventsEmit(ctx, "ui:intent", row)`. Rows older than 10 s are deleted unread, so an intent queued while the app was closed never fires later.
- **Frontend** (`App.tsx`): `EventsOn('ui:intent', i => i.verb === 'go' ? setView(i.args) : (setQuery(i.args.q), spotlight.open()))`. `args` is the existing `View` type (`components.tsx`), so no new routing.
- Only the app consumes intents. `conveyor daemon` ignores the table.

Rejected:
- **`conveyor://` URL scheme** (Wails `Info.Protocols` + `Mac.OnUrlOpen`): needs a bundled build registered with LaunchServices, does nothing under `wails dev` or a throwaway `--home`, and gives the caller no exit code. Add later as a thin adapter that inserts the same intent rows if links in Slack or docs become a need.
- **Local HTTP/socket listener**: a new port, auth and lifecycle when the DB is already a shared bus.

## 3. `conveyor ui`

```
conveyor ui go board [--label L] [--repo R]
conveyor ui go task KEY            # also: conveyor ui go KEY
conveyor ui go epic KEY [--task KEY]
conveyor ui go learnings|automations|apps|agents|logs|settings|focus
conveyor ui palette [QUERY]        # opens Cmd+K, prefilled
conveyor ui current [--json]       # what the app shows now (§4); exit 3 if the app is not running
```

`ui go KEY` (bare key) is the voice-friendly form: "open CV one two eight" becomes `conveyor ui go CV-128`. `focus` opens the Focus popup (`onFocus` in `App.tsx`).

## 4. The current view and the `@` token

The app writes its current view to `<home>/ui-state.json` on each change (binding `SetView(v View)`, called from `nav`/`setView` in `App.tsx`; atomic write via temp file and rename):

```json
{"view":{"name":"epic","key":"CV-120","task":"CV-120B"},"pid":4242,"at":"2026-11-13T09:00:00Z"}
```

A file, not a DB row, so navigation does not bump `data_version` and trigger a refetch everywhere.

**Current task** = `view.task` if set (the epic view's open modal), else `view.key` when `view.name` is `task` or `epic`, else none.

**`@`**: in `cmd/conveyor/main.go`, before dispatch, any positional argument that is exactly `@` is replaced with the current task key. Errors (exit 3): app not running (`pid` dead, checked with `kill -0`), or no task open (`no task is open in Conveyor`). One rewrite in one place instead of a change to every command. A bare `@` as a comment body or a title is the only collision, and nobody types or says that.

```
conveyor task move @ review
conveyor app run "Open in IDE" open @
conveyor task check-pr @
```

## 5. `conveyor actions [KEY|@] [--json]`

One list of everything a tool can run, each with a ready argv, so the external tool needs no hard-coded Conveyor knowledge and new app actions show up for voice with no change:

```json
[
  {"label":"Check PR","kind":"builtin","argv":["conveyor","task","check-pr","CV-128"]},
  {"label":"Move to review","kind":"builtin","argv":["conveyor","task","move","CV-128","review"]},
  {"label":"Cancel","kind":"builtin","argv":["conveyor","task","cancel","CV-128"],"confirm":"Cancel CV-128?"},
  {"label":"Comment","kind":"builtin","argv":["conveyor","task","comment","CV-128","{text}"],"needs_text":true},
  {"label":"Open task","kind":"nav","argv":["conveyor","ui","go","CV-128"]},
  {"label":"Run tests","kind":"app","app":"Make","argv":["conveyor","app","run","Make","test","CV-128","--place","task_sidebar"],"confirm":"..."}
]
```

- **builtin**: the moves allowed from the task's status (`internal/store/status.go`, human transitions), `stop` when running, `check-pr` when the task has an open PR, `open --shell/--resume/--fresh`, `comment` (`needs_text`).
- **app**: `apps.PlacedActions` for each place, and `app open` per app; `confirm` comes from the manifest's `Confirm`.
- **nav**: task page, epic view when the task has a tree.
- Without KEY: only actions that need no task (nav targets, `ui palette`).
- `confirm` is a contract for the caller: the CLI does not prompt. Destructive built-ins (`cancel`, `done`, `stop`, `move` to cancelled) always carry `confirm`.

## 6. The external tool (not built here)

Shown only to prove §2–5 are enough. Roughly 30 lines of shell or a Raycast script:

```sh
utter="$1"                                   # from voice-to-text or a text box
acts=$(conveyor actions @ --json 2>/dev/null || conveyor actions --json)
cmd=$(jq -r --arg u "$utter" '...fuzzy match .label...' <<<"$acts")
if [ -n "$cmd" ]; then eval "$cmd"; exit; fi  # parse first: ask before running when confirm is set
# AI fallback: the user's conveyor and conveyor-doctor skills are already in ~/.claude
claude -p --allowedTools 'Bash(conveyor:*)' \
  "Current Conveyor view: $(conveyor ui current --json). Available actions: $acts. Do: $utter"
```

## 7. Tests

- `cmd/conveyor/cli_test.go`: `ui go` with no app times out with exit 3 and leaves no row; `ui go BAD-1` fails before inserting; `@` with no state file, with a dead pid, with a task view, with an epic modal; `actions` for a running task with an open PR and one placed app action (golden JSON).
- `internal/ui`: the consume step marks rows, drops rows older than 10 s, and emits once per row (inject `now` and the emit func, as `pollDataVersion` already takes `emit`).
- `task check-pr`: schedules the same check as the button (`gh.ScheduleCheck(db, t, 0, 0)`).

## 8. Work split

1. `conveyor task check-pr` and `conveyor actions` (CLI and `internal/core` only, no UI).
2. `ui_intents` + `ui-state.json` + `conveyor ui` + the `@` rewrite (store migration, `internal/ui`, `App.tsx`, CLI).

Both update `docs/guide.md` and the command list in `.claude/skills/conveyor/SKILL.md`.

Out of scope: the voice/parsing tool itself, the `conveyor://` scheme, an in-app orb.
