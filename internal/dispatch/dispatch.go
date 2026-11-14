// Package dispatch turns todo tasks into background Claude runs (tech-spec §2, §7).
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/agy"
	"github.com/JayaSuryaOolio/conveyor/internal/automation"
	"github.com/JayaSuryaOolio/conveyor/internal/brief"
	"github.com/JayaSuryaOolio/conveyor/internal/chat"
	"github.com/JayaSuryaOolio/conveyor/internal/claude"
	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/gh"
	"github.com/JayaSuryaOolio/conveyor/internal/hook"
	"github.com/JayaSuryaOolio/conveyor/internal/jira"
	"github.com/JayaSuryaOolio/conveyor/internal/runner"
	"github.com/JayaSuryaOolio/conveyor/internal/slack"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

const (
	tickEvery  = 2 * time.Second
	reapGrace  = 30 * time.Second
	prPollEach = 60 * time.Second
	autoEach   = 30 * time.Second
)

// Dispatcher launches and supervises auto runs. Run it from one process at a time (see Lock).
type Dispatcher struct {
	Svc *core.Service    // DB, home and CLI binary; every dispatcher call goes through this one
	Now func() time.Time // injectable; defaults to time.Now
	Log *log.Logger      // defaults to log.Default()

	lastPoll time.Time
	lastAuto time.Time
	lastHook time.Time
	autoBusy atomic.Bool // an automation pass is in flight (prechecks can take a minute)
}

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Dispatcher) logf(format string, a ...any) {
	l := d.Log
	if l == nil {
		l = log.Default()
	}
	l.Printf(format, a...)
}

func (d *Dispatcher) intSetting(key string) int {
	n, err := strconv.Atoi(store.GetSetting(d.Svc.DB, key))
	if err != nil {
		n, _ = strconv.Atoi(store.Defaults[key])
	}
	return n
}

// pollJira runs the Jira source poller; a var so tests can see the dispatcher start it.
var pollJira = func(ctx context.Context, svc *core.Service) { (&jira.Poller{}).Run(ctx, svc) }

// Run ticks every 2s until ctx is done. It also starts the chat bridge and the Jira poller,
// so they run once, under the dispatcher lock, in the app and in `conveyor daemon` alike.
func (d *Dispatcher) Run(ctx context.Context) error {
	go chat.Run(ctx, d.Svc, d.logf, map[string]chat.Factory{"slack": slack.New})
	go pollJira(ctx, d.Svc)
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		if err := d.Tick(ctx); err != nil && ctx.Err() == nil {
			d.logf("tick: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Tick does one pass: reap, PR poll, launch.
func (d *Dispatcher) Tick(ctx context.Context) error {
	if err := d.reap(ctx); err != nil {
		d.logf("reap: %v", err)
	}
	d.pollPRs(ctx)
	d.ensureWebhook()
	d.tickAutomations(ctx)
	d.wakeEpics()
	return d.launchTodo(ctx)
}

// wakeEpics requeues epic roots whose sub-tasks have all settled (docs/epic-builder.md §9).
// It only wakes: restarting a flow is always a human decision.
func (d *Dispatcher) wakeEpics() {
	epics, err := store.EpicsToWake(d.Svc.DB)
	if err != nil {
		d.logf("wake epics: %v", err)
		return
	}
	for _, e := range epics {
		stage := store.EpicStage(e)
		if _, err := d.Svc.AddComment(e.Key, store.AuthorSystem, fmt.Sprintf("all sub-tasks settled (stage %s)", stage), core.Ask); err != nil {
			d.logf("wake %s: %v", e.Key, err)
			continue
		}
		store.Audit("epic.wake", "task_id", e.ID, "stage", stage)
	}
}

// tickAutomations runs due automations every autoEach, off the main loop so a slow precheck never stalls launches.
func (d *Dispatcher) tickAutomations(ctx context.Context) {
	if !d.lastAuto.IsZero() && d.now().Sub(d.lastAuto) < autoEach || !d.autoBusy.CompareAndSwap(false, true) {
		return
	}
	d.lastAuto = d.now()
	now := d.now()
	go func() {
		defer d.autoBusy.Store(false)
		if err := automation.Tick(ctx, d.Svc, now); err != nil {
			d.logf("automations: %v", err)
		}
	}()
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func ptr(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// reap fails active auto runs whose background session no longer exists, or that never got one,
// and finishes interactive runs whose session is gone from the agents list.
func (d *Dispatcher) reap(ctx context.Context) error {
	runs, err := store.ActiveRuns(d.Svc.DB)
	if err != nil {
		return err
	}
	var cand []store.Run
	needClaudeAgents := false
	for _, r := range runs {
		task, _ := store.GetTask(d.Svc.DB, r.TaskID)
		bgRunner := d.runnerForRun(r, task)
		if r.Kind == "interactive" {
			if r.SessionID != nil { // without a session id there is nothing to match
				if bgRunner.Name() == "claude" {
					needClaudeAgents = true
				}
				cand = append(cand, r)
			}
		} else if r.BgID != nil {
			if bgRunner.Name() == "claude" {
				needClaudeAgents = true
			}
			cand = append(cand, r)
		} else if r.Status == "launching" { // dispatcher died before saving the bg id
			cand = append(cand, r)
		}
	}
	if len(cand) == 0 {
		return nil
	}
	alive, sessions := map[string]claude.Agent{}, map[string]bool{}
	if needClaudeAgents {
		// An error must abort the reap: an empty alive map would fail every live Claude run as vanished.
		agents, err := claude.Agents(ctx)
		if err != nil {
			return err
		}
		for _, a := range agents {
			alive[a.ID] = a
			sessions[a.SessionID] = true
		}
	}
	for _, r := range cand {
		if r.Kind == "interactive" {
			if err := d.reapInteractive(r, sessions[*r.SessionID]); err != nil {
				return err
			}
			continue
		}

		// Determine which backend this run uses.
		task, taskErr := store.GetTask(d.Svc.DB, r.TaskID)
		bgRunner := d.runnerForRun(r, task)

		if bgRunner.Name() == "agy" {
			if err := d.reapAgy(ctx, r, task, taskErr); err != nil {
				return err
			}
			continue
		}
		a, ok := alive[deref(r.BgID)]
		ok = ok && r.BgID != nil
		// A session that errored at startup (e.g. an unknown model) stays listed as failed and never ends.
		failed := ok && a.State == "failed"
		// The agents list knows the bg session's real id: adopt it when no hook fired yet, and follow it when
		// a resume ran as a copy under a new id, so the run's later hooks (its Stop) still find it.
		if ok && !failed && a.SessionID != "" && (r.Status == "launching" || deref(r.SessionID) != a.SessionID) {
			r.SessionID, r.Status = &a.SessionID, "running"
			if err := store.UpdateRun(d.Svc.DB, r); err != nil {
				return err
			}
			continue
		}
		if !failed && (ok || d.now().Sub(parseTime(r.StartedAt)) <= reapGrace) {
			continue
		}
		reason, note := "session vanished", "The Claude session vanished before finishing."
		if failed {
			reason, note = "session failed", "The Claude session failed to start or run. A common cause is an unknown model: check the task's model, its agent's model and the default_model setting (`claude logs "+deref(r.BgID)+"` shows the error), fix it, then comment to retry."
		} else if r.BgID == nil {
			reason, note = "launch interrupted", "The Claude launch was interrupted before a session started."
		}
		d.logf("reap run %d (task %d, bg %s, session %s, status %s): %s", r.ID, r.TaskID, deref(r.BgID), deref(r.SessionID), r.Status, reason)
		ended := d.now().UTC().Format(time.RFC3339)
		r.Status, r.Error, r.EndedAt = "failed", ptr(reason), &ended
		if err := store.UpdateRun(d.Svc.DB, r); err != nil {
			return err
		}
		task, err := store.GetTask(d.Svc.DB, r.TaskID)
		if err != nil {
			return err
		}
		if task.Status != store.StatusRunning {
			continue
		}
		if _, err := store.AddComment(d.Svc.DB, store.Comment{TaskID: task.ID, Author: store.AuthorSystem, Body: note}); err != nil {
			return err
		}
		if _, err := store.UpdateTaskStatus(d.Svc.DB, task.ID, store.StatusNeedsYou, store.ActorSystem); err != nil {
			return err
		}
	}
	return nil
}

// reapAgy handles reaping for agy-backed runs (PID-based liveness check).
func (d *Dispatcher) reapAgy(ctx context.Context, r store.Run, task store.Task, taskErr error) error {
	if r.BgID == nil {
		if r.Status == "launching" {
			if d.now().Sub(parseTime(r.StartedAt)) <= reapGrace {
				return nil
			}
			ended := d.now().UTC().Format(time.RFC3339)
			r.Status, r.Error, r.EndedAt = "failed", ptr("launch interrupted"), &ended
			if err := store.UpdateRun(d.Svc.DB, r); err != nil {
				return err
			}
		}
		return nil
	}
	// If the PID is still alive (or within the grace period), leave it.
	if runner.IsPIDRunning(*r.BgID) {
		updated := false
		if (r.SessionID == nil || *r.SessionID == "") && r.BgID != nil {
			if pid, err := strconv.Atoi(*r.BgID); err == nil {
				if convID := runner.DetectConversationID(pid); convID != "" {
					r.SessionID = &convID
					updated = true
				}
			}
		}
		// Still running: if the run was "launching", promote to "running".
		if r.Status == "launching" {
			r.Status = "running"
			updated = true
		}
		if updated {
			return store.UpdateRun(d.Svc.DB, r)
		}
		return nil
	}
	// PID is gone: if within grace, wait longer; else the run finished.
	if d.now().Sub(parseTime(r.StartedAt)) <= reapGrace {
		return nil
	}
	// The process exited. If already transitioned (e.g. by a Stop hook), nothing to do.
	if r.Status != "launching" && r.Status != "running" {
		return nil
	}
	if taskErr != nil {
		ended := d.now().UTC().Format(time.RFC3339)
		r.Status, r.EndedAt = "finished", &ended
		return store.UpdateRun(d.Svc.DB, r)
	}

	// Read the session log if present to extract the assistant response / <conveyor> status block.
	logPath := runner.BgLogPath(d.Svc.Home, task.Key)
	var logOutput string
	if b, err := os.ReadFile(logPath); err == nil {
		logOutput = agy.LastTurnOutput(string(b))
	}

	tail := logOutput
	if len(tail) > 300 {
		tail = tail[len(tail)-300:]
	}
	interrupted := strings.Contains(tail, "error: interrupted") || strings.Contains(tail, "signal: interrupt")

	// If there is log output and not interrupted, run ProcessStop which extracts the <conveyor> block and handles transitions.
	if logOutput != "" && !interrupted {
		if err := hook.ProcessStop(d.Svc.DB, task, r, logOutput, ""); err == nil {
			return nil
		}
	}

	ended := d.now().UTC().Format(time.RFC3339)
	r.Status, r.EndedAt = "finished", &ended
	if logOutput != "" {
		trimmed := strings.TrimSpace(logOutput)
		r.FinalMessage = &trimmed
	}
	if err := store.UpdateRun(d.Svc.DB, r); err != nil {
		return err
	}
	if task.Status != store.StatusRunning {
		return nil
	}
	note := "The agy session exited without a status block."
	if interrupted {
		note = "The agy session was interrupted."
	}
	if _, err := store.AddComment(d.Svc.DB, store.Comment{TaskID: task.ID, Author: store.AuthorSystem, Body: note}); err != nil {
		return err
	}
	_, err := store.UpdateTaskStatus(d.Svc.DB, task.ID, store.StatusNeedsYou, store.ActorSystem)
	return err
}

// reapInteractive finishes a manual session's run once its session is gone (terminal closed, process
// killed). The user ended it, so the run is finished, not failed. A running task then goes to needs_you
// (the same edge as a vanished auto run): nothing says the work is done, so it is the human's turn.
func (d *Dispatcher) reapInteractive(r store.Run, alive bool) error {
	if alive || d.now().Sub(parseTime(r.StartedAt)) <= reapGrace {
		return nil
	}
	ended := d.now().UTC().Format(time.RFC3339)
	d.logf("reap interactive run %d (task %d, session %s): session ended", r.ID, r.TaskID, deref(r.SessionID))
	r.Status, r.EndedAt = "finished", &ended
	if err := store.UpdateRun(d.Svc.DB, r); err != nil {
		return err
	}
	task, err := store.GetTask(d.Svc.DB, r.TaskID)
	if err != nil || task.Status != store.StatusRunning {
		return err
	}
	if active, err := store.ActiveRuns(d.Svc.DB); err != nil {
		return err
	} else {
		for _, o := range active {
			if o.TaskID == task.ID { // another session still works the task
				return nil
			}
		}
	}
	if _, err := store.AddComment(d.Svc.DB, store.Comment{TaskID: task.ID, Author: store.AuthorSystem, Body: "The manual Claude session ended."}); err != nil {
		return err
	}
	_, err = store.UpdateTaskStatus(d.Svc.DB, task.ID, store.StatusNeedsYou, store.ActorSystem)
	return err
}

// pollPRs is the fallback for the Stop hook's per-task refresh (hook.Handle) and covers PRs merged elsewhere.
func (d *Dispatcher) pollPRs(ctx context.Context) {
	if !d.lastPoll.IsZero() && d.now().Sub(d.lastPoll) < prPollEach {
		return
	}
	d.lastPoll = d.now()
	tasks, err := store.ListTasks(d.Svc.DB, store.StatusRunning, store.StatusNeedsYou, store.StatusReview)
	if err != nil {
		d.logf("pr poll: %v", err)
		return
	}
	for _, t := range tasks {
		if err := gh.SyncTask(ctx, d.Svc.DB, t); err != nil {
			d.logf("pr poll %s: %v", t.Key, err)
		}
		if got, err := store.GetTask(d.Svc.DB, t.ID); err == nil && got.Status == store.StatusDone {
			d.Svc.CleanupSessions(t.ID)
		}
	}
}

// ensureWebhook keeps services running: every enabled app's autostart service, and the built-in
// review-webhook listener (under the installed app's id) while an epic is open (no-ops when running; without the gh extension tasks keep backoff checks).
func (d *Dispatcher) ensureWebhook() {
	if d.Svc.Bin == "" || !d.lastHook.IsZero() && d.now().Sub(d.lastHook) < time.Minute {
		return
	}
	d.lastHook = d.now()
	if err := d.Svc.EnsureServices(); err != nil {
		d.logf("app services: %v", err)
	}
	if !d.Svc.HasOpenEpic() {
		return
	}
	args := []string{"--home", d.Svc.Home, "review-webhook"}
	if id := d.Svc.ReviewWebhookAppID(); id != "" { // share the installed app's instance
		args = append(args, "--app", id)
	}
	if out, err := exec.Command(d.Svc.Bin, args...).CombinedOutput(); err != nil {
		d.logf("review-webhook: %v: %s", err, out)
	}
}

func (d *Dispatcher) launchTodo(ctx context.Context) error {
	runs, err := store.ActiveRuns(d.Svc.DB)
	if err != nil {
		return err
	}
	busy, auto := map[int64]bool{}, 0
	for _, r := range runs {
		busy[r.TaskID] = true
		if r.Kind == "auto" {
			auto++
		}
	}
	tasks, err := store.ListTasks(d.Svc.DB, store.StatusTodo) // priority desc
	if err != nil {
		return err
	}
	max := d.intSetting("max_parallel")
	debounce := time.Duration(d.intSetting("comment_debounce_secs")) * time.Second
	for _, t := range tasks {
		if auto >= max || ctx.Err() != nil {
			break
		}
		if busy[t.ID] {
			continue
		}
		if store.IsEpic(t) { // an epic waits while its sub-tasks run (R6)
			if wait, err := store.EpicHasActiveChildren(d.Svc.DB, t); err != nil {
				return err
			} else if wait {
				continue
			}
		}
		if blockers, err := store.OpenBlockers(d.Svc.DB, t.ID); err != nil {
			return err
		} else if len(blockers) > 0 {
			continue
		}
		pending, err := store.UnconsumedComments(d.Svc.DB, t.ID)
		if err != nil {
			return err
		}
		if n := len(pending); n > 0 && d.now().Sub(parseTime(pending[n-1].CreatedAt)) < debounce {
			continue
		}
		run, err := d.start(ctx, t, pending)
		if err != nil {
			d.logf("launch %s: %v", t.Key, err)
			if ferr := d.fail(t, run, err); ferr != nil {
				d.logf("record failure %s: %v", t.Key, ferr)
			}
			continue
		}
		auto++
	}
	return nil
}

// start launches one run. On error the returned run is the launching row if one was created.
func (d *Dispatcher) start(ctx context.Context, task store.Task, delta []store.Comment) (store.Run, error) {
	task, err := workspace.Ensure(d.Svc.DB, d.Svc.Home, task)
	if err != nil {
		return store.Run{}, err
	}
	bgRunner := d.runnerForTask(task)
	settings, err := bgRunner.WriteSettings(d.Svc.Home, d.Svc.Bin)
	if err != nil {
		return store.Run{}, err
	}
	briefPath, err := brief.Render(d.Svc.DB, d.Svc.Home, task)
	if err != nil {
		return store.Run{}, err
	}
	prev, err := d.resumable(task, sessionDirective(delta))
	if err != nil {
		return store.Run{}, err
	}
	if prev != nil && d.runnerForRun(*prev, task).Name() != bgRunner.Name() {
		prev = nil // the task switched backends: one cannot resume the other's session
	}
	cwd := workspace.PrimaryPath(task)
	run := store.Run{TaskID: task.ID, Kind: "auto", Mode: "fresh", Status: "launching", Runner: bgRunner.Name()}
	if prev != nil {
		run.Mode, run.SessionID = "resume", prev.SessionID // the hook matches resumed sessions by id
		run.Prompt = brief.WithInjected(d.Svc.DB, brief.ResumePrompt(task, briefPath, delta, store.GetSetting(d.Svc.DB, "prompt_resume")))
	} else {
		run.Prompt = brief.WithInjected(d.Svc.DB, brief.FreshPrompt(task, briefPath, delta, store.GetSetting(d.Svc.DB, "prompt_fresh")))
	}
	if run, err = store.CreateRun(d.Svc.DB, run); err != nil {
		return store.Run{}, err
	}
	ids := make([]int64, len(delta))
	for i, c := range delta {
		ids[i] = c.ID
	}
	if err := store.ConsumeComments(d.Svc.DB, ids, run.ID); err != nil {
		return run, err
	}
	if _, err := store.UpdateTaskStatus(d.Svc.DB, task.ID, store.StatusRunning, store.ActorSystem); err != nil {
		d.unconsume(run) // the run never started
		return run, err
	}
	var bg, sessionID string
	spec := d.spec(task, cwd, settings, run.Prompt)
	if prev != nil {
		bg, err = bgRunner.Resume(ctx, deref(prev.BgID), deref(prev.SessionID), spec)
		sessionID = deref(prev.SessionID)
	} else {
		bg, sessionID, err = bgRunner.Launch(ctx, spec)
	}
	if err != nil {
		d.unconsume(run) // the run never started
		return run, err
	}
	// Re-read: a hook may have adopted the run (session id) while we launched.
	if run, err = store.GetRun(d.Svc.DB, run.ID); err != nil {
		return run, err
	}
	run.BgID = &bg
	if sessionID != "" && run.SessionID == nil {
		run.SessionID = &sessionID
		run.Status = "running"
	}
	if err := store.UpdateRun(d.Svc.DB, run); err != nil {
		return run, err
	}
	d.logf("launch %s: run %d (%s/%s) bg %s", task.Key, run.ID, bgRunner.Name(), run.Mode, bg)
	store.RecordEvent(d.Svc.DB, task.ID, "run_started", fmt.Sprintf("run %d (%s/%s) bg %s", run.ID, bgRunner.Name(), run.Mode, bg))
	return run, nil
}

// unconsume gives a never-started run's comments back so the retry's prompt carries them.
func (d *Dispatcher) unconsume(run store.Run) {
	if err := store.UnconsumeComments(d.Svc.DB, run.ID); err != nil {
		d.logf("unconsume comments for run %d: %v", run.ID, err)
	}
}

func (d *Dispatcher) spec(task store.Task, cwd, settings, prompt string) runner.LaunchSpec {
	s := runner.LaunchSpec{Cwd: cwd, AddDirs: workspace.OtherPaths(task), Prompt: prompt, TaskKey: task.Key, SettingsPath: settings, Bypass: true}
	if task.Agent != nil {
		s.Agent = *task.Agent
	}
	s.Model = d.model(task)
	if mcp := filepath.Join(cwd, ".mcp.json"); fileExists(mcp) {
		s.MCPConfig = mcp
	}
	return s
}

// runnerForTask returns the appropriate Runner for a task.
func (d *Dispatcher) runnerForTask(task store.Task) runner.Runner {
	taskRunner := ""
	if task.Runner != nil {
		taskRunner = *task.Runner
	}
	return runner.ForTask(taskRunner, store.GetSetting(d.Svc.DB, "default_runner"), d.Svc.Home)
}

// runnerForRun returns the Runner a run was started with, falling back to the task's for runs from
// before runs stored it. Changing the task's runner or default_runner must not affect a live run.
func (d *Dispatcher) runnerForRun(r store.Run, task store.Task) runner.Runner {
	if r.Runner != "" {
		return runner.ForTask(r.Runner, "", d.Svc.Home)
	}
	return d.runnerForTask(task)
}

// model picks the task's model, else "" (no --model) when its agent sets one, else default_model.
func (d *Dispatcher) model(task store.Task) string {
	if task.Model != nil {
		return *task.Model
	}
	if task.Agent != nil {
		var dirs []string
		for _, r := range task.Repos {
			if r.WorktreePath != nil {
				dirs = append(dirs, *r.WorktreePath)
			}
			if repo, err := store.GetRepoByName(d.Svc.DB, r.RepoName); err == nil {
				dirs = append(dirs, repo.Path)
			}
		}
		if core.AgentModel(*task.Agent, dirs...) != "" {
			return ""
		}
	}
	return store.GetSetting(d.Svc.DB, "default_model")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// sessionDirective returns "fresh" or "resume" when a pending human comment starts with #fresh or #resume
// (the latest wins), else "" (decide by resume_threshold).
func sessionDirective(delta []store.Comment) string {
	mode := ""
	for _, c := range delta {
		if c.Author != store.AuthorHuman {
			continue
		}
		switch f := strings.Fields(c.Body); {
		case len(f) == 0:
		case f[0] == "#fresh":
			mode = "fresh"
		case f[0] == "#resume":
			mode = "resume"
		}
	}
	return mode
}

// resumable returns the latest auto run if it can be resumed: finished, with session and bg ids, and a small
// context. directive "fresh" never resumes; "resume" ignores resume_threshold and the rules below.
// With no directive, a task whose last run failed, and an epic root in build or verify (it is about to check
// the AC), start fresh: a session judging its own failed attempt or its own plan tends to repeat it.
func (d *Dispatcher) resumable(task store.Task, directive string) (*store.Run, error) {
	if directive == "" {
		stage := store.EpicStage(task)
		if task.ConsecutiveFailures > 0 || store.IsEpic(task) && (stage == store.StageBuild || stage == store.StageVerify) {
			directive = "fresh"
		}
	}
	if directive == "fresh" {
		return nil, nil
	}
	runs, err := store.ListRuns(d.Svc.DB, task.ID)
	if err != nil {
		return nil, err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if r.Kind != "auto" {
			continue
		}
		if r.Status == "finished" && r.SessionID != nil && r.BgID != nil && (directive == "resume" || r.CtxPeak < int64(d.intSetting("resume_threshold"))) {
			return &r, nil
		}
		break
	}
	return nil, nil
}

// fail records a failed launch: the run fails, the task returns to todo, and at the
// failure cap the task fails with a system comment.
func (d *Dispatcher) fail(task store.Task, run store.Run, cause error) error {
	if run.ID != 0 {
		ended := d.now().UTC().Format(time.RFC3339)
		run.Status, run.Error, run.EndedAt = "failed", ptr(cause.Error()), &ended
		if err := store.UpdateRun(d.Svc.DB, run); err != nil {
			return err
		}
	} else if err := store.RecordEvent(d.Svc.DB, task.ID, "error", "launch: "+cause.Error()); err != nil {
		return err
	}
	t, err := store.GetTask(d.Svc.DB, task.ID)
	if err != nil {
		return err
	}
	t.ConsecutiveFailures++
	if err := store.UpdateTask(d.Svc.DB, t); err != nil {
		return err
	}
	if t.ConsecutiveFailures < d.intSetting("max_consecutive_failures") {
		if t.Status == store.StatusRunning {
			_, err = store.UpdateTaskStatus(d.Svc.DB, t.ID, store.StatusTodo, store.ActorSystem)
		}
		return err
	}
	body := fmt.Sprintf("Launch failed %d times in a row: %v", t.ConsecutiveFailures, cause)
	if errors.Is(cause, claude.ErrUntrusted) {
		for _, r := range t.Repos {
			if r.IsPrimary {
				if repo, e := store.GetRepoByName(d.Svc.DB, r.RepoName); e == nil {
					body += fmt.Sprintf("\nHint: open %s in Claude once to trust it.", repo.Path)
				}
			}
		}
	}
	if _, err := store.AddComment(d.Svc.DB, store.Comment{TaskID: t.ID, Author: store.AuthorSystem, Body: body}); err != nil {
		return err
	}
	if t.Status == store.StatusTodo { // todo -> failed is not an edge; go through running
		if _, err := store.UpdateTaskStatus(d.Svc.DB, t.ID, store.StatusRunning, store.ActorSystem); err != nil {
			return err
		}
	}
	_, err = store.UpdateTaskStatus(d.Svc.DB, t.ID, store.StatusFailed, store.ActorSystem)
	return err
}
