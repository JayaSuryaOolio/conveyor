package store

import (
	"database/sql"
	"fmt"
	"regexp"
)

var jiraKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[0-9]+$`)

// JiraSource attaches a Jira epic to a Conveyor epic root. Auth stays in acli.
type JiraSource struct {
	EpicID      int64   `json:"epic_id"`
	JiraKey     string  `json:"jira_key"`
	DefaultRepo string  `json:"default_repo"`
	IntervalSec int     `json:"interval_sec"`
	StatusMap   string  `json:"status_map"` // JSON overrides of the status -> Jira status name map; "" = defaults
	LastSync    *string `json:"last_sync"`
	LastError   string  `json:"last_error"`
	Done        bool    `json:"done"`
}

const jiraCols = `epic_id, jira_key, default_repo, interval_sec, status_map, last_sync, last_error, done`

func scanJiraSource(r interface{ Scan(...any) error }) (JiraSource, error) {
	var s JiraSource
	err := r.Scan(&s.EpicID, &s.JiraKey, &s.DefaultRepo, &s.IntervalSec, &s.StatusMap, &s.LastSync, &s.LastError, &s.Done)
	return s, err
}

// GetJiraSource returns the epic's source, or a wrapped sql.ErrNoRows.
func GetJiraSource(db *sql.DB, epicID int64) (JiraSource, error) {
	s, err := scanJiraSource(db.QueryRow(`SELECT `+jiraCols+` FROM jira_sources WHERE epic_id=?`, epicID))
	if err != nil {
		return JiraSource{}, fmt.Errorf("jira source %d: %w", epicID, err)
	}
	return s, nil
}

// SetJiraSource creates or reconfigures the epic's source (key, default repo, interval, status map).
// Changing the Jira key resets sync state; a zero interval means 300s.
func SetJiraSource(db *sql.DB, s JiraSource) error {
	if !jiraKeyRe.MatchString(s.JiraKey) { // the key is interpolated into JQL
		return fmt.Errorf("invalid jira key %q (want PROJ-123)", s.JiraKey)
	}
	if s.IntervalSec <= 0 {
		s.IntervalSec = 300
	}
	_, err := db.Exec(`INSERT INTO jira_sources (epic_id, jira_key, default_repo, interval_sec, status_map) VALUES (?,?,?,?,?)
 ON CONFLICT(epic_id) DO UPDATE SET
  last_sync=CASE WHEN jira_key<>excluded.jira_key THEN NULL ELSE last_sync END,
  last_error=CASE WHEN jira_key<>excluded.jira_key THEN '' ELSE last_error END,
  done=CASE WHEN jira_key<>excluded.jira_key THEN 0 ELSE done END,
  jira_key=excluded.jira_key, default_repo=excluded.default_repo, interval_sec=excluded.interval_sec, status_map=excluded.status_map`,
		s.EpicID, s.JiraKey, s.DefaultRepo, s.IntervalSec, s.StatusMap)
	if err != nil {
		return fmt.Errorf("set jira source: %w", err)
	}
	return nil
}

// ClearJiraSource detaches the source; synced tasks stay.
func ClearJiraSource(db *sql.DB, epicID int64) error {
	_, err := db.Exec(`DELETE FROM jira_sources WHERE epic_id=?`, epicID)
	return err
}

// ListJiraSources returns every source.
func ListJiraSources(db *sql.DB) ([]JiraSource, error) {
	rows, err := db.Query(`SELECT ` + jiraCols + ` FROM jira_sources ORDER BY epic_id`)
	if err != nil {
		return nil, fmt.Errorf("list jira sources: %w", err)
	}
	defer rows.Close()
	var out []JiraSource
	for rows.Next() {
		s, err := scanJiraSource(rows)
		if err != nil {
			return nil, fmt.Errorf("scan jira source: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// RecordJiraSync stores the outcome of a pull. last_sync (RFC3339) advances only when errMsg is empty.
func RecordJiraSync(db *sql.DB, epicID int64, syncedAt, errMsg string) error {
	_, err := db.Exec(`UPDATE jira_sources SET last_error=?, last_sync=CASE WHEN ?='' THEN ? ELSE last_sync END WHERE epic_id=?`,
		errMsg, errMsg, syncedAt, epicID)
	return err
}

// TaskByExternalKey returns the task imported from an external key, or a wrapped sql.ErrNoRows.
func TaskByExternalKey(db *sql.DB, key string) (Task, error) {
	return getTask(db, `external_key=?`, key, key)
}

// OnTaskChange, when set, is called after a task's status or branch/PR changes (Jira write-back).
// It must not block: implementations hand off to a goroutine.
var OnTaskChange func(taskID int64)

func notifyTask(id int64) {
	if f := OnTaskChange; f != nil {
		f(id)
	}
}

// SetJiraError records a write-back failure on the source; the pull's last_sync is untouched.
func SetJiraError(db *sql.DB, epicID int64, msg string) error {
	_, err := db.Exec(`UPDATE jira_sources SET last_error=? WHERE epic_id=?`, msg, epicID)
	return err
}

// SetJiraDone marks the source complete so polling stops.
func SetJiraDone(db *sql.DB, epicID int64) error {
	_, err := db.Exec(`UPDATE jira_sources SET done=1 WHERE epic_id=?`, epicID)
	return err
}

// JiraSourceOf returns the source of the epic that t sits under (t itself or an ancestor).
func JiraSourceOf(db *sql.DB, t Task) (JiraSource, bool) {
	for i := 0; i < 20 && t.ID != 0; i++ { // depth cap guards a parent cycle
		if s, err := GetJiraSource(db, t.ID); err == nil {
			return s, true
		}
		if t.ParentID == nil {
			break
		}
		var err error
		if t, err = GetTask(db, *t.ParentID); err != nil {
			break
		}
	}
	return JiraSource{}, false
}

// SyncedProgress counts the Jira-synced tasks under an epic and how many are not done/cancelled.
func SyncedProgress(db *sql.DB, epicID int64) (total, open int, err error) {
	err = db.QueryRow(`WITH RECURSIVE d(id) AS (
 SELECT id FROM tasks WHERE parent_id=? UNION SELECT t.id FROM tasks t JOIN d ON t.parent_id=d.id)
SELECT count(*), coalesce(sum(status NOT IN (?,?)),0) FROM tasks
WHERE id IN (SELECT id FROM d) AND source='jira' AND external_key IS NOT NULL`, epicID, StatusDone, StatusCancelled).Scan(&total, &open)
	return
}

// JiraSync is what Conveyor last wrote to Jira for one task.
type JiraSync struct{ Status, CommentID, Comment string }

// GetJiraSync returns the task's write-back state (zero value when nothing was written yet).
func GetJiraSync(db *sql.DB, taskID int64) JiraSync {
	var s JiraSync
	db.QueryRow(`SELECT status, comment_id, comment FROM jira_sync WHERE task_id=?`, taskID).Scan(&s.Status, &s.CommentID, &s.Comment)
	return s
}

// PutJiraSync stores the task's write-back state.
func PutJiraSync(db *sql.DB, taskID int64, s JiraSync) error {
	_, err := db.Exec(`INSERT INTO jira_sync (task_id, status, comment_id, comment) VALUES (?,?,?,?)
 ON CONFLICT(task_id) DO UPDATE SET status=excluded.status, comment_id=excluded.comment_id, comment=excluded.comment`,
		taskID, s.Status, s.CommentID, s.Comment)
	return err
}
