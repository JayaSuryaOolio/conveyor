// Package ui holds the Wails bindings: a thin layer over core and store.
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/audit"
	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/dispatch"
	"github.com/JayaSuryaOolio/conveyor/internal/gh"
	"github.com/JayaSuryaOolio/conveyor/internal/jira"
	"github.com/JayaSuryaOolio/conveyor/internal/learning"
	"github.com/JayaSuryaOolio/conveyor/internal/runner"
	"github.com/JayaSuryaOolio/conveyor/internal/slack"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/term"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// pollEvery is the data_version poll interval (tech-spec §2).
const pollEvery = 700 * time.Millisecond

// slowCall is how long a binding may run before trace logs it.
const slowCall = 3 * time.Second

// trace logs a binding only when it is slow: "still running" once after slowCall (so a call that never
// returns shows up), and "finished" with the duration if it was reported. Quiet otherwise.
func trace(name string) func() {
	t := time.Now()
	var slow atomic.Bool
	timer := time.AfterFunc(slowCall, func() {
		slow.Store(true)
		log.Printf("ui: %s still running after %s", name, slowCall)
	})
	return func() {
		timer.Stop()
		if slow.Load() {
			log.Printf("ui: %s finished after %s", name, time.Since(t).Round(time.Millisecond))
		}
	}
}

// App is bound to the frontend. Methods return plain JSON-friendly structs.
type App struct {
	ctx        context.Context // Wails' context, for native dialogs
	svc        *core.Service
	cancel     context.CancelFunc
	dispatcher atomic.Bool   // true while this process holds the dispatcher lock
	pathErr    string        // why the login shell's PATH could not be loaded; empty on success
	ready      chan struct{} // closed when Startup returns; see Ready
}

// NewApp returns an App; the DB is opened in Startup.
func NewApp() *App { return &App{ready: make(chan struct{})} }

// Startup opens the DB under CONVEYOR_HOME (default ~/.conveyor), starts the
// change poller and the dispatcher hook. It matches Wails' OnStartup.
func (a *App) Startup(ctx context.Context) {
	defer close(a.ready)
	a.ctx = ctx
	if err := loadShellPATH(); err != nil {
		a.pathErr = err.Error()
	}
	home, err := core.Home("")
	if err != nil {
		runtime.LogFatalf(ctx, "conveyor home: %v", err)
		return
	}
	audit.Install(home)
	// A Finder-launched app's stderr goes nowhere; keep a log file in the home too.
	if f, err := core.OpenLog(core.LogPath(home)); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}
	pe := filepath.SplitList(os.Getenv("PATH"))
	log.Printf("app start: home=%s cli=%s PATH entries=%d first=%v", home, conveyorBin(), len(pe), pe[:min(len(pe), 5)])
	if a.pathErr != "" {
		log.Printf("app start: %s", a.pathErr)
	}
	db, err := core.OpenDB(home)
	if err != nil {
		log.Printf("open db: %v", err) // LogFatalf writes only to the Wails logger
		runtime.LogFatalf(ctx, "open db: %v", err)
		return
	}
	_, _, _ = runner.AutoConfigureDefaultRunner(db)
	a.svc = &core.Service{DB: db, Home: home, Bin: conveyorBin()}
	jira.Attach(a.svc)
	a.startTerms()
	ctx, a.cancel = context.WithCancel(ctx)
	go pollDataVersion(ctx, core.DBPath(home), pollEvery, func() { runtime.EventsEmit(ctx, "db:changed") })
	go a.startDispatcher(ctx)
}

// Shutdown stops background work and closes the DB.
func (a *App) Shutdown(context.Context) {
	term.CloseAll()
	if a.cancel != nil {
		a.cancel()
	}
	if a.svc != nil {
		a.svc.DB.Close()
	}
}

// lockRetry is how often a UI-only app retries taking the dispatcher lock.
const lockRetry = 10 * time.Second

