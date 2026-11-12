package slack

import (
	"sort"
	"strings"
)

// The thread<->task links are part of Conveyor's state: they live in the settings table, so
// ExportState carries them. The UI hides them from Settings -> General and lists them under
// the Slack integration instead (see StripLinks and Links).
const (
	threadPrefix = "slack_thread:" // slack_thread:CHANNEL:ROOT_TS -> KEY
	taskPrefix   = "slack_task:"   // slack_task:KEY -> CHANNEL:ROOT_TS
)

// Link is one Slack thread routed to a task.
type Link struct {
	Task    string `json:"task"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
	Notify  bool   `json:"notify"` // the task's notifications are posted in this thread
}

// StripLinks returns settings without the link rows.
func StripLinks(settings map[string]string) map[string]string {
	out := make(map[string]string, len(settings))
	for k, v := range settings {
		if !strings.HasPrefix(k, threadPrefix) && !strings.HasPrefix(k, taskPrefix) {
			out[k] = v
		}
	}
	return out
}

// Links lists the thread links in settings, sorted by task then thread, and returns the
// page of size rows starting at offset together with the total.
func Links(settings map[string]string, offset, size int) ([]Link, int) {
	var all []Link
	for k, task := range settings {
		rest, ok := strings.CutPrefix(k, threadPrefix)
		if !ok {
			continue
		}
		ch, ts, _ := strings.Cut(rest, ":")
		all = append(all, Link{task, ch, ts, settings[taskPrefix+task] == ch+":"+ts})
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Task != b.Task {
			return a.Task < b.Task
		}
		return a.Channel+":"+a.TS < b.Channel+":"+b.TS
	})
	total := len(all)
	if offset < 0 || offset > total {
		offset = total
	}
	return all[offset:min(offset+size, total)], total
}
