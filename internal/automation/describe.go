package automation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/robfig/cron/v3"
)

// Validate checks an automation before it is stored.
func Validate(a store.Automation) error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch a.ActionKind {
	case "create_task", "comment", "cli":
	default:
		return fmt.Errorf("action must be create_task, comment or cli")
	}
	if !json.Valid([]byte(a.ActionArg)) {
		return fmt.Errorf("action_arg is not valid JSON")
	}
	return ValidSchedule(a.Schedule)
}

// Next returns the next n run times of a cron expression after from, parsed like Due does.
func Next(expr string, from time.Time, n int) ([]time.Time, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, err
	}
	var out []time.Time
	for t := from; len(out) < n; {
		if t = sched.Next(t); t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

var dayNames = map[string]string{"1-5": "weekday", "1": "Monday", "2": "Tuesday", "3": "Wednesday", "4": "Thursday", "5": "Friday", "6": "Saturday", "0": "Sunday", "7": "Sunday"}

// Describe gives a short human-readable form of common cron expressions and
// falls back to a field-by-field reading.
func Describe(expr string) string {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return expr
	}
	min, hr, dom, mon, dow := f[0], f[1], f[2], f[3], f[4]
	plain := func(s string) bool { return s != "" && !strings.ContainsAny(s, "*/,-") }
	if dom == "*" && mon == "*" {
		at := ""
		if plain(min) && plain(hr) {
			var m, h int
			fmt.Sscan(min, &m)
			fmt.Sscan(hr, &h)
			at = fmt.Sprintf("%02d:%02d", h, m)
		}
		switch {
		case min == "*" && hr == "*" && dow == "*":
			return "Every minute"
		case strings.HasPrefix(min, "*/") && hr == "*" && dow == "*":
			return "Every " + min[2:] + " minutes"
		case plain(min) && hr == "*" && dow == "*":
			return "Every hour at minute " + min
		case at != "" && dow == "*":
			return "Every day at " + at
		case at != "" && dayNames[dow] != "":
			return "Every " + dayNames[dow] + " at " + at
		}
	}
	return fmt.Sprintf("minute %s, hour %s, day-of-month %s, month %s, weekday %s", min, hr, dom, mon, dow)
}
