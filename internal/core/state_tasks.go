package core

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// StateTask is a task with its repo links, runs and comments. Key is only the name
// other tasks use in Parent/Blocks/Relates inside the file: import assigns new keys.
// Blocks and Relates are the outgoing links ("this blocks X").
type StateTask struct {
	Key         string          `json:"key"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Priority    int             `json:"priority"`
	Model       *string         `json:"model,omitempty"`
	Agent       *string         `json:"agent,omitempty"`
	Source      string          `json:"source,omitempty"`
	Failures    int             `json:"consecutive_failures"`
	CreatedAt   string          `json:"created_at,omitempty"`
	UpdatedAt   string          `json:"updated_at,omitempty"`
	Parent      string          `json:"parent,omitempty"`
	Blocks      []string        `json:"blocks,omitempty"`
	Relates     []string        `json:"relates,omitempty"`
	Repos       []StateTaskRepo `json:"repos"`
	Runs        []StateRun      `json:"runs,omitempty"`
	Comments    []StateComment  `json:"comments,omitempty"`
}

// StateTaskRepo's Repo is a placeholder; Branch is the slug after cv/<KEY>-.
type StateTaskRepo struct {
	Repo    string `json:"repo"`
	Primary bool   `json:"primary"`
	Branch  string `json:"branch"`
	PRURL   string `json:"pr_url,omitempty"`
	PRState string `json:"pr_state,omitempty"`
}

// StateRun leaves out the machine-local bg_id and session_id.
type StateRun struct {
	Kind         string `json:"kind"`
	Mode         string `json:"mode"`
	Status       string `json:"status"`
	Prompt       string `json:"prompt"`
	BlockStatus  string `json:"block_status,omitempty"`
	Summary      string `json:"summary,omitempty"`
	State        string `json:"state,omitempty"`
	Next         string `json:"next,omitempty"`
	Criteria     string `json:"criteria,omitempty"`
	FinalMessage string `json:"final_message,omitempty"`
	TokensIn     int64  `json:"tokens_in"`
	TokensOut    int64  `json:"tokens_out"`
	CacheRead    int64  `json:"cache_read"`
	CacheWrite   int64  `json:"cache_write"`
	CtxPeak      int64  `json:"ctx_peak"`
	APICalls     int64  `json:"api_calls"`
	Error        string `json:"error,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	EndedAt      string `json:"ended_at,omitempty"`
}

// StateComment's Run and ConsumedBy are 1-based positions in the task's Runs, 0 for none.
type StateComment struct {
	Author     string `json:"author"`
	Body       string `json:"body"`
	Run        int    `json:"run,omitempty"`
	ConsumedBy int    `json:"consumed_by,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// exportTasks lists every task in id order. repo turns a repo name into its placeholder.
func (s *Service) exportTasks(repo func(string) string) ([]StateTask, error) {
	tasks, err := store.ListTasks(s.DB)
	if err != nil {
		return nil, err
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	keys := map[int64]string{}
	for _, t := range tasks {
		keys[t.ID] = t.Key
	}
	out := make([]StateTask, 0, len(tasks))
	idx := map[int64]int{}
	for _, t := range tasks {
		st := StateTask{Key: t.Key, Title: t.Title, Description: t.Description, Status: string(t.Status), Priority: t.Priority,
			Model: t.Model, Agent: t.Agent, Source: t.Source, Failures: t.ConsecutiveFailures, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Repos: []StateTaskRepo{}}
		if t.ParentID != nil {
			st.Parent = keys[*t.ParentID]
		}
		for _, r := range t.Repos {
			st.Repos = append(st.Repos, StateTaskRepo{repo(r.RepoName), r.IsPrimary, strings.TrimPrefix(r.Branch, "cv/"+t.Key+"-"), deref(r.PRURL), deref(r.PRState)})
		}
		runs, err := store.ListRuns(s.DB, t.ID)
		if err != nil {
			return nil, err
		}
		runPos := map[int64]int{}
		for i, r := range runs {
			runPos[r.ID] = i + 1
			st.Runs = append(st.Runs, StateRun{r.Kind, r.Mode, r.Status, r.Prompt, deref(r.BlockStatus), deref(r.Summary), deref(r.State), deref(r.Next),
				deref(r.Criteria), deref(r.FinalMessage), r.TokensIn, r.TokensOut, r.CacheRead, r.CacheWrite, r.CtxPeak, r.APICalls, deref(r.Error), r.StartedAt, deref(r.EndedAt)})
		}
		comments, err := store.ListComments(s.DB, t.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range comments {
			sc := StateComment{Author: c.Author, Body: c.Body, CreatedAt: c.CreatedAt}
			if c.RunID != nil {
				sc.Run = runPos[*c.RunID]
			}
			if c.ConsumedByRunID != nil {
				sc.ConsumedBy = runPos[*c.ConsumedByRunID]
			}
			st.Comments = append(st.Comments, sc)
		}
		idx[t.ID] = len(out)
		out = append(out, st)
	}
	rows, err := s.DB.Query(`SELECT from_id, to_id, kind FROM task_links ORDER BY from_id, to_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var from, to int64
		var kind string
		if err := rows.Scan(&from, &to, &kind); err != nil {
			return nil, err
		}
		t := &out[idx[from]]
		if kind == store.LinkBlocks {
			t.Blocks = append(t.Blocks, keys[to])
		} else {
			t.Relates = append(t.Relates, keys[to])
		}
	}
	return out, rows.Err()
}

// importTasks creates the tasks (parents first) under fresh keys and then their links.
// A task whose repo placeholder is unmapped is skipped, as is one already present
// (same title and created_at), and links to skipped tasks are dropped.
func (s *Service) importTasks(tasks []StateTask, repoMap map[string]string) ([]string, error) {
	var out []string
	byKey := map[string]*StateTask{}
	for i := range tasks {
		t := &tasks[i]
		if t.Key == "" || byKey[t.Key] != nil {
			return nil, fmt.Errorf("task %q: key is empty or repeated", t.Key)
		}
		if !slices.Contains(store.AllStatuses, store.Status(t.Status)) {
			return nil, fmt.Errorf("task %s: unknown status %q", t.Key, t.Status)
		}
		byKey[t.Key] = t
	}
	ids := map[string]int64{} // old key -> new id, only for tasks created now
	seen := map[string]bool{}
	var create func(t *StateTask) error
	create = func(t *StateTask) error {
		if seen[t.Key] {
			return nil
		}
		seen[t.Key] = true // set first, so a parent loop ends here
		var parentID *int64
		if p := byKey[t.Parent]; p != nil {
			if err := create(p); err != nil {
				return err
			}
			if id, ok := ids[p.Key]; ok {
				parentID = &id
			}
		}
		if t.CreatedAt != "" {
			var n int
			if err := s.DB.QueryRow(`SELECT count(*) FROM tasks WHERE title=? AND created_at=?`, t.Title, t.CreatedAt).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				out = append(out, "skipped task "+t.Key+": exists")
				return nil
			}
		}
		var repos []store.TaskRepo
		var missing []string
		for _, r := range t.Repos {
			repo, err := store.GetRepoByName(s.DB, repoMap[r.Repo])
			if err != nil {
				missing = append(missing, r.Repo)
				continue
			}
			repos = append(repos, store.TaskRepo{RepoID: repo.ID, IsPrimary: r.Primary, Branch: r.Branch, PRURL: sp(r.PRURL), PRState: sp(r.PRState)})
		}
		if len(missing) > 0 {
			out = append(out, fmt.Sprintf("skipped task %s: map repo %s with --repo %s=<name>", t.Key, strings.Join(missing, ","), missing[0]))
			return nil
		}
		c, err := store.CreateTask(s.DB, store.Task{Title: t.Title, Description: t.Description, Priority: t.Priority, Model: t.Model, Agent: t.Agent,
			Source: t.Source, ParentID: parentID, ConsecutiveFailures: t.Failures}, repos)
		if err != nil {
			return fmt.Errorf("task %s: %w", t.Key, err)
		}
		ids[t.Key] = c.ID
		// Status is set directly (the transition table is for the live system); timestamps are kept when the file has them.
		if _, err := s.DB.Exec(`UPDATE tasks SET status=?, created_at=COALESCE(NULLIF(?,''),created_at), updated_at=COALESCE(NULLIF(?,''),updated_at) WHERE id=?`,
			t.Status, t.CreatedAt, t.UpdatedAt, c.ID); err != nil {
			return err
		}
		runIDs := make([]int64, len(t.Runs)+1) // 1-based
		for i, r := range t.Runs {
			run, err := store.CreateRun(s.DB, store.Run{TaskID: c.ID, Kind: r.Kind, Mode: r.Mode, Status: r.Status, Prompt: r.Prompt, StartedAt: r.StartedAt})
			if err != nil {
				return err
			}
			run.BlockStatus, run.Summary, run.State, run.Next, run.Criteria, run.FinalMessage, run.Error, run.EndedAt =
				sp(r.BlockStatus), sp(r.Summary), sp(r.State), sp(r.Next), sp(r.Criteria), sp(r.FinalMessage), sp(r.Error), sp(r.EndedAt)
			run.TokensIn, run.TokensOut, run.CacheRead, run.CacheWrite, run.CtxPeak, run.APICalls = r.TokensIn, r.TokensOut, r.CacheRead, r.CacheWrite, r.CtxPeak, r.APICalls
			if err := store.UpdateRun(s.DB, run); err != nil {
				return err
			}
			runIDs[i+1] = run.ID
		}
		for _, cm := range t.Comments {
			if cm.Run < 0 || cm.Run >= len(runIDs) || cm.ConsumedBy < 0 || cm.ConsumedBy >= len(runIDs) {
				return fmt.Errorf("task %s: comment refers to a run that is not in the file", t.Key)
			}
			if _, err := s.DB.Exec(`INSERT INTO comments (task_id, author, body, run_id, consumed_by_run_id, created_at)
 VALUES (?,?,?,NULLIF(?,0),NULLIF(?,0),COALESCE(NULLIF(?,''),strftime('%Y-%m-%dT%H:%M:%SZ','now')))`,
				c.ID, cm.Author, cm.Body, runIDs[cm.Run], runIDs[cm.ConsumedBy], cm.CreatedAt); err != nil {
				return err
			}
		}
		out = append(out, fmt.Sprintf("added task %s as %s: %s", t.Key, c.Key, t.Title))
		return nil
	}
	for i := range tasks {
		if err := create(&tasks[i]); err != nil {
			return out, err
		}
	}
	for i := range tasks {
		from, ok := ids[tasks[i].Key]
		if !ok {
			continue
		}
		for kind, to := range map[string][]string{store.LinkBlocks: tasks[i].Blocks, store.LinkRelates: tasks[i].Relates} {
			for _, k := range to {
				if id, ok := ids[k]; ok {
					if err := store.AddLink(s.DB, from, id, kind); err != nil {
						return out, fmt.Errorf("task %s %s %s: %w", tasks[i].Key, kind, k, err)
					}
				}
			}
		}
	}
	return out, nil
}
