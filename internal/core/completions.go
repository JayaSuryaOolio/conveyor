package core

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Command is a Claude Code slash command: a skill or a .claude/commands file.
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Suggestion is one editor autocomplete entry; Insert replaces the typed @ or / token.
type Suggestion struct {
	Insert string `json:"insert"`
	Label  string `json:"label"`
	Hint   string `json:"hint"`
}

const maxSuggestions = 8

type refDir struct{ name, dir string }

// refDirs is where the task's @file and /command references resolve: its repos' worktrees when
// they exist, else their main checkouts; the named repos when key is "". The first is the
// session's cwd; files of the others are written "../<repo>/<file>".
func (s *Service) refDirs(key string, repos []string) ([]refDir, error) {
	var out []refDir
	if key != "" {
		t, err := store.GetTaskByKey(s.DB, key)
		if err != nil {
			return nil, err
		}
		repos = nil
		for _, r := range t.Repos {
			if r.WorktreePath != nil {
				if _, err := os.Stat(*r.WorktreePath); err == nil {
					out = append(out, refDir{r.RepoName, *r.WorktreePath})
					continue
				}
			}
			repos = append(repos, r.RepoName)
		}
	}
	for _, n := range repos {
		if repo, err := store.GetRepoByName(s.DB, n); err == nil { // unregistered repo: skipped
			out = append(out, refDir{n, repo.Path})
		}
	}
	return out, nil
}

// match ranks s against the lower-cased query: 0 = s or its basename starts with q, 1 = contains q, -1 = no match.
func match(s, q string) int {
	s = strings.ToLower(s)
	base := s[strings.LastIndex(s, "/")+1:]
	switch {
	case strings.HasPrefix(s, q) || strings.HasPrefix(base, q):
		return 0
	case strings.Contains(s, q):
		return 1
	}
	return -1
}

// Complete returns up to 8 suggestions for the token being typed: kind "@" matches tracked
// files, "/" matches skills and commands (the repos' first, then the user's). Prefix matches
// rank before substring matches. Files are streamed from git ls-files per call, never cached.
func (s *Service) Complete(key string, repos []string, kind, q string) ([]Suggestion, error) {
	dirs, err := s.refDirs(key, repos)
	if err != nil {
		return nil, err
	}
	q = strings.ToLower(q)
	var best, rest []Suggestion
	add := func(sg Suggestion, name string) bool { // true once 8 prefix matches are in
		switch match(name, q) {
		case 0:
			best = append(best, sg)
		case 1:
			if len(rest) < maxSuggestions {
				rest = append(rest, sg)
			}
		}
		return len(best) >= maxSuggestions
	}
	if kind == "@" {
	files:
		for i, d := range dirs {
			prefix := ""
			if i > 0 {
				prefix = "../" + d.name + "/"
			}
			cmd := exec.Command("git", "-C", d.dir, "ls-files")
			out, err := cmd.StdoutPipe()
			if err != nil || cmd.Start() != nil {
				continue
			}
			sc := bufio.NewScanner(out)
			for sc.Scan() {
				f := prefix + sc.Text()
				if add(Suggestion{Insert: "@" + f, Label: f}, f) {
					cmd.Process.Kill()
					cmd.Wait()
					break files
				}
			}
			cmd.Wait()
		}
	} else {
		seen := map[string]bool{}
		var cmds []Command
		for _, d := range dirs {
			cmds = append(cmds, scanCommands(filepath.Join(d.dir, ".claude"), seen)...)
		}
		h, _ := os.UserHomeDir()
		cmds = append(cmds, scanCommands(filepath.Join(h, ".claude"), seen)...)
		for _, c := range cmds {
			if add(Suggestion{Insert: "/" + c.Name, Label: "/" + c.Name, Hint: c.Description}, c.Name) {
				break
			}
		}
	}
	out := append(best, rest...)
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out, nil
}

// FileOpeners lists the apps an @file reference of the task can open in: the configured preset
// editors, then Finder (reveal). It errors when the reference is not a file in the task's repos.
func (s *Service) FileOpeners(key, ref string) ([]string, error) {
	if _, err := s.resolveRef(key, ref); err != nil {
		return nil, err
	}
	apps, err := store.ListApps(s.DB)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, a := range apps {
		if slices.Contains(editorApps, a.Name) {
			out = append(out, a.Name)
		}
	}
	return append(out, "Finder"), nil
}

// resolveRef maps an @file reference to an existing file inside one of the task's repo directories.
func (s *Service) resolveRef(key, ref string) (string, error) {
	dirs, err := s.refDirs(key, nil)
	if err != nil {
		return "", err
	}
	if len(dirs) == 0 {
		return "", fmt.Errorf("%s has no repo checkout", key)
	}
	ref = strings.TrimPrefix(ref, "@")
	d, rel := dirs[0], ref
	if rest, ok := strings.CutPrefix(ref, "../"); ok {
		name, file, _ := strings.Cut(rest, "/")
		for _, x := range dirs {
			if x.name == name {
				d, rel = x, file
			}
		}
	}
	p := filepath.Join(d.dir, rel)
	if !strings.HasPrefix(p, filepath.Clean(d.dir)+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repo", ref)
	}
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return "", fmt.Errorf("%s is not a file in %s", ref, d.name)
	}
	return p, nil
}

// OpenFile opens an @file reference of the task in app, one of its FileOpeners.
func (s *Service) OpenFile(key, ref, app string) error {
	ok, err := s.FileOpeners(key, ref)
	if err != nil {
		return err
	}
	if !slices.Contains(ok, app) {
		return fmt.Errorf("%q cannot open files", app)
	}
	p, _ := s.resolveRef(key, ref)
	if app == "Finder" {
		return runShell("reveal in Finder", "open -R "+shq(p))
	}
	return runShell("open file", "open -a "+shq(app)+" "+shq(p))
}

// scanCommands reads <dir>/skills/*/SKILL.md and <dir>/commands/**/*.md (nested dirs become "a:b").
func scanCommands(dir string, seen map[string]bool) []Command {
	var out []Command
	add := func(name, file string) {
		if seen[name] {
			return
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return
		}
		fm, _ := claude.Frontmatter(b)
		if fm["name"] != "" {
			name = fm["name"]
		}
		seen[name] = true
		out = append(out, Command{Name: name, Description: fm["description"]})
	}
	skills, _ := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md"))
	for _, f := range skills {
		add(filepath.Base(filepath.Dir(f)), f)
	}
	cmds := filepath.Join(dir, "commands")
	filepath.WalkDir(cmds, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			rel, _ := filepath.Rel(cmds, strings.TrimSuffix(p, ".md"))
			add(strings.ReplaceAll(filepath.ToSlash(rel), "/", ":"), p)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
