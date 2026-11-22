package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PruneClosed soft-deletes done/cancelled tasks last updated before now-olderThan: the tasks row stays
// (deleted_at set) so its key is never reused, its dependent rows are deleted, then every Slack link row of a task that no longer exists, so orphans from
// earlier deletions are cleared too. It returns the counts removed.
// The link keys mirror internal/slack/links.go (slack_thread:CH:TS -> KEY, slack_task:KEY -> CH:TS).
func PruneClosed(db *sql.DB, olderThan time.Duration, at time.Time) (tasks, links int, err error) {
	cutoff := at.Add(-olderThan).UTC().Format(time.RFC3339)
	err = inTx(db, func(tx *sql.Tx) error {
		const old = `SELECT id FROM tasks WHERE deleted_at IS NULL AND status IN ('done','cancelled') AND updated_at < ?`
		// Survivors must not point at a hidden task.
		if _, err := tx.Exec(`UPDATE tasks SET parent_id=NULL WHERE parent_id IN (`+old+`)`, cutoff); err != nil {
			return fmt.Errorf("detach children: %w", err)
		}
		if _, err := tx.Exec(`UPDATE learnings SET applied_task_id=NULL WHERE applied_task_id IN (`+old+`)`, cutoff); err != nil {
			return fmt.Errorf("detach learnings: %w", err)
		}
		// The row stays, so FK cascade no longer fires: drop dependents by hand (before deleted_at is set).
		for _, d := range []struct{ table, col string }{
			{"task_repos", "task_id"}, {"runs", "task_id"}, {"comments", "task_id"}, {"events", "task_id"},
			{"pr_reviews", "task_id"}, {"task_links", "from_id"}, {"task_links", "to_id"}, {"task_labels", "task_id"},
			{"epic_flows", "epic_id"}, {"epic_flow_prs", "task_id"}, {"jira_sources", "epic_id"},
			{"jira_sync", "task_id"}, {"decisions", "task_id"}, {"automations", "task_id"},
		} {
			if _, err := tx.Exec(`DELETE FROM `+d.table+` WHERE `+d.col+` IN (`+old+`)`, cutoff); err != nil {
				return fmt.Errorf("prune %s: %w", d.table, err)
			}
		}
		res, err := tx.Exec(`UPDATE tasks SET deleted_at=? WHERE id IN (`+old+`)`, at.UTC().Format(time.RFC3339), cutoff)
		if err != nil {
			return fmt.Errorf("prune tasks: %w", err)
		}
		n, _ := res.RowsAffected()
		tasks = int(n)

		rows, err := tx.Query(`SELECT key, value FROM settings WHERE key LIKE 'slack_thread:%' OR key LIKE 'slack_task:%'`)
		if err != nil {
			return err
		}
		var dead []string
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				rows.Close()
				return err
			}
			task := v // slack_thread -> value is the task key
			if rest, ok := strings.CutPrefix(k, "slack_task:"); ok {
				task = rest
			}
			var one int
			if tx.QueryRow(`SELECT 1 FROM tasks WHERE key=? AND deleted_at IS NULL`, task).Scan(&one) == sql.ErrNoRows {
				dead = append(dead, k)
			}
		}
		rows.Close()
		for _, k := range dead {
			if _, err := tx.Exec(`DELETE FROM settings WHERE key=?`, k); err != nil {
				return err
			}
		}
		links = len(dead)
		return nil
	})
	return
}
