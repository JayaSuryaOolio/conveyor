package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestAppsCRUD(t *testing.T) {
	db := openTest(t)
	if _, err := CreateApp(db, App{Name: "Zed2", Manifest: "{}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateApp(db, App{Name: "Zed2"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	CreateApp(db, App{Name: "finder"})
	if got, err := GetApp(db, "Zed2"); err != nil || got.Manifest != "{}" {
		t.Fatal(got, err)
	}
	as, err := ListApps(db)
	if err != nil || len(as) != 2 || as[0].Name != "finder" {
		t.Fatal(as, err)
	}
	if err := DeleteApp(db, "Zed2"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteApp(db, "Zed2"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}

// An apps table with the legacy global column loses it on Migrate, keeping rows.
func TestMigrateDropsAppGlobal(t *testing.T) {
	db := openTest(t)
	if _, err := db.Exec(`DROP TABLE apps; CREATE TABLE apps (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, command TEXT NOT NULL, global INTEGER NOT NULL DEFAULT 0); INSERT INTO apps (name, command, global) VALUES ('old','x',1)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var cmd string
	if err := db.QueryRow(`SELECT command FROM apps WHERE name='old'`).Scan(&cmd); err != nil || cmd != "x" {
		t.Fatal(cmd, err)
	}
}
