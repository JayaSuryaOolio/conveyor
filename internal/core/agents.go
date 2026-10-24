package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/claude"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Agent is a Claude Code subagent file. Scope is the repo name or "user".
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	Model       string `json:"model"`
}

var agentName = regexp.MustCompile(`^[a-z0-9-]+$`)

func scanAgents(dir, scope string) []Agent {
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	var out []Agent
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fm, _ := claude.Frontmatter(b)
		if fm["name"] == "" {
			continue
		}
		out = append(out, Agent{Name: fm["name"], Description: fm["description"], Scope: scope, Model: fm["model"]})
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
func (s *Service) AddAgent(name, description, prompt, scope, model string) error {
	if !agentName.MatchString(name) {
		return fmt.Errorf("agent name %q must match [a-z0-9-]+", name)
	}
	if strings.TrimSpace(description) == "" || strings.ContainsAny(description, "\r\n") {
		return fmt.Errorf("description is required and must be one line")
	}
	if model = strings.TrimSpace(model); strings.ContainsAny(model, "\r\n") {
		return fmt.Errorf("model must be one line")
	}
	dir, err := s.agentsDir(scope)
	if err != nil {
		return err
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
	if model != "" {
		model = "model: " + model + "\n"
	}
	_, err = fmt.Fprintf(f, "---\nname: %s\ndescription: %q\n%s---\n\n%s\n", name, strings.TrimSpace(description), model, strings.TrimSpace(prompt))
	return err
}

func (s *Service) agentsDir(scope string) (string, error) {
	if scope == "user" {
		return userAgentsDir(), nil
	}
	r, err := store.GetRepoByName(s.DB, scope)
	if err != nil {
		return "", fmt.Errorf("repo %q is not registered: %w", scope, err)
	}
	return filepath.Join(r.Path, ".claude", "agents"), nil
}

// setFrontmatterModel rewrites, inserts, or (model "") removes the top-level model: line, leaving the rest as is.
func setFrontmatterModel(b []byte, model string) ([]byte, error) {
	lines := strings.Split(string(b), "\n")
	if strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("no frontmatter")
	}
	end, at := -1, -1
	for i := 1; i < len(lines) && end < 0; i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
		} else if strings.HasPrefix(lines[i], "model:") {
			at = i
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	line := "model: " + model
	if strings.HasSuffix(lines[0], "\r") {
		line += "\r"
	}
	switch {
	case at >= 0 && model == "":
		lines = append(lines[:at], lines[at+1:]...)
	case at >= 0:
		lines[at] = line
	case model != "":
		lines = append(lines[:end], append([]string{line}, lines[end:]...)...)
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// SetAgentModel sets (or with "" removes) the model: line of the named agent's file in scope.
func (s *Service) SetAgentModel(scope, name, model string) error {
	if model = strings.TrimSpace(model); strings.ContainsAny(model, "\r\n") {
		return fmt.Errorf("model must be one line")
	}
	dir, err := s.agentsDir(scope)
	if err != nil {
		return err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if fm, _ := claude.Frontmatter(b); fm["name"] != name {
			continue
		}
		if b, err = setFrontmatterModel(b, model); err != nil {
			return err
		}
		return os.WriteFile(f, b, 0o644)
	}
	return fmt.Errorf("agent %q not found in %s", name, scope)
}

// AgentModel returns the model set by the named agent's file, looking in each dir's .claude/agents, then the user's. "" if none.
func AgentModel(name string, dirs ...string) string {
	for _, d := range dirs {
		for _, a := range scanAgents(filepath.Join(d, ".claude", "agents"), "") {
			if a.Name == name {
				return a.Model
			}
		}
	}
	for _, a := range scanAgents(userAgentsDir(), "") {
		if a.Name == name {
			return a.Model
		}
	}
	return ""
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

// SetTaskModel sets (or with "" clears) the task's model override.
func (s *Service) SetTaskModel(key, model string) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	t.Model = optional(model)
	return store.UpdateTask(s.DB, t)
}
