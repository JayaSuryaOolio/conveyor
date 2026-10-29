// Package slack is Conveyor's Slack bridge: it DMs you when a task needs you, is in review,
// done or failed (with PR links), and lets you answer from Slack, so the Slack mobile app is
// Conveyor's mobile client. It uses Socket Mode, so no public URL is needed.
//
// Settings: slack_bot_token (xoxb-), slack_app_token (xapp-, scope connections:write) and
// slack_user_id (your member id: the only user the bridge talks to or listens to).
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/core"
	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/gorilla/websocket"
)

const cursorKey = "slack_cursor"

// api is the Slack Web API base (swapped in tests).
var api = "https://slack.com/api/"

type bridge struct {
	svc  *core.Service
	logf func(string, ...any)
}

// Run bridges Slack until ctx is done. It idles until the tokens are configured and
// re-reads settings every few seconds, so `conveyor config set` takes effect without a restart.
func Run(ctx context.Context, svc *core.Service, logf func(string, ...any)) {
	b := &bridge{svc, logf}
	go b.listen(ctx)
	for ctx.Err() == nil {
		b.notify()
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

func (b *bridge) setting(k string) string { return store.GetSetting(b.svc.DB, k) }

// call posts a JSON Web API method with token and returns the decoded response.
func call(token, method string, args any) (map[string]any, error) {
	body, _ := json.Marshal(args)
	req, _ := http.NewRequest("POST", api+method, strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	if ok, _ := out["ok"].(bool); !ok {
		return nil, fmt.Errorf("slack %s: %v", method, out["error"])
	}
	return out, nil
}

// ---- outbound: status changes -> DM ----

var toStatus = regexp.MustCompile(`-> (\w+) \(system\)$`)

var headline = map[string]string{
	"needs_you": ":raising_hand: needs you",
	"review":    ":mag: ready for review",
	"done":      ":white_check_mark: done",
	"failed":    ":x: failed",
}

// message renders the DM for a task that just moved to status. The first line is
// "*KEY* ..." so thread replies can be routed back (see keyOf).
func message(t store.Task, status string, last string) string {
	s := fmt.Sprintf("*%s* %s — %s", t.Key, t.Title, headline[status])
	if last = strings.TrimSpace(last); last != "" {
		if len(last) > 2500 {
			last = last[:2500] + "…"
		}
		s += "\n>" + strings.ReplaceAll(last, "\n", "\n>")
	}
	for _, r := range t.Repos {
		if r.PRURL != nil {
			s += "\n:link: <" + *r.PRURL + "|" + r.RepoName + " PR>"
		}
	}
	if status == "needs_you" || status == "review" {
		s += "\n_Reply in this thread to comment, or `/conveyor move " + t.Key + " done`._"
	}
	return s
}

func (b *bridge) notify() {
	bot, user := b.setting("slack_bot_token"), b.setting("slack_user_id")
	if bot == "" || user == "" {
		return
	}
	cur, err := strconv.ParseInt(b.setting(cursorKey), 10, 64)
	if err != nil { // first run: start from now, never replay history
		b.svc.DB.QueryRow(`SELECT COALESCE(MAX(id),0) FROM events`).Scan(&cur)
		store.SetSetting(b.svc.DB, cursorKey, strconv.FormatInt(cur, 10))
		return
	}
	rows, err := b.svc.DB.Query(`SELECT id, task_id, detail FROM events WHERE kind='status_changed' AND id>? ORDER BY id`, cur)
	if err != nil {
		b.logf("slack: events: %v", err)
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
			if m := toStatus.FindStringSubmatch(detail); m != nil && headline[m[1]] != "" {
				e.to = m[1]
			}
			evs = append(evs, e)
		}
	}
	rows.Close()
	for _, e := range evs {
		if e.to != "" {
			t, err := store.GetTask(b.svc.DB, e.task)
			if err != nil {
				continue
			}
			var last string
			cs, _ := store.ListComments(b.svc.DB, t.ID)
			for i := len(cs) - 1; i >= 0; i-- {
				if cs[i].Author == store.AuthorAgent {
					last = cs[i].Body
					break
				}
			}
			if _, err := call(bot, "chat.postMessage", map[string]any{"channel": user, "text": message(t, e.to, last)}); err != nil {
				b.logf("slack: %v", err)
				return // retry this event next tick
			}
		}
		store.SetSetting(b.svc.DB, cursorKey, strconv.FormatInt(e.id, 10))
	}
}

// ---- inbound: Socket Mode ----

func (b *bridge) listen(ctx context.Context) {
	for ctx.Err() == nil {
		if app := b.setting("slack_app_token"); app != "" && b.setting("slack_bot_token") != "" && b.setting("slack_user_id") != "" {
			if err := b.session(ctx, app); err != nil && ctx.Err() == nil {
				b.logf("slack: socket: %v", err)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

type envelope struct {
	Type       string          `json:"type"`
	EnvelopeID string          `json:"envelope_id"`
	Payload    json.RawMessage `json:"payload"`
}

// session runs one socket connection until it drops.
func (b *bridge) session(ctx context.Context, app string) error {
	r, err := call(app, "apps.connections.open", struct{}{})
	if err != nil {
		return err
	}
	u, _ := r["url"].(string)
	c, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	go func() { <-ctx.Done(); c.Close() }()
	for {
		var e envelope
		if err := c.ReadJSON(&e); err != nil {
			return err
		}
		switch e.Type {
		case "disconnect":
			return nil // Slack asks for a fresh connection
		case "events_api":
			c.WriteJSON(map[string]any{"envelope_id": e.EnvelopeID})
			b.event(e.Payload)
		case "slash_commands":
			var p struct {
				UserID string `json:"user_id"`
				Text   string `json:"text"`
			}
			json.Unmarshal(e.Payload, &p)
			reply := "not allowed"
			if p.UserID == b.setting("slack_user_id") {
				reply = b.command(p.Text)
			}
			c.WriteJSON(map[string]any{"envelope_id": e.EnvelopeID, "payload": map[string]any{"text": reply}})
		}
	}
}

var keyRe = regexp.MustCompile(`^\*([A-Za-z0-9]+-[A-Za-z0-9]+)\*`)

// keyOf extracts the task key from a message made by message().
func keyOf(text string) string {
	if m := keyRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// event turns a thread reply to one of our DMs into a comment on that task.
func (b *bridge) event(payload []byte) {
	var p struct {
		Event struct {
			Type, Subtype, User, Text, Channel string
			BotID                              string `json:"bot_id"`
			ThreadTS                           string `json:"thread_ts"`
		} `json:"event"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	e := p.Event
	if e.Type != "message" || e.Subtype != "" || e.BotID != "" || e.ThreadTS == "" || e.User != b.setting("slack_user_id") {
		return
	}
	bot := b.setting("slack_bot_token")
	h, err := call(bot, "conversations.history", map[string]any{"channel": e.Channel, "latest": e.ThreadTS, "inclusive": true, "limit": 1})
	if err != nil {
		b.logf("slack: %v", err)
		return
	}
	msgs, _ := h["messages"].([]any)
	if len(msgs) == 0 {
		return
	}
	m, _ := msgs[0].(map[string]any)
	text, _ := m["text"].(string)
	key := keyOf(text)
	if key == "" {
		return
	}
	reply := b.comment(key, e.Text)
	call(bot, "chat.postMessage", map[string]any{"channel": e.Channel, "thread_ts": e.ThreadTS, "text": reply})
}

func (b *bridge) comment(key, body string) string {
	if _, err := b.svc.AddComment(key, store.AuthorHuman, strings.TrimSpace(body), core.FollowUp); err != nil {
		return ":warning: " + err.Error()
	}
	return ":speech_balloon: comment added to " + key
}

// command runs a /conveyor command: "" or "list", "move KEY STATUS", or "KEY text".
func (b *bridge) command(text string) string {
	f := strings.Fields(text)
	switch {
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
			s = append(s, fmt.Sprintf("*%s* %s — %s", t.Key, t.Title, t.Status))
		}
		return strings.Join(s, "\n")
	case f[0] == "move" && len(f) == 3:
		t, err := store.GetTaskByKey(b.svc.DB, f[1])
		if err != nil {
			return err.Error()
		}
		if _, err := store.UpdateTaskStatus(b.svc.DB, t.ID, store.Status(f[2]), store.ActorHuman); err != nil {
			return ":warning: " + err.Error()
		}
		return fmt.Sprintf("%s → %s", f[1], f[2])
	case len(f) >= 2:
		return b.comment(f[0], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), f[0])))
	}
	return "Usage: `/conveyor` · `/conveyor KEY comment…` · `/conveyor move KEY todo|done|cancelled`"
}
