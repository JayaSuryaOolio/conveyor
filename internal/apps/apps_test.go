package apps

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestAppPresets(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "Zed.app"), 0o755)
	old := appDirs
	appDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { appDirs = old })
	ps := AppPresets()
	if ps[0].Name != "Finder" || ps[1].Name != "Zed" || !strings.Contains(ps[1].Manifest, "open -a 'Zed' ${{ worktree.path }}") {
		t.Fatal(ps)
	}
	for _, p := range ps {
		if p.Name == "Cursor" {
			t.Fatal("uninstalled preset offered")
		}
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

func TestManifest(t *testing.T) {
	raw := `{"name":"Demo","actions":[{"id":"hi","label":"Hi","run":"echo ${{ task.key }} ${{ worktree.path }}","places":["board_card"],"output":true,"icon":"data:image/svg+xml,<svg/>"}]}`
	if _, err := ParseManifest(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"name":"x"}`,
		`{"name":"x","actions":[{"id":"a","label":"A","run":"${{ nope }}","places":["global_nav"]}]}`,
		`{"name":"x","actions":[{"id":"a","label":"A","run":"x","places":["moon"]}]}`,
		`{"name":"x","bogus":1,"actions":[{"id":"a","label":"A","run":"x","places":["global_nav"]}]}`,
		`{"name":"x","actions":[{"id":"a","label":"A","run":"x","places":["global_nav"],"icon":"https://evil/x.png"}]}`,
		`{"name":"x","actions":[{"id":"a","label":"A","run":"x","places":["global_nav"],"icon":"data:image/svg+xml,<svg fill='#000'/>"}]}`,
	} {
		if _, err := ParseManifest(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if got := RenderManifest("echo ${{task.key}}", map[string]string{"task.key": "it's"}); got != `echo 'it'\''s'` {
		t.Errorf("render: %s", got)
	}
}

func TestManifestFromApp(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "Visual Studio Code.app"), 0o755)
	old := appDirs
	appDirs = func() []string { return []string{dir} }
	defer func() { appDirs = old }()
	raw, err := ManifestFromApp("Visual Studio Code")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ParseManifest(raw)
	a := m.Actions[0]
	if a.ID != "visual-studio-code" || a.Run != "open -a 'Visual Studio Code' ${{ worktree.path }}" || a.Places[0] != "task_sidebar" || !strings.HasSuffix(a.IconPath, ".app") {
		t.Fatalf("%+v", a)
	}
	if _, err := ManifestFromApp("Nope"); err == nil {
		t.Fatal("want not found")
	}
}

func TestManifestIconFromApp(t *testing.T) {
	finder := "/System/Library/CoreServices/Finder.app"
	icns := bundleIcns(finder)
	if icns == "" {
		t.Skip("no Finder icon")
	}
	dir := t.TempDir()
	res := filepath.Join(dir, "Zed.app", "Contents", "Resources")
	os.MkdirAll(res, 0o755)
	b, _ := os.ReadFile(icns)
	os.WriteFile(filepath.Join(res, "Z.icns"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "Zed.app", "Contents", "Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIconFile</key><string>Z</string></dict></plist>`), 0o644)
	old := appDirs
	appDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { appDirs = old })

	db, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// a legacy plain app migrates to a manifest app with its icon, and the columns go
	if _, err := db.Exec(`ALTER TABLE apps ADD COLUMN command TEXT NOT NULL DEFAULT ''; ALTER TABLE apps ADD COLUMN output INTEGER NOT NULL DEFAULT 0; INSERT INTO apps (name, command) VALUES ('Zed', 'open -a Zed {cwd}')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // second run is a no-op
		if err := MigratePlain(db, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	if p, _ := PlacedActions(db, "task_sidebar"); len(p) != 1 || !strings.HasPrefix(p[0].Icon, "data:image/png") {
		t.Fatal(p)
	}
}
func TestGraftService(t *testing.T) {
	// a fake graft CLI listing one instance: status and stop must find the root by worktree
	dir := t.TempDir()
	log := filepath.Join(dir, "stopped")
	os.WriteFile(filepath.Join(dir, "graft"), []byte("#!/bin/sh\nif [ \"$1\" = list ]; then echo '/r/a  <-  /w/one two  (pid 12)'; else echo \"$2\" > "+log+"; fi\n"), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	sv := graftService()
	run := func(script, wt string) error {
		return exec.Command("sh", "-c", RenderManifest(script, map[string]string{"worktree.path": wt})).Run()
	}
	if run(sv.Status, "/w/one two") != nil || run(sv.Status, "/w/other") == nil {
		t.Fatal("status")
	}
	if run(sv.Stop, "/w/one two") != nil {
		t.Fatal("stop")
	}
	if b, _ := os.ReadFile(log); strings.TrimSpace(string(b)) != "/r/a" {
		t.Fatalf("stopped %q", b)
	}
}

func TestManifestSettingsValidation(t *testing.T) {
	act := func(run string) string {
		return `"actions":[{"id":"a","label":"A","places":["global_nav"],"run":"` + run + `"}]`
	}
	for name, raw := range map[string]string{
		"needs v3":        `{"version":2,"name":"X","settings":[{"key":"k","type":"string","default":""}],` + act("x") + `}`,
		"undeclared var":  `{"version":3,"name":"X",` + act("${{ setting.k }}") + `}`,
		"bad key":         `{"version":3,"name":"X","settings":[{"key":"A-b","type":"string","default":""}],` + act("x") + `}`,
		"bad default":     `{"version":3,"name":"X","settings":[{"key":"k","type":"number","default":"x"}],` + act("x") + `}`,
		"unknown type":    `{"version":3,"name":"X","settings":[{"key":"k","type":"date","default":""}],` + act("x") + `}`,
		"key as bare var": `{"version":3,"name":"X","settings":[{"key":"k","type":"string","default":""}],` + act("${{ k }}") + `}`,
	} {
		if _, err := ParseManifest(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestExampleApps(t *testing.T) {
	apps := ExampleApps()
	if len(apps) == 0 {
		t.Fatal("no example apps found")
	}
	names := make(map[string]bool)
	for _, app := range apps {
		names[app.File] = true
	}
	for _, expected := range []string{"acli", "review-webhooks", "graft", "vscode", "nautilus"} {
		if !names[expected] {
			t.Errorf("missing expected example app: %s", expected)
		}
	}
}
