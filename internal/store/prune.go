package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DeleteTasks soft-deletes the tasks with ids: the tasks row stays (deleted_at set) so its key is never
// reused and its dependent rows are deleted. Then it removes every Slack link row of a task that no longer
// exists, so orphans from earlier deletions are cleared too. It returns the counts removed.
// The link keys mirror internal/slack/links.go (slack_thread:CH:TS -> KEY, slack_task:KEY -> CH:TS).
func DeleteTasks(db *sql.DB, ids []int64, at time.Time) (tasks, links int, err error) {
	err = inTx(db, func(tx *sql.Tx) error {
		for _, id := range ids {
			// Survivors must not point at a hidden task.
			if _, err := tx.Exec(`UPDATE tasks SET parent_id=NULL WHERE parent_id=?`, id); err != nil {
				return fmt.Errorf("detach children: %w", err)
			}
			if _, err := tx.Exec(`UPDATE learnings SET applied_task_id=NULL WHERE applied_task_id=?`, id); err != nil {
				return fmt.Errorf("detach learnings: %w", err)
			}
			// The row stays, so FK cascade no longer fires: drop dependents by hand.
			for _, d := range []struct{ table, col string }{
				{"task_repos", "task_id"}, {"runs", "task_id"}, {"comments", "task_id"}, {"events", "task_id"},
				{"pr_reviews", "task_id"}, {"task_links", "from_id"}, {"task_links", "to_id"}, {"task_labels", "task_id"},
				{"epic_flows", "epic_id"}, {"epic_flow_prs", "task_id"}, {"jira_sources", "epic_id"},
				{"jira_sync", "task_id"}, {"decisions", "task_id"}, {"automations", "task_id"},
			} {
				if _, err := tx.Exec(`DELETE FROM `+d.table+` WHERE `+d.col+`=?`, id); err != nil {
					return fmt.Errorf("prune %s: %w", d.table, err)
				}
			}
			res, err := tx.Exec(`UPDATE tasks SET deleted_at=? WHERE id=? AND deleted_at IS NULL`, at.UTC().Format(time.RFC3339), id)
			if err != nil {
				return fmt.Errorf("prune task: %w", err)
			}
			n, _ := res.RowsAffected()
			tasks += int(n)
		}

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

// DeletedTaskKeys lists the keys of soft-deleted tasks.
func DeletedTaskKeys(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT key FROM tasks WHERE deleted_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
