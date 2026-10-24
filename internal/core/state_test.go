package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	src := newSvc(t)
	store.SetSetting(src.DB, "max_parallel", "5")
	store.CreateApp(src.DB, store.App{Name: "Zed", Command: "zed {cwd}"})
	store.CreateAutomation(src.DB, store.Automation{Name: "weekly", Schedule: "0 9 * * 1", ActionKind: "create_task", ActionArg: `{"title":"t","repos":["secret-repo","other"]}`, Enabled: true})
	store.CreateAutomation(src.DB, store.Automation{Name: "again", Schedule: "0 9 * * 1", ActionKind: "create_task", ActionArg: `{"title":"t","repos":["other"]}`, Enabled: true})
	if err := src.AddAgent("rev", "Reviews", "Be strict.", "user", ""); err != nil {
		t.Fatal(err)
	}
	st, err := src.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "secret-repo") || strings.Join(st.Repos, ",") != "A,B" || len(st.Agents) != 1 || st.Settings["max_parallel"] != "5" {
		t.Fatalf("export: %s", b)
	}

	t.Setenv("HOME", t.TempDir())
	dst := newSvc(t)
	store.AddRepo(dst.DB, store.Repo{Name: "mine", Path: t.TempDir(), DefaultBranch: "main"})
	if _, err := dst.ImportState(st, map[string]string{"A": "nope"}); err == nil {
		t.Fatal("unregistered repo should fail")
	}
	lines, err := dst.ImportState(st, map[string]string{"A": "mine"})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := store.GetAutomation(dst.DB, "weekly")
	a, _ := store.GetAutomation(dst.DB, "again")
	if w.Enabled || w.ActionArg != `{"repos":["B","mine"],"title":"t"}` || !a.Enabled || a.ActionArg != `{"repos":["mine"],"title":"t"}` {
		t.Fatalf("automations: %+v %+v\n%v", w, a, lines)
	}
	if store.GetSetting(dst.DB, "max_parallel") != "5" || AgentModel("rev") != "" {
		t.Fatal("settings/agents not imported")
	}
	if ag, _ := dst.ListAgents(nil); len(ag) != 1 || ag[0].Name != "rev" {
		t.Fatalf("agents: %+v", ag)
	}
	again, _ := dst.ImportState(st, nil)
	for _, l := range again {
		if !strings.HasPrefix(l, "skipped") {
			t.Fatalf("second import changed something: %v", again)
		}
	}
}

func TestSeedMock(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{DB: db}
	if err := s.SeedMock(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ts, _ := store.ListTasks(db)
	seen := map[store.Status]bool{}
	for _, x := range ts {
		seen[x.Status] = true
	}
	for _, st := range store.AllStatuses {
		if !seen[st] {
			t.Errorf("no mock task in %s", st)
		}
	}
	if len(ts) != len(mockTasks) || s.SeedMock(t.TempDir()) == nil {
		t.Fatal("want every scenario seeded once and a second seed refused")
	}
}

func TestSeedMockState(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := (&Service{DB: db}).SeedMock(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	apps, _ := store.ListApps(db)
	autos, _ := store.ListAutomations(db)
	var global, local, out, enabled, disabled bool
	for _, a := range apps {
		global, local, out = global || a.Global, local || !a.Global, out || a.Output
	}
	kinds := map[string]bool{}
	for _, a := range autos {
		enabled, disabled = enabled || a.Enabled, disabled || !a.Enabled
		kinds[a.ActionKind] = true
	}
	if store.GetSetting(db, "max_parallel") != "2" || !global || !local || !out || !enabled || !disabled || len(kinds) != 3 {
		t.Fatalf("mock state incomplete: apps %+v autos %+v", apps, autos)
	}
}
