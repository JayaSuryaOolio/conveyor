# 010: Automations and Jira import

**Wave 4** · depends on: 007 · branch `task/010-automations`

## Goal
orca-style automations: a cron schedule, a shell precheck that costs zero tokens, and an action (design §9).

## API
```go
package automation
// action_arg JSON per kind:
// create_task: {"title": "...", "description": "...{output}...", "repos": ["a"], "start": true}
// comment:     {"task_status": "review", "body": "...{output}..."}  → a system comment on every task in that status
//              (via core.AddComment, which moves it to todo); or {"task_key": "CV-3", "body": "..."}
// cli:         {"args": ["import", "jira", "--jql", "..."]}  → runs the conveyor binary with these args
// Precheck: `sh -c <precheck>` with env CONVEYOR_TASK_KEY and CONVEYOR_TASK_BRANCH unset (global),
// a 60 s timeout, exit 0 = run, stdout (≤ 8 KB) → {output}.
// For the comment kind with task_status, the precheck runs once per matching task, with
// CONVEYOR_TASK_KEY, CONVEYOR_TASK_BRANCH (primary) and CONVEYOR_TASK_PR (primary pr_url) set.
func Due(a store.Automation, now time.Time) bool   // the cron matched between last_run_at and now; a missed run fires once
func RunOne(ctx context.Context, svc *core.Service, a store.Automation) (result string, err error)
func Tick(ctx context.Context, svc *core.Service, now time.Time) error  // called by the dispatcher every 30 s
```
- Allowed new dependency: `github.com/robfig/cron/v3`, for parsing only.
- Store: `CreateAutomation`, `ListAutomations`, `GetAutomation(name)`, `UpdateAutomation`, `DeleteAutomation`.
- Wire `automation.Tick` into `dispatch.Tick` every 30 s.
- **CLI:** `conveyor automation list|add|enable|disable|run|rm`.
- **`conveyor import jira --jql "…" --repo a`** uses `acli jira workitem search --jql … --json`. Inspect `acli jira workitem search --help` for the exact flags and record what you found in the Outcome. For each issue without an existing task (matched on `external_key`), create a backlog task with the summary as title, the description, `source=jira`, the external_key and the URL.
- **Built-in templates:** `conveyor automation add --template pr-sweep` installs this automation:
  - schedule `*/15 * * * *`
  - kind `comment` with `task_status: review`
  - precheck: `gh pr view "$CONVEYOR_TASK_PR" --json reviews,comments --jq '[.reviews[],.comments[]] | map(select(.updatedAt > env.SINCE)) | length' | grep -qv '^0$'`, with `SINCE` provided as the automation's last_run_at
  - body "New PR review comments since the last sweep. Address them, push, and reply on the PR threads."
- **App:** an Automations page that lists automations with their enabled toggle, last run and result, and has a "Run now" button.

## Tests
- `Due` cases: every 15 min, a missed window and a disabled automation.
- `RunOne` for each kind, using fake `conveyor`, `gh` and `acli`.
- Jira import de-dupes on external_key.

## Outcome
Built:
- `internal/store/automations.go`: Create/List/Get/Update/DeleteAutomation. `CreateAutomation` stamps a nil `last_run_at` with the creation time, so a schedule counts from creation instead of firing at once.
- `internal/automation`: `Due`, `RunOne`, `Tick`, plus `Record` (stores last_run_at/last_result), `ValidSchedule` and the `Templates` map (`pr-sweep`). Parsing uses `github.com/robfig/cron/v3` (`ParseStandard`, 5 fields).
- `dispatch.Tick` calls `automation.Tick` every 30 s in a goroutine (guarded so passes never overlap), so a 60 s precheck never stalls launches.
- `internal/jira` and `conveyor import jira --jql "…" --repo a`. `core.NewTaskInput` gained `Source`, `ExternalKey` and `ExternalURL`.
- CLI `conveyor automation list|add|enable|disable|run|rm` (`add --template pr-sweep`, or `add NAME --schedule --kind --arg [--precheck]`).
- App: bound `ListAutomations`, `SetAutomationEnabled`, `RunAutomation`, and an Automations page (toggle, last run, result, Run now). Bindings regenerated with `wails generate module`.

`acli jira workitem search --help` flags used: `--jql`, `--fields key,summary,description`, `--paginate`, `--json`. Others: `--limit`, `--count`, `--csv`, `--filter`, `--web`. With `--json` the output is an array of issues: `key`, `self` (an API URL, not a browse URL) and `fields.{summary,description}`. The description is Atlassian Document Format, or null, and is flattened to text.

Deviations and decisions:
- `external_url`: `self` is not a browse URL, so the URL is `<jira_base_url>/browse/KEY` when the new setting `jira_base_url` is set (`conveyor config set jira_base_url https://x.atlassian.net`), else `self`.
- The cli kind runs `automation.Bin` (set by the dispatcher from `ConveyorBin` and by the app from `conveyorBin()`), else the current executable. It passes `--home <svc.Home>`.
- Precheck gets `SINCE=<last_run_at>` in its env (epoch if never run), as the pr-sweep template needs. A failing precheck yields result "skipped: precheck failed", not an error. A manual "Run now" also updates last_run_at.

Manual checks: not run against a live Jira or GitHub (fakes only). `wails build` run result is noted in the PR.

Follow-ups: no UI to create or edit automations (CLI only); the pr-sweep precheck needs `gh` authenticated in the daemon's environment.
