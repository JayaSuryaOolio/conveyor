package core

import (
	"os"
	"path/filepath"
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

// Completions feeds the editor's @file and /command autocomplete.
type Completions struct {
	Files    []string  `json:"files"`
	Commands []Command `json:"commands"`
}

const maxFiles = 20000 // ponytail: whole list sent to the UI; switch to a server-side query if repos outgrow this

// Completions lists the tracked files and slash commands of the task's repos (its worktrees
// when they exist), or of the named repos when key is "". Files of repos after the first are
// prefixed "../<repo>/", as a session started in the first repo sees them. User-level
// skills and commands come last.
func (s *Service) Completions(key string, repos []string) (Completions, error) {
	type src struct{ name, dir string }
	var srcs []src
	if key != "" {
		t, err := store.GetTaskByKey(s.DB, key)
		if err != nil {
			return Completions{}, err
		}
		for _, r := range t.Repos {
			d := ""
			if r.WorktreePath != nil {
				if _, err := os.Stat(*r.WorktreePath); err == nil {
					d = *r.WorktreePath
				}
			}
			srcs = append(srcs, src{r.RepoName, d})
		}
	} else {
		for _, n := range repos {
			srcs = append(srcs, src{n, ""})
		}
	}
	out := Completions{Files: []string{}, Commands: []Command{}}
	seen := map[string]bool{}
	for i, r := range srcs {
		dir := r.dir
		if dir == "" {
			repo, err := store.GetRepoByName(s.DB, r.name)
			if err != nil {
				continue // unregistered repo: nothing to suggest
			}
			dir = repo.Path
		}
		if ls, err := git(dir, "ls-files"); err == nil && ls != "" {
			for _, f := range strings.Split(ls, "\n") {
				if len(out.Files) >= maxFiles {
					break
				}
				if i > 0 {
					f = "../" + r.name + "/" + f
				}
				out.Files = append(out.Files, f)
			}
		}
		out.Commands = append(out.Commands, scanCommands(filepath.Join(dir, ".claude"), seen)...)
	}
	h, _ := os.UserHomeDir()
	out.Commands = append(out.Commands, scanCommands(filepath.Join(h, ".claude"), seen)...)
	return out, nil
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
