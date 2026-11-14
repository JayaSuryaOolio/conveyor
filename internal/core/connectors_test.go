package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/connectors"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestGraftRoot(t *testing.T) {
	list := "/r/a  <-  /w/one two  (pid 12)\n/r/b  <-  /w/three  (pid 3)\n"
	if got := graftRoot(list, "/w/one two"); got != "/r/a" {
		t.Fatal(got)
	}
	if graftRoot(list, "/w/other") != "" || graftRoot("no graft instances running\n", "/w/three") != "" {
		t.Fatal("matched inactive worktree")
	}
}

func TestOpenApp(t *testing.T) {
	s := newSvc(t)
	tk := actionTask(t, s)
	if p, err := s.WorktreePath(tk.Key, ""); err != nil || filepath.Base(p) != "a" {
		t.Fatal(p, err)
	}
	if _, err := s.WorktreePath(tk.Key, "zz"); err == nil {
		t.Fatal("unknown repo accepted")
	}
	out := filepath.Join(t.TempDir(), "out")
	s.InstallCommand("t", "echo {cwd} > "+shq(out), false)
	if _, err := s.OpenConnector("t", tk.Key, "b"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); filepath.Base(strings.TrimSpace(string(b))) != "b" {
		t.Fatal(string(b))
	}
}

func TestOutputApp(t *testing.T) {
	s := newSvc(t)
	tk := actionTask(t, s)
	wt, _ := s.WorktreePath(tk.Key, "")
	os.MkdirAll(wt, 0o755)
	s.InstallCommand("o", "echo {key}; basename \"$PWD\"", true)
	if got, err := s.OpenConnector("o", tk.Key, ""); err != nil || got != tk.Key+"\na" {
		t.Fatalf("%q %v", got, err)
	}
	s.InstallCommand("f", "echo nope; exit 3", true)
	if _, err := s.OpenConnector("f", tk.Key, ""); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatal(err)
	}
}

func TestSessionIDs(t *testing.T) {
	s := newSvc(t)
	tk := actionTask(t, s)
	for _, id := range []string{"s1", "s1", "", "s2"} {
		r := store.Run{TaskID: tk.ID, Kind: "bg", Status: "finished"}
		if id != "" {
			r.SessionID = &id
		}
		if _, err := store.CreateRun(s.DB, r); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(s.ProjectsDir, nonAlnumPath.ReplaceAllString(*tk.Repos[0].WorktreePath, "-"))
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "m1.jsonl"), []byte("{}\n"), 0o644)
	if ids, err := s.SessionIDs(tk.Key); err != nil || strings.Join(ids, ",") != "s1,s2,m1" {
		t.Fatal(ids, err)
	}
}