// startDispatcher tries the dispatcher lock every lockRetry; once held it runs the
// dispatcher until ctx ends. Another process (conveyor daemon, a second app) may hold it.
func (a *App) startDispatcher(ctx context.Context) {
	for {
		if release, err := dispatch.Lock(a.svc.Home); err == nil {
			a.runDispatcher(ctx)
			release()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(lockRetry):
		}
	}
}

func (a *App) runDispatcher(ctx context.Context) {
	db, err := store.Open(core.DBPath(a.svc.Home))
	if err != nil {
		log.Printf("dispatcher: %v", err)
		return
	}
	defer db.Close()
	a.dispatcher.Store(true)
	defer a.dispatcher.Store(false)
	// Tee to logs/dispatcher.log so the Logs view's dispatcher source works whichever process dispatches.
	w := log.Writer()
	if f, err := core.OpenLog(core.DispatcherLogPath(a.svc.Home)); err == nil {
		defer f.Close()
		w = io.MultiWriter(w, f)
	} else {
		log.Printf("dispatcher log: %v", err)
	}
	d := &dispatch.Dispatcher{Svc: &core.Service{DB: db, Home: a.svc.Home, Bin: a.svc.Bin}, Log: log.New(w, "dispatcher: ", log.LstdFlags)}
	d.Log.Printf("started (app)")
	d.Run(ctx)
}

// conveyorBin finds the CLI the hooks should call on PATH. It never looks next to the app:
// on a case-insensitive filesystem Contents/MacOS/conveyor is the app binary itself.
func conveyorBin() string {
	if p, err := exec.LookPath("conveyor"); err == nil {
		return p
	}
	return "conveyor"
}

// Home is the conveyor home directory (the markdown renderer maps attachment paths under it).
func (a *App) Home() string { return a.svc.Home }

// SaveAttachment stores a pasted/dropped image (base64) and returns its absolute path.
func (a *App) SaveAttachment(key, name, dataBase64 string) (string, error) {
	return core.SaveAttachment(a.svc.Home, key, name, dataBase64)
}

// ShowError logs a frontend error and shows it in a native dialog
// (the macOS webview silently drops window.alert).
func (a *App) ShowError(msg string) {
	log.Printf("ui error: %s", msg)
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{Type: runtime.ErrorDialog, Title: "Conveyor", Message: msg})
}

// Confirm asks a yes/no question in a native dialog (window.confirm always returns false in the macOS webview).
func (a *App) Confirm(msg string) (bool, error) {
	r, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Conveyor", Message: msg,
		Buttons: []string{"Yes", "No"}, DefaultButton: "No", CancelButton: "No"}) // darwin returns "" without explicit Buttons
	return r == "Yes", err
}

// Version is set at build time by scripts/install.sh (-ldflags -X); "dev" for plain builds.
var Version = "dev"

// AppVersion returns the build version shown in the sidebar.
func (a *App) AppVersion() string { return Version }

// ShellPATHError says why the login shell's PATH could not be loaded at startup; empty on success.
func (a *App) ShellPATHError() string { return a.pathErr }

// Requirement is an external tool Conveyor shells out to and whether it is on PATH.
type Requirement struct {
	Name     string `json:"name"`
	Purpose  string `json:"purpose"`
	Path     string `json:"path"`
	Found    bool   `json:"found"`
	Optional bool   `json:"optional"`
}

// Requirements checks each external tool on the app's PATH (after the login-shell PATH merge).
func (a *App) Requirements() []Requirement {
	reqs := []Requirement{
		{Name: "git", Purpose: "worktrees, branches and diffs"},
		{Name: "gh", Purpose: "pull requests and webhooks"},
		{Name: "conveyor", Purpose: "CLI that agent hooks call back"},
	}
	for _, r := range runner.KnownRunners() {
		reqs = append(reqs, Requirement{Name: r.Command, Purpose: r.Name + " runner"})
	}
	reqs = append(reqs,
		Requirement{Name: "acli", Purpose: "Jira sync", Optional: true},
		Requirement{Name: "graft", Purpose: "run connectors from a worktree", Optional: true})
	for i := range reqs {
		p, err := exec.LookPath(reqs[i].Name)
		reqs[i].Path, reqs[i].Found = p, err == nil
	}
	return reqs
}

