package core

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

var openA = regexp.MustCompile(`^open -a ('[^']*'|"[^"]*"|\S+)`)

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// appBundle returns the .app a command opens (`open -a NAME …`, or Finder for `open {cwd}`), "" if none is installed.
func appBundle(command string) string {
	command = strings.TrimSpace(command)
	name := "Finder"
	if m := openA.FindStringSubmatch(command); m != nil {
		name = strings.Trim(m[1], `'"`)
	} else if !strings.HasPrefix(command, "open {cwd}") {
		return ""
	}
	for _, d := range append(appDirs(), "/System/Library/CoreServices") {
		if p := filepath.Join(d, name+".app"); exists(p) {
			return p
		}
	}
	return ""
}

// bundleIcns returns the .icns named by CFBundleIconFile in the bundle's Info.plist, "" if none.
func bundleIcns(app string) string {
	out, err := exec.Command("plutil", "-extract", "CFBundleIconFile", "raw", filepath.Join(app, "Contents", "Info.plist")).Output()
	f := strings.TrimSpace(string(out))
	if err != nil || f == "" {
		return ""
	}
	if filepath.Ext(f) != ".icns" {
		f += ".icns"
	}
	if p := filepath.Join(app, "Contents", "Resources", f); exists(p) {
		return p
	}
	return ""
}

// AppIcons maps app name to a PNG data: URL of its macOS icon; apps without one are omitted.
// PNGs are cached under <home>/icons.
func AppIcons(home string, apps []store.App) map[string]string {
	out := map[string]string{}
	dir := filepath.Join(home, "icons")
	for _, a := range apps {
		app := appBundle(a.Command)
		if app == "" {
			continue
		}
		png := filepath.Join(dir, strings.TrimSuffix(filepath.Base(app), ".app")+".png")
		if !exists(png) {
			icns := bundleIcns(app)
			if icns == "" || os.MkdirAll(dir, 0o755) != nil || exec.Command("sips", "-s", "format", "png", "-Z", "64", icns, "--out", png).Run() != nil {
				continue
			}
		}
		if b, err := os.ReadFile(png); err == nil {
			out[a.Name] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
		}
	}
	return out
}
