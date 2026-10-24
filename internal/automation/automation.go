// Package automation runs scheduled actions: a cron schedule, an optional
// shell precheck that costs no tokens, and an action (design §9).
package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/robfig/cron/v3"
)

const (
	precheckTimeout = 60 * time.Second
	maxOutput       = 8 << 10
)

// Bin is the conveyor binary the cli kind runs; empty means this executable.
var Bin string

func bin() (string, error) {
	if Bin != "" {
		return Bin, nil
	}
	return os.Executable()
}

// Due reports whether the cron schedule matched between last_run_at and now.
// A missed run fires once. Disabled or unparsable automations are never due.
func Due(a store.Automation, now time.Time) bool {
	if !a.Enabled || a.LastRunAt == nil {
		return false
	}
	sched, err := cron.ParseStandard(a.Schedule)
	if err != nil {
		return false
	}
	last, err := time.Parse(time.RFC3339, *a.LastRunAt)
	if err != nil {
		return false
	}
	return !sched.Next(last).After(now)
}

// ValidSchedule checks a cron expression.
func ValidSchedule(s string) error {
	if _, err := cron.ParseStandard(s); err != nil {
		return fmt.Errorf("schedule %q: %w", s, err)
	}
	return nil
}

type action struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Repos       []string `json:"repos"`
	Agent       string   `json:"agent"`
	Start       bool     `json:"start"`
	TaskStatus  string   `json:"task_status"`
	TaskKey     string   `json:"task_key"`
	Body        string   `json:"body"`
	Args        []string `json:"args"`
}

// precheck runs `sh -c script`. ok is false when it exits non-zero; out is stdout (≤ 8 KB).
func precheck(ctx context.Context, script string, env []string) (out string, ok bool, err error) {
	if strings.TrimSpace(script) == "" {
		return "", true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, precheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stdout = &buf
	runErr := cmd.Run()
	out = strings.TrimSpace(buf.String())
	if len(out) > maxOutput {
		out = out[:maxOutput]
	}
	var ee *exec.ExitError
	switch {
	case runErr == nil:
		return out, true, nil
	case ctx.Err() != nil:
		return out, false, fmt.Errorf("precheck timed out: %w", ctx.Err())
	case errors.As(runErr, &ee):
		return out, false, nil
	}
	return out, false, fmt.Errorf("precheck: %w", runErr)
}

// baseEnv is the process env without the per-task variables, plus extra.
func baseEnv(a store.Automation, extra ...string) []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CONVEYOR_TASK_") && !strings.HasPrefix(e, "SINCE=") {
			env = append(env, e)
		}
	}
	since := "1970-01-01T00:00:00Z"
	if a.LastRunAt != nil {
		since = *a.LastRunAt
	}
	return append(append(env, "SINCE="+since), extra...)
}

// RunOne runs the automation's precheck and action and returns a one-line result.
// A failing precheck is a skip, not an error.
func RunOne(ctx context.Context, svc *core.Service, a store.Automation) (string, error) {
	var act action
	if err := json.Unmarshal([]byte(a.ActionArg), &act); err != nil {
		return "", fmt.Errorf("action_arg: %w", err)
	}
	if a.ActionKind == "comment" && act.TaskStatus != "" {
		return commentByStatus(ctx, svc, a, act)
	}
	out, ok, err := precheck(ctx, a.Precheck, baseEnv(a))
	if err != nil {
		return "", err
	}
	if !ok {
		return "skipped: precheck failed", nil
	}
	return act.run(ctx, svc, a.ActionKind, out, act.TaskKey)
}

func (act action) run(ctx context.Context, svc *core.Service, kind, out, key string) (string, error) {
	sub := func(s string) string { return strings.ReplaceAll(s, "{output}", out) }
	switch kind {
	case "create_task":
		t, err := svc.CreateTask(core.NewTaskInput{Title: sub(act.Title), Description: sub(act.Description), Repos: act.Repos, Agent: act.Agent, Start: act.Start})
		if err != nil {
			return "", err
		}
		return "created " + t.Key, nil
	case "cli":
		b, err := bin()
		if err != nil {
			return "", fmt.Errorf("find conveyor binary: %w", err)
		}
		o, err := exec.CommandContext(ctx, b, append([]string{"--home", svc.Home}, act.Args...)...).CombinedOutput()
		res := strings.TrimSpace(string(o))
		if len(res) > maxOutput {
			res = res[:maxOutput]
		}
		if err != nil {
			return res, fmt.Errorf("conveyor %s: %w", strings.Join(act.Args, " "), err)
		}
		return res, nil
	case "comment":
		if key == "" || act.Body == "" {
			return "", errors.New("comment needs task_status or task_key, and body")
		}
		if _, err := svc.AddComment(key, store.AuthorSystem, sub(act.Body)); err != nil {
			return "", err
		}
		return "commented on " + key, nil
	}
	return "", fmt.Errorf("unknown action kind %q", kind)
}

// commentByStatus runs the precheck once per task in the status and comments where it passes.
func commentByStatus(ctx context.Context, svc *core.Service, a store.Automation, act action) (string, error) {
	tasks, err := store.ListTasks(svc.DB, store.Status(act.TaskStatus))
	if err != nil {
		return "", err
	}
	var done []string
	var errs []error
	for _, t := range tasks {
		var branch, pr string
		for _, r := range t.Repos {
			if r.IsPrimary {
				branch, pr = r.Branch, deref(r.PRURL)
			}
		}
		out, ok, err := precheck(ctx, a.Precheck, baseEnv(a, "CONVEYOR_TASK_KEY="+t.Key, "CONVEYOR_TASK_BRANCH="+branch, "CONVEYOR_TASK_PR="+pr))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.Key, err))
			continue
		}
		if !ok {
			continue
		}
		if _, err := act.run(ctx, svc, a.ActionKind, out, t.Key); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.Key, err))
			continue
		}
		done = append(done, t.Key)
	}
	res := "commented on " + strings.Join(done, ", ")
	if len(done) == 0 {
		res = fmt.Sprintf("no matching tasks (%d in %s)", len(tasks), act.TaskStatus)
	}
	return res, errors.Join(errs...)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Tick runs every due automation and records last_run_at and last_result.
func Tick(ctx context.Context, svc *core.Service, now time.Time) error {
	list, err := store.ListAutomations(svc.DB)
	if err != nil {
		return err
	}
	var errs []error
	for _, a := range list {
		if ctx.Err() != nil {
			break
		}
		if !Due(a, now) {
			continue
		}
		res, err := RunOne(ctx, svc, a)
		if err != nil {
			errs = append(errs, fmt.Errorf("automation %s: %w", a.Name, err))
			res = strings.TrimSpace("error: " + err.Error() + " " + res)
		}
		if err := Record(svc, a, res, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Record stores a run's time and result on the automation.
func Record(svc *core.Service, a store.Automation, result string, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339)
	a.LastRunAt, a.LastResult = &ts, &result
	return store.UpdateAutomation(svc.DB, a)
}

// Templates are the built-in automations installed by `automation add --template`.
var Templates = map[string]store.Automation{
	"pr-sweep": {
		Name:       "pr-sweep",
		Schedule:   "*/15 * * * *",
		Precheck:   `gh pr view "$CONVEYOR_TASK_PR" --json reviews,comments --jq '[.reviews[],.comments[]] | map(select(.updatedAt > env.SINCE)) | length' | grep -qv '^0$'`,
		ActionKind: "comment",
		ActionArg:  `{"task_status":"review","body":"New PR review comments since the last sweep. Address them, push, and reply on the PR threads."}`,
		Enabled:    true,
	},
}
