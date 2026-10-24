# 007: Dispatcher, brief, daemon

**Wave 3** · depends on: 003, 004, 005, 009 · branch `task/007-dispatcher`

## Goal
The loop that turns `todo` tasks into Claude runs (design §5–6, tech-spec §2, §7).

## API
```go
package brief
// Render writes <ws>/BRIEF.md and returns its path. The content, in order:
// title; key; repos (name → worktree path, branch, PR); description; acceptance criteria
// (from the latest run.criteria, else "see description"); latest run state/next; the last 3 run
// summaries, one line each; human comments, first line each, newest last, capped at 20; the
// status-block protocol (tech-spec §9) with the instruction to end every final reply with it, or
// to omit it to ask the human a question; rules: work only inside the listed worktrees, at most one
// PR per repo, never push to the default branch; cold-tier pointer: `conveyor task show <KEY>
// --thread --json`.
func Render(db *sql.DB, home string, task store.Task) (string, error)
// FreshPrompt: "You are working on Conveyor task <KEY>: <title>. Read <briefPath> first and follow it." + delta.
// ResumePrompt: "New comments on <KEY>:\n" + delta + "\n(The brief at <briefPath> is up to date.)"
// The delta is the unconsumed comments, each as "- [author] body".
func FreshPrompt(task store.Task, briefPath string, delta []store.Comment) string
func ResumePrompt(task store.Task, briefPath string, delta []store.Comment) string

package dispatch
type Dispatcher struct {
    DB *sql.DB; Home string; ConveyorBin string
    Now func() time.Time // injectable
    Log *log.Logger
}
// Lock takes the exclusive flock on <home>/dispatcher.lock (syscall.Flock LOCK_EX|LOCK_NB).
func Lock(home string) (release func(), err error)
var ErrLocked = errors.New("dispatcher already running")
// Run ticks every 2s until ctx is done: Tick(ctx).
func (d *Dispatcher) Run(ctx context.Context) error
// Tick does one pass, in this order:
// 1. Reap: for ActiveRuns, if claude.Agents shows no entry with the run's bg_id and the run is older than 30 s → run failed
//    with error "session vanished", task running→needs_you, plus a system comment.
// 2. PR poll (every 60 s): gh.SyncTask for tasks in review (task 009).
// 3. Launch: while active auto runs < max_parallel, pick the highest-priority todo task with no active run whose newest
//    unconsumed comment is older than comment_debounce_secs (or that has none). For it:
//    workspace.Ensure → claude.WriteSettings → brief.Render → choose mode → CreateRun(launching) → ConsumeComments →
//    UpdateTaskStatus(todo→running, system) → Launch or Resume → save bg_id. On error: run failed + error, task back to todo,
//    consecutive_failures++, and at the max → failed with a system comment (ErrUntrusted gets its hint text).
// Mode: resume iff the latest auto run is finished, has a session_id and bg_id, and ctx_peak < resume_threshold. Otherwise fresh.
func (d *Dispatcher) Tick(ctx context.Context) error
```
- `core.Service.Stop` is wired to `claude.Stop`. On cancel and on done, also call `claude.Remove` for every run's bg_id (best effort).
- `conveyor daemon` (`cmd_daemon.go`): Lock, or exit 1 with "dispatcher already running". Then `Run` with SIGINT handling, logging to stderr and `<home>/logs/dispatcher.log`.
- App: fill in `ui.startDispatcher`. Try `Lock` every 10 s; once held, run the dispatcher until the app quits. Show a dispatcher badge in the header ("dispatching" or "UI only") via a bound `DispatcherState()`.
- `conveyor task brief KEY` prints `brief.Render` output.
- Dedupe any `KeyFromCwd` copy left by task 004 so it uses `workspace.KeyFromCwd`.

## Tests
Use fake `claude` and `gh` scripts plus real git temp repos.
- A todo task launches a fresh run with the correct argv and cwd, sets the bg_id and moves the task to running.
- Debounce: a 5 s-old comment doesn't launch, but a 25 s-old one does (inject `Now`).
- Resume vs fresh by ctx_peak.
- The max_parallel cap.
- Launch errors increment failures, ending in failed.
- Reap of a vanished session.
- The lock is exclusive between two `Lock` calls.
- Brief golden-file test.

## Outcome

Built `internal/brief` (Render, FreshPrompt, ResumePrompt), `internal/dispatch` (Dispatcher, Lock, ErrLocked, Run, Tick), `conveyor daemon`, `conveyor task brief KEY`, `core.Service.Stop` plus stop/remove of bg sessions when a task moves to done or cancelled (in `Service.Move`, so `MarkDone`, `Cancel` and the UI's Move all get it), the app's lock-retry loop with a `DispatcherState()` header badge, and `hook` now uses `workspace.KeyFromCwd`.

Deviations:
- A launch failure before a run row exists (workspace, settings or brief errors) records an `error` event instead of a failed run. At the failure cap such a task goes todo -> running -> failed, because todo -> failed is not an edge in the status table.
- BRIEF "acceptance criteria" uses the newest run that has criteria, not strictly the newest run, so an interactive run without a block does not erase them.
- A resume run is created with the previous run's session id, so the hook (which matches by session id) attributes the Stop to the new run.
- The app looks for the `conveyor` CLI next to the app binary, then on PATH, for the hook command in claude-settings.json.
- `wails` is not installed on this machine, so `wails build` was not run. `go build .`, `npm ci && npm run build`, `gofmt`, `go vet` and `go test ./...` pass. The two Wails binding files were edited by hand to add `DispatcherState`.

Manual checks: two `conveyor daemon` processes on one home: the second exits 1 with "dispatcher already running".

Follow-ups:
- Comments are consumed before the launch, so a failed launch loses the delta for the retry; an unconsume step would fix it.
- The PR poller in the dispatcher is untested against a fake `gh`; `gh.SyncTask` has its own tests.
- Marking done through the PR poller (review -> done) does not call `claude.Remove`.
