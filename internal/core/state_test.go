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

func seededState(t *testing.T) State {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	src := &Service{DB: db}
	if err := src.SeedMock(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	st, err := src.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(st) // through JSON, as a file would be
	var out State
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStateTasksRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := seededState(t)
	if st.Version != 2 || len(st.Tasks) != len(mockTasks) || strings.Join(st.Repos, ",") != "A,B,C" {
		t.Fatalf("export: version %d, %d tasks, repos %v", st.Version, len(st.Tasks), st.Repos)
	}

	dst := newSvc(t)
	// An existing task makes the exported keys collide.
	existing, err := store.CreateTask(dst.DB, store.Task{Title: "mine"}, []store.TaskRepo{{RepoID: 1, IsPrimary: true, Branch: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	lines, err := dst.ImportState(st, map[string]string{"A": "a", "B": "b", "C": "a"})
	if err != nil {
		t.Fatal(err)
	}
	all, _ := store.ListTasks(dst.DB)
	if len(all) != len(mockTasks)+1 {
		t.Fatalf("want %d tasks, got %d\n%v", len(mockTasks)+1, len(all), lines)
	}
	if got, _ := store.GetTask(dst.DB, existing.ID); got.Title != "mine" || got.Status != store.StatusBacklog {
		t.Fatalf("existing task touched: %+v", got)
	}
	byTitle := map[string]store.Task{}
	for _, x := range all {
		byTitle[x.Title] = x
	}

	// placeholder mapping: mock-api=A, mock-web=B, mock-infra=C
	multi := byTitle["Running: add pagination"]
	if len(multi.Repos) != 2 || multi.Repos[0].RepoName != "a" || multi.Repos[1].RepoName != "b" || !multi.Repos[0].IsPrimary {
		t.Fatalf("repos: %+v", multi.Repos)
	}
	if got := byTitle["Done: terraform module"]; got.Repos[0].RepoName != "a" || deref(got.Repos[0].PRState) != "merged" || !strings.HasPrefix(got.Repos[0].Branch, "cv/"+got.Key+"-mock") {
		t.Fatalf("pr/branch: %+v", got.Repos[0])
	}
	failed := byTitle["Failed: three launch failures"]
	if failed.ConsecutiveFailures != 3 || failed.Status != store.StatusFailed {
		t.Fatalf("failed: %+v", failed)
	}
	if rs, _ := store.ListRuns(dst.DB, failed.ID); len(rs) != 1 || deref(rs[0].Error) == "" || rs[0].TokensIn != 1200 || rs[0].SessionID != nil {
		t.Fatalf("runs: %+v", rs)
	}
	if cs, _ := store.ListComments(dst.DB, byTitle["Needs you: which DB index?"].ID); len(cs) != 1 || cs[0].Author != "agent" {
		t.Fatalf("comments: %+v", cs)
	}

	// links remapped to the new ids and keys
	child, parent := byTitle["Child of epic"], byTitle["Parent epic"]
	if child.ParentID == nil || *child.ParentID != parent.ID || !strings.HasPrefix(child.Key, parent.Key) {
		t.Fatalf("parent: child %+v parent %+v", child, parent)
	}
	blocked := byTitle["Running: add pagination"]
	if bl, _ := store.OpenBlockers(dst.DB, blocked.ID); len(bl) != 1 || bl[0] != byTitle["Queued: upgrade React"].Key {
		t.Fatalf("blockers: %v", bl)
	}

	// importing the same file again adds nothing
	again, err := dst.ImportState(st, map[string]string{"A": "a", "B": "b", "C": "a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range again {
		if !strings.HasPrefix(l, "skipped") {
			t.Fatalf("second import changed something: %v", again)
		}
	}
}

func TestStateTasksUnmappedRepo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := seededState(t)
	dst := newSvc(t)
	lines, err := dst.ImportState(st, map[string]string{"A": "a"}) // B and C unmapped
	if err != nil {
		t.Fatal(err)
	}
	all, _ := store.ListTasks(dst.DB)
	for _, x := range all {
		if x.Repos[0].RepoName != "a" {
			t.Fatalf("task on an unmapped repo was imported: %+v", x)
		}
	}
	if len(all) == 0 || len(all) >= len(mockTasks) || !strings.Contains(strings.Join(lines, "\n"), "skipped task") {
		t.Fatalf("got %d tasks\n%v", len(all), lines)
	}
}

func TestStateV1StillImports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var st State
	if err := json.Unmarshal([]byte(`{"version":1,"repos":[],"settings":{"max_parallel":"7"},"apps":[],"automations":[],"agents":[]}`), &st); err != nil {
		t.Fatal(err)
	}
	dst := newSvc(t)
	if _, err := dst.ImportState(st, nil); err != nil {
		t.Fatal(err)
	}
	if store.GetSetting(dst.DB, "max_parallel") != "7" {
		t.Fatal("v1 setting not imported")
	}
	if _, err := dst.ImportState(State{Version: 99}, nil); err == nil {
		t.Fatal("a newer version should be refused")
	}
}