func TestInstallManifest(t *testing.T) {
	s := newSvc(t)
	t.Setenv("HOME", t.TempDir())
	raw := `{"name":"Demo","actions":[{"id":"hi","label":"Hi","run":"echo ${{ app.name }} in ${{ worktree.path }}","places":["global_nav"],"output":true}],
	 "automations":[{"name":"demo-auto","schedule":"every 1h","action_kind":"comment","action_arg":"{}"}]}`
	if _, err := s.InstallManifest(raw, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InstallManifest(raw, false); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if _, err := s.InstallManifest(raw, true); err != nil {
		t.Fatal("replace:", err)
	}
	if exp, err := s.ExportConnector("Demo"); err != nil || exp != raw {
		t.Fatal("export:", exp, err)
	}
	if p, _ := s.PlacedActions("global_nav"); len(p) != 1 || p[0].ID != "hi" {
		t.Fatal(p)
	}
	if p, _ := s.PlacedActions("board_card"); len(p) != 0 {
		t.Fatal(p)
	}
	if out, err := s.RunConnectorAction("Demo", "hi", "", ""); err != nil || !strings.HasPrefix(out, "Demo in ") {
		t.Fatal(out, err)
	}
	if err := s.UninstallConnector("Demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAutomation(s.DB, "demo-auto"); err == nil {
		t.Fatal("automation left behind")
	}
}

func TestManifestVersion(t *testing.T) {
	if _, err := connectors.ParseManifest(`{"version":99,"name":"x","actions":[{"id":"a","label":"A","run":"x","places":["global_nav"]}]}`); err == nil {
		t.Fatal("accepted future version")
	}
}

func TestServiceManifest(t *testing.T) {
	bad := `{"name":"X","actions":[{"id":"a","label":"A","run":"true","places":["%s"],%s}]}`
	svc := `"service":{"status":"true","stop":"true"}`
	if _, err := connectors.ParseManifest(fmt.Sprintf(bad, "task_sidebar", svc)); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{{"task_sidebar", `"service":{"status":"true"}`}, {"task_sidebar", `"output":true,` + svc}} {
		if _, err := connectors.ParseManifest(fmt.Sprintf(bad, c[0], c[1])); err == nil {
			t.Fatalf("accepted %v", c)
		}
	}
	// v1 `service` migrates to `services` at each place; v2 rejects it, v1 rejects `services`
	if m, err := connectors.ParseManifest(fmt.Sprintf(bad, "board_card", svc)); err != nil || m.Version != connectors.ManifestVersion || m.Actions[0].Services["board_card"].Stop != "true" {
		t.Fatal("migrate:", m, err)
	}
	for _, x := range []string{
		`{"version":2,"name":"X","actions":[{"id":"a","label":"A","run":"x","places":["task_sidebar"],` + svc + `}]}`,
		`{"name":"X","actions":[{"id":"a","label":"A","run":"x","places":["task_sidebar"],"services":{"task_sidebar":{"status":"true","stop":"true"}}}]}`,
		`{"version":2,"name":"X","actions":[{"id":"a","label":"A","run":"x","places":["task_sidebar"],"services":{"board_card":{"status":"true","stop":"true"}}}]}`,
	} {
		if _, err := connectors.ParseManifest(x); err == nil {
			t.Fatalf("accepted %s", x)
		}
	}
	// v2: each place has its own service (or none)
	s := newSvc(t)
	if _, err := s.InstallManifest(`{"version":2,"name":"Multi","actions":[{"id":"a","label":"A","run":"x","places":["global_nav","task_sidebar","board_card"],
	 "services":{"global_nav":{"run":"y","status":"true","stop":"true"},"task_sidebar":{"status":"false","stop":"true"}}}]}`, false); err != nil {
		t.Fatal(err)
	}
	for place, want := range map[string]bool{"global_nav": true, "task_sidebar": true, "board_card": false} {
		if p, _ := s.PlacedActions(place); len(p) != 1 || p[0].Service != want {
			t.Fatal(place, p)
		}
	}
	if on, err := s.ConnectorServiceRunning("Multi", "a", "global_nav", ""); !on || err != nil {
		t.Fatal("global_nav service not running", err)
	}
	if on, _ := s.ConnectorServiceRunning("Multi", "a", "board_card", ""); on {
		t.Fatal("board_card has no service")
	}
	if err := s.StopConnectorService("Multi", "a", "board_card", ""); err == nil {
		t.Fatal("stopped a missing service")
	}
}

func TestManifestIconPath(t *testing.T) {
	s := newSvc(t)
	s.Home = t.TempDir()
	src := filepath.Join(t.TempDir(), "i.png")
	os.WriteFile(src, []byte("png"), 0o644)
	raw := fmt.Sprintf(`{"name":"Ic","actions":[{"id":"a","label":"A","run":"x","places":["global_nav"],"iconPath":%q}]}`, src)
	if _, err := s.InstallManifest(raw, false); err != nil {
		t.Fatal(err)
	}
	os.Remove(src)
	p, err := s.PlacedActions("global_nav")
	if err != nil || len(p) != 1 || p[0].Icon != "data:image/png;base64,cG5n" {
		t.Fatal(p, err)
	}
	if err := s.UninstallConnector("Ic"); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(connectors.IconsDir(s.Home), "*")); len(m) != 0 {
		t.Fatal("icon copy left behind", m)
	}
	for _, bad := range []string{
		`{"name":"x","actions":[{"id":"a","label":"A","run":"x","places":["global_nav"],"iconPath":"rel.png"}]}`,
		`{"name":"x","actions":[{"id":"a","label":"A","run":"x","places":["global_nav"],"iconPath":"/a.exe"}]}`,
		`{"name":"x","actions":[{"id":"a","label":"A","run":"x","places":["global_nav"],"iconPath":"/a.png","icon":"data:image/png,x"}]}`,
	} {
		if _, err := connectors.ParseManifest(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestAppBundleIconAndStateExport(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("needs plutil and sips")
	}
	app := "/System/Applications/Calculator.app"
	if _, err := os.Stat(app); err != nil {
		t.Skip(err)
	}
	src := newSvc(t)
	src.Home = t.TempDir()
	raw := fmt.Sprintf(`{"name":"Calc","actions":[{"id":"a","label":"A","run":"x","places":["global_nav"],"iconPath":%q}]}`, app)
	if _, err := src.InstallManifest(raw, false); err != nil {
		t.Fatal(err)
	}
	st, err := src.ExportState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Connectors) != 1 || !strings.Contains(st.Connectors[0].Manifest, `"icon": "data:image/png;base64,`) || strings.Contains(st.Connectors[0].Manifest, "iconPath") {
		t.Fatal(st.Connectors)
	}
	dst := newSvc(t)
	if _, err := dst.ImportState(st, nil); err != nil {
		t.Fatal(err)
	}
	if p, _ := dst.PlacedActions("global_nav"); len(p) != 1 || !strings.HasPrefix(p[0].Icon, "data:image/png") {
		t.Fatal(p)
	}
}

func TestManifestInject(t *testing.T) {
	const m = `{"version":%d,"name":"X","inject":{"prompt":"P"},"actions":[{"id":"a","label":"A","run":"true","places":["global_nav"]}]}`
	if _, err := connectors.ParseManifest(fmt.Sprintf(m, 3)); err == nil {
		t.Fatal("inject accepted below v4")
	}
	s := newSvc(t)
	if _, err := s.InstallManifest(fmt.Sprintf(m, 4), false); err != nil {
		t.Fatal(err)
	}
	if got := connectors.Injected(s.DB, false, func(i connectors.ManifestInject) string { return i.Prompt }); got != "P" {
		t.Fatal(got)
	}
	if got := connectors.Injected(s.DB, true, func(i connectors.ManifestInject) string { return i.Prompt }); got != "## App: X\n\nP" {
		t.Fatal(got)
	}
}
