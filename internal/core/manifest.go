package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Manifest declares an app as a plugin: actions placed in UI slots, plus agents and
// automations installed with it. Action `run` scripts may use ${{ var }} placeholders
// (like GitHub Actions), filled with shell-quoted values from the Vars table.
type Manifest struct {
	// Version is the manifest schema version; absent means 1. See migrateManifest.
	Version     int              `json:"version,omitempty"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Actions     []ManifestAction `json:"actions,omitempty"`
	Agents      []ManifestAgent  `json:"agents,omitempty"`
	Automations []ManifestAuto   `json:"automations,omitempty"`
}

// ManifestAction is one button. Output actions run to completion and toast their output.
type ManifestAction struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Run    string   `json:"run"`
	Places []string `json:"places"`
	Output bool     `json:"output,omitempty"`
	// Icon, a data:image/ URL (e.g. an inline SVG), shows the action as an icon button with
	// Label as its tooltip.
	Icon string `json:"icon,omitempty"`
	// IconPath is an image file (absolute or ~/, png/jpg/gif/webp/svg/icns, <= 1 MB) or a macOS .app
	// bundle (its icon is extracted) used instead of Icon. Install copies it to <home>/app-icons
	// as an image and the stored manifest points at the copy.
	IconPath string `json:"iconPath,omitempty"`
	// Services (v2), keyed by place, makes the action a long-running service there: Status
	// (exit 0 = running) lights the button's dot and a click then runs Stop instead of Run.
	// Each place gets its own service; places without one are plain buttons. Excludes Output.
	Services map[string]ManifestService `json:"services,omitempty"`
	// Service is the v1 form (one service, task_sidebar only); migrateManifest moves it to Services.
	Service *ManifestService `json:"service,omitempty"`
}

// ManifestService tells Conveyor how to start, detect and stop a service action; all are
// ${{ var }} scripts like Run. Run, when set, replaces the action's Run at that place.
type ManifestService struct {
	Run    string `json:"run,omitempty"`
	Status string `json:"status"`
	Stop   string `json:"stop"`
}

type ManifestAgent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model,omitempty"`
}

type ManifestAuto struct {
	Name       string `json:"name"`
	Schedule   string `json:"schedule"`
	Precheck   string `json:"precheck,omitempty"`
	ActionKind string `json:"action_kind"`
	ActionArg  string `json:"action_arg"`
}

// ManifestVersion is the schema version this build writes and understands.
const ManifestVersion = 2

// migrateManifest upgrades m to ManifestVersion in place, so installed apps keep working.
// When the schema changes, bump ManifestVersion and convert vN to vN+1 here.
// v1 -> v2: an action's single `service` becomes `services` at each of its places.
func migrateManifest(m *Manifest) error {
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Version > ManifestVersion {
		return fmt.Errorf("manifest: version %d is newer than this Conveyor supports (%d)", m.Version, ManifestVersion)
	}
	for i := range m.Actions {
		a := &m.Actions[i]
		switch {
		case m.Version == 1 && a.Services != nil:
			return fmt.Errorf("manifest: action %q: services needs \"version\": 2", a.ID)
		case m.Version >= 2 && a.Service != nil:
			return fmt.Errorf("manifest: action %q: service is version 1; use services keyed by place", a.ID)
		case a.Service != nil:
			a.Services = map[string]ManifestService{}
			for _, p := range a.Places {
				a.Services[p] = *a.Service
			}
			a.Service = nil
		}
	}
	m.Version = ManifestVersion
	return nil
}

// Placements are the UI slots an action can ask for; the first needs no task.
var Placements = map[string]bool{"global_nav": false, "task_sidebar": true, "board_card": true}

// ManifestVars are the placeholders available to `run`, e.g. ${{ task.key }}.
var ManifestVars = []string{"app.name", "task.key", "task.title", "task.status", "repo.name", "worktree.path"}

var (
	varRe = regexp.MustCompile(`\$\{\{\s*([\w.]+)\s*\}\}`)
	idRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// ParseManifest decodes and validates manifest JSON.
func ParseManifest(raw string) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("manifest: %w", err)
	}
	if err := migrateManifest(&m); err != nil {
		return m, err
	}
	if strings.TrimSpace(m.Name) == "" {
		return m, fmt.Errorf("manifest: name is required")
	}
	if len(m.Actions)+len(m.Agents)+len(m.Automations) == 0 {
		return m, fmt.Errorf("manifest: declare at least one action, agent or automation")
	}
	seen := map[string]bool{}
	for i, a := range m.Actions {
		if !idRe.MatchString(a.ID) || seen[a.ID] {
			return m, fmt.Errorf("manifest: action %d: id %q must be unique and match [a-z0-9-]+", i, a.ID)
		}
		seen[a.ID] = true
		if strings.TrimSpace(a.Label) == "" || strings.TrimSpace(a.Run) == "" || len(a.Places) == 0 {
			return m, fmt.Errorf("manifest: action %q needs label, run and places", a.ID)
		}
		for _, p := range a.Places {
			if _, ok := Placements[p]; !ok {
				return m, fmt.Errorf("manifest: action %q: unknown place %q (have global_nav, task_sidebar, board_card)", a.ID, p)
			}
		}
		if a.Icon != "" && (!strings.HasPrefix(a.Icon, "data:image/") || strings.Contains(a.Icon, "#")) {
			return m, fmt.Errorf("manifest: action %q: icon must be a data:image/ URL (write # as %%23)", a.ID)
		}
		if a.IconPath != "" {
			if a.Icon != "" {
				return m, fmt.Errorf("manifest: action %q: set icon or iconPath, not both", a.ID)
			}
			if _, ok := iconTypes[strings.ToLower(filepath.Ext(a.IconPath))]; !ok || !filepath.IsAbs(iconSrc(a.IconPath)) {
				return m, fmt.Errorf("manifest: action %q: iconPath must be an absolute (or ~/) png, jpg, gif, webp, svg or icns file, or .app bundle", a.ID)
			}
		}
		scripts := a.Run
		for p, sv := range a.Services {
			if !contains(a.Places, p) {
				return m, fmt.Errorf("manifest: action %q: service for %q, which is not in its places", a.ID, p)
			}
			if a.Output || strings.TrimSpace(sv.Status) == "" || strings.TrimSpace(sv.Stop) == "" {
				return m, fmt.Errorf("manifest: action %q: service at %s needs status and stop, and cannot be output", a.ID, p)
			}
			scripts += "\n" + sv.Run + "\n" + sv.Status + "\n" + sv.Stop
		}
		for _, v := range varRe.FindAllStringSubmatch(scripts, -1) {
			if !contains(ManifestVars, v[1]) {
				return m, fmt.Errorf("manifest: action %q: unknown variable %q (have %s)", a.ID, v[1], strings.Join(ManifestVars, ", "))
			}
		}
	}
	for _, a := range m.Automations {
		if a.Name == "" || a.ActionKind == "" {
			return m, fmt.Errorf("manifest: automations need name and action_kind")
		}
	}
	return m, nil
}

var iconTypes = map[string]string{".app": "", ".icns": "", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml"}

// iconSrc expands a leading ~/ in an iconPath.
func iconSrc(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, "~/") {
		return filepath.Join(h, p[2:])
	}
	return p
}