// DispatcherState says whether this app is dispatching or only showing the UI.
func (a *App) DispatcherState() string {
	if a.dispatcher.Load() {
		return "dispatching"
	}
	return "UI only"
}

// pollDataVersion emits whenever PRAGMA data_version changes. It uses its own
// connection: data_version only moves for commits made by other connections,
// so the app's own writes count as changes too.
func pollDataVersion(ctx context.Context, dbPath string, every time.Duration, emit func()) {
	db, err := store.Open(dbPath)
	if err != nil {
		return
	}
	defer db.Close()
	version := func() (int64, error) {
		var v int64
		err := db.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&v)
		return v, err
	}
	last, _ := version()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if v, err := version(); err == nil && v != last {
				last = v
				emit()
			}
		}
	}
}

// TaskCard is one board/focus card.
type TaskCard struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Priority    int      `json:"priority"`
	Parent      string   `json:"parent"` // parent task key, "" for top-level
	Repos       []string `json:"repos"`
	Labels      []string `json:"labels"`
	LastSummary string   `json:"lastSummary"`
	CtxPeak     int64    `json:"ctxPeak"`
	Runner      string   `json:"runner"`
	UpdatedAt   string   `json:"updatedAt"`
	LastMsgAt   string   `json:"lastMsgAt"` // newest comment (human or agent); "" when none
	LastMsgBy   string   `json:"lastMsgBy"` // its author: human, agent or system
	OpenPR      bool     `json:"openPR"`    // any repo has an OPEN PR (shows the board's "check reviews" button)
}

// TaskDetail is the task page payload; Runs are oldest first (newest last).
type TaskDetail struct {
	Task      store.Task      `json:"task"`
	Comments  []store.Comment `json:"comments"`
	Runs      []store.Run     `json:"runs"`
	Relations store.Relations `json:"relations"`
	Creator   string          `json:"creator"` // who made the task, for the sidebar
}

// creator labels who made t: from created_by, else (rows from before it existed) from its parent and source.
func creator(t store.Task, parent *store.TaskRef, user string) string {
	if user == "" {
		user = "human"
	}
	if t.CreatedBy != nil {
		kind, arg, _ := strings.Cut(*t.CreatedBy, ":")
		switch kind {
		case "human":
			return user
		case "agent":
			return "agent · " + arg
		case "followup":
			return "follow-up of " + arg
		case "automation":
			return "automation · " + arg
		}
		return *t.CreatedBy
	}
	switch {
	case parent != nil && t.Source == "followup":
		return "follow-up of " + parent.Key
	case parent != nil:
		return "agent · " + parent.Key
	case t.Source == "jira" || t.Source == "learning":
		return t.Source
	}
	return user
}

// ListTasks returns every task as a card.
func (a *App) ListTasks() ([]TaskCard, error) {
	defer trace("ListTasks")()
	tasks, err := store.ListTasks(a.svc.DB)
	if err != nil {
		return nil, err
	}
	runs, err := store.LatestRuns(a.svc.DB)
	if err != nil {
		return nil, err
	}
	msgs, err := store.LatestComments(a.svc.DB)
	if err != nil {
		return nil, err
	}
	cards := make([]TaskCard, 0, len(tasks))
	keys := map[int64]string{}
	for _, t := range tasks {
		keys[t.ID] = t.Key
	}
	for _, t := range tasks {
		c := TaskCard{Key: t.Key, Title: t.Title, Status: string(t.Status), Priority: t.Priority, Repos: []string{}, Labels: t.Labels, UpdatedAt: t.UpdatedAt}
		if t.Runner != nil {
			c.Runner = *t.Runner
		}
		if t.ParentID != nil {
			c.Parent = keys[*t.ParentID]
		}
		for _, r := range t.Repos {
			c.Repos = append(c.Repos, r.RepoName)
			c.OpenPR = c.OpenPR || (r.PRState != nil && *r.PRState == "OPEN")
		}
		if r, ok := runs[t.ID]; ok {
			c.CtxPeak = r.CtxPeak
			if r.Summary != nil {
				c.LastSummary = *r.Summary
			}
		}
		if m, ok := msgs[t.ID]; ok {
			c.LastMsgAt, c.LastMsgBy = m.CreatedAt, string(m.Author)
		}
		cards = append(cards, c)
	}
	return cards, nil
}

