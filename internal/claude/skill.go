package claude

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
)

// SetSkill replaces dir with the files of src or, if !install, removes it.
func SetSkill(src fs.FS, dir string, install bool) error {
	if err := os.RemoveAll(dir); err != nil || !install {
		return err
	}
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

// skillCurrent reports whether every file of src is in dir with the same content.
func skillCurrent(src fs.FS, dir string) bool {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		if got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p))); err != nil || !bytes.Equal(got, want) {
			return fs.ErrNotExist
		}
		return nil
	}) == nil
}

// SkillStatus folds the skill installed with the hooks into their status: a missing
// or stale skill beside installed hooks, or one left behind without them, is "outdated".
func SkillStatus(hooks string, src fs.FS, dir string) string {
	_, err := os.Stat(dir)
	switch {
	case hooks == "installed" && !skillCurrent(src, dir), hooks == "not installed" && err == nil:
		return "outdated"
	}
	return hooks
}
