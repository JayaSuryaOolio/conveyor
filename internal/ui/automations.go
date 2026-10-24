package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/automation"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ListAutomations returns every automation.
func (a *App) ListAutomations() ([]store.Automation, error) {
	list, err := store.ListAutomations(a.svc.DB)
	if list == nil {
		list = []store.Automation{}
	}
	return list, err
}

// SetAutomationEnabled toggles an automation.
func (a *App) SetAutomationEnabled(name string, enabled bool) error {
	x, err := store.GetAutomation(a.svc.DB, name)
	if err != nil {
		return err
	}
	x.Enabled = enabled
	return store.UpdateAutomation(a.svc.DB, x)
}

// RunAutomation runs one now (ignoring its schedule) and records the result.
func (a *App) RunAutomation(name string) (string, error) {
	x, err := store.GetAutomation(a.svc.DB, name)
	if err != nil {
		return "", err
	}
	res, runErr := automation.RunOne(context.Background(), a.svc, x)
	if runErr != nil {
		res = "error: " + runErr.Error()
	}
	if err := automation.Record(a.svc, x, res, time.Now()); err != nil {
		return res, fmt.Errorf("record result: %w", err)
	}
	return res, runErr
}

// AddAutomation validates and creates an enabled automation.
func (a *App) AddAutomation(x store.Automation) error {
	x.Enabled = true
	x.LastRunAt, x.LastResult = nil, nil
	if err := automation.Validate(x); err != nil {
		return err
	}
	_, err := store.CreateAutomation(a.svc.DB, x)
	return err
}

// UpdateAutomation edits an automation's schedule, precheck and action (looked up by name; keeps its run history).
func (a *App) UpdateAutomation(x store.Automation) error {
	if err := automation.Validate(x); err != nil {
		return err
	}
	old, err := store.GetAutomation(a.svc.DB, x.Name)
	if err != nil {
		return err
	}
	old.Schedule, old.Precheck, old.ActionKind, old.ActionArg = x.Schedule, x.Precheck, x.ActionKind, x.ActionArg
	return store.UpdateAutomation(a.svc.DB, old)
}

// DeleteAutomation removes an automation.
func (a *App) DeleteAutomation(name string) error { return store.DeleteAutomation(a.svc.DB, name) }

// ScheduleInfo describes a cron expression for the form.
type ScheduleInfo struct {
	Valid       bool     `json:"valid"`
	Error       string   `json:"error"`
	Description string   `json:"description"`
	Next        []string `json:"next"`
}

// DescribeSchedule parses expr with the dispatcher's parser and lists the next 3 runs.
func (a *App) DescribeSchedule(expr string) ScheduleInfo {
	next, err := automation.Next(expr, time.Now(), 3)
	if err != nil {
		return ScheduleInfo{Error: err.Error(), Next: []string{}}
	}
	info := ScheduleInfo{Valid: true, Description: automation.Describe(expr), Next: []string{}}
	for _, t := range next {
		info.Next = append(info.Next, t.Local().Format("Mon Jan 2, 15:04"))
	}
	return info
}
