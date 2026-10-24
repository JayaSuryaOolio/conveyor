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
