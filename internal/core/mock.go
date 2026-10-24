package core

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// mockTask is one board scenario. Status is forced directly (the transition table
// is for the live system); Run, when set, becomes the task's only run.
type mockTask struct {
	Title, Desc, Status string
	Repos               []string // first is primary
	Priority, Failures  int
	PRState, PRURL      string
	Run                 *mockRun
	Comments            [][2]string // author, body
	Parent              int         // 1-based index into mockTasks, 0 = none
	Blocks              int         // 1-based index of a task this one blocks
}

type mockRun struct{ Status, Summary, Next, Error string }

var mockRepos = []string{"mock-api", "mock-web", "mock-infra"}

// mockTasks cover every column of the board and the states that need special UI:
// open/merged PRs, ask comments, failures, links, multi-repo. Add a scenario here
// whenever a bug needs a particular state to reproduce.
var mockTasks = []mockTask{
	{Title: "Backlog idea: rate limit login", Status: "backlog", Repos: []string{"mock-api"}},
	{Title: "Queued: upgrade React", Status: "todo", Repos: []string{"mock-web"}, Priority: 1, Blocks: 3},
	{Title: "Running: add pagination", Status: "running", Repos: []string{"mock-api", "mock-web"}, Run: &mockRun{Status: "running"}},
	{Title: "Needs you: which DB index?", Status: "needs_you", Repos: []string{"mock-api"},
		Run:      &mockRun{Status: "ended", Summary: "Two indexes fit", Next: "Pick one"},
		Comments: [][2]string{{"agent", "Composite or partial index? Need your call."}}},
	{Title: "Review: open PR", Status: "review", Repos: []string{"mock-web"}, PRState: "open", PRURL: "https://example.com/mock-web/pull/1",
		Run: &mockRun{Status: "ended", Summary: "Added dark mode toggle", Next: "Review the PR"}},
	{Title: "Review: merged PR with follow-up comment", Status: "review", Repos: []string{"mock-web"}, PRState: "merged", PRURL: "https://example.com/mock-web/pull/2",
		Run:      &mockRun{Status: "ended", Summary: "Fixed flaky test"},
		Comments: [][2]string{{"human", "Also bump the timeout"}}},
	{Title: "Done: terraform module", Status: "done", Repos: []string{"mock-infra"}, PRState: "merged", PRURL: "https://example.com/mock-infra/pull/3",
		Run: &mockRun{Status: "ended", Summary: "Module extracted"}},
	{Title: "Failed: three launch failures", Status: "failed", Repos: []string{"mock-infra"}, Failures: 3,
		Run: &mockRun{Status: "failed", Error: "claude exited 1: repo not trusted"}},
	{Title: "Cancelled: superseded", Status: "cancelled", Repos: []string{"mock-api"}},
	{Title: "Parent epic", Status: "backlog", Repos: []string{"mock-api"}},
	{Title: "Child of epic", Status: "todo", Repos: []string{"mock-api"}, Parent: 10},
}

// mockState is the non-task fixtures, loaded through ImportState: settings, apps
// (global/per-repo, with/without output) and automations (enabled/disabled, every
// action kind). Repos are the mockRepos names. Agents are left out on purpose:
// import writes them to the real ~/.claude/agents. Add a fixture here whenever a
// bug needs a particular settings/apps/automations state to reproduce.
var mockState = State{
	Version: 1,
	Settings: map[string]string{
		"user_name":         "Mock User",
		"max_parallel":      "2",
		"brief_extra_rules": "Mock rule: keep changes small.",
	},
	Apps: []StateApp{
		{Name: "Mock Editor", Command: "mock-editor {cwd}", Global: true},
		{Name: "Mock Tests", Command: "mock-test --watch", Output: true},
		{Name: "Mock Global Logs", Command: "mock-logs -f", Global: true, Output: true},
		{Name: "Mock Repo Shell", Command: "mock-shell {cwd}"},
	},
	Automations: []StateAutomation{
		{Name: "mock-weekly-task", Schedule: "0 9 * * 1", ActionKind: "create_task", Enabled: true,
			ActionArg: `{"title":"Mock weekly review","description":"Fake recurring task","repos":["mock-api","mock-web"],"start":false}`},
		{Name: "mock-nudge", Schedule: "*/30 * * * *", ActionKind: "comment", Enabled: true, Precheck: "true",
			ActionArg: `{"body":"Mock nudge: status?","task_status":"needs_you"}`},
		{Name: "mock-cli-disabled", Schedule: "0 0 * * *", ActionKind: "cli", Enabled: false,
			ActionArg: `{"args":["task","list"]}`},
	},
}

