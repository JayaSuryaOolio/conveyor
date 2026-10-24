package core

import (
	"os"
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
