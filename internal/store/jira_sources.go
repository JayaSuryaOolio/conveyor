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
