package core

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// SetLabel sets key's label in label's prefix ("epic_stage:build"), replacing only that prefix.
func (s *Service) SetLabel(key, label string) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	return store.SetPrefixedLabel(s.DB, t.ID, label)
}

func (s *Service) epicRoot(key string) (store.Task, error) {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return t, err
	}
	if !store.IsEpic(t) {
		return t, fmt.Errorf("%s is not an epic (no epic label)", key)
	}
	return t, nil
}

// EpicApprove passes the plan gate: starts flow 1, sets stage decide and wakes the epic.
func (s *Service) EpicApprove(key string) error {
	t, err := s.epicRoot(key)
	if err != nil {
		return err
	}
	if gate, err := store.EpicAtGate(s.DB, t); err != nil {
		return err
	} else if !gate {
		return fmt.Errorf("%s is not waiting at the plan gate (stage %s)", key, store.EpicStage(t))
	}
	if _, err := store.ApproveEpic(s.DB, t.ID); err != nil {
		return err
	}
	_, err = s.AddComment(key, store.AuthorSystem, "plan approved: flow 1 started, stage decide", FollowUp)
	return err
}

// EpicRestart abandons the active flow, cancels every open sub-task and starts the next flow at spec.
// Only a human starts it: no code path calls this on its own.
func (s *Service) EpicRestart(key, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("a reason is required")
	}
	t, err := s.epicRoot(key)
	if err != nil {
		return err
	}
	if f, err := store.ActiveFlow(s.DB, t.ID); err != nil {
		return err
	} else if f == nil {
		return fmt.Errorf("%s has no active flow to restart", key)
	}
	kids, err := store.Descendants(s.DB, t.ID)
	if err != nil {
		return err
	}
	for _, k := range kids {
		if k.Status == store.StatusDone || k.Status == store.StatusCancelled {
			continue
		}
		if err := s.Cancel(k.Key); err != nil {
			return fmt.Errorf("cancel %s: %w", k.Key, err)
		}
	}
	if t.Status == store.StatusRunning {
		if err := s.Stop(key); err != nil {
			return err
		}
	}
	f, err := store.RestartFlow(s.DB, t.ID, reason)
	if err != nil {
		return err
	}
	_, err = s.AddComment(key, store.AuthorSystem, fmt.Sprintf("restarted (flow %d): %s", f.Attempt, reason), FollowUp)
	return err
}

// ACItem is one acceptance criterion of the epic root and the build tasks citing it.
type ACItem struct {
	ID      string   `json:"id"`
	Done    bool     `json:"done"`
	Covered []string `json:"covered_by"`
}

// EpicTask is one task of the epic as the skill sees it.
type EpicTask struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Kind     string   `json:"kind"`
	Agent    string   `json:"agent"`
	Repos    []string `json:"repos"`
	Blockers []string `json:"blocked_by"`
	PRs      []string `json:"prs"`
	Labels   []string `json:"labels"`
}

// EpicStatusView is `epic status --json`: the state every epic run starts from.
type EpicStatusView struct {
	Key    string          `json:"key"`
	Name   string          `json:"name"`
	Stage  string          `json:"stage"`
	Gate   bool            `json:"gate"`
	Status string          `json:"status"`
	Flow   *store.EpicFlow `json:"flow"`
	Tasks  []EpicTask      `json:"tasks"`
	PRs    []store.FlowPR  `json:"prs"`
	AC     []ACItem        `json:"ac"`
}

var (
	acID      = regexp.MustCompile(`\bAC\d+\b`)
	acLine    = regexp.MustCompile(`^\s*(?:[-*]|\d+[.)])?\s*(?:\[([ xX])\])?\s*\*{0,2}(AC\d+)\b`)
	acHeading = regexp.MustCompile(`(?im)^#{1,6}\s*acceptance criteria\s*$`)
)

// parseAC returns the criteria listed under the description's "Acceptance criteria" heading.
func parseAC(desc string) []ACItem {
	loc := acHeading.FindStringIndex(desc)
	if loc == nil {
		return nil
	}
	var out []ACItem
	for _, line := range strings.Split(desc[loc[1]:], "\n") {
		if strings.HasPrefix(line, "#") {
			break
		}
		if m := acLine.FindStringSubmatch(line); m != nil {
			out = append(out, ACItem{ID: m[2], Done: strings.EqualFold(m[1], "x")})
		}
	}
	return out
}

func repoLabels(labels []string) []string {
	var out []string
	for _, l := range labels {
		if len(l) > 5 && strings.EqualFold(l[:5], "repo:") {
			out = append(out, l[5:])
		}
	}
	slices.Sort(out)
	return out
}

