// Package ui holds the Wails bindings: a thin layer over core and store.
package ui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// pollEvery is the data_version poll interval (tech-spec §2).
const pollEvery = 700 * time.Millisecond

// App is bound to the frontend. Methods return plain JSON-friendly structs.
type App struct {
	svc    *core.Service
	cancel context.CancelFunc
}

// NewApp returns an App; the DB is opened in Startup.
func NewApp() *App { return &App{} }

// Startup opens the DB under CONVEYOR_HOME (default ~/.conveyor), starts the
// change poller and the dispatcher hook. It matches Wails' OnStartup.
func (a *App) Startup(ctx context.Context) {
	home, err := core.Home("")
	if err != nil {
		runtime.LogFatalf(ctx, "conveyor home: %v", err)
		return
	}
	db, err := store.Open(core.DBPath(home))
	if err != nil {
		runtime.LogFatalf(ctx, "open db: %v", err)
		return
	}
	a.svc = &core.Service{DB: db, Home: home}
	ctx, a.cancel = context.WithCancel(ctx)
	go pollDataVersion(ctx, core.DBPath(home), pollEvery, func() { runtime.EventsEmit(ctx, "db:changed") })
	startDispatcher(ctx)
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

// startDispatcher is the hook point for the dispatcher (task 007).
// It is a no-op until then.
func startDispatcher(ctx context.Context) {}

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
