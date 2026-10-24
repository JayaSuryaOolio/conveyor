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
	if got := RenderApp("open -a Zed {cwd} && echo {cwd}", "/a b/it's"); got != `open -a Zed '/a b/it'\''s' && echo '/a b/it'\''s'` {
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
	if err := s.OpenApp("t", tk.Key, "b"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); filepath.Base(strings.TrimSpace(string(b))) != "b" {
		t.Fatal(string(b))
	}
}
