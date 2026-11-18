package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// Audit is the call site for the epic audit log; the audit task replaces the no-op.
// Fields are key/value pairs, e.g. Audit("epic.wake", "epic", "os-4", "stage", "build").
var Audit = func(event string, kv ...any) {}

// Epic label vocabulary (docs/epic-builder.md §2.1).
const (
	LabelEpic       = "epic"        // bare: this task is an epic root
	PrefixEpic      = "epic:"       // membership: epic:<name>
	PrefixStage     = "epic_stage:" // the root's stage
	PrefixKind      = "epic_kind:"
	LabelGateOff    = "epic_gate:off"
	StageChart      = "chart"
	StageDecide     = "decide"
	StageSpec       = "spec"
	StageBuild      = "build"
	StageVerify     = "verify"
	FlowActive      = "active"
	FlowAbandoned   = "abandoned"
	FlowDone        = "done"
	prefixEpicStage = PrefixStage
)

func hasLabel(labels []string, name string) bool {
	for _, l := range labels {
		if strings.EqualFold(l, name) {
			return true
		}
	}
	return false
}

// labelValue is the value of the first label starting with prefix, "" if none.
func labelValue(labels []string, prefix string) string {
	for _, l := range labels {
		if len(l) >= len(prefix) && strings.EqualFold(l[:len(prefix)], prefix) {
			return l[len(prefix):]
		}
	}
	return ""
}

// LabelValue is the value of the task's first label with the prefix, "" if none.
func LabelValue(t Task, prefix string) string { return labelValue(t.Labels, prefix) }

// IsEpic reports whether t is an epic root.
func IsEpic(t Task) bool { return hasLabel(t.Labels, LabelEpic) }

// IsEpicMember reports whether t belongs to an epic (the root carries epic:<name> too).
func IsEpicMember(t Task) bool { return labelValue(t.Labels, PrefixEpic) != "" }

// EpicName is the name in the task's epic:<name> label.
func EpicName(t Task) string { return labelValue(t.Labels, PrefixEpic) }

// EpicStage is the root's stage; with no epic_stage label it is chart.
func EpicStage(t Task) string {
	if s := labelValue(t.Labels, PrefixStage); s != "" {
		return s
	}
	return StageChart
}

// EpicGateOn is false when the epic has epic_gate:off.
func EpicGateOn(t Task) bool { return !hasLabel(t.Labels, LabelGateOff) }

// EpicNameTaken returns the key of another open (not done or cancelled) epic named name, or "".
func EpicNameTaken(db dbtx, exceptID int64, name string) (string, error) {
	var key string
	err := db.QueryRow(`SELECT t.key FROM tasks t WHERE t.id<>? AND t.status NOT IN (?,?) AND
 EXISTS (SELECT 1 FROM task_labels tl JOIN labels l ON l.id=tl.label_id WHERE tl.task_id=t.id AND l.name=?) AND
 EXISTS (SELECT 1 FROM task_labels tl JOIN labels l ON l.id=tl.label_id WHERE tl.task_id=t.id AND l.name=?) LIMIT 1`,
		exceptID, StatusDone, StatusCancelled, LabelEpic, PrefixEpic+name).Scan(&key)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return key, err
}

// checkEpicName fails when the task is an epic root whose name another open epic has.
func checkEpicName(db dbtx, taskID int64) error {
	labels, err := TaskLabels(db, taskID)
	if err != nil || !hasLabel(labels, LabelEpic) {
		return err
	}
	name := labelValue(labels, PrefixEpic)
	if name == "" {
		return nil
	}
	if other, err := EpicNameTaken(db, taskID, name); err != nil {
		return fmt.Errorf("check epic name: %w", err)
	} else if other != "" {
		return fmt.Errorf("epic name %q is taken by %s", name, other)
	}
	return nil
}

// MakeEpic labels the task as an epic root named name, with the epic name unique among epics not done.
func MakeEpic(db *sql.DB, taskID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("epic name %q must be one word", name)
	}
	return inTx(db, func(tx *sql.Tx) error {
		if err := addLabel(tx, taskID, LabelEpic); err != nil {
			return err
		}
		return setPrefixed(tx, taskID, PrefixEpic, PrefixEpic+name) // checks name uniqueness
	})
}

