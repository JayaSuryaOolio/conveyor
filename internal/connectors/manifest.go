package connectors

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Manifest declares an app as a plugin: actions placed in UI slots, plus agents and
// automations installed with it. Action `run` scripts may use ${{ var }} placeholders
// (like GitHub Actions), filled with shell-quoted values from the Vars table.
type Manifest struct {
	// Version is the manifest schema version; absent means 1. See migrateManifest.
	Version int    `json:"version,omitempty"`
	Name    string `json:"name"`
	// ID names the app's runtime files (logs/app/<id>.log, apps/<id>.pid) and is ${{ connector.id }}; default: Name as a slug.
	ID          string `json:"id,omitempty"`
	Description string `json:"description,omitempty"`
	// Check is a sample-run script (`sh -c`, 30s); install fails, storing nothing, if it exits non-zero.
	Check string `json:"check,omitempty"`
	// Settings (v3) are user-editable values, shown in the app's settings modal and usable in scripts as ${{ setting.key }}.
	Settings    []ManifestSetting `json:"settings,omitempty"`
	Actions     []ManifestAction  `json:"actions,omitempty"`
	Agents      []ManifestAgent   `json:"agents,omitempty"`
	Automations []ManifestAuto    `json:"automations,omitempty"`
	// Inject (v4) adds text to what agents see while the app is enabled.
	Inject *ManifestInject `json:"inject,omitempty"`
}

// ManifestInject is plain text (no placeholders) Conveyor adds for every task while the app is enabled.
type ManifestInject struct {
	// Prompt is appended to each launch prompt (fresh and resume).
	Prompt string `json:"prompt,omitempty"`
	// Brief becomes a "## App: <name>" section of BRIEF.md.
	Brief string `json:"brief,omitempty"`
	// Review is appended to the comment that hands PR review feedback to the agent.
	Review string `json:"review,omitempty"`
}

// ManifestSetting declares one setting, modelled on VS Code extension settings. Type is string,
// boolean, number or enum (then Options lists the choices); Default is a JSON value of that type.
type ManifestSetting struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Default     any      `json:"default"`
	Description string   `json:"description,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// ManifestAction is one button. Output actions run to completion and toast their output.
type ManifestAction struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Run    string   `json:"run"`
	Places []string `json:"places"`
	Output bool     `json:"output,omitempty"`
	// Timeline (v4) records a "connector" entry in the task's timeline whenever the action runs for a task.
	Timeline bool `json:"timeline,omitempty"`
	// Confirm, when set, is asked (OK/Cancel) before the action runs or starts its service; stopping is never confirmed.
	Confirm string `json:"confirm,omitempty"`
	// Icon, a data:image/ URL (e.g. an inline SVG), shows the action as an icon button with
	// Label as its tooltip.
	Icon string `json:"icon,omitempty"`
	// IconPath is an image file (absolute or ~/, png/jpg/gif/webp/svg/icns, <= 1 MB) or a macOS .app
	// bundle (its icon is extracted) used instead of Icon. Install copies it to <home>/connector-icons
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
	// Autostart makes the dispatcher start this service (Run) whenever Status fails while the app is enabled.
	// It runs with no task, so scripts must not use task.* or repo.* variables.
	Autostart bool `json:"autostart,omitempty"`
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
const ManifestVersion = 4

// migrateManifest upgrades m to ManifestVersion in place, so installed apps keep working.
// When the schema changes, bump ManifestVersion and convert vN to vN+1 here.
// v1 -> v2: an action's single `service` becomes `services` at each of its places.
// v2 -> v3: adds `settings`; nothing to convert.
// v3 -> v4: adds `inject` and an action's `timeline`; nothing to convert.
func migrateManifest(m *Manifest) error {
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Version > ManifestVersion {
		return fmt.Errorf("manifest: version %d is newer than this Conveyor supports (%d)", m.Version, ManifestVersion)
	}
	if m.Version < 3 && len(m.Settings) > 0 {
		return fmt.Errorf("manifest: settings needs \"version\": 3")
	}
	if m.Version < 4 {
		if m.Inject != nil {
			return fmt.Errorf("manifest: inject needs \"version\": 4")
		}
		for _, a := range m.Actions {
			if a.Timeline {
				return fmt.Errorf("manifest: action %q: timeline needs \"version\": 4", a.ID)
			}
		}
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
var ManifestVars = []string{"connector.name", "connector.id", "task.key", "task.title", "task.status", "repo.name", "worktree.path"}

// legacyVars are the pre-rebrand names of connector.name and connector.id; old manifests keep working.
var legacyVars = map[string]string{"app.name": "connector.name", "app.id": "connector.id"}

var (
	varRe = regexp.MustCompile(`\$\{\{\s*([\w.]+)\s*\}\}`)
	idRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	// settingKeyRe keeps keys usable as ${{ setting.key }}.
	settingKeyRe = regexp.MustCompile(`^[a-z0-9_]+$`)
)

// ConnectorID is the manifest's id, else its name as a slug ("Review webhooks" -> "review-webhooks").
func (m Manifest) ConnectorID() string {
	if m.ID != "" {
		return m.ID
	}
	return strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(m.Name), "-"), "-")
}

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
	if m.ID != "" && !idRe.MatchString(m.ID) {
		return m, fmt.Errorf("manifest: id %q must match [a-z0-9-]+", m.ID)
	}
	keys := map[string]bool{}
	for _, st := range m.Settings {
		if !settingKeyRe.MatchString(st.Key) || keys[st.Key] {
			return m, fmt.Errorf("manifest: setting key %q must be unique and match [a-z0-9_]+", st.Key)
		}
		keys[st.Key] = true
		if err := st.check(fmt.Sprint(st.Default)); err != nil {
			return m, fmt.Errorf("manifest: setting %q default: %w", st.Key, err)
		}
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
			if !slices.Contains(a.Places, p) {
				return m, fmt.Errorf("manifest: action %q: service for %q, which is not in its places", a.ID, p)
			}
			if a.Output || strings.TrimSpace(sv.Status) == "" || strings.TrimSpace(sv.Stop) == "" {
				return m, fmt.Errorf("manifest: action %q: service at %s needs status and stop, and cannot be output", a.ID, p)
			}
			scripts += "\n" + sv.Run + "\n" + sv.Status + "\n" + sv.Stop
		}
		for _, v := range varRe.FindAllStringSubmatch(scripts, -1) {
			if k, ok := strings.CutPrefix(v[1], "setting."); !slices.Contains(ManifestVars, v[1]) && legacyVars[v[1]] == "" && !(ok && keys[k]) {
				return m, fmt.Errorf("manifest: action %q: unknown variable %q (have %s, setting.<key>)", a.ID, v[1], strings.Join(ManifestVars, ", "))
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

// PortableManifest inlines iconPath files as icon data URLs, so the manifest works on another machine.
func PortableManifest(raw string) (string, error) {
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

// IconsDir is where installed manifests' icon copies live.
func IconsDir(home string) string { return filepath.Join(home, "connector-icons") }

// RenderManifest fills ${{ var }} placeholders in a run script with shell-quoted values.
func RenderManifest(run string, vars map[string]string) string {
	return varRe.ReplaceAllStringFunc(run, func(m string) string {
		return shq(vars[varRe.FindStringSubmatch(m)[1]])
	})
}

// ReadIcons renders each action's iconPath (by action index) as image bytes.
func ReadIcons(m Manifest) (map[int][]byte, error) {
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

// WriteIcons copies icons into <home>/connector-icons, points m's iconPaths at the copies and returns
// the manifest JSON; with no icons it returns raw unchanged.
func WriteIcons(home string, m *Manifest, icons map[int][]byte, raw string) (string, error) {
	if len(icons) == 0 {
		return raw, nil
	}
	if err := os.MkdirAll(IconsDir(home), 0o755); err != nil {
		return "", err
	}
	for i, b := range icons {
		x := &m.Actions[i]
		p := filepath.Join(IconsDir(home), nonAlnumPath.ReplaceAllString(m.Name, "-")+"-"+x.ID+iconExt(x.IconPath))
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return "", err
		}
		x.IconPath = p
	}
	out, err := json.MarshalIndent(m, "", "  ")
	return string(out), err
}

// Placed is one manifest action ready to show in a UI slot.
type Placed struct {
	Connector string `json:"connector"`
	ID        string `json:"id"`
	Label     string `json:"label"`
	Output    bool   `json:"output"`
	Service   bool   `json:"service"`
	Icon      string `json:"icon,omitempty"`
	Confirm   string `json:"confirm,omitempty"`
}

// PlacedActions lists the manifest actions that asked for place.
func PlacedActions(db *sql.DB, place string) ([]Placed, error) {
	apps, err := store.ListConnectors(db)
	if err != nil {
		return nil, err
	}
	out := []Placed{}
	for _, a := range apps {
		if a.Manifest == "" || a.Disabled {
			continue
		}
		m, err := ParseManifest(a.Manifest)
		if err != nil {
			continue
		}
		for _, x := range m.Actions {
			if slices.Contains(x.Places, place) {
				_, svc := x.Services[place]
				icon := x.Icon
				if x.IconPath != "" {
					icon = iconURL(x.IconPath)
				}
				out = append(out, Placed{a.Name, x.ID, x.Label, x.Output, svc, icon, x.Confirm})
			}
		}
	}
	return out, nil
}

// RunConnectorAction runs one manifest action as shown at place (whose service may override Run;
// "" = the action's own Run). key "" means no task (worktree.path is then the home directory).
// Returns an output action's output.
func RunConnectorAction(db *sql.DB, app, id, place, key string) (string, error) {
	return runConnectorAction(db, app, id, place, key, "")
}

// appTimeout bounds an output action (a quick action); launching actions return at once.
var appTimeout = 2 * time.Minute

// runConnectorAction is RunConnectorAction in the worktree of the task's repo ("" = primary).
func runConnectorAction(db *sql.DB, app, id, place, key, repo string) (string, error) {
	act, vars, cwd, err := resolveAction(db, app, id, key, repo)
	if err != nil {
		return "", err
	}
	run := act.Run
	if sv := act.Services[place]; sv.Run != "" {
		run = sv.Run
	}
	cmd := RenderManifest(run, vars)
	if act.Timeline && key != "" {
		if t, err := store.GetTaskByKey(db, key); err == nil {
			_ = store.RecordEvent(db, t.ID, "connector", app+": "+act.Label)
		}
	}
	if !act.Output {
		return "", runShell("open app", cmd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = cwd
	b, err := c.CombinedOutput()
	out := strings.TrimSpace(string(b))
	if len(out) > 4096 {
		out = out[:4096] + "…"
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", act.Label, err, out)
	}
	return out, nil
}

// ConnectorServiceRunning reports whether the action's service at place has its status script exit 0.
func ConnectorServiceRunning(db *sql.DB, app, id, place, key string) (bool, error) {
	act, vars, cwd, err := resolveAction(db, app, id, key, "")
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

// StopConnectorService runs the stop script of the action's service at place.
func StopConnectorService(db *sql.DB, app, id, place, key string) error {
	act, vars, _, err := resolveAction(db, app, id, key, "")
	if err != nil {
		return err
	}
	sv, ok := act.Services[place]
	if !ok {
		return fmt.Errorf("action %q has no service at %q", id, place)
	}
	return runShell("stop "+act.Label, RenderManifest(sv.Stop, vars))
}

// resolveAction finds the action and the variables and directory its scripts run with (the worktree of repo, "" = primary).
func resolveAction(db *sql.DB, app, id, key, repo string) (*ManifestAction, map[string]string, string, error) {
	a, err := store.GetConnector(db, app)
	if err != nil {
		return nil, nil, "", err
	}
	if a.Disabled {
		return nil, nil, "", fmt.Errorf("app %q is disabled", app)
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
	vars := map[string]string{"connector.name": m.Name, "connector.id": m.ConnectorID()}
	for old, cur := range legacyVars {
		vars[old] = vars[cur]
	}
	vals, err := settingValues(db, m, app)
	if err != nil {
		return nil, nil, "", err
	}
	for k, v := range vals {
		vars["setting."+k] = v
	}
	cwd := ""
	if key == "" {
		if cwd, err = os.UserHomeDir(); err != nil {
			return nil, nil, "", err
		}
	} else {
		t, err := store.GetTaskByKey(db, key)
		if err != nil {
			return nil, nil, "", err
		}
		if cwd, err = WorktreePath(db, key, repo); err != nil {
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

// graftService detects and stops the graft instance mirroring ${{ worktree.path }}:
// `graft list` prints "ROOT  <-  WORKTREE  (pid N)" and stop takes the root.
func graftService() ManifestService {
	bin := shq(GraftBin())
	root := bin + ` list | awk -v w=${{ worktree.path }} -F '  <-  ' '{sub(/  \(pid [0-9]+\)$/, "", $2); if ($2 == w) print $1}'`
	return ManifestService{Status: root + " | grep -q .", Stop: `r=$(` + root + `); [ -n "$r" ] && ` + bin + ` stop "$r"`}
}

// CommandManifest expresses a shell command ({cwd} and {key} placeholders) as a manifest app:
// one action in the task sidebar, with the icon of the .app it opens. output makes it a quick
// action. The action runs in the task's primary worktree only.
func CommandManifest(name, command string, output bool) string {
	place := "task_sidebar"
	run := strings.NewReplacer("{cwd}", "${{ worktree.path }}", "{key}", "${{ task.key }}").Replace(command)
	act := ManifestAction{ID: "open", Label: name, Run: run, Places: []string{place}, Output: output, IconPath: appBundle(command)}
	if name == "Graft" && !output {
		act.Services = map[string]ManifestService{place: graftService()}
	}
	b, _ := json.MarshalIndent(Manifest{Version: ManifestVersion, Name: name, Actions: []ManifestAction{act}}, "", "  ")
	return string(b)
}

// MigratePlain converts the legacy plain command apps (apps.command/output columns) into
// manifest apps, then drops the columns. Idempotent; run once at startup.
func MigratePlain(db *sql.DB, home string) error {
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('connectors') WHERE name='command'`).Scan(&n); err != nil || n == 0 {
		return err
	}
	out := "0"
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('connectors') WHERE name='output'`).Scan(&n); err != nil {
		return err
	} else if n > 0 {
		out = "output"
	}
	rows, err := db.Query(`SELECT id, name, command, ` + out + ` FROM connectors WHERE manifest=''`)
	if err != nil {
		return err
	}
	type plain struct {
		id            int64
		name, command string
		output        bool
	}
	var todo []plain
	for rows.Next() {
		var p plain
		if err := rows.Scan(&p.id, &p.name, &p.command, &p.output); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()
	for _, p := range todo {
		raw, err := withIcons(home, CommandManifest(p.name, p.command, p.output))
		if err != nil {
			return fmt.Errorf("migrate app %q: %w", p.name, err)
		}
		if _, err := db.Exec(`UPDATE connectors SET manifest=? WHERE id=?`, raw, p.id); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`ALTER TABLE connectors DROP COLUMN command`); err != nil {
		return err
	}
	if out == "output" {
		_, err = db.Exec(`ALTER TABLE connectors DROP COLUMN output`)
	}
	return err
}

// withIcons copies the icons raw's actions point at under home and returns the manifest pointing at the copies.
func withIcons(home, raw string) (string, error) {
	m, err := ParseManifest(raw)
	if err != nil {
		return "", err
	}
	icons, err := ReadIcons(m)
	if err != nil {
		return "", err
	}
	return WriteIcons(home, &m, icons, raw)
}

// ExportConnector returns the manifest JSON of a manifest app, ready for InstallManifest elsewhere.
func ExportConnector(db *sql.DB, name string) (string, error) {
	a, err := store.GetConnector(db, name)
	if err != nil {
		return "", err
	}
	return PortableManifest(a.Manifest)
}

// EnsureServices starts every enabled app's autostart service that is not running; errors are returned together.
func EnsureServices(db *sql.DB) error {
	apps, err := store.ListConnectors(db)
	if err != nil {
		return err
	}
	var errs []error
	for _, a := range apps {
		m, perr := ParseManifest(a.Manifest)
		if a.Disabled || perr != nil {
			continue
		}
		for _, x := range m.Actions {
			for place, sv := range x.Services {
				if !sv.Autostart {
					continue
				}
				if up, _ := ConnectorServiceRunning(db, a.Name, x.ID, place, ""); up {
					continue
				}
				if _, err := RunConnectorAction(db, a.Name, x.ID, place, ""); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errors.Join(errs...)
}

// Injected joins the text enabled apps inject with pick (e.g. func(i ManifestInject) string { return i.Prompt }),
// one block per app, headed by the app name when named is true. "" when no app injects any.
func Injected(db *sql.DB, named bool, pick func(ManifestInject) string) string {
	list, _ := store.ListConnectors(db)
	var out []string
	for _, a := range list {
		m, err := ParseManifest(a.Manifest)
		if a.Manifest == "" || a.Disabled || err != nil || m.Inject == nil {
			continue
		}
		if t := strings.TrimSpace(pick(*m.Inject)); t != "" {
			if named {
				t = "## App: " + m.Name + "\n\n" + t
			}
			out = append(out, t)
		}
	}
	return strings.Join(out, "\n\n")
}
