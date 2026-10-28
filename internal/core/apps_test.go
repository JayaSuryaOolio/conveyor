package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestRenderApp(t *testing.T) {
	if got := RenderApp("open -a Zed {cwd} && echo {cwd} {key}", "/a b/it's", "CV-1"); got != `open -a Zed '/a b/it'\''s' && echo '/a b/it'\''s' 'CV-1'` {
		t.Fatal(got)
	}
}

func TestAppPresets(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "Zed.app"), 0o755)
	old := appDirs
	appDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { appDirs = old })
	ps := AppPresets()
	if ps[0].Name != "Finder" || ps[1] != (store.App{Name: "Zed", Command: "open -a 'Zed' {cwd}"}) {
		t.Fatal(ps)
	}
	for _, p := range ps {
		if p.Name == "Cursor" {
			t.Fatal("uninstalled preset offered")
		}
	}
}

func TestSpotlightRoot(t *testing.T) {
	list := "/r/a  <-  /w/one two  (pid 12)\n/r/b  <-  /w/three  (pid 3)\n"
	if got := spotlightRoot(list, "/w/one two"); got != "/r/a" {
		t.Fatal(got)
	}
	if spotlightRoot(list, "/w/other") != "" || spotlightRoot("no spotlight instances running\n", "/w/three") != "" {
		t.Fatal("matched inactive worktree")
	}
}

func TestAppBundleIcns(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("no plutil")
	}
	dir := t.TempDir()
	res := filepath.Join(dir, "Zed Preview.app", "Contents", "Resources")
	os.MkdirAll(res, 0o755)
	os.WriteFile(filepath.Join(res, "Z.icns"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "Zed Preview.app", "Contents", "Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIconFile</key><string>Z</string></dict></plist>`), 0o644)
	old := appDirs
	appDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { appDirs = old })
	app := appBundle("open -a 'Zed Preview' {cwd}")
	if app == "" || appBundle("echo hi") != "" || appBundle("open -a Nope {cwd}") != "" {
		t.Fatal(app)
	}
	if got := bundleIcns(app); filepath.Base(got) != "Z.icns" {
		t.Fatal(got)
	}
	if exists("/System/Library/CoreServices/Finder.app") && appBundle("open {cwd}") == "" {
		t.Fatal("finder not found")
	}
}

func TestOpenGlobalApp(t *testing.T) {
	s := newSvc(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	out := filepath.Join(t.TempDir(), "out")
	store.CreateApp(s.DB, store.App{Name: "g", Command: "echo {cwd} > " + shq(out), Global: true})
	store.CreateApp(s.DB, store.App{Name: "n", Command: "true"})
	if _, err := s.OpenApp("g", "", ""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); strings.TrimSpace(string(b)) != home {
		t.Fatal(string(b))
	}
	if _, err := s.OpenApp("n", "", ""); err == nil {
		t.Fatal("non-global app opened without a task")
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
	store.CreateApp(s.DB, store.App{Name: "t", Command: "echo {cwd} > " + shq(out)})
	if _, err := s.OpenApp("t", tk.Key, "b"); err != nil {
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
	store.CreateApp(s.DB, store.App{Name: "o", Command: "echo {key}; basename \"$PWD\"", Output: true})
	if got, err := s.OpenApp("o", tk.Key, ""); err != nil || got != tk.Key+"\na" {
		t.Fatalf("%q %v", got, err)
	}
	store.CreateApp(s.DB, store.App{Name: "f", Command: "echo nope; exit 3", Output: true})
	if _, err := s.OpenApp("f", tk.Key, ""); err == nil || !strings.Contains(err.Error(), "nope") {
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

func TestManifest(t *testing.T) {
	raw := `{"name":"Demo","actions":[{"id":"hi","label":"Hi","run":"echo ${{ task.key }} ${{ worktree.path }}","places":["board_card"],"output":true}]}`
	if _, err := ParseManifest(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"name":"x"}`,
		`{"name":"x","actions":[{"id":"a","label":"A","run":"${{ nope }}","places":["global_nav"]}]}`,
		`{"name":"x","actions":[{"id":"a","label":"A","run":"x","places":["moon"]}]}`,
		`{"name":"x","bogus":1,"actions":[{"id":"a","label":"A","run":"x","places":["global_nav"]}]}`,
	} {
		if _, err := ParseManifest(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if got := RenderManifest("echo ${{task.key}}", map[string]string{"task.key": "it's"}); got != `echo 'it'\''s'` {
		t.Errorf("render: %s", got)
	}
}

func TestInstallManifest(t *testing.T) {
	s := newSvc(t)
	t.Setenv("HOME", t.TempDir())
	raw := `{"name":"Demo","actions":[{"id":"hi","label":"Hi","run":"echo ${{ app.name }} in ${{ worktree.path }}","places":["global_nav"],"output":true}],
	 "automations":[{"name":"demo-auto","schedule":"every 1h","action_kind":"comment","action_arg":"{}"}]}`
	if _, err := s.InstallManifest(raw); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PlacedActions("global_nav"); len(p) != 1 || p[0].ID != "hi" {
		t.Fatal(p)
	}
	if p, _ := s.PlacedActions("board_card"); len(p) != 0 {
		t.Fatal(p)
	}
	if out, err := s.RunAppAction("Demo", "hi", ""); err != nil || !strings.HasPrefix(out, "Demo in ") {
		t.Fatal(out, err)
	}
	if err := s.UninstallApp("Demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAutomation(s.DB, "demo-auto"); err == nil {
		t.Fatal("automation left behind")
	}
}
