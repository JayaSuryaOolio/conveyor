package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func init() {
	register("ui", runUI, "drive the open app (go VIEW|KEY|board|task|epic ... | palette [QUERY] | current [--json])")
}

// exitErr makes run() exit with code (3: the app is not running or shows no task) instead of 1.
type exitErr struct {
	code int
	err  error
}

func (e exitErr) Error() string { return e.err.Error() }
func (e exitErr) Unwrap() error { return e.err }

// uiWait is how long `ui go` waits for the app to pick the intent up.
const (
	uiWait = 3 * time.Second
	uiPoll = 100 * time.Millisecond
)

// uiViews are the views with no argument; focus is the Focus popup, not a route.
var uiViews = []string{"learnings", "automations", "apps", "agents", "logs", "settings", "focus"}

var taskKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+[A-Za-z]*$`)

// currentTaskKey resolves the `@` token from the app's ui-state.json; failures exit 3.
func currentTaskKey(home string) (string, error) {
	s, err := core.ReadUIState(home)
	if err == nil {
		var k string
		if k, err = s.CurrentTask(); err == nil {
			return k, nil
		}
	}
	return "", exitErr{3, err}
}

func runUI(home string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: conveyor ui go|palette|current")
	}
	switch args[0] {
	case "go":
		return withService(home, func(s *core.Service) error {
			v, err := parseGo(s, args[1:])
			if err != nil {
				return err
			}
			b, _ := json.Marshal(v)
			return sendIntent(s, "go", b)
		})
	case "palette":
		b, _ := json.Marshal(map[string]string{"q": strings.Join(args[1:], " ")})
		return withService(home, func(s *core.Service) error { return sendIntent(s, "palette", b) })
	case "current":
		return uiCurrent(home, args[1:])
	}
	return fmt.Errorf("unknown ui command %q", args[0])
}

// parseGo turns `ui go ...` args into a View, rejecting unknown views and missing tasks here
// (exit 1) so a typo never reaches the app.
func parseGo(s *core.Service, args []string) (core.UIView, error) {
	fs := flag.NewFlagSet("ui go", flag.ContinueOnError)
	label := fs.String("label", "", "board: filter by label")
	repo := fs.String("repo", "", "board: filter by repo")
	task := fs.String("task", "", "epic: open this task's modal")
	q := fs.String("q", "", "automations: search text (partial name)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return core.UIView{}, err
	}
	usage := fmt.Errorf("usage: conveyor ui go board [--label L] [--repo R] | automations [--q TEXT] | task KEY | KEY | epic KEY [--task KEY] | %s", strings.Join(uiViews, "|"))
	if len(pos) == 0 {
		return core.UIView{}, usage
	}
	needTask := func(k string) error {
		_, err := store.GetTaskByKey(s.DB, k)
		return err
	}
	switch name := pos[0]; {
	case name == "board" && len(pos) == 1:
		return core.UIView{Name: "board", Label: *label, Repo: *repo}, nil
	case slices.Contains(uiViews, name) && len(pos) == 1:
		v := core.UIView{Name: name}
		if name == "automations" {
			v.Q = *q
		}
		return v, nil
	case (name == "task" || name == "epic") && len(pos) == 2:
		if err := needTask(pos[1]); err != nil {
			return core.UIView{}, err
		}
		v := core.UIView{Name: name, Key: pos[1]}
		if *task != "" && name == "epic" {
			if err := needTask(*task); err != nil {
				return core.UIView{}, err
			}
			v.Task = *task
		}
		return v, nil
	case len(pos) == 1 && taskKeyRe.MatchString(name): // `ui go CV-128`: the voice-friendly form
		if err := needTask(name); err != nil {
			return core.UIView{}, err
		}
		return core.UIView{Name: "task", Key: name}, nil
	}
	return core.UIView{}, usage
}

// sendIntent queues the intent and waits for the app to consume it; on timeout it removes the row (exit 3).
func sendIntent(s *core.Service, verb string, args json.RawMessage) error {
	id, err := store.AddIntent(s.DB, verb, args, time.Now())
	if err != nil {
		return err
	}
	for deadline := time.Now().Add(uiWait); time.Now().Before(deadline); time.Sleep(uiPoll) {
		if ok, err := store.IntentConsumed(s.DB, id); err != nil {
			return err
		} else if ok {
			return nil
		}
	}
	if err := store.DeleteIntent(s.DB, id); err != nil {
		return err
	}
	return exitErr{3, core.ErrNoApp}
}

func uiCurrent(home string, args []string) error {
	fs := flag.NewFlagSet("ui current", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the view as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	st, err := core.ReadUIState(home)
	if err != nil {
		return exitErr{3, err}
	}
	if *asJSON {
		return printJSON(st)
	}
	v := st.View
	parts := slices.DeleteFunc([]string{v.Name, v.Key, v.Task, v.Label, v.Repo}, func(s string) bool { return s == "" })
	fmt.Fprintln(out, strings.Join(parts, " "))
	return nil
}
