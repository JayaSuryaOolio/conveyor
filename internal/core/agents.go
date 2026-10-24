package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Agent is a Claude Code subagent file. Scope is the repo name or "user".
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
}

var agentName = regexp.MustCompile(`^[a-z0-9-]+$`)

// frontmatter returns the top-level scalar keys of a leading ---/--- block.
// ponytail: single-line values only (no YAML folding); add a parser if descriptions need it.
func frontmatter(b []byte) map[string]string {
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	m := map[string]string{}
	if strings.TrimSpace(lines[0]) != "---" {
		return m
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		if k, v, ok := strings.Cut(l, ":"); ok && !strings.HasPrefix(l, " ") {
			v = strings.TrimSpace(v)
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
				v = v[1 : len(v)-1]
			}
			m[strings.TrimSpace(k)] = v
		}
	}
	return m
}

func scanAgents(dir, scope string) []Agent {
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	var out []Agent
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fm := frontmatter(b)
		if fm["name"] == "" {
			continue
		}
		out = append(out, Agent{Name: fm["name"], Description: fm["description"], Scope: scope})
	}
	return out
}

func userAgentsDir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude", "agents")
}

// ListAgents returns the subagents of the named repos' main checkouts, then the user's.
func (s *Service) ListAgents(repos []string) ([]Agent, error) {
	out := []Agent{}
	seen := map[string]bool{}
	for _, n := range repos {
		if seen[n] {
			continue
		}
		seen[n] = true
		r, err := store.GetRepoByName(s.DB, n)
		if err != nil {
			return nil, fmt.Errorf("repo %q is not registered: %w", n, err)
		}
		out = append(out, scanAgents(filepath.Join(r.Path, ".claude", "agents"), n)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return append(out, scanAgents(userAgentsDir(), "user")...), nil
}

// AddAgent writes <repo>/.claude/agents/<name>.md (scope = repo name) or ~/.claude/agents (scope "user").
// It never overwrites an existing file.
func (s *Service) AddAgent(name, description, prompt, scope string) error {
	if !agentName.MatchString(name) {
		return fmt.Errorf("agent name %q must match [a-z0-9-]+", name)
	}
	if strings.TrimSpace(description) == "" || strings.ContainsAny(description, "\r\n") {
		return fmt.Errorf("description is required and must be one line")
	}
	dir := userAgentsDir()
	if scope != "user" {
		r, err := store.GetRepoByName(s.DB, scope)
		if err != nil {
			return fmt.Errorf("repo %q is not registered: %w", scope, err)
		}
		dir = filepath.Join(r.Path, ".claude", "agents")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, name+".md"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("agent %q already exists in %s", name, scope)
		}
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "---\nname: %s\ndescription: %q\n---\n\n%s\n", name, strings.TrimSpace(description), strings.TrimSpace(prompt))
	return err
}

// SetTaskAgent sets (or with "" clears) the task's agent.
func (s *Service) SetTaskAgent(key, agent string) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	t.Agent = optional(agent)
	return store.UpdateTask(s.DB, t)
}
