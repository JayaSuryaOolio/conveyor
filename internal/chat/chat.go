// Package chat is the transport-neutral side of Conveyor's chat bridge: it watches task
// status events, pushes a Msg to the configured Transport, and turns replies and commands
// coming back into comments and status moves. A Transport (Slack, Teams, ...) only knows
// how to render/send a Msg and deliver inbound text; it lives in its own package.
//
// Settings: chat_enabled (on|off), chat_transport (name), chat_statuses (comma list of
// needs_you,review,done,failed that trigger a message). Transports read their own keys.
package chat

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
)

const cursorKey = "chat_cursor"

// Msg is one task notification, rendered by the transport in its own markup.
type Msg struct {
	Key, Title, Status string
	Last               string // the agent's last comment
	PRs                []Link
}

type Link struct{ Name, URL string }

// In is one inbound item: a reply to a Msg thread (Key set) or a command (Key empty).
type In struct{ Key, Text string }

// Transport delivers Msgs to the user and inbound items back. Implementations must be
// safe to Send from one goroutine while Listen runs in another.
type Transport interface {
	Ready() bool                                         // configured enough to run
	Send(Msg) error                                      // an error is retried next tick
	Listen(ctx context.Context, h func(In) string) error // one session; returns when it drops
}

// Factory builds a Transport reading its config through get (a settings getter).
type Factory func(get func(string) string) Transport

type bridge struct {
	svc  *core.Service
	logf func(string, ...any)
}

func (b *bridge) setting(k string) string { return store.GetSetting(b.svc.DB, k) }

// active returns the configured, enabled transport or nil.
func (b *bridge) active(fs map[string]Factory) Transport {
	if b.setting("chat_enabled") == "off" {
		return nil
	}
	f := fs[b.setting("chat_transport")]
	if f == nil {
		return nil
	}
	if t := f(b.setting); t.Ready() {
		return t
	}
	return nil
}

// Run bridges chat until ctx is done. Settings are re-read every few seconds, so toggling
// or reconfiguring takes effect without a restart.
func Run(ctx context.Context, svc *core.Service, logf func(string, ...any), fs map[string]Factory) {
	b := &bridge{svc, logf}
	go b.listen(ctx, fs)
	for ctx.Err() == nil {
		b.notify(b.active(fs))
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

func (b *bridge) listen(ctx context.Context, fs map[string]Factory) {
	for ctx.Err() == nil {
		if t := b.active(fs); t != nil {
			sctx, cancel := context.WithCancel(ctx)
			go func() { // drop the session as soon as it is switched off or reconfigured
				for sctx.Err() == nil {
					time.Sleep(3 * time.Second)
					if b.active(fs) == nil {
						cancel()
					}
				}
			}()
			if err := t.Listen(sctx, b.handle); err != nil && sctx.Err() == nil {
				b.logf("chat: listen: %v", err)
			}
			cancel()
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

var toStatus = regexp.MustCompile(`-> (\w+) \(system\)$`)

func (b *bridge) wanted() map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Split(b.setting("chat_statuses"), ",") {
		m[strings.TrimSpace(s)] = true
	}
	return m
}

func (b *bridge) maxEvent() (cur int64) {
	b.svc.DB.QueryRow(`SELECT COALESCE(MAX(id),0) FROM events`).Scan(&cur)
	return
}

// notify sends a Msg for each new system status change the user asked for. A nil t
// (disabled/unconfigured) skips events rather than replaying them when re-enabled.
func (b *bridge) notify(t Transport) {
	cur, err := strconv.ParseInt(b.setting(cursorKey), 10, 64)
	if err != nil || t == nil { // first run or off: start from now, never replay history
		store.SetSetting(b.svc.DB, cursorKey, strconv.FormatInt(b.maxEvent(), 10))
		return
	}
	want := b.wanted() // before Query: the DB has one connection
	rows, err := b.svc.DB.Query(`SELECT id, task_id, detail FROM events WHERE kind='status_changed' AND id>? ORDER BY id`, cur)
	if err != nil {
		b.logf("chat: events: %v", err)
		return
	}
	type ev struct {
		id, task int64
		to       string
	}
	var evs []ev
	for rows.Next() {
		var e ev
		var detail string
		if rows.Scan(&e.id, &e.task, &detail) == nil {
			if m := toStatus.FindStringSubmatch(detail); m != nil && want[m[1]] {
				e.to = m[1]
			}
			evs = append(evs, e)
		}
	}
	rows.Close()
	for _, e := range evs {
		if e.to != "" {
			task, err := store.GetTask(b.svc.DB, e.task)
			if err != nil {
				continue
			}
			m := Msg{Key: task.Key, Title: task.Title, Status: e.to}
			cs, _ := store.ListComments(b.svc.DB, task.ID)
			for i := len(cs) - 1; i >= 0; i-- {
				if cs[i].Author == store.AuthorAgent {
					m.Last = cs[i].Body
					break
				}
			}
			for _, r := range task.Repos {
				if r.PRURL != nil {
					m.PRs = append(m.PRs, Link{r.RepoName, *r.PRURL})
				}
			}
			if err := t.Send(m); err != nil {
				b.logf("chat: send: %v", err)
				return // retry this event next tick
			}
		}
		store.SetSetting(b.svc.DB, cursorKey, strconv.FormatInt(e.id, 10))
	}
}

// handle serves one inbound item and returns the reply text.
func (b *bridge) handle(in In) string {
	if in.Key != "" {
		return b.comment(in.Key, in.Text)
	}
	return b.command(in.Text)
}

func (b *bridge) comment(key, body string) string {
	if _, err := b.svc.AddComment(key, store.AuthorHuman, strings.TrimSpace(body), core.FollowUp); err != nil {
		return "warning: " + err.Error()
	}
	return "comment added to " + key
}

// Help is the command guide shown for "help" and unknown commands; transports may reuse it.
const Help = `*Conveyor commands* (send as a DM, or /conveyor …)
• list — tasks waiting on you
• KEY your comment — comment on a task (e.g. CV-12 use the v2 API)
• move KEY todo|done|cancelled — change status
• help — this guide
Replying in a task's thread also comments on it.`

// command runs "help", "" or "list", "move KEY STATUS", or "KEY text".
func (b *bridge) command(text string) string {
	f := strings.Fields(text)
	switch {
	case len(f) > 0 && (f[0] == "help" || f[0] == "?"):
		return Help
	case len(f) == 0 || f[0] == "list":
		ts, err := store.ListTasks(b.svc.DB, store.StatusNeedsYou, store.StatusReview)
		if err != nil {
			return err.Error()
		}
		if len(ts) == 0 {
			return "Nothing needs you."
		}
		var s []string
		for _, t := range ts {
			s = append(s, fmt.Sprintf("%s %s — %s", t.Key, t.Title, t.Status))
		}
		return strings.Join(s, "\n")
	case f[0] == "move" && len(f) == 3:
		t, err := store.GetTaskByKey(b.svc.DB, f[1])
		if err != nil {
			return err.Error()
		}
		if _, err := store.UpdateTaskStatus(b.svc.DB, t.ID, store.Status(f[2]), store.ActorHuman); err != nil {
			return "warning: " + err.Error()
		}
		return fmt.Sprintf("%s → %s", f[1], f[2])
	case len(f) >= 2:
		return b.comment(f[0], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), f[0])))
	}
	return Help
}
