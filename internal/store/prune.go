package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PruneClosed deletes done/cancelled tasks last updated before now-olderThan (dependent rows go
// by FK cascade), then every Slack link row of a task that no longer exists, so orphans from
// earlier deletions are cleared too. It returns the counts removed.
// The link keys mirror internal/slack/links.go (slack_thread:CH:TS -> KEY, slack_task:KEY -> CH:TS).
func PruneClosed(db *sql.DB, olderThan time.Duration, at time.Time) (tasks, links int, err error) {
	cutoff := at.Add(-olderThan).UTC().Format(time.RFC3339)
	err = inTx(db, func(tx *sql.Tx) error {
		const old = `SELECT id FROM tasks WHERE status IN ('done','cancelled') AND updated_at < ?`
		// parent_id and applied_task_id have no cascade: detach survivors so the delete can't fail.
		if _, err := tx.Exec(`UPDATE tasks SET parent_id=NULL WHERE parent_id IN (`+old+`)`, cutoff); err != nil {
			return fmt.Errorf("detach children: %w", err)
		}
		if _, err := tx.Exec(`UPDATE learnings SET applied_task_id=NULL WHERE applied_task_id IN (`+old+`)`, cutoff); err != nil {
			return fmt.Errorf("detach learnings: %w", err)
		}
		res, err := tx.Exec(`DELETE FROM tasks WHERE id IN (`+old+`)`, cutoff)
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
			if tx.QueryRow(`SELECT 1 FROM tasks WHERE key=?`, task).Scan(&one) == sql.ErrNoRows {
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