// GetTask returns the task with its comments and runs.
func (a *App) GetTask(key string) (TaskDetail, error) {
	t, err := store.GetTaskByKey(a.svc.DB, key)
	if err != nil {
		return TaskDetail{}, err
	}
	cs, err := store.ListComments(a.svc.DB, t.ID)
	if err != nil {
		return TaskDetail{}, err
	}
	rs, err := store.ListRuns(a.svc.DB, t.ID)
	if err != nil {
		return TaskDetail{}, err
	}
	if cs == nil {
		cs = []store.Comment{}
	}
	if rs == nil {
		rs = []store.Run{}
	}
	rel, err := store.ListRelations(a.svc.DB, t)
	if err != nil {
		return TaskDetail{}, err
	}
	by := creator(t, rel.Parent, store.GetSetting(a.svc.DB, "user_name"))
	return TaskDetail{Task: t, Comments: cs, Runs: rs, Relations: rel, Creator: by}, nil
}

// Link records "from <kind> to" (blocks, relates).
func (a *App) Link(from, kind, to string) error { return a.svc.Link(from, kind, to) }

// Unlink removes "from <kind> to".
func (a *App) Unlink(from, kind, to string) error { return a.svc.Unlink(from, kind, to) }

// AddLabel puts a label (created if new) on a task.
func (a *App) AddLabel(key, name string) error { return a.svc.AddLabel(key, name) }

// RemoveLabel takes a label off a task.
func (a *App) RemoveLabel(key, name string) error { return a.svc.RemoveLabel(key, name) }

// SetLabel atomically sets key's prefix label to value ("epic_kind", "build" -> "epic_kind:build"),
// dropping its other labels with that prefix. Mirrors `task label set`.
func (a *App) SetLabel(key, prefix, value string) error {
	return a.svc.SetLabel(key, strings.TrimSuffix(prefix, ":")+":"+value)
}

// ListLabels returns every known label name.
func (a *App) ListLabels() ([]string, error) { return store.ListLabels(a.svc.DB) }

// AdoptChild makes child a sub-task of parent; errors if child already has another parent.
func (a *App) AdoptChild(parent, child string) error { return a.svc.AdoptChild(parent, child) }

// SetParent makes parent (empty: none) the parent of the task; keys don't change.
func (a *App) SetParent(key, parent string) error { return a.svc.SetParent(key, parent) }

// CreateTask creates a task and returns its key.
func (a *App) CreateTask(in core.NewTaskInput) (string, error) {
	in.AgentSet = true // the dialog prefills the parent's agent; a cleared field means none
	in.RunnerSet = true
	t, err := a.svc.CreateTask(in)
	return t.Key, err
}

// SetTaskRunner changes the task's runner override ("" clears it).
func (a *App) SetTaskRunner(key, runner string) error { return a.svc.SetTaskRunner(key, runner) }

// CLIAgent represents a detected CLI runner.
type CLIAgent = runner.CLIAgent

// ListCLIAgents lists detected CLI runners on the system.
func (a *App) ListCLIAgents() []CLIAgent { return runner.DetectRunners() }

// EpicGraph returns the task tree around key with its links and the epic flow's PRs.
func (a *App) EpicGraph(key string) (core.Graph, error) { return a.svc.TaskGraph(key) }

