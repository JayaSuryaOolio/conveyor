package store

import (
	"database/sql"
	"strconv"
	"sync/atomic"
	"time"
)

// wakeFn is the dispatcher's wake-up, set only in the process that runs it. Writers in other
// processes (CLI, UI) never reach it; the dispatcher's tick poll covers them.
var wakeFn atomic.Pointer[func(at time.Time)]

// SetWake installs f (nil clears it). f(at) asks the dispatcher to run its launch pass at at;
// a zero or past time means now. f must not block.
func SetWake(f func(at time.Time)) {
	if f == nil {
		wakeFn.Store(nil)
		return
	}
	wakeFn.Store(&f)
}

// Wake asks the dispatcher to run at at (zero: now). A no-op when none runs in this process.
func Wake(at time.Time) {
	if f := wakeFn.Load(); f != nil {
		(*f)(at)
	}
}

const (
	metaLastTick = "dispatch_last_tick_at" // RFC3339 start of the dispatcher's latest tick
	metaTickSecs = "dispatch_tick_secs"    // the interval that tick ran with
)

// ClampTickSecs bounds dispatch_tick_secs to 1-15; anything unparsable is the default.
func ClampTickSecs(v string) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		n, _ = strconv.Atoi(Defaults["dispatch_tick_secs"])
	}
	return min(max(n, 1), 15)
}

// TickSecs is the dispatcher's tick interval in seconds.
func TickSecs(db *sql.DB) int { return ClampTickSecs(GetSetting(db, "dispatch_tick_secs")) }

// GetMeta returns a meta value, or "".
func GetMeta(db *sql.DB, key string) string {
	var v string
	_ = db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	return v
}

// SetMeta upserts a meta value. meta holds daemon-written state that is not a user setting.
func SetMeta(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO meta (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// RecordTick is the dispatcher heartbeat: the tick's start time and the interval it runs on.
func RecordTick(db *sql.DB, at time.Time, secs int) error {
	if err := SetMeta(db, metaTickSecs, strconv.Itoa(secs)); err != nil {
		return err
	}
	return SetMeta(db, metaLastTick, at.UTC().Format(time.RFC3339Nano))
}

// Heartbeat returns the last tick and its interval; ok is false before the first tick. Readers
// derive the next tick as last+every, and treat now-last > 3*every as a dispatcher that is not running.
func Heartbeat(db *sql.DB) (last time.Time, every time.Duration, ok bool) {
	last, err := time.Parse(time.RFC3339Nano, GetMeta(db, metaLastTick))
	n, err2 := strconv.Atoi(GetMeta(db, metaTickSecs))
	if err != nil || err2 != nil || n < 1 {
		return time.Time{}, 0, false
	}
	return last, time.Duration(n) * time.Second, true
}

// HeartbeatStale reports whether the dispatcher has missed more than 3 intervals (or never ticked).
func HeartbeatStale(db *sql.DB, now time.Time) bool {
	last, every, ok := Heartbeat(db)
	return !ok || now.Sub(last) > 3*every
}

func parseRFC(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }
