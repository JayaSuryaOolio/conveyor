package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestAppsCRUD(t *testing.T) {
	db := openTest(t)
	if _, err := CreateConnector(db, Connector{Name: "Zed2", Manifest: "{}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateConnector(db, Connector{Name: "Zed2"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	CreateConnector(db, Connector{Name: "finder"})
	if got, err := GetConnector(db, "Zed2"); err != nil || got.Manifest != "{}" {
		t.Fatal(got, err)
	}
	as, err := ListConnectors(db)
	if err != nil || len(as) != 2 || as[0].Name != "finder" {
		t.Fatal(as, err)
	}
	if err := DeleteConnector(db, "Zed2"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConnector(db, "Zed2"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}

// A legacy apps table is renamed to connectors on Migrate and loses its global column, keeping rows.
func TestMigrateDropsAppGlobal(t *testing.T) {
	db := openTest(t)
	if _, err := db.Exec(`DROP TABLE connectors; CREATE TABLE apps (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, command TEXT NOT NULL, global INTEGER NOT NULL DEFAULT 0); INSERT INTO apps (name, command, global) VALUES ('old','x',1)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var cmd string
	if err := db.QueryRow(`SELECT command FROM connectors WHERE name='old'`).Scan(&cmd); err != nil || cmd != "x" {
		t.Fatal(cmd, err)
	}
}

// A pre-rebrand database (apps table, "app" events) keeps its rows under the new names.
func TestMigrateRenamesApps(t *testing.T) {
	db := openTest(t)
	if _, err := db.Exec(`DROP TABLE connectors; CREATE TABLE apps (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, manifest TEXT NOT NULL DEFAULT '', disabled INTEGER NOT NULL DEFAULT 0, settings TEXT NOT NULL DEFAULT '{}');
		INSERT INTO apps (name, manifest) VALUES ('old','{}'); INSERT INTO events (kind, created_at) VALUES ('app','now')`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := GetConnector(db, "old"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM events WHERE kind='connector'`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
