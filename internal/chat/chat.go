// Package chat is the transport-neutral side of Conveyor's chat bridge: it watches task
// status events, pushes a Msg to the configured Transport, and turns replies and commands
// coming back into comments and status moves. A Transport (Slack, Teams, ...) only knows
// how to render/send a Msg and deliver inbound text; it lives in its own package.
//
// Settings: chat_enabled (on|off), chat_transport (name), chat_statuses (comma list of
// needs_you,review,done,failed that trigger a message). Transports read their own keys.
package chat

import (
	"cmp"
	"context"
	"encoding/base64"
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
// Fwd, when set, returns the message the command refers to (e.g. one forwarded to the
// bot) as Markdown plus its images; it is called only by "new", so nothing is fetched otherwise.
type In struct {
	Key, Text string
	Fwd       func() (string, []File)
}

// File is an image carried by a forwarded message.
type File struct {
	Name string
	Data []byte
}

// Transport delivers Msgs to the user and inbound items back. Implementations must be
// safe to Send from one goroutine while Listen runs in another.
type Transport interface {
	Ready() bool                                         // configured enough to run
	Send(Msg) error                                      // an error is retried next tick
	Listen(ctx context.Context, h func(In) string) error // one session; returns when it drops
}

// Factory builds a Transport reading its config through get and saving changed config (e.g. rotated tokens) through set.
type Factory func(get func(string) string, set func(k, v string) error) Transport

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
	if t := f(b.setting, func(k, v string) error { return store.SetSetting(b.svc.DB, k, v) }); t.Ready() {
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

// retryDelay is 5s doubling per consecutive listen failure, capped at 10 minutes.
func retryDelay(fails int) time.Duration {
	if fails > 8 { // 5s<<8 already exceeds the cap; also avoids shift overflow
		fails = 8
	}
	return min(5*time.Second<<max(fails-1, 0), 10*time.Minute)
}

func (b *bridge) listen(ctx context.Context, fs map[string]Factory) {
	fails, last := 0, ""
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
			if err := t.Listen(sctx, b.handle); err == nil {
				fails, last = 0, ""
			} else if sctx.Err() == nil {
				fails++
				if err.Error() != last { // a persistent error (e.g. invalid_refresh_token) logs once
					last = err.Error()
					b.logf("chat: listen: %v", err)
				}
			}
			cancel()
		}
		select {
		case <-ctx.Done():
		case <-time.After(retryDelay(fails)):
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

// maxRetry bounds how long one failing event can hold the cursor; past it the event is
// dropped, so a permanent error (invalid_blocks, revoked token) can't stall the queue and
// later flood Slack with the backlog when it clears.
const maxRetry = time.Hour

// notify sends a Msg for each new system status change the user asked for. A nil t
// (disabled/unconfigured) skips events rather than replaying them when re-enabled.
// An event the task has since moved past (any later status change) is skipped as stale.
func (b *bridge) notify(t Transport) {
	cur, err := strconv.ParseInt(b.setting(cursorKey), 10, 64)
	if err != nil || t == nil { // first run or off: start from now, never replay history
		store.SetSetting(b.svc.DB, cursorKey, strconv.FormatInt(b.maxEvent(), 10))
		return
	}
	want := b.wanted() // before Query: the DB has one connection
	rows, err := b.svc.DB.Query(`SELECT id, task_id, detail, created_at FROM events WHERE kind='status_changed' AND id>? ORDER BY id`, cur)
	if err != nil {
		b.logf("chat: events: %v", err)
		return
	}
	type ev struct {
		id, task int64
		to       string
		at       time.Time
	}
	var evs []ev
	for rows.Next() {
		var e ev
		var detail, at string
		if rows.Scan(&e.id, &e.task, &detail, &at) == nil {
			e.at, _ = time.Parse(time.RFC3339, at)
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
			var later bool // the task changed status again since: this one is stale
			b.svc.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE task_id=? AND kind='status_changed' AND id>?)`, e.task, e.id).Scan(&later)
			if err != nil || later {
				store.SetSetting(b.svc.DB, cursorKey, strconv.FormatInt(e.id, 10))
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
				if time.Since(e.at) < maxRetry {
					b.logf("chat: send %s: %v", task.Key, err)
					return // retry this event next tick
				}
				b.logf("chat: send %s: %v; dropped after %s", task.Key, err, maxRetry)
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
	if f := strings.Fields(in.Text); len(f) > 0 && strings.EqualFold(f[0], "new") { // phones capitalise "New"
		return b.create(strings.TrimSpace(strings.TrimSpace(in.Text)[len(f[0]):]), in.Fwd)
	}
	return b.command(in.Text)
}

// create makes a backlog task from "new [REPO] comment": the comment, then the forwarded
// message and its images. REPO may be omitted when only one repo is registered.
func (b *bridge) create(text string, fwd func() (string, []File)) string {
	repos, err := store.ListRepos(b.svc.DB)
	if err != nil {
		return err.Error()
	}
	var names []string
	repo := ""
	for _, r := range repos {
		names = append(names, r.Name)
		if f := strings.Fields(text); len(f) > 0 && strings.EqualFold(f[0], r.Name) {
			repo = r.Name
		}
	}
	if repo != "" {
		text = strings.TrimSpace(text[len(repo):])
	} else if len(names) == 1 {
		repo = names[0]
	} else {
		return "Name a repo: new REPO your comment (repos: " + strings.Join(names, ", ") + ")"
	}
	desc, quote := text, ""
	if fwd != nil {
		var imgs []File
		quote, imgs = fwd()
		for _, f := range imgs {
			p, err := core.SaveAttachment(b.svc.Home, "", f.Name, base64.StdEncoding.EncodeToString(f.Data))
			if err != nil {
				quote += "\n\n(image " + f.Name + " not saved: " + err.Error() + ")"
				continue
			}
			quote += "\n\n![" + f.Name + "](" + p + ")"
		}
		if quote = strings.TrimSpace(quote); quote != "" {
			desc = strings.TrimSpace(desc + "\n\n## Forwarded message\n\n" + quote)
		}
	}
	title, _, _ := strings.Cut(strings.TrimSpace(strings.TrimLeft(cmp.Or(text, quote), "> ")), "\n")
	title = strings.TrimSpace(strings.Trim(title, `"“”'‘’`))
	if title == "" {
		return "Usage: new [REPO] your comment (forward a message to me first to include it)"
	}
	if r := []rune(title); len(r) > 80 {
		title = string(r[:80]) + "…"
	}
	t, err := b.svc.CreateTask(core.NewTaskInput{Title: title, Description: desc, Repos: []string{repo}, Source: b.setting("chat_transport")})
	if err != nil {
		return "warning: " + err.Error()
	}
	return fmt.Sprintf("created %s %s (backlog; move %s todo to start it)", t.Key, t.Title, t.Key)
}

func (b *bridge) comment(key, body string) string {
	if _, err := b.svc.AddCommentVia(key, store.AuthorHuman, b.setting("chat_transport"), strings.TrimSpace(body), core.FollowUp); err != nil {
		return "warning: " + err.Error()
	}
	return "comment added to " + key
}

// Help is the command guide shown for "help" and unknown commands; transports may reuse it.
const Help = `*Conveyor commands* (send as a DM, or /conveyor …)
• list — tasks waiting on you
• new [REPO] your comment — new backlog task from your comment and the message you forwarded to me just before (images included)
• KEY your comment — comment on a task (e.g. CV-12 use the v2 API)
• show KEY (or just KEY) — status, repos, PRs and the last agent message
• move KEY todo|done|cancelled — change status
• help — this guide
Replying in a task's thread also comments on it.`

var keyArg = regexp.MustCompile(`^[A-Za-z0-9]+-[A-Za-z0-9]+$`)

// show summarizes one task: status, repos with PRs, and the agent's last comment.
func (b *bridge) show(key string) string {
	t, err := store.GetTaskByKey(b.svc.DB, key)
	if err != nil {
		return err.Error()
	}
	s := fmt.Sprintf("%s %s — %s", t.Key, t.Title, t.Status)
	for _, r := range t.Repos {
		s += "\n" + r.RepoName + " (" + r.Branch + ")"
		if r.PRURL != nil {
			s += " " + *r.PRURL
		}
	}
	cs, _ := store.ListComments(b.svc.DB, t.ID)
	for i := len(cs) - 1; i >= 0; i-- {
		if cs[i].Author == store.AuthorAgent {
			last := strings.TrimSpace(cs[i].Body)
			if len(last) > 1000 {
				last = last[:1000] + "…"
			}
			s += "\n> " + strings.ReplaceAll(last, "\n", "\n> ")
			break
		}
	}
	return s
}

// command runs "help", "" or "list", "show KEY" (or bare KEY), "move KEY STATUS", or "KEY text".
func (b *bridge) command(text string) string {
	f := strings.Fields(text)
	if len(f) == 4 && f[0] == "move" && f[2] == "to" { // "move KEY to done"
		f = []string{f[0], f[1], f[3]}
	}
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
	case f[0] == "show" && len(f) == 2:
		return b.show(f[1])
	case len(f) == 1 && keyArg.MatchString(f[0]):
		return b.show(f[0])
	case f[0] == "move" && len(f) == 3:
		t, err := store.GetTaskByKey(b.svc.DB, f[1])
		if err != nil {
			return err.Error()
		}
		if _, err := store.UpdateTaskStatus(b.svc.DB, t.ID, store.Status(f[2]), store.ActorHuman); err != nil {
			return "warning: " + err.Error()
		}
		return fmt.Sprintf("%s → %s", f[1], f[2])
	case len(f) >= 2 && keyArg.MatchString(f[0]):
		return b.comment(f[0], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), f[0])))
	}
	return Help
}
