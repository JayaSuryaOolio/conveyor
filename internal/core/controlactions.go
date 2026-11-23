package core

import (
	"slices"

	"github.com/JayaSuryaOolio/conveyor/internal/connectors"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// ControlAction is one thing an external tool can run: a ready argv, with `confirm` (ask first;
// the CLI never prompts) and `needs_text` ({text} in argv is for the caller to fill).
type ControlAction struct {
	Label     string   `json:"label"`
	Kind      string   `json:"kind"` // builtin | app | nav
	App       string   `json:"app,omitempty"`
	Argv      []string `json:"argv"`
	Confirm   string   `json:"confirm,omitempty"`
	NeedsText bool     `json:"needs_text,omitempty"`
}

var appPlaces = []string{"task_sidebar", "board_card"}

// ControlActions lists what can be run on task key, or, with key "", what needs no task.
func (s *Service) ControlActions(key string) ([]ControlAction, error) {
	cv := func(a ...string) []string { return append([]string{"conveyor"}, a...) }
	var out []ControlAction
	if key == "" {
		out = append(out,
			ControlAction{Label: "Open board", Kind: "nav", Argv: cv("ui", "go", "board")},
			ControlAction{Label: "Search", Kind: "nav", Argv: cv("ui", "palette", "{text}"), NeedsText: true})
		for _, v := range []string{"learnings", "automations", "apps", "agents", "logs", "settings", "focus"} {
			out = append(out, ControlAction{Label: "Open " + v, Kind: "nav", Argv: cv("ui", "go", v)})
		}
		return s.appendApps(out, "global_nav", "")
	}
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return nil, err
	}
	for _, to := range store.AllStatuses {
		if store.ValidateTransition(t.Status, to, store.ActorHuman) != nil {
			continue
		}
		switch to {
		case store.StatusCancelled:
			out = append(out, ControlAction{"Cancel", "builtin", "", cv("task", "cancel", key), "Cancel " + key + "?", false})
		case store.StatusDone:
			out = append(out, ControlAction{"Mark done", "builtin", "", cv("task", "done", key), "Mark " + key + " done?", false})
		default:
			out = append(out, ControlAction{Label: "Move to " + string(to), Kind: "builtin", Argv: cv("task", "move", key, string(to))})
		}
	}
	if t.Status == store.StatusRunning {
		out = append(out, ControlAction{"Stop", "builtin", "", cv("task", "stop", key), "Stop " + key + "?", false})
	}
	for _, f := range []string{"shell", "resume", "fresh"} {
		out = append(out, ControlAction{Label: "Open " + f, Kind: "builtin", Argv: cv("task", "open", key, "--"+f)})
	}
	out = append(out, ControlAction{Label: "Comment", Kind: "builtin", Argv: cv("task", "comment", key, "{text}"), NeedsText: true})
	for _, p := range appPlaces {
		var err error
		if out, err = s.appendApps(out, p, key); err != nil {
			return nil, err
		}
	}
	cs, err := store.ListConnectors(s.DB)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		if m, err := connectors.ParseManifest(c.Manifest); err == nil && !c.Disabled && len(m.Actions) > 0 {
			out = append(out, ControlAction{"Open in " + c.Name, "app", c.Name, cv("app", "open", c.Name, key), m.Actions[0].Confirm, false})
		}
	}
	out = append(out, ControlAction{Label: "Open task", Kind: "nav", Argv: cv("ui", "go", key)})
	if kids, _ := store.ListTasks(s.DB); slices.ContainsFunc(kids, func(k store.Task) bool { return k.ParentID != nil && *k.ParentID == t.ID }) {
		out = append(out, ControlAction{Label: "Open epic", Kind: "nav", Argv: cv("ui", "go", "epic", key)})
	}
	return out, nil
}

func (s *Service) appendApps(out []ControlAction, place, key string) ([]ControlAction, error) {
	ps, err := connectors.PlacedActions(s.DB, place)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		argv := []string{"conveyor", "app", "run", p.Connector, p.ID}
		if key != "" {
			argv = append(argv, key)
		}
		out = append(out, ControlAction{p.Label, "app", p.Connector, append(argv, "--place", place), p.Confirm, false})
	}
	return out, nil
}
