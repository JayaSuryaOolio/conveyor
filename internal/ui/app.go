// Package ui holds the Wails bindings: a thin layer over core and store.
package ui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/automation"
	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/dispatch"
	"github.com/JayaSuryaOolio/conveyor/internal/learning"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// pollEvery is the data_version poll interval (tech-spec §2).
const pollEvery = 700 * time.Millisecond

// App is bound to the frontend. Methods return plain JSON-friendly structs.
type App struct {
	ctx        context.Context // Wails' context, for native dialogs
	svc        *core.Service
	cancel     context.CancelFunc
	dispatcher atomic.Bool // true while this process holds the dispatcher lock
}

// NewApp returns an App; the DB is opened in Startup.
func NewApp() *App { return &App{} }

// Startup opens the DB under CONVEYOR_HOME (default ~/.conveyor), starts the
// change poller and the dispatcher hook. It matches Wails' OnStartup.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	home, err := core.Home("")
	if err != nil {
		runtime.LogFatalf(ctx, "conveyor home: %v", err)
		return
	}
	// A Finder-launched app's stderr goes nowhere; keep a log file in the home too.
	if f, err := openLog(core.LogPath(home)); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}
	pe := filepath.SplitList(os.Getenv("PATH"))
	log.Printf("app start: home=%s cli=%s PATH entries=%d first=%v", home, conveyorBin(), len(pe), pe[:min(len(pe), 5)])
	db, err := store.Open(core.DBPath(home))
	if err != nil {
		runtime.LogFatalf(ctx, "open db: %v", err)
		return
	}
	a.svc = &core.Service{DB: db, Home: home, Bin: conveyorBin()}
	automation.Bin = conveyorBin() // the cli automation kind runs the CLI, not the app binary
	ctx, a.cancel = context.WithCancel(ctx)
	go pollDataVersion(ctx, core.DBPath(home), pollEvery, func() { runtime.EventsEmit(ctx, "db:changed") })
	go a.startDispatcher(ctx)
}

// Shutdown stops background work and closes the DB.
func (a *App) Shutdown(context.Context) {
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
	d := &dispatch.Dispatcher{DB: db, Home: a.svc.Home, ConveyorBin: a.svc.Bin, Log: log.New(log.Writer(), "dispatcher: ", log.LstdFlags)}
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
	r, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Conveyor", Message: msg})
	return r == "Yes", err
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
	Repos       []string `json:"repos"`
	LastSummary string   `json:"lastSummary"`
	CtxPeak     int64    `json:"ctxPeak"`
	UpdatedAt   string   `json:"updatedAt"`
}

// TaskDetail is the task page payload; Runs are oldest first (newest last).
type TaskDetail struct {
	Task     store.Task      `json:"task"`
	Comments []store.Comment `json:"comments"`
	Runs     []store.Run     `json:"runs"`
}

// ListTasks returns every task as a card.
func (a *App) ListTasks() ([]TaskCard, error) {
	tasks, err := store.ListTasks(a.svc.DB)
	if err != nil {
		return nil, err
	}
	cards := make([]TaskCard, 0, len(tasks))
	for _, t := range tasks {
		c := TaskCard{Key: t.Key, Title: t.Title, Status: string(t.Status), Priority: t.Priority, Repos: []string{}, UpdatedAt: t.UpdatedAt}
		for _, r := range t.Repos {
			c.Repos = append(c.Repos, r.RepoName)
		}
		switch r, err := store.LatestRun(a.svc.DB, t.ID); {
		case err == nil:
			c.CtxPeak = r.CtxPeak
			if r.Summary != nil {
				c.LastSummary = *r.Summary
			}
		case !errors.Is(err, sql.ErrNoRows):
			return nil, err
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
	return TaskDetail{Task: t, Comments: cs, Runs: rs}, nil
}

// CreateTask creates a task and returns its key.
func (a *App) CreateTask(in core.NewTaskInput) (string, error) {
	t, err := a.svc.CreateTask(in)
	return t.Key, err
}

// AddComment posts a human comment.
func (a *App) AddComment(key, body string) error {
	if body == "" {
		return fmt.Errorf("comment is empty")
	}
	_, err := a.svc.AddComment(key, store.AuthorHuman, body)
	return err
}

// Move changes a task's status as a human.
func (a *App) Move(key, status string) error { return a.svc.Move(key, store.Status(status)) }

// Cancel cancels a task.
func (a *App) Cancel(key string) error { return a.svc.Cancel(key) }

// SetTaskRepo sets a task repo's branch (before the worktree exists) and PR URL.
func (a *App) SetTaskRepo(key, repo, branch, prURL string) error {
	return a.svc.SetTaskRepo(key, repo, branch, prURL)
}

// ListRepos returns the registered repos.
func (a *App) ListRepos() ([]store.Repo, error) {
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

// GetSettings returns every setting with defaults applied.
func (a *App) GetSettings() (map[string]string, error) { return store.ListSettings(a.svc.DB), nil }

// SetSetting stores one setting.
func (a *App) SetSetting(k, v string) error { return store.SetSetting(a.svc.DB, k, v) }

// Actions lists the terminal/editor actions for a task's workspace.
func (a *App) Actions(key string) ([]core.Action, error) { return a.svc.Actions(key) }

// RunAction opens an action. Kind "resume:<id>" resumes a manual session.
func (a *App) RunAction(key, kind, repo string) error { return a.svc.RunAction(key, kind, repo) }

// Sessions lists manual Claude sessions in the task's worktrees.
func (a *App) Sessions(key string) ([]core.Session, error) { return a.svc.Sessions(key) }

// RunLog returns the run's session chat entries after byte offset after.
func (a *App) RunLog(runID, after int64) (core.RunLog, error) { return a.svc.RunLog(runID, after) }

// AppLog returns the app log from offset after; pass -1 for the recent tail.
func (a *App) AppLog(after int64) (core.AppLog, error) { return core.ReadAppLog(a.svc.Home, after) }

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
func (a *App) ListAgents(repos []string) ([]core.Agent, error) { return a.svc.ListAgents(repos) }

// AddAgent creates an agent file in the repo's main checkout (scope = repo name) or ~/.claude/agents ("user").
func (a *App) AddAgent(name, description, prompt, scope string) error {
	return a.svc.AddAgent(name, description, prompt, scope)
}

// SetTaskAgent changes the task's agent ("" clears it).
func (a *App) SetTaskAgent(key, agent string) error { return a.svc.SetTaskAgent(key, agent) }

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
	repos, err := store.ListRepos(a.svc.DB)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, r := range repos {
		names[r.ID] = r.Name
	}
	out := make([]LearningRow, 0, len(ls))
	for _, l := range ls {
		row := LearningRow{Learning: l}
		if l.RepoID != nil {
			row.Repo = names[*l.RepoID]
		}
		out = append(out, row)
	}
	return out, nil
}

// AcceptLearning accepts a learning for target (default CLAUDE.md).
func (a *App) AcceptLearning(id int64, target string) error {
	return learning.Accept(a.svc.DB, id, target)
}

// RejectLearning rejects a learning.
func (a *App) RejectLearning(id int64) error { return learning.Reject(a.svc.DB, id) }

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

// openLog appends to path, first moving it to path.1 once it passes maxLog.
// ponytail: rotates only at startup; rotate on write if a single session ever logs that much.
func openLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLog {
		os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

const maxLog = 5 << 20
