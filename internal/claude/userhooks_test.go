package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserHooks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles.json")
	os.WriteFile(real, []byte(`{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"}]}]}}`), 0o600)
	path := filepath.Join(dir, "settings.json")
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	status := func(bin, want string) {
		t.Helper()
		if got, err := UserHooksStatus(path, bin, "/h"); err != nil || got != want {
			t.Fatal(got, err)
		}
	}
	status("/bin/cv", "not installed")
	if err := SetUserHooks(path, "/old/cv", "/h", true); err != nil {
		t.Fatal(err)
	}
	status("/old/cv", "installed")
	status("/bin/cv", "outdated")
	if err := SetUserHooks(path, "/bin/cv", "/h", true); err != nil { // reinstall replaces, never duplicates
		t.Fatal(err)
	}
	status("/bin/cv", "installed")
	b, _ := os.ReadFile(real)
	if strings.Count(string(b), " hook Stop") != 1 || !strings.Contains(string(b), "say done") || !strings.Contains(string(b), `"model": "opus"`) {
		t.Fatal(string(b))
	}
	if fi, _ := os.Lstat(path); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	if fi, _ := os.Stat(real); fi.Mode().Perm() != 0o600 {
		t.Fatal("mode changed")
	}
	if _, err := os.Stat(real + ".conveyor.bak"); err != nil {
		t.Fatal("no backup")
	}

	// The user-level command must equal the --settings one (WriteSettings uses hookEntry too) so Claude Code runs it once.
	if !strings.Contains(string(b), hookCommand("/bin/cv", "/h", "Stop")) {
		t.Fatal("command differs")
	}

	if err := SetUserHooks(path, "/bin/cv", "/h", false); err != nil {
		t.Fatal(err)
	}
	status("/bin/cv", "not installed")
	b, _ = os.ReadFile(real)
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || len(m["hooks"].(map[string]any)) != 1 || !strings.Contains(string(b), "say done") {
		t.Fatal(string(b))
	}
	os.WriteFile(real, []byte("{bad"), 0o600)
	if err := SetUserHooks(path, "/bin/cv", "/h", true); err == nil {
		t.Fatal("invalid JSON overwritten")
	}
}
