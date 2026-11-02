// Package audit writes epic audit records as slog JSON lines to logs/audit.jsonl (docs/epic-builder.md §12).
package audit

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

// Path is the audit log under home.
func Path(home string) string { return filepath.Join(home, "logs", "audit.jsonl") }

// NewLogger returns a slog JSON logger with UTC timestamps; reuse it for any other structured log.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			a.Value = slog.StringValue(a.Value.Time().UTC().Format(time.RFC3339))
		}
		return a
	}}))
}

// fileWriter appends one record per Write, reopening the file so size-based rotation (core.OpenLog)
// holds for long-lived processes and concurrent writers (O_APPEND keeps a line whole).
type fileWriter string

func (p fileWriter) Write(b []byte) (int, error) {
	f, err := core.OpenLog(string(p))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.Write(b)
}

// Install routes store.Audit into the audit log of home. Records about tasks outside an epic are dropped.
// Best effort: audit failures never fail the caller.
func Install(home string) {
	log := NewLogger(fileWriter(Path(home)))
	var once sync.Once
	var rdb *sql.DB
	store.Audit = func(event string, kv ...any) {
		// A second connection: Audit also runs inside store transactions, and the main pool has one conn.
		// ponytail: never closed; the process exit does it.
		once.Do(func() {
			rdb, _ = sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=query_only(1)", core.DBPath(home)))
		})
		if rdb == nil {
			return
		}
		if msg, attrs, ok := resolve(rdb, event, kv); ok {
			log.Info(msg, attrs...)
		}
	}
}

// resolve turns call-site fields (task_id or task, optional flow, msg, actor, anything else) into a record.
func resolve(db *sql.DB, event string, kv []any) (msg string, attrs []any, ok bool) {
	var t store.Task
	var err error
	var flow any
	msg, actor := event, "conveyor"
	var rest []any
	have := false
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		switch v := kv[i+1]; k {
		case "task_id":
			id, _ := v.(int64)
			t, err = store.GetTask(db, id)
			have = err == nil
		case "task":
			t, err = store.GetTaskByKey(db, fmt.Sprint(v))
			have = err == nil
		case "msg":
			msg = fmt.Sprint(v)
		case "actor":
			actor = fmt.Sprint(v)
		case "flow":
			flow = v
		default:
			rest = append(rest, k, v)
		}
	}
	if !have {
		return
	}
	root, err := store.EpicRoot(db, t)
	if err != nil || !store.IsEpic(root) {
		return
	}
	if flow == nil {
		if f, err := store.ActiveFlow(db, root.ID); err == nil && f != nil {
			flow = f.Attempt
		}
	}
	attrs = []any{"event", event, "epic", store.EpicName(root), "root", root.Key, "task", t.Key, "actor", actor}
	if flow != nil {
		attrs = append(attrs, "flow", flow)
	}
	return msg, append(attrs, rest...), true
}

// Record is one decoded audit line.
type Record map[string]any

// Filter selects records of one epic; zero fields match everything.
type Filter struct {
	Root  string    // root task key
	Flow  int       // flow attempt
	Since time.Time // records at or after
}

// Read returns the matching records, oldest first, from the rotated file and the current one.
func Read(home string, f Filter) ([]Record, error) {
	var out []Record
	for _, p := range []string{Path(home) + ".1", Path(home)} {
		fh, err := os.Open(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			var r Record
			if json.Unmarshal(sc.Bytes(), &r) != nil || !f.match(r) {
				continue
			}
			out = append(out, r)
		}
		fh.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (f Filter) match(r Record) bool {
	if f.Root != "" && r["root"] != f.Root {
		return false
	}
	if n, _ := r["flow"].(float64); f.Flow != 0 && int(n) != f.Flow {
		return false
	}
	if !f.Since.IsZero() {
		ts, err := time.Parse(time.RFC3339, fmt.Sprint(r["time"]))
		return err == nil && !ts.Before(f.Since)
	}
	return true
}