// EpicFlow is one attempt at finishing an epic.
type EpicFlow struct {
	ID        int64   `json:"id"`
	EpicID    int64   `json:"epic_id"`
	Attempt   int     `json:"attempt"`
	Status    string  `json:"status"`
	Reason    string  `json:"reason"`
	StartedAt string  `json:"started_at"`
	EndedAt   *string `json:"ended_at"`
}

const flowCols = `id, epic_id, attempt, status, reason, started_at, ended_at`

func scanFlow(r interface{ Scan(...any) error }) (EpicFlow, error) {
	var f EpicFlow
	err := r.Scan(&f.ID, &f.EpicID, &f.Attempt, &f.Status, &f.Reason, &f.StartedAt, &f.EndedAt)
	return f, err
}

func startFlow(tx dbtx, epicID int64) (EpicFlow, error) {
	var attempt int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(attempt),0)+1 FROM epic_flows WHERE epic_id=?`, epicID).Scan(&attempt); err != nil {
		return EpicFlow{}, fmt.Errorf("next flow: %w", err)
	}
	res, err := tx.Exec(`INSERT INTO epic_flows (epic_id, attempt, status, started_at) VALUES (?,?,?,?)`, epicID, attempt, FlowActive, now())
	if err != nil {
		return EpicFlow{}, fmt.Errorf("start flow (is one already active?): %w", err)
	}
	id, _ := res.LastInsertId()
	return scanFlow(tx.QueryRow(`SELECT `+flowCols+` FROM epic_flows WHERE id=?`, id))
}

// StartFlow begins the epic's next attempt. It fails while another flow of the epic is active.
func StartFlow(db *sql.DB, epicID int64) (EpicFlow, error) {
	var f EpicFlow
	err := inTx(db, func(tx *sql.Tx) (err error) { f, err = startFlow(tx, epicID); return })
	if err == nil {
		Audit("epic.flow.start", "task_id", epicID, "flow", f.Attempt)
	}
	return f, err
}

// ActiveFlow returns the epic's active flow, or nil.
func ActiveFlow(db dbtx, epicID int64) (*EpicFlow, error) {
	f, err := scanFlow(db.QueryRow(`SELECT `+flowCols+` FROM epic_flows WHERE epic_id=? AND status=?`, epicID, FlowActive))
	switch err {
	case nil:
		return &f, nil
	case sql.ErrNoRows:
		return nil, nil
	}
	return nil, fmt.Errorf("active flow: %w", err)
}

// ListFlows returns the epic's flows, oldest first.
func ListFlows(db *sql.DB, epicID int64) ([]EpicFlow, error) {
	rows, err := db.Query(`SELECT `+flowCols+` FROM epic_flows WHERE epic_id=? ORDER BY attempt`, epicID)
	if err != nil {
		return nil, fmt.Errorf("list flows: %w", err)
	}
	defer rows.Close()
	out := []EpicFlow{}
	for rows.Next() {
		f, err := scanFlow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan flow: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// EnsureFlow starts flow 1 for an epic past the chart stage that never had a flow (moved there by a label or
// an import instead of `epic approve`). An epic whose flows have all ended is left alone.
func EnsureFlow(db *sql.DB, t Task) error {
	if !IsEpic(t) || EpicStage(t) == StageChart {
		return nil
	}
	var f EpicFlow
	started := false
	err := inTx(db, func(tx *sql.Tx) (err error) {
		var n int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM epic_flows WHERE epic_id=?`, t.ID).Scan(&n); err != nil || n > 0 {
			return err
		}
		f, err = startFlow(tx, t.ID)
		started = err == nil
		return err
	})
	if started {
		Audit("epic.flow.start", "task_id", t.ID, "flow", f.Attempt, "reason", "no flow")
	}
	return err
}