// iconURL returns the image at path as a data: URL, "" if it cannot be read.
func iconURL(path string) string {
	b, err := os.ReadFile(iconSrc(path))
	if err != nil {
		return ""
	}
	return "data:" + iconTypes[strings.ToLower(filepath.Ext(path))] + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// iconExt is the extension the installed copy of an iconPath gets: an .app yields a png.
func iconExt(p string) string {
	if e := strings.ToLower(filepath.Ext(p)); e != ".app" && e != ".icns" {
		return e
	}
	return ".png"
}

// appIconPNG renders the icon of a macOS .app bundle (its CFBundleIconFile .icns) as a 128px png.
// Bundles whose icon only lives in an asset catalog are not supported.
func appIconPNG(app string) ([]byte, error) {
	name, err := exec.Command("plutil", "-extract", "CFBundleIconFile", "raw", "-o", "-", filepath.Join(app, "Contents", "Info.plist")).Output()
	if err != nil {
		return nil, fmt.Errorf("no CFBundleIconFile in %s: %w", app, err)
	}
	n := strings.TrimSpace(string(name))
	if filepath.Ext(n) == "" {
		n += ".icns"
	}
	return icnsPNG(filepath.Join(app, "Contents", "Resources", n))
}

// icnsPNG renders an .icns file as a 128px png.
func icnsPNG(icns string) ([]byte, error) {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("conveyor-icon-%d.png", time.Now().UnixNano()))
	defer os.Remove(tmp)
	if out, err := exec.Command("sips", "-s", "format", "png", "-Z", "128", icns, "--out", tmp).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sips: %v: %s", err, out)
	}
	return os.ReadFile(tmp)
}

// portableManifest inlines iconPath files as icon data URLs, so the manifest works on another machine.
func portableManifest(raw string) (string, error) {
	m, err := ParseManifest(raw)
	if err != nil {
		return "", err
	}
	changed := false
	for i := range m.Actions {
		if a := &m.Actions[i]; a.IconPath != "" {
			a.Icon, a.IconPath, changed = iconURL(a.IconPath), "", true
		}
	}
	if !changed {
		return raw, nil
	}
	b, err := json.MarshalIndent(m, "", "  ")
	return string(b), err
}

// ManifestFromApp builds a v2 manifest JSON for an installed macOS app: one task_sidebar action
// that opens the worktree in it, with the bundle as iconPath (InstallManifest extracts the icon).
func ManifestFromApp(name string) (string, error) {
	app := ""
	for _, d := range appDirs() {
		if p := filepath.Join(d, name+".app"); exists(p) {
			app = p
			break
		}
	}
	if app == "" {
		return "", fmt.Errorf("%s.app not found in %s", name, strings.Join(appDirs(), ", "))
	}
	id := strings.Trim(nonAlnumPath.ReplaceAllString(strings.ToLower(name), "-"), "-")
	b, err := json.MarshalIndent(Manifest{
		Version: ManifestVersion, Name: name, Description: "Open the task worktree in " + name,
		Actions: []ManifestAction{{ID: id, Label: name, Run: "open -a " + shq(name) + " ${{ worktree.path }}", Places: []string{"task_sidebar"}, IconPath: app}},
	}, "", "  ")
	if err != nil {
		return "", err
	}
	_, err = ParseManifest(string(b))
	return string(b), err
}

func (s *Service) iconsDir() string { return filepath.Join(s.Home, "app-icons") }

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// RenderManifest fills ${{ var }} placeholders in a run script with shell-quoted values.
func RenderManifest(run string, vars map[string]string) string {
	return varRe.ReplaceAllStringFunc(run, func(m string) string {
		return shq(vars[varRe.FindStringSubmatch(m)[1]])
	})
}

// readIcons renders each action's iconPath (by action index) as image bytes.
func readIcons(m Manifest) (map[int][]byte, error) {
	icons := map[int][]byte{}
	for i, a := range m.Actions {
		if a.IconPath == "" {
			continue
		}
		var b []byte
		var err error
		switch strings.ToLower(filepath.Ext(a.IconPath)) {
		case ".app":
			b, err = appIconPNG(iconSrc(a.IconPath))
		case ".icns":
			b, err = icnsPNG(iconSrc(a.IconPath))
		default:
			b, err = os.ReadFile(iconSrc(a.IconPath))
		}
		if err != nil || len(b) > 1<<20 {
			return nil, fmt.Errorf("manifest: action %q: iconPath must be a readable file of at most 1 MB (%v)", a.ID, err)
		}
		icons[i] = b
	}
	return icons, nil
}

