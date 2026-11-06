package core

import (
	"io/fs"
	"slices"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/examples"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ReviewWebhooksApp is the example app's name; installing it turns on event-driven review checks for all tasks.
const ReviewWebhooksApp = "Review webhooks"

// ReviewWebhooksInstalled reports whether the app is installed and enabled.
func (s *Service) ReviewWebhooksInstalled() bool {
	a, err := store.GetApp(s.DB, ReviewWebhooksApp)
	return err == nil && !a.Disabled
}

// ReviewWebhooksWanted is true when the app is installed or an unfinished epic task exists:
// the epic flow starts the listener itself, the app only opts non-epic work in.
func (s *Service) ReviewWebhooksWanted() bool {
	if s.ReviewWebhooksInstalled() {
		return true
	}
	ts, _ := store.ListTasks(s.DB)
	return slices.ContainsFunc(ts, func(t store.Task) bool {
		return t.Status != store.StatusDone && t.Status != store.StatusCancelled &&
			slices.ContainsFunc(t.Labels, func(l string) bool { return l == "epic" || strings.HasPrefix(l, "epic:") })
	})
}

// Example is a bundled example app (examples/*.json), offered but never auto-installed.
type Example struct {
	File        string `json:"file"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ExampleApps lists the bundled examples.
func ExampleApps() []Example {
	var out []Example
	files, _ := fs.Glob(examples.FS, "*.json")
	for _, f := range files {
		raw, _ := examples.FS.ReadFile(f)
		if m, err := ParseManifest(string(raw)); err == nil {
			out = append(out, Example{strings.TrimSuffix(f, ".json"), m.Name, m.Description})
		}
	}
	return out
}

// InstallExample installs a bundled example by its ExampleApps File (the same path as importing a manifest).
func (s *Service) InstallExample(file string) error {
	raw, err := examples.FS.ReadFile(file + ".json")
	if err != nil {
		return err
	}
	_, err = s.InstallManifest(string(raw), false)
	return err
}
