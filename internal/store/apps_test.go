package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestAppsCRUD(t *testing.T) {
	db := openTest(t)
	a, err := CreateApp(db, App{Name: "Zed", Command: "zed {cwd}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateApp(db, App{Name: "Zed", Command: "x"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	CreateApp(db, App{Name: "finder", Command: "open {cwd}"})
	a.Name, a.Command = "Zed2", "open -a Zed {cwd}"
	if err := UpdateApp(db, a); err != nil {
		t.Fatal(err)
	}
	if got, err := GetApp(db, "Zed2"); err != nil || got.Command != "open -a Zed {cwd}" {
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
	if got, err := GetApp(db, "old"); err != nil || got.Command != "x" {
		t.Fatal(got, err)
	}
}
