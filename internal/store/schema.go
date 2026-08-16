package store

import "database/sql"

const schemaSQL = `
CREATE TABLE IF NOT EXISTS kanban_tasks (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT,
  status TEXT NOT NULL DEFAULT 'draft',
  mode TEXT NOT NULL DEFAULT 'headless',
  remote_control_enabled INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL,
  external_key TEXT,
  external_url TEXT,
  worktree_path TEXT,
  wts_worktree_id TEXT,
  wts_terminal_id TEXT,
  claude_session_id TEXT,
  claim_lock TEXT,
  claimed_at TEXT,
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  latest_message TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_kanban_tasks_external_key ON kanban_tasks(external_key) WHERE external_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS kanban_task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL REFERENCES kanban_tasks(id) ON DELETE CASCADE,
  event TEXT NOT NULL,
  detail TEXT,
  created_at TEXT NOT NULL
);
`

// Migrate creates kanban_tasks and kanban_task_events if they don't already
// exist. Future schema changes get their own migrateAddX(db) step, called
// from here after the base CREATE TABLEs, each idempotent via PRAGMA
// table_info before ALTER TABLE.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(schemaSQL)
	return err
}