// ApproveEpic starts flow 1 and moves the epic to the decide stage. It fails once a flow exists.
func ApproveEpic(db *sql.DB, epicID int64) (EpicFlow, error) {
	var f EpicFlow
	err := inTx(db, func(tx *sql.Tx) (err error) {
		if a, err := ActiveFlow(tx, epicID); err != nil || a != nil {
			if err == nil {
				err = fmt.Errorf("epic already has an active flow (attempt %d)", a.Attempt)
			}
			return err
		}
		if f, err = startFlow(tx, epicID); err != nil {
			return err
		}
		return setPrefixed(tx, epicID, PrefixStage, PrefixStage+StageDecide)
	})
	if err == nil {
		Audit("epic.flow.start", "task_id", epicID, "flow", f.Attempt)
		Audit("epic.gate", "task_id", epicID, "result", "approved", "actor", "human")
	}
	return f, err
}

// RestartFlow ends the active flow, if any, as abandoned with reason and starts the next one at the spec stage.
func RestartFlow(db *sql.DB, epicID int64, reason string) (EpicFlow, error) {
	var f EpicFlow
	ended := false
	err := inTx(db, func(tx *sql.Tx) (err error) {
		a, err := ActiveFlow(tx, epicID)
		if err != nil {
			return err
		}
		if a != nil {
			ended = true
			if _, err = tx.Exec(`UPDATE epic_flows SET status=?, reason=?, ended_at=? WHERE id=?`, FlowAbandoned, reason, now(), a.ID); err != nil {
				return fmt.Errorf("end flow: %w", err)
			}
		}
		if f, err = startFlow(tx, epicID); err != nil {
			return err
		}
		return setPrefixed(tx, epicID, PrefixStage, PrefixStage+StageSpec)
	})
	if err == nil && ended {
		Audit("epic.flow.end", "task_id", epicID, "flow", f.Attempt-1, "status", FlowAbandoned, "reason", reason, "actor", "human")
	}
	if err == nil {
		Audit("epic.flow.start", "task_id", epicID, "flow", f.Attempt)
	}
	return f, err
}

// Descendants returns every task below rootID in the parent tree, in id order.
func Descendants(db *sql.DB, rootID int64) ([]Task, error) {
	all, err := ListTasks(db)
	if err != nil {
		return nil, err
	}
	in := map[int64]bool{rootID: true}
	var out []Task
	for grew := true; grew; { // ListTasks order is not parent-first, so sweep until stable
		grew = false
		for _, t := range all {
			if t.ParentID != nil && in[*t.ParentID] && !in[t.ID] {
				in[t.ID] = true
				out = append(out, t)
				grew = true
			}
		}
	}
	return out, nil
}

// EpicRoot returns the root of t's parent tree.
func EpicRoot(db *sql.DB, t Task) (Task, error) {
	for t.ParentID != nil {
		p, err := GetTask(db, *t.ParentID)
		if err != nil {
			return Task{}, err
		}
		t = p
	}
	return t, nil
}

// FlowPR is a flow's row for one task and repo: its PR and review state.
type FlowPR struct {
	FlowID     int64   `json:"flow_id"`
	TaskKey    string  `json:"task"`
	RepoName   string  `json:"repo"`
	PRURL      *string `json:"pr_url"`
	HeadSHA    *string `json:"head_sha"`
	Review     *string `json:"review"`
	Checks     *string `json:"checks"`
	Unresolved int     `json:"unresolved"`
}