// SeedMock fills an empty install with fake repos (empty dirs under home/mock-repos)
// and one task per scenario in mockTasks, plus the settings, apps and
// automations in mockState. It refuses a database that has repos or
// tasks, so real data is never mixed with mocks.
func (s *Service) SeedMock(home string) error {
	var n int
	if err := s.DB.QueryRow(`SELECT (SELECT count(*) FROM repos) + (SELECT count(*) FROM tasks)`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("refusing to seed: %s already has repos or tasks (use an empty --home or CONVEYOR_HOME)", DBPath(home))
	}
	ids := map[string]int64{}
	for _, r := range mockRepos {
		p := filepath.Join(home, "mock-repos", r)
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
		repo, err := store.AddRepo(s.DB, store.Repo{Name: r, Path: p, DefaultBranch: "main"})
		if err != nil {
			return err
		}
		ids[r] = repo.ID
	}
	created := make([]store.Task, len(mockTasks))
	for i, m := range mockTasks {
		t := store.Task{Title: m.Title, Description: "Mock scenario: " + m.Title, Priority: m.Priority, ConsecutiveFailures: m.Failures}
		if m.Parent > 0 {
			t.ParentID = &created[m.Parent-1].ID
		}
		var repos []store.TaskRepo
		for j, r := range m.Repos {
			repos = append(repos, store.TaskRepo{RepoID: ids[r], IsPrimary: j == 0, Branch: "mock", PRURL: sp(m.PRURL), PRState: sp(m.PRState)})
		}
		c, err := store.CreateTask(s.DB, t, repos)
		if err != nil {
			return err
		}
		created[i] = c
		if _, err := s.DB.Exec(`UPDATE tasks SET status=? WHERE id=?`, m.Status, c.ID); err != nil {
			return err
		}
		if r := m.Run; r != nil {
			run, err := store.CreateRun(s.DB, store.Run{TaskID: c.ID, Kind: "task", Mode: "new", Status: r.Status, Prompt: "mock prompt"})
			if err != nil {
				return err
			}
			run.Summary, run.Next, run.Error = sp(r.Summary), sp(r.Next), sp(r.Error)
			run.TokensIn, run.TokensOut, run.CtxPeak, run.APICalls = 1200, 800, 30000, 12
			if r.Status != "running" {
				run.EndedAt = sp(run.StartedAt)
			}
			if err := store.UpdateRun(s.DB, run); err != nil {
				return err
			}
		}
		for _, cm := range m.Comments {
			if _, err := store.AddComment(s.DB, store.Comment{TaskID: c.ID, Author: cm[0], Body: cm[1]}); err != nil {
				return err
			}
		}
	}
	for i, m := range mockTasks {
		if m.Blocks > 0 {
			if err := store.AddLink(s.DB, created[i].ID, created[m.Blocks-1].ID, "blocks"); err != nil {
				return err
			}
		}
	}
	// identity map: the fixtures already name the mock repos
	repoMap := map[string]string{}
	for _, r := range mockRepos {
		repoMap[r] = r
	}
	_, err := s.ImportState(mockState, repoMap)
	return err
}

// sp returns &v, or nil for "".
func sp(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