// EpicApprove passes the epic's plan gate.
func (a *App) EpicApprove(key string) error { return a.svc.EpicApprove(key) }

// EpicRestart abandons the epic's flow, cancels its open sub-tasks and starts the next flow.
func (a *App) EpicRestart(key, reason string) error { return a.svc.EpicRestart(key, reason) }

// SetTaskSkills replaces the task's skill:* labels.
func (a *App) SetTaskSkills(key string, skills []string) error {
	return a.svc.SetTaskSkills(key, skills)
}

// AddComment posts a human comment; after ("", "ask" or "note") applies once a PR of the task is merged.
func (a *App) AddComment(key, body, after string) error {
	_, err := a.svc.AddComment(key, store.AuthorHuman, body, core.AfterMerge(after))
	return err
}

// CheckReviews schedules a PR check (reviews, CI, conflicts) for the task right now (the "Check PR" button).
func (a *App) CheckReviews(key string) error {
	t, err := store.GetTaskByKey(a.svc.DB, key)
	if err != nil {
		return err
	}
	return gh.ScheduleCheck(a.svc.DB, t, 0, 0)
}

// TakeQueuedComment pulls a still-queued human comment out of the thread and returns its
// body for re-editing; it errors if the agent already received it.
func (a *App) TakeQueuedComment(key string, id int64) (string, error) {
	t, err := store.GetTaskByKey(a.svc.DB, key)
	if err != nil {
		return "", err
	}
	body, ok, err := store.TakeQueuedComment(a.svc.DB, t.ID, id)
	if err == nil && !ok {
		err = fmt.Errorf("the agent already received this message")
	}
	if err == nil {
		err = store.ReleaseIfNothingQueued(a.svc.DB, t.ID)
	}
	return body, err
}

// SendQueuedNow releases the task's queued messages past the debounce; it errors if the agent already received them.
func (a *App) SendQueuedNow(key string) error {
	t, err := store.GetTaskByKey(a.svc.DB, key)
	if err != nil {
		return err
	}
	n, err := store.SendQueuedNow(a.svc.DB, t.ID)
	if err == nil && n == 0 {
		err = fmt.Errorf("the agent already received this message")
	}
	return err
}

// Stop stops the task's running session and moves the task to needs_you.
func (a *App) Stop(key string) error { return a.svc.Stop(key) }

// MergedPR is the merged PR a comment on the task would trip over, or "".
func (a *App) MergedPR(key string) (string, error) { return a.svc.MergedPR(key) }

// Move changes a task's status as a human. When the move would end the task's
// running runs and stop is false, nothing moves and it returns how many, so the
// UI can confirm and call again with stop.
func (a *App) Move(key, status string, stop bool) (int, error) {
	err := a.svc.Move(key, store.Status(status), stop)
	var ar *core.ActiveRunsError
	if errors.As(err, &ar) {
		return ar.N, nil
	}
	return 0, err
}

// SetTaskRepo sets a task repo's branch (before the worktree exists) and PR URL.
func (a *App) SetTaskRepo(key, repo, branch, prURL string) error {
	return a.svc.SetTaskRepo(key, repo, branch, prURL)
}

// Ready blocks until Startup has finished. Wails runs Startup concurrently with the page load, and a binding
// called before a.svc is set panics inside Wails and never answers, so the frontend waits on this first.
func (a *App) Ready() { <-a.ready }

// ListRepos returns the registered repos.
func (a *App) ListRepos() ([]store.Repo, error) {
	defer trace("ListRepos")()
	r, err := store.ListRepos(a.svc.DB)
	if r == nil {
		r = []store.Repo{}
	}
	return r, err
}

// AddRepo registers a repo, with the same validation as the CLI.
func (a *App) AddRepo(path, name string) error {
	_, err := a.svc.AddRepo(path, name)
	return err
}

// SetRepoSetupScript sets the script run in each new worktree of the repo.
func (a *App) SetRepoSetupScript(name, script string) error {
	return store.SetRepoSetupScript(a.svc.DB, name, script)
}

