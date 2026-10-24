package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

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

// SeedMock fills an empty install with fake repos (empty dirs under home/mock-repos)
// and one task per scenario in mockTasks. It refuses a database that has repos or
// tasks, so real data is never mixed with mocks.
func (s *Service) SeedMock(home string) error {
	var n int
	if err := s.DB.QueryRow(`SELECT (SELECT count(*) FROM repos) + (SELECT count(*) FROM tasks)`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("refusing to seed: %s already has repos or tasks (use an empty --home or CONVEYOR_HOME)", DBPath(home))
	}
	for _, r := range mockRepos {
		p := filepath.Join(home, "mock-repos", r)
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
		if _, err := store.AddRepo(s.DB, store.Repo{Name: r, Path: p, DefaultBranch: "main"}); err != nil {
			return err
		}
	}
	repoMap := map[string]string{}
	for _, r := range mockRepos {
		repoMap[r] = r // the state's placeholders are the repo names
	}
	_, err := s.ImportState(mockState(), repoMap)
	return err
}

// mockState turns mockTasks into the state a real export would hold, so seeding
// goes through the one import path.
func mockState() State {
	st := State{Version: stateVersion}
	ts := time.Now().UTC().Format(time.RFC3339)
	key := func(i int) string { return strconv.Itoa(i) } // mock indexes are 1-based
	for i, m := range mockTasks {
		t := StateTask{Key: key(i + 1), Title: m.Title, Description: "Mock scenario: " + m.Title, Status: m.Status, Priority: m.Priority, Failures: m.Failures}
		if m.Parent > 0 {
			t.Parent = key(m.Parent)
		}
		if m.Blocks > 0 {
			t.Blocks = []string{key(m.Blocks)}
		}
		for j, r := range m.Repos {
			t.Repos = append(t.Repos, StateTaskRepo{Repo: r, Primary: j == 0, Branch: "mock", PRURL: m.PRURL, PRState: m.PRState})
		}
		if r := m.Run; r != nil {
			run := StateRun{Kind: "task", Mode: "new", Status: r.Status, Prompt: "mock prompt", Summary: r.Summary, Next: r.Next, Error: r.Error,
				TokensIn: 1200, TokensOut: 800, CtxPeak: 30000, APICalls: 12, StartedAt: ts}
			if r.Status != "running" {
				run.EndedAt = ts
			}
			t.Runs = []StateRun{run}
		}
		for _, c := range m.Comments {
			t.Comments = append(t.Comments, StateComment{Author: c[0], Body: c[1]})
		}
		st.Tasks = append(st.Tasks, t)
	}
	return st
}

// sp returns &v, or nil for "".
func sp(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
