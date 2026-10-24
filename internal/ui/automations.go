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
