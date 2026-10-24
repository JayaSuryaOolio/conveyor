package automation

import (
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

func TestDescribe(t *testing.T) {
	for in, want := range map[string]string{
		"0 * * * *":    "Every hour at minute 0",
		"0 9 * * 1-5":  "Every weekday at 09:00",
		"30 6 * * *":   "Every day at 06:30",
		"0 9 * * 1":    "Every Monday at 09:00",
		"*/15 * * * *": "Every 15 minutes",
		"0 9 1 * *":    "minute 0, hour 9, day-of-month 1, month *, weekday *",
	} {
		if got := Describe(in); got != want {
			t.Errorf("Describe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNext(t *testing.T) {
	from := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC) // a Friday
	got, err := Next("0 9 * * 1-5", from, 3)
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	if want := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC); !got[0].Equal(want) {
		t.Fatalf("first = %v, want %v", got[0], want)
	}
	if _, err := Next("nope", from, 3); err == nil {
		t.Fatal("want error")
	}
}

func TestValidate(t *testing.T) {
	ok := store.Automation{Name: "n", Schedule: "0 9 * * *", ActionKind: "cli", ActionArg: `{"args":["list"]}`}
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	for _, mod := range []func(*store.Automation){
		func(a *store.Automation) { a.Name = " " },
		func(a *store.Automation) { a.ActionKind = "x" },
		func(a *store.Automation) { a.ActionArg = "{" },
		func(a *store.Automation) { a.Schedule = "bad" },
	} {
		a := ok
		mod(&a)
		if Validate(a) == nil {
			t.Errorf("want error for %+v", a)
		}
	}
}