// SyncFlowTasks makes the flow's rows match the epic's live tasks and their PR URLs (cancelled tasks drop out).
// Review state columns are left to the review check.
func SyncFlowTasks(db *sql.DB, flowID int64, tasks []Task) error {
	return inTx(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM epic_flow_prs WHERE flow_id=? AND task_id IN (SELECT id FROM tasks WHERE status=?)`, flowID, StatusCancelled); err != nil {
			return fmt.Errorf("sync flow: %w", err)
		}
		for _, t := range tasks {
			if t.Status == StatusCancelled {
				continue
			}
			for _, r := range t.Repos {
				if _, err := tx.Exec(`INSERT INTO epic_flow_prs (flow_id, task_id, repo_id, pr_url, updated_at) VALUES (?,?,?,?,?)
 ON CONFLICT(flow_id, task_id, repo_id) DO UPDATE SET pr_url=excluded.pr_url, updated_at=excluded.updated_at`,
					flowID, t.ID, r.RepoID, r.PRURL, now()); err != nil {
					return fmt.Errorf("sync flow: %w", err)
				}
			}
		}
		return nil
	})
}

// ListFlowPRs returns the flow's rows ordered by task key then repo.
func ListFlowPRs(db *sql.DB, flowID int64) ([]FlowPR, error) {
	rows, err := db.Query(`SELECT p.flow_id, t.key, r.name, p.pr_url, p.head_sha, p.review, p.checks, p.unresolved
 FROM epic_flow_prs p JOIN tasks t ON t.id=p.task_id JOIN repos r ON r.id=p.repo_id WHERE p.flow_id=? ORDER BY t.id, r.name`, flowID)
	if err != nil {
		return nil, fmt.Errorf("list flow prs: %w", err)
	}
	defer rows.Close()
	out := []FlowPR{}
	for rows.Next() {
		var p FlowPR
		if err := rows.Scan(&p.FlowID, &p.TaskKey, &p.RepoName, &p.PRURL, &p.HeadSHA, &p.Review, &p.Checks, &p.Unresolved); err != nil {
			return nil, fmt.Errorf("scan flow pr: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EpicsToWake returns epic roots to requeue (docs/epic-builder.md §9): not done, cancelled, running or
// already queued, with an active flow, no sub-task in todo, running or needs_you, and a sub-task whose
// run ended after the epic's last run.
func EpicsToWake(db *sql.DB) ([]Task, error) {
	all, err := ListTasks(db)
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, t := range all {
		switch t.Status {
		case StatusNeedsYou, StatusReview, StatusFailed:
		default:
			continue
		}
		if !IsEpic(t) {
			continue
		}
		if f, err := ActiveFlow(db, t.ID); err != nil {
			return nil, err
		} else if f == nil {
			continue
		}
		var last sql.NullString
		if err := db.QueryRow(`SELECT MAX(ended_at) FROM runs WHERE task_id=?`, t.ID).Scan(&last); err != nil {
			return nil, fmt.Errorf("epic last run: %w", err)
		}
		if !last.Valid {
			continue
		}
		kids, err := Descendants(db, t.ID)
		if err != nil {
			return nil, err
		}
		open, finished := false, false
		for _, k := range kids {
			switch k.Status {
			case StatusTodo, StatusRunning, StatusNeedsYou:
				open = true
			}
			var end sql.NullString
			if err := db.QueryRow(`SELECT MAX(ended_at) FROM runs WHERE task_id=?`, k.ID).Scan(&end); err != nil {
				return nil, fmt.Errorf("sub-task last run: %w", err)
			}
			finished = finished || end.Valid && end.String > last.String
		}
		if !open && finished {
			out = append(out, t)
		}
	}
	return out, nil
}

// EpicHasActiveChildren reports whether a sub-task of the epic is in todo or running (the epic waits, R6).
func EpicHasActiveChildren(db *sql.DB, epic Task) (bool, error) {
	kids, err := Descendants(db, epic.ID)
	for _, k := range kids {
		if k.Status == StatusTodo || k.Status == StatusRunning {
			return true, err
		}
	}
	return false, err
}

// EpicAtGate reports whether the epic is a root in the chart stage with the plan gate on and no flow yet.
func EpicAtGate(db dbtx, t Task) (bool, error) {
	if !IsEpic(t) || EpicStage(t) != StageChart || !EpicGateOn(t) {
		return false, nil
	}
	f, err := ActiveFlow(db, t.ID)
	return f == nil, err
}

// Link is a task link by key: From <Kind> To.
type Link struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// ListLinks returns every link between tasks, as keys, plus one "loop" link per worker/checker pair.
func ListLinks(db *sql.DB) ([]Link, error) {
	rows, err := db.Query(`SELECT * FROM (SELECT f.key, t.key, l.kind FROM task_links l JOIN tasks f ON f.id=l.from_id JOIN tasks t ON t.id=l.to_id ORDER BY l.rowid)
 UNION ALL SELECT DISTINCT f.key, t.key, 'loop' FROM loops l JOIN tasks f ON f.id=l.worker_id JOIN tasks t ON t.id=l.checker_id`)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.From, &l.To, &l.Kind); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
