# 002: Store v2, core operations, CLI skeleton

**Wave 1** · depends on: nothing · blocks: everything · branch `task/002-store-core-cli`

## Goal
Replace the v1 schema with the tech-spec §5 schema. Add the domain operations that the CLI and the app share, and the CLI skeleton with the `repo`, `task` (basic) and `config` commands. Delete the chi server.

## Do
1. **`internal/store`**
   - Rewrite `schema.go` with the exact DDL from tech-spec §5. Drop the v1 tables in `Migrate` with `DROP TABLE IF EXISTS kanban_task_events; DROP TABLE IF EXISTS kanban_tasks;`.
   - `Open(path)`: DSN pragmas `busy_timeout(5000)`, `journal_mode(WAL)`, `foreign_keys(1)`. Call `Migrate` inside `Open`.
   - Keep `SetMaxOpenConns(1)`.
   - `status.go`: the new `Status` set (backlog, todo, running, needs_you, review, done, failed, cancelled) and the transition table from tech-spec §6. Keep the `Actor` type and add `ActorSystem` semantics as specified.
   - Models (`models.go`): `Repo`, `Task` (including `Repos []TaskRepo` when loaded), `TaskRepo`, `Run`, `Comment`, `Event`, `Learning`, `Automation`. Use Go field names matching the columns, with `json:"snake_case"` tags. Nullable columns become pointers or `sql.Null*`, your choice, but be consistent.
   - Queries, one file per table. **Exact names; later tasks call these:**
     ```go
     // repos.go
     AddRepo(db, Repo) (Repo, error); ListRepos(db) ([]Repo, error); GetRepoByName(db, name) (Repo, error); RemoveRepo(db, name) error
     // tasks.go
     CreateTask(db, Task, repos []TaskRepo) (Task, error)   // assigns key = prefix-N in the same tx; exactly one primary
     GetTask(db, id int64) (Task, error); GetTaskByKey(db, key string) (Task, error)  // both load Repos
     ListTasks(db, statuses ...Status) ([]Task, error)       // priority desc, created_at asc
     UpdateTaskStatus(db, id int64, to Status, actor Actor) (Task, error)  // guarded + events row
     UpdateTask(db, Task) error                              // title, description, priority, model, agent, consecutive_failures
     UpdateTaskRepo(db, TaskRepo) error                      // worktree_path, pr_url, pr_state
     // runs.go
     CreateRun(db, Run) (Run, error); GetRun(db, id) (Run, error); UpdateRun(db, Run) error
     RunBySession(db, sessionID string) (Run, error); LatestRun(db, taskID int64) (Run, error)
     ListRuns(db, taskID int64) ([]Run, error); ActiveRuns(db) ([]Run, error)  // status in (launching, running)
     // comments.go
     AddComment(db, Comment) (Comment, error); ListComments(db, taskID) ([]Comment, error)
     UnconsumedComments(db, taskID) ([]Comment, error)  // author in (human, system) and consumed_by_run_id IS NULL
     ConsumeComments(db, ids []int64, runID int64) error
     // events.go
     RecordEvent(db, taskID int64, kind, detail string) error; ListEvents(db, taskID) ([]Event, error)
     // settings.go
     GetSetting(db, key) string   // falls back to Defaults[key]
     SetSetting(db, key, value string) error; ListSettings(db) map[string]string
     var Defaults = map[string]string{...}  // tech-spec §5 table
     ```
   - Learnings and automations get their tables now. Their queries are added by tasks 011 and 010.
   - Missing rows return a wrapped `sql.ErrNoRows`, so `errors.Is` works.
2. **`internal/core`**: `type Service struct{ DB *sql.DB; Home string }`.
   - `CreateTask(in NewTaskInput) (store.Task, error)`
     - validates that the repos exist, at least one is given, and the primary is among them (defaulting to the first)
     - branch = `cv/<KEY>-<slug(title, 40)>`
     - status `backlog`, or `todo` if `in.Start`
     - The key is only known after insert, so `CreateTask` in the store fills in the branch. Pass the slug, and the store composes `cv/<key>-<slug>`.
   - `AddComment(key, author, body string) (store.Comment, error)`: if the author is human or system and the task is in needs_you, review, failed or done, move it to `todo` (actor human for a human author, system for system). backlog stays backlog, and todo or running is unchanged.
   - `Move(key string, to store.Status) error` (actor human), `MarkDone(key)`.
   - `Cancel(key)`: moves to cancelled. Stopping the session is added in task 007 through a `Stopper` func field on `Service`: `Stop func(bgID string) error`, nil-safe.
   - `Home()` helper: `--home` flag, else `$CONVEYOR_HOME`, else `~/.conveyor`. Creates the directory. `DBPath(home)`, `WorkspacesDir(home)`.
3. **`cmd/conveyor`**
   - `main.go`: parses the global `--home`, then dispatches through the `register(name, fn, help)` registry (tech-spec §11), and prints usage.
   - `cmd_repo.go`: `repo add` resolves the absolute path, checks that `git -C path rev-parse --show-toplevel` equals the path, and detects the default branch with `git symbolic-ref refs/remotes/origin/HEAD`, falling back to `git branch --show-current`. Also `repo list` and `repo rm`.
   - `cmd_task.go`: `new`, `list`, `show [--thread] [--runs]`, `comment`, `move`, `cancel`, `done`. Human-readable tables, plus `--json` for list and show.
   - `cmd_config.go`: `list`, `get`, `set`.
   - Delete the `serve` command and the chi dependency (`go mod tidy`).
4. Update `progress/progress.md`: mark 000 and 001 as superseded by v2, and set row 002.

## Tests
- Every transition-table edge (valid and a sample of invalid ones), and the human-only cancel.
- `CreateTask` assigns sequential keys (`CV-1`, `CV-2`) and rejects 0 or 2 primaries.
- `AddComment` transitions per the rules above.
- `UnconsumedComments` / `ConsumeComments`.
- Settings fallback to defaults.
- A CLI test that builds the binary in `TestMain` (`go build -o $tmp/conveyor`) and runs `repo add` against a temp git repo, `task new … --todo`, `task list --json` and `task comment` with `--home $tmp`.

## Out of scope
Worktrees, runs launching, the app.

## Outcome

**Built:** the v2 schema (`Migrate` drops the v1 tables), `Open` with WAL, busy_timeout and foreign_keys, the status machine from tech-spec §6, the models and per-table queries with the exact names from this spec, `core.Service` (`CreateTask`, `AddComment`, `Move`, `MarkDone`, `Cancel`, `Home`, `DBPath`, `WorkspacesDir`), and the CLI (`repo`, `task`, `config`) behind the `register(name, fn, help)` registry. The chi server and dependency are gone, along with the unused `google/uuid`.

**Conventions later tasks should know:**
- A command function has the signature `func(home string, args []string) error`. `withService(home, fn)` opens and closes the DB. `parseArgs` allows flags after positionals.
- Timestamps are RFC3339 strings on the models, and nullable columns are pointers.
- `store.CreateTask` takes the slug in each `TaskRepo.Branch` and stores `cv/<KEY>-<slug>`. `TaskRepo.RepoName` is joined on load and ignored on write.
- Comment authors are the constants `store.AuthorHuman`, `AuthorAgent` and `AuthorSystem`.

**Deviations:**
- `progress/progress.md` is not edited, because the workflow's merge step does that.
- `Service.Cancel` only moves the status. The `Stop` field arrives with task 007, as the spec says.
- `running -> review` is allowed for both actors, because §6 lists it as a hook transition and as a human override.

**Tests:** `go test ./...` covers every transition edge for both actors, the human-only cancel, sequential keys, the primary-repo rule, the comment rules, settings defaults, and a CLI test that builds the binary. No manual checks.

**Follow-ups:** Key allocation uses a deferred transaction, so two processes creating tasks at the same instant may get `SQLITE_BUSY` rather than a duplicate. The `UNIQUE` constraint prevents duplicates, but a retry could be added.