// writeIcons copies icons into <home>/app-icons, points m's iconPaths at the copies and returns
// the manifest JSON; with no icons it returns raw unchanged.
func (s *Service) writeIcons(m *Manifest, icons map[int][]byte, raw string) (string, error) {
	if len(icons) == 0 {
		return raw, nil
	}
	if err := os.MkdirAll(s.iconsDir(), 0o755); err != nil {
		return "", err
	}
	for i, b := range icons {
		x := &m.Actions[i]
		p := filepath.Join(s.iconsDir(), nonAlnumPath.ReplaceAllString(m.Name, "-")+"-"+x.ID+iconExt(x.IconPath))
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return "", err
		}
		x.IconPath = p
	}
	out, err := json.MarshalIndent(m, "", "  ")
	return string(out), err
}

// InstallManifest validates raw, stores the app and creates its agents (user scope) and
// automations (disabled until the user enables them). A failure rolls the app back.
// An app of the same name is an error unless replace, which uninstalls it first.
func (s *Service) InstallManifest(raw string, replace bool) (store.App, error) {
	m, err := ParseManifest(raw)
	if err != nil {
		return store.App{}, err
	}
	// read icons before a replace uninstalls the copies an exported manifest points at
	icons, err := readIcons(m)
	if err != nil {
		return store.App{}, err
	}
	if _, err := store.GetApp(s.DB, m.Name); err == nil && replace {
		// ponytail: not atomic; a failed install after this leaves the old app gone
		if err := s.UninstallApp(m.Name); err != nil {
			return store.App{}, err
		}
	}
	if raw, err = s.writeIcons(&m, icons, raw); err != nil {
		return store.App{}, err
	}
	a, err := store.CreateApp(s.DB, store.App{Name: m.Name, Manifest: raw})
	if err != nil {
		return a, err
	}
	for _, g := range m.Agents {
		if err = s.AddAgent(g.Name, g.Description, g.Prompt, "user", g.Model); err != nil {
			break
		}
	}
	if err == nil {
		for _, x := range m.Automations {
			if _, err = store.CreateAutomation(s.DB, store.Automation{Name: x.Name, Schedule: x.Schedule, Precheck: x.Precheck, ActionKind: x.ActionKind, ActionArg: x.ActionArg}); err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = s.UninstallApp(m.Name)
		return store.App{}, err
	}
	return a, nil
}

// UninstallApp deletes the app; a manifest app's automations and agents go with it.
func (s *Service) UninstallApp(name string) error {
	a, err := store.GetApp(s.DB, name)
	if err != nil {
		return err
	}
	if a.Manifest != "" {
		if m, err := ParseManifest(a.Manifest); err == nil {
			for _, x := range m.Automations {
				_ = store.DeleteAutomation(s.DB, x.Name)
			}
			for _, x := range m.Actions {
				if x.IconPath != "" && filepath.Dir(x.IconPath) == s.iconsDir() {
					_ = os.Remove(x.IconPath)
				}
			}
			for _, g := range m.Agents {
				_ = os.Remove(userAgentsDir() + "/" + g.Name + ".md")
			}
		}
	}
	return store.DeleteApp(s.DB, name)
}

// Placed is one manifest action ready to show in a UI slot.
type Placed struct {
	App     string `json:"app"`
	ID      string `json:"id"`
	Label   string `json:"label"`
	Output  bool   `json:"output"`
	Service bool   `json:"service"`
	Icon    string `json:"icon,omitempty"`
}

// PlacedActions lists the manifest actions that asked for place.
func (s *Service) PlacedActions(place string) ([]Placed, error) {
	apps, err := store.ListApps(s.DB)
	if err != nil {
		return nil, err
	}
	out := []Placed{}
	for _, a := range apps {
		if a.Manifest == "" {
			continue
		}
		m, err := ParseManifest(a.Manifest)
		if err != nil {
			continue
		}
		for _, x := range m.Actions {
			if contains(x.Places, place) {
				_, svc := x.Services[place]
				icon := x.Icon
				if x.IconPath != "" {
					icon = iconURL(x.IconPath)
				}
				out = append(out, Placed{a.Name, x.ID, x.Label, x.Output, svc, icon})
			}
		}
	}
	return out, nil
}

// RunAppAction runs one manifest action as shown at place (whose service may override Run;
// "" = the action's own Run). key "" means no task (worktree.path is then the home directory).
// Returns an output action's output.
func (s *Service) RunAppAction(app, id, place, key string) (string, error) {
	act, vars, cwd, err := s.resolveAction(app, id, key)
	if err != nil {
		return "", err
	}
	run := act.Run
	if sv := act.Services[place]; sv.Run != "" {
		run = sv.Run
	}
	return RunApp(store.App{Name: act.Label, Command: RenderManifest(run, vars), Output: act.Output}, cwd, key)
}

// AppServiceRunning reports whether the action's service at place has its status script exit 0.
func (s *Service) AppServiceRunning(app, id, place, key string) (bool, error) {
	act, vars, cwd, err := s.resolveAction(app, id, key)
	if err != nil {
		return false, err
	}
	sv, ok := act.Services[place]
	if !ok {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", RenderManifest(sv.Status, vars))
	c.Dir = cwd
	return c.Run() == nil, nil
}

// StopAppService runs the stop script of the action's service at place.
func (s *Service) StopAppService(app, id, place, key string) error {
	act, vars, _, err := s.resolveAction(app, id, key)
	if err != nil {
		return err
	}
	sv, ok := act.Services[place]
	if !ok {
		return fmt.Errorf("action %q has no service at %q", id, place)
	}
	return runShell("stop "+act.Label, RenderManifest(sv.Stop, vars))
}

// resolveAction finds the action and the variables and directory its scripts run with.
func (s *Service) resolveAction(app, id, key string) (*ManifestAction, map[string]string, string, error) {
	a, err := store.GetApp(s.DB, app)
	if err != nil {
		return nil, nil, "", err
	}
	m, err := ParseManifest(a.Manifest)
	if err != nil {
		return nil, nil, "", err
	}
	var act *ManifestAction
	for i := range m.Actions {
		if m.Actions[i].ID == id {
			act = &m.Actions[i]
		}
	}
	if act == nil {
		return nil, nil, "", fmt.Errorf("app %q has no action %q", app, id)
	}
	vars := map[string]string{"app.name": m.Name}
	cwd := ""
	if key == "" {
		if cwd, err = os.UserHomeDir(); err != nil {
			return nil, nil, "", err
		}
	} else {
		t, err := store.GetTaskByKey(s.DB, key)
		if err != nil {
			return nil, nil, "", err
		}
		if cwd, err = s.WorktreePath(key, ""); err != nil {
			return nil, nil, "", err
		}
		vars["task.key"], vars["task.title"], vars["task.status"] = t.Key, t.Title, string(t.Status)
		for _, r := range t.Repos {
			if r.IsPrimary {
				vars["repo.name"] = r.RepoName
			}
		}
	}
	vars["worktree.path"] = cwd
	return act, vars, cwd, nil
}

// spotlightService detects and stops the spotlight instance mirroring ${{ worktree.path }}:
// `spotlight list` prints "ROOT  <-  WORKTREE  (pid N)" and stop takes the root.
func spotlightService() ManifestService {
	bin := shq(spotlightBin())
	root := bin + ` list | awk -v w=${{ worktree.path }} -F '  <-  ' '{sub(/  \(pid [0-9]+\)$/, "", $2); if ($2 == w) print $1}'`
	return ManifestService{Status: root + " | grep -q .", Stop: `r=$(` + root + `); [ -n "$r" ] && ` + bin + ` stop "$r"`}
}

// LegacyManifest expresses a plain command app (the old {cwd}/{key} template) as a manifest:
// one action in the nav bar (global) or task sidebar, with the icon of the .app it opens. Unlike the legacy sidebar icons, the
// action runs in the task's primary worktree only.
func LegacyManifest(a store.App) (string, error) {
	place := "task_sidebar"
	if a.Global {
		place = "global_nav"
	}
	run := strings.NewReplacer("{cwd}", "${{ worktree.path }}", "{key}", "${{ task.key }}").Replace(a.Command)
	act := ManifestAction{ID: "open", Label: a.Name, Run: run, Places: []string{place}, Output: a.Output, IconPath: appBundle(a.Command)}
	if a.Name == "Spotlight" && !a.Global && !a.Output {
		act.Services = map[string]ManifestService{place: spotlightService()}
	}
	b, err := json.MarshalIndent(Manifest{Version: ManifestVersion, Name: a.Name, Actions: []ManifestAction{act}}, "", "  ")
	return string(b), err
}

// ConvertApp turns the plain command app into a manifest app in place.
func (s *Service) ConvertApp(name string) error {
	a, err := store.GetApp(s.DB, name)
	if err != nil {
		return err
	}
	if a.Manifest != "" {
		return fmt.Errorf("app %q is already a manifest app", name)
	}
	raw, err := LegacyManifest(a)
	if err != nil {
		return err
	}
	return s.setManifest(a, raw)
}

// setManifest validates raw, extracts its icons and stores it as a's manifest.
func (s *Service) setManifest(a store.App, raw string) error {
	m, err := ParseManifest(raw)
	if err != nil {
		return err
	}
	icons, err := readIcons(m)
	if err != nil {
		return err
	}
	if raw, err = s.writeIcons(&m, icons, raw); err != nil {
		return err
	}
	return store.UpdateApp(s.DB, store.App{ID: a.ID, Name: a.Name, Manifest: raw})
}

// BackfillAppIcons gives every manifest action that opens an installed .app (`open -a NAME …`)
// and has no icon that app's icon. Apps whose bundle is missing are left alone.
func (s *Service) BackfillAppIcons() error {
	apps, err := store.ListApps(s.DB)
	if err != nil {
		return err
	}
	for _, a := range apps {
		m, err := ParseManifest(a.Manifest)
		if a.Manifest == "" || err != nil {
			continue
		}
		changed := false
		for i := range m.Actions {
			x := &m.Actions[i]
			if x.Icon == "" && x.IconPath == "" {
				if x.IconPath = appBundle(strings.NewReplacer("${{ worktree.path }}", "{cwd}").Replace(x.Run)); x.IconPath != "" {
					changed = true
				}
			}
		}
		if !changed {
			continue
		}
		b, err := json.MarshalIndent(m, "", "  ")
		if err == nil {
			err = s.setManifest(a, string(b))
		}
		if err != nil {
			return fmt.Errorf("backfill icon for %q: %w", a.Name, err)
		}
	}
	return nil
}

// InstallLegacy installs a plain command app (e.g. a detected preset) as a manifest app.
func (s *Service) InstallLegacy(a store.App) (store.App, error) {
	raw, err := LegacyManifest(a)
	if err != nil {
		return store.App{}, err
	}
	return s.InstallManifest(raw, false)
}

// ExportApp returns the manifest JSON of a manifest app, ready for InstallManifest elsewhere.
func (s *Service) ExportApp(name string) (string, error) {
	a, err := store.GetApp(s.DB, name)
	if err != nil {
		return "", err
	}
	if a.Manifest == "" {
		return "", fmt.Errorf("app %q is not a manifest app; convert it first", name)
	}
	return portableManifest(a.Manifest)
}
