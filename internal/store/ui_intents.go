package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// IntentTTL is how long an unconsumed intent stays valid: one queued while the app was closed must not fire later.
const IntentTTL = 10 * time.Second

// Intent is one CLI request for the running app. Verb is go or palette; Args is JSON (a View, or {"q":...}).
type Intent struct {
	ID   int64           `json:"id"`
	Verb string          `json:"verb"`
	Args json.RawMessage `json:"args"`
}

func intentTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// AddIntent queues an intent for the app and returns its id.
func AddIntent(db *sql.DB, verb string, args json.RawMessage, now time.Time) (int64, error) {
	r, err := db.Exec(`INSERT INTO ui_intents (verb, args, created_at) VALUES (?,?,?)`, verb, string(args), intentTime(now))
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// IntentConsumed reports whether the app has picked up intent id.
func IntentConsumed(db *sql.DB, id int64) (bool, error) {
	var at sql.NullString
	err := db.QueryRow(`SELECT consumed_at FROM ui_intents WHERE id=?`, id).Scan(&at)
	return at.Valid, err
}

// DeleteIntent removes intent id (the CLI's cleanup after a timeout).
func DeleteIntent(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM ui_intents WHERE id=?`, id)
	return err
}

// ConsumeIntents deletes intents older than IntentTTL unread, marks the rest consumed and returns them
// oldest first. RFC3339Nano UTC strings compare chronologically only at equal precision, so the cutoff
// compares parsed times in Go, not in SQL.
func ConsumeIntents(db *sql.DB, now time.Time) ([]Intent, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, verb, args, created_at FROM ui_intents WHERE consumed_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var live []Intent
	var stale []int64
	for rows.Next() {
		var i Intent
		var args, created string
		if err := rows.Scan(&i.ID, &i.Verb, &args, &created); err != nil {
			rows.Close()
			return nil, err
		}
		i.Args = json.RawMessage(args)
		if at, err := time.Parse(time.RFC3339Nano, created); err != nil || now.Sub(at) > IntentTTL {
			stale = append(stale, i.ID)
		} else {
			live = append(live, i)
		}
	}
	rows.Close()
	// Consumed rows only matter to the CLI polling for them; drop old ones so the table stays small.
	if _, err := tx.Exec(`DELETE FROM ui_intents WHERE consumed_at IS NOT NULL AND consumed_at < ?`, intentTime(now.Add(-time.Minute))); err != nil {
		return nil, err
	}
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM ui_intents WHERE id=?`, id); err != nil {
			return nil, err
		}
	}
	for _, i := range live {
		if _, err := tx.Exec(`UPDATE ui_intents SET consumed_at=? WHERE id=?`, intentTime(now), i.ID); err != nil {
			return nil, err
		}
	}
	return live, tx.Commit()
}