// GetSettings returns every setting with defaults applied, except the Slack thread links
// (see SlackLinks).
func (a *App) GetSettings() (map[string]string, error) {
	defer trace("GetSettings")()
	return slack.StripLinks(store.ListSettings(a.svc.DB)), nil
}

// SlackLinkPage is one page of Slack thread links and the total count.
type SlackLinkPage struct {
	Rows  []slack.Link `json:"rows"`
	Total int          `json:"total"`
}

// SlackLinks returns a page of the Slack thread<->task links (state kept in settings).
func (a *App) SlackLinks(offset, size int) (SlackLinkPage, error) {
	rows, total := slack.Links(store.ListSettings(a.svc.DB), offset, size)
	return SlackLinkPage{rows, total}, nil
}

// SetSetting stores one setting.
func (a *App) SetSetting(k, v string) error { return store.SetSetting(a.svc.DB, k, v) }

// ExportState saves the portable state (no repos) to a file the user picks and returns its path ("" if cancelled).
func (a *App) ExportState() (string, error) {
	st, err := a.svc.ExportState()
	if err != nil {
		return "", err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Export state", DefaultFilename: "conveyor-state.json"})
	if err != nil || path == "" {
		return "", err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(b, '\n'), 0o600)
}

// ImportState adds what is missing from a state file the user picks and returns the report lines.
// ponytail: no repo mapping, so automations with repo placeholders import disabled; add a picker if that bites.
func (a *App) ImportState() ([]string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import state", Filters: []runtime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st core.State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return a.svc.ImportState(st, nil)
}

// Actions lists the terminal actions for a task's workspace.
func (a *App) Actions(key string) ([]core.Action, error) {
	r, err := a.svc.Actions(key)
	if err != nil {
		log.Printf("actions %s: %v", key, err)
	}
	return r, err
}

// RunAction opens an action. Kind "resume:<id>" resumes a manual session.
func (a *App) RunAction(key, kind, repo string) error { return a.svc.RunAction(key, kind, repo) }

// Sessions lists manual Claude sessions in the task's worktrees.
func (a *App) Sessions(key string) ([]core.Session, error) {
	r, err := a.svc.Sessions(key)
	if err != nil {
		log.Printf("sessions %s: %v", key, err)
	}
	return r, err
}

// RunLog returns the run's session chat entries after byte offset after.
func (a *App) RunLog(runID, after int64) (core.RunLog, error) { return a.svc.RunLog(runID, after) }

// AppLog returns a log (source: app, dispatcher or hooks) from offset after; pass -1 for the recent tail.
func (a *App) AppLog(source string, after int64) (core.AppLog, error) {
	return core.ReadAppLog(a.svc.Home, source, after)
}

// EnsureWorkspace creates the task's worktrees (workspace.Ensure).
func (a *App) EnsureWorkspace(key string) error {
	t, err := store.GetTaskByKey(a.svc.DB, key)
	if err != nil {
		return err
	}
	_, err = workspace.Ensure(a.svc.DB, a.svc.Home, t)
	return err
}

// ListAgents lists the Claude subagents of the given repos plus the user's.
func (a *App) ListAgents(repos []string) ([]core.Agent, error) {
	defer trace("ListAgents")()
	return a.svc.ListAgents(repos)
}

// Complete returns the editor's @file or /command suggestions for query q:
// the task's repos when key is set, else the named repos.
func (a *App) Complete(key string, repos []string, kind, q string) ([]core.Suggestion, error) {
	return a.svc.Complete(key, repos, kind, q)
}

// FileOpeners lists the apps a task's @file reference can open in; errors when it is not a file.
func (a *App) FileOpeners(key, ref string) ([]string, error) { return a.svc.FileOpeners(key, ref) }

// OpenFile opens a task's @file reference in app.
func (a *App) OpenFile(key, ref, app string) error { return a.svc.OpenFile(key, ref, app) }

// AddAgent creates an agent file in the repo's main checkout (scope = repo name) or ~/.claude/agents ("user").
func (a *App) AddAgent(name, description, prompt, scope, model string) error {
	return a.svc.AddAgent(name, description, prompt, scope, model)
}

// SetAgentModel sets the agent's default model in its file ("" removes it).
func (a *App) SetAgentModel(scope, name, model string) error {
	return a.svc.SetAgentModel(scope, name, model)
}

// ListModels returns the models the pickers offer for a runner ("" is the default runner).
func (a *App) ListModels(runner string) core.ModelList { return a.svc.ModelsFor(runner) }

// ListSkills lists the skills the cards' skill pickers offer for the given repos.
func (a *App) ListSkills(repos []string) []core.Command { return a.svc.ListSkills(repos) }

// SetTaskAgent changes the task's agent ("" clears it).
func (a *App) SetTaskAgent(key, agent string) error { return a.svc.SetTaskAgent(key, agent) }

// SetTaskModel changes the task's model override ("" clears it).
func (a *App) SetTaskModel(key, model string) error { return a.svc.SetTaskModel(key, model) }

// SetTaskDescription replaces the task's description (see core.Service.SetTaskDescription).
func (a *App) SetTaskDescription(key, desc string) error { return a.svc.SetTaskDescription(key, desc) }

// LearningRow is a learning with its repo name ("" = personal).
type LearningRow struct {
	store.Learning
	Repo string `json:"repo"`
}

// Learnings returns proposed and accepted learnings.
func (a *App) Learnings() ([]LearningRow, error) {
	ls, err := learning.List(a.svc.DB, store.LearningProposed, store.LearningAccepted)
	if err != nil {
		return nil, err
	}
	names, err := store.RepoMap(a.svc.DB, func(r store.Repo) string { return r.Name })
	if err != nil {
		return nil, err
	}
	out := make([]LearningRow, 0, len(ls))
	for _, l := range ls {
		row := LearningRow{Learning: l}
		if l.RepoID != nil {
			row.Repo = names[*l.RepoID]
		}
		if row.Target == nil || *row.Target == "" {
			t := learning.TargetForRepo(a.svc.DB, l.RepoID)
			row.Target = &t
		}
		out = append(out, row)
	}
	return out, nil
}

// AcceptLearning accepts a learning for target (default: Conveyor's local LEARNINGS.md for the repo).
func (a *App) AcceptLearning(id int64, target string) error {
	return learning.Accept(a.svc.DB, a.svc.Home, id, target)
}

// RejectLearning rejects a learning.
func (a *App) RejectLearning(id int64) error { return learning.Reject(a.svc.DB, id) }

// RejectSimilarLearnings rejects this learning and every proposed one sharing its first 40 characters; returns the count.
func (a *App) RejectSimilarLearnings(id int64) (int, error) {
	return store.RejectSamePrefix(a.svc.DB, id)
}

// RejectLearningCategory bulk-rejects proposed learnings in a junk category ("placeholder", "personal-env"); returns the count.
func (a *App) RejectLearningCategory(category string) (int, error) {
	return store.RejectCategory(a.svc.DB, category)
}

// EditLearning replaces a learning's body.
func (a *App) EditLearning(id int64, body string) error { return learning.Edit(a.svc.DB, id, body) }

// ApplyLearnings creates the "Apply learnings" task for repo and returns its key (none: "").
func (a *App) ApplyLearnings(repo string) (string, error) {
	keys, err := learning.Apply(a.svc, repo)
	if err != nil || len(keys) == 0 {
		return "", err
	}
	return keys[0], nil
}

// AddTaskRepo adds a registered repo to the task.
func (a *App) AddTaskRepo(key, repo string) error { return a.svc.AddTaskRepo(key, repo) }

// RemoveTaskRepo removes a repo from the task unless it has a PR.
func (a *App) RemoveTaskRepo(key, repo string) error { return a.svc.RemoveTaskRepo(key, repo) }