func repoNames(t store.Task) []string {
	out := []string{}
	for _, r := range t.Repos {
		out = append(out, r.RepoName)
	}
	slices.Sort(out)
	return out
}

func (s *Service) epicTask(t store.Task) (EpicTask, error) {
	et := EpicTask{Key: t.Key, Title: t.Title, Status: string(t.Status), Kind: store.LabelValue(t, store.PrefixKind),
		Repos: repoNames(t), Labels: t.Labels, PRs: []string{}}
	if t.Agent != nil {
		et.Agent = *t.Agent
	}
	b, err := store.OpenBlockers(s.DB, t.ID)
	et.Blockers = b
	if et.Blockers == nil {
		et.Blockers = []string{}
	}
	for _, r := range t.Repos {
		if r.PRURL != nil && *r.PRURL != "" {
			et.PRs = append(et.PRs, *r.PRURL)
		}
	}
	return et, err
}

// flowFor returns the active flow, or attempt n when n > 0, or nil when the epic has none.
func (s *Service) flowFor(root store.Task, n int) (*store.EpicFlow, error) {
	if n <= 0 {
		return store.ActiveFlow(s.DB, root.ID)
	}
	flows, err := store.ListFlows(s.DB, root.ID)
	for i := range flows {
		if flows[i].Attempt == n {
			return &flows[i], err
		}
	}
	return nil, fmt.Errorf("%s has no flow %d", root.Key, n)
}

// EpicStatus assembles flow, tasks, PRs and AC coverage. flow 0 means the active flow.
func (s *Service) EpicStatus(key string, flow int) (EpicStatusView, error) {
	root, err := s.epicRoot(key)
	if err != nil {
		return EpicStatusView{}, err
	}
	v := EpicStatusView{Key: root.Key, Name: store.EpicName(root), Stage: store.EpicStage(root),
		Gate: store.EpicGateOn(root), Status: string(root.Status), Tasks: []EpicTask{}, PRs: []store.FlowPR{}}
	if v.Flow, err = s.flowFor(root, flow); err != nil {
		return v, err
	}
	kids, err := store.Descendants(s.DB, root.ID)
	if err != nil {
		return v, err
	}
	if v.Flow != nil {
		if v.Flow.Status == store.FlowActive { // refresh the PR rows from the live tasks
			if err := store.SyncFlowTasks(s.DB, v.Flow.ID, kids); err != nil {
				return v, err
			}
		}
		if v.PRs, err = store.ListFlowPRs(s.DB, v.Flow.ID); err != nil {
			return v, err
		}
	}
	if flow > 0 { // a past flow shows only the tasks it had
		in := map[string]bool{}
		for _, p := range v.PRs {
			in[p.TaskKey] = true
		}
		kids = slices.DeleteFunc(kids, func(t store.Task) bool { return !in[t.Key] })
	}
	v.AC = parseAC(root.Description)
	for _, k := range kids {
		et, err := s.epicTask(k)
		if err != nil {
			return v, err
		}
		v.Tasks = append(v.Tasks, et)
		if et.Kind == "build" && k.Status != store.StatusCancelled {
			cited := acID.FindAllString(k.Title+"\n"+k.Description, -1)
			for i := range v.AC {
				if slices.Contains(cited, v.AC[i].ID) {
					v.AC[i].Covered = append(v.AC[i].Covered, k.Key)
				}
			}
		}
	}
	return v, nil
}

// EpicCheck runs the mechanical checks of docs/epic-builder.md §2.4 and returns what fails.
// With fix it repairs labels only (membership and repo labels) and reports what is left.
func (s *Service) EpicCheck(key string, fix bool) ([]string, error) {
	root, err := s.epicRoot(key)
	if err != nil {
		return nil, err
	}
	name := store.EpicName(root)
	kids, err := store.Descendants(s.DB, root.ID)
	if err != nil {
		return nil, err
	}
	all, err := store.ListTasks(s.DB)
	if err != nil {
		return nil, err
	}
	inTree := map[int64]bool{root.ID: true}
	for _, k := range kids {
		inTree[k.ID] = true
	}
	var out []string
	label := PrefixLabel(name)
	for _, t := range append([]store.Task{root}, kids...) {
		if !slices.ContainsFunc(t.Labels, func(l string) bool { return strings.EqualFold(l, label) }) {
			if fix && store.AddLabel(s.DB, t.ID, label) == nil {
				continue
			}
			out = append(out, fmt.Sprintf("%s: missing label %s", t.Key, label))
		}
		if got, want := repoLabels(t.Labels), repoNames(t); !slices.Equal(got, want) {
			if fix && s.fixRepoLabels(t) == nil {
				continue
			}
			out = append(out, fmt.Sprintf("%s: repo labels %v != repos %v", t.Key, got, want))
		}
	}
	for _, t := range all {
		if !inTree[t.ID] && slices.ContainsFunc(t.Labels, func(l string) bool { return strings.EqualFold(l, label) }) {
			if fix && store.RemoveLabel(s.DB, t.ID, label) == nil {
				continue
			}
			out = append(out, fmt.Sprintf("%s: has %s but is outside the epic tree", t.Key, label))
		}
	}
	v, err := s.EpicStatus(key, 0)
	if err != nil {
		return out, err
	}
	if len(v.AC) == 0 {
		out = append(out, root.Key+": no AC list under '## Acceptance criteria'")
	}
	for _, a := range v.AC {
		if len(a.Covered) == 0 {
			out = append(out, fmt.Sprintf("%s: %s is covered by no build task", root.Key, a.ID))
		}
	}
	onFlow := map[string]bool{}
	for _, p := range v.PRs {
		if p.PRURL != nil && *p.PRURL != "" {
			onFlow[p.TaskKey] = true
		}
	}
	for _, k := range kids {
		if store.LabelValue(k, store.PrefixKind) != "build" || k.Status == store.StatusCancelled {
			continue
		}
		if len(repoNames(k)) != 1 {
			out = append(out, fmt.Sprintf("%s: a build task needs exactly one repo, has %d", k.Key, len(k.Repos)))
		}
		if k.Status == store.StatusTodo {
			if k.Agent == nil || *k.Agent == "" {
				out = append(out, k.Key+": build task in todo has no agent")
			}
			if len(acID.FindAllString(k.Title+"\n"+k.Description, -1)) == 0 {
				out = append(out, k.Key+": build task in todo cites no AC id")
			}
		}
		if (k.Status == store.StatusReview || k.Status == store.StatusDone) && !onFlow[k.Key] {
			out = append(out, k.Key+": build task in "+string(k.Status)+" has no PR on the flow")
		}
	}
	return out, nil
}

// PrefixLabel is the membership label of the epic named name.
func PrefixLabel(name string) string { return store.PrefixEpic + name }

func (s *Service) fixRepoLabels(t store.Task) error {
	return store.SetPrefixedLabels(s.DB, t.ID, "repo:", repoNames(t))
}

// GraphNode is a task in an epic or task graph.
type GraphNode struct {
	EpicTask
	ParentKey string `json:"parent"`
}

// Graph is the parent tree around a task with its links and, for an epic, the flow's PR rows.
type Graph struct {
	Root  string         `json:"root"`
	Nodes []GraphNode    `json:"nodes"`
	Links []store.Link   `json:"links"`
	PRs   []store.FlowPR `json:"prs"`
}

// TaskGraph returns the tree under the topmost ancestor of key, the links among those tasks, and links to
// outside tasks when the tree is just the one task.
func (s *Service) TaskGraph(key string) (Graph, error) {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return Graph{}, err
	}
	root, err := store.EpicRoot(s.DB, t)
	if err != nil {
		return Graph{}, err
	}
	kids, err := store.Descendants(s.DB, root.ID)
	if err != nil {
		return Graph{}, err
	}
	g := Graph{Root: root.Key, Nodes: []GraphNode{}, PRs: []store.FlowPR{}}
	keys := map[string]store.Task{}
	for _, n := range append([]store.Task{root}, kids...) {
		keys[n.Key] = n
	}
	links, err := store.ListLinks(s.DB)
	if err != nil {
		return g, err
	}
	g.Links = []store.Link{}
	for _, l := range links {
		_, a := keys[l.From]
		_, b := keys[l.To]
		if (a && b) || (len(kids) == 0 && (a || b)) { // a lone task also shows its neighbours
			g.Links = append(g.Links, l)
		}
	}
	for _, l := range g.Links {
		for _, k := range []string{l.From, l.To} {
			if _, ok := keys[k]; !ok {
				n, err := store.GetTaskByKey(s.DB, k)
				if err != nil {
					return g, err
				}
				keys[k] = n
			}
		}
	}
	ids := map[int64]string{}
	for k, n := range keys {
		ids[n.ID] = k
	}
	for _, n := range keys {
		et, err := s.epicTask(n)
		if err != nil {
			return g, err
		}
		gn := GraphNode{EpicTask: et}
		if n.ParentID != nil {
			gn.ParentKey = ids[*n.ParentID]
		}
		g.Nodes = append(g.Nodes, gn)
	}
	slices.SortFunc(g.Nodes, func(a, b GraphNode) int { return strings.Compare(a.Key, b.Key) })
	if store.IsEpic(root) {
		if f, err := store.ActiveFlow(s.DB, root.ID); err != nil {
			return g, err
		} else if f != nil {
			g.PRs, err = store.ListFlowPRs(s.DB, f.ID)
			return g, err
		}
	}
	return g, nil
}

// EpicBranch is one PR branch of the epic's active flow.
type EpicBranch struct {
	Task     string `json:"task"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	PRURL    string `json:"pr_url"`
	repoPath string
	base     string
}

// Log runs `git log` on the branch's range against the default branch with extra args.
func (b EpicBranch) Log(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", b.repoPath, "log", "--format=%h %s"}, append(args, b.base+".."+b.Branch)...)...)
	o, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git log %s/%s: %v: %s", b.Repo, b.Branch, err, strings.TrimSpace(string(o)))
	}
	return string(o), nil
}

// EpicBranches lists the PR branches of the epic's tasks that have a PR recorded.
func (s *Service) EpicBranches(key string) ([]EpicBranch, error) {
	root, err := s.epicRoot(key)
	if err != nil {
		return nil, err
	}
	kids, err := store.Descendants(s.DB, root.ID)
	if err != nil {
		return nil, err
	}
	out := []EpicBranch{}
	for _, k := range kids {
		if k.Status == store.StatusCancelled {
			continue
		}
		for _, r := range k.Repos {
			if r.PRURL == nil || *r.PRURL == "" {
				continue
			}
			repo, err := store.GetRepoByName(s.DB, r.RepoName)
			if err != nil {
				return nil, err
			}
			out = append(out, EpicBranch{Task: k.Key, Repo: r.RepoName, Branch: r.Branch, PRURL: *r.PRURL, repoPath: repo.Path, base: repo.DefaultBranch})
		}
	}
	return out, nil
}

// EpicSession is one Claude session of a task in the epic.
type EpicSession struct {
	Task       string   `json:"task"`
	Run        int64    `json:"run"`
	Session    string   `json:"session"`
	Transcript string   `json:"transcript"`
	Matches    []string `json:"matches,omitempty"`
}

// EpicSessions lists every run's session of the epic's tasks with its transcript path. With grep it keeps
// only sessions whose transcript contains the text (case-insensitive) and shows up to three matching lines.
func (s *Service) EpicSessions(key, grep string) ([]EpicSession, error) {
	root, err := s.epicRoot(key)
	if err != nil {
		return nil, err
	}
	kids, err := store.Descendants(s.DB, root.ID)
	if err != nil {
		return nil, err
	}
	out := []EpicSession{}
	for _, t := range append([]store.Task{root}, kids...) {
		runs, err := store.ListRuns(s.DB, t.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range runs {
			if r.SessionID == nil || *r.SessionID == "" {
				continue
			}
			es := EpicSession{Task: t.Key, Run: r.ID, Session: *r.SessionID}
			for _, tr := range t.Repos {
				if tr.WorktreePath == nil {
					continue
				}
				p := filepath.Join(s.projectsDir(), nonAlnumPath.ReplaceAllString(*tr.WorktreePath, "-"), es.Session+".jsonl")
				if _, err := os.Stat(p); err == nil {
					es.Transcript = p
					break
				}
			}
			if grep != "" {
				if es.Transcript == "" {
					continue
				}
				if es.Matches = grepFile(es.Transcript, grep); len(es.Matches) == 0 {
					continue
				}
			}
			out = append(out, es)
		}
	}
	return out, nil
}

// grepFile returns up to three lines of the file containing text, case-insensitively, cut to 160 runes.
func grepFile(path, text string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	text = strings.ToLower(text)
	for sc.Scan() && len(out) < 3 {
		if line := sc.Text(); strings.Contains(strings.ToLower(line), text) {
			r := []rune(line)
			out = append(out, string(r[:min(len(r), 160)]))
		}
	}
	return out
}

// Flows lists the epic's flows, oldest first.
func (s *Service) Flows(key string) ([]store.EpicFlow, error) {
	root, err := s.epicRoot(key)
	if err != nil {
		return nil, err
	}
	return store.ListFlows(s.DB, root.ID)
}

// SetTaskSkills replaces the task's skill:* labels with skills.
func (s *Service) SetTaskSkills(key string, skills []string) error {
	t, err := store.GetTaskByKey(s.DB, key)
	if err != nil {
		return err
	}
	return store.SetPrefixedLabels(s.DB, t.ID, "skill:", skills)
}
