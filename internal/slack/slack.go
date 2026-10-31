// Package slack is the Slack chat.Transport: it DMs task Msgs (with PR links) and delivers
// thread replies and /conveyor commands back, so the Slack mobile app is Conveyor's mobile
// client. It uses Socket Mode, so no public URL is needed.
//
// Settings: slack_bot_token (xoxb-) or slack_user_token (xoxp-, used only while
// slack_impersonate is on: the app then acts as you instead of as a bot), slack_app_token (xapp-, scope connections:write) and
// slack_user_id (your member id: the only user the bridge talks to or listens to).
// Buttons need Interactivity enabled in the app (Socket Mode delivers the clicks).
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/chat"
	"github.com/gorilla/websocket"
)

const cursorKey = "slack_cursor"

// api is the Slack Web API base (swapped in tests).
var api = "https://slack.com/api/"

// mine holds the ts of messages we posted. With a user token our own posts come back as
// message events from slack_user_id, so they must not be handled as commands.
var mine sync.Map

// threads maps a task key to the ts of its thread root, so later notifications and replies
// for that task stay in one thread.
// ponytail: in memory, so a restart starts a new thread per task; persist if that matters.
var threads sync.Map

// Slack is the transport; get reads settings.
type Slack struct{ get func(string) string }

// New is a chat.Factory.
func New(get func(string) string) chat.Transport { return &Slack{get} }

// token is the user token when impersonating, else the bot token.
func (s *Slack) token() string {
	if t := s.get("slack_user_token"); t != "" && s.get("slack_impersonate") == "on" {
		return t
	}
	return s.get("slack_bot_token")
}

// post sends chat.postMessage and remembers its ts (see mine).
func (s *Slack) post(args map[string]any) (string, error) {
	r, err := call(s.token(), "chat.postMessage", args)
	ts, _ := r["ts"].(string)
	if err == nil && ts != "" {
		mine.Store(ts, true)
	}
	return ts, err
}

func (s *Slack) Ready() bool {
	return s.token() != "" && s.get("slack_app_token") != "" && s.get("slack_user_id") != ""
}

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

// ---- outbound: Msg -> DM ----

var headline = map[string]string{
	"needs_you": ":raising_hand: needs you",
	"review":    ":mag: ready for review",
	"done":      ":white_check_mark: done",
	"failed":    ":x: failed",
}

// message renders the DM for a task that just moved to status. The first line is
// "*KEY* ..." so thread replies can be routed back (see keyOf).
func message(m chat.Msg) string {
	t, status, last := m, m.Status, m.Last
	s := fmt.Sprintf("*%s* %s — %s", t.Key, t.Title, headline[status])
	if last = strings.TrimSpace(last); last != "" {
		if len(last) > 2500 {
			last = last[:2500] + "…"
		}
		s += "\n>" + strings.ReplaceAll(last, "\n", "\n>")
	}
	for _, r := range t.PRs {
		s += "\n:link: <" + r.URL + "|" + r.Name + " PR>"
	}
	if status == "needs_you" || status == "review" {
		s += "\n_Reply in this thread to comment, or `/conveyor move " + t.Key + " done`._"
	}
	return s
}

// buttons are the action buttons under a DM: Done and Requeue while a task waits on you
// (action_id is the command, value the key), and one link button per PR.
func buttons(m chat.Msg) []any {
	var els []any
	btn := func(text, id string) any {
		return map[string]any{"type": "button", "text": map[string]any{"type": "plain_text", "text": text}, "action_id": id, "value": m.Key}
	}
	if m.Status == "needs_you" || m.Status == "review" || m.Status == "failed" {
		els = append(els, btn("Done", "move_done"), btn("Requeue", "move_todo"))
	}
	for _, r := range m.PRs {
		els = append(els, map[string]any{"type": "button", "text": map[string]any{"type": "plain_text", "text": "Open " + r.Name + " PR"}, "action_id": "open_pr", "url": r.URL})
	}
	return els
}

// blocks renders m for display: Slack mrkdwn for the header and links, and a markdown block
// for the agent's text, which is standard Markdown that mrkdwn would show raw.
func blocks(m chat.Msg) []any {
	sec := func(t string) any {
		return map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": t}}
	}
	b := []any{sec(fmt.Sprintf("*%s* %s — %s", m.Key, m.Title, headline[m.Status]))}
	if last := strings.TrimSpace(m.Last); last != "" {
		if len(last) > 11000 { // markdown blocks cap at 12000 chars
			last = last[:11000] + "…"
		}
		b = append(b, map[string]any{"type": "markdown", "text": last})
	}
	var foot []string
	for _, r := range m.PRs {
		foot = append(foot, ":link: <"+r.URL+"|"+r.Name+" PR>")
	}
	if m.Status == "needs_you" || m.Status == "review" {
		foot = append(foot, "_Reply in this thread to comment, or send `move "+m.Key+" done`._")
	}
	if len(foot) > 0 {
		b = append(b, sec(strings.Join(foot, "\n")))
	}
	if els := buttons(m); len(els) > 0 {
		b = append(b, map[string]any{"type": "actions", "elements": els})
	}
	return b
}

// Send posts m into the task's thread, or starts one. text (message) is the notification
// and fallback; keyOf reads it back from the thread root.
func (s *Slack) Send(m chat.Msg) error {
	args := map[string]any{"channel": s.get("slack_user_id"), "text": message(m), "blocks": blocks(m)}
	root, threaded := threads.Load(m.Key)
	if threaded {
		args["thread_ts"] = root
	}
	ts, err := s.post(args)
	if err == nil && !threaded {
		threads.Store(m.Key, ts)
	}
	return err
}

// ---- inbound: Socket Mode ----

type envelope struct {
	Type       string          `json:"type"`
	EnvelopeID string          `json:"envelope_id"`
	Payload    json.RawMessage `json:"payload"`
}

// session runs one socket connection until it drops.
func (s *Slack) Listen(ctx context.Context, h func(chat.In) string) error {
	r, err := call(s.get("slack_app_token"), "apps.connections.open", struct{}{})
	if err != nil {
		return err
	}
	u, _ := r["url"].(string)
	c, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	// the Home tab keeps its content, so publishing the guide once per connection is enough
	call(s.token(), "views.publish", map[string]any{"user_id": s.get("slack_user_id"), "view": map[string]any{"type": "home", "blocks": home}})
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
			s.event(e.Payload, h)
		case "interactive": // button clicks
			c.WriteJSON(map[string]any{"envelope_id": e.EnvelopeID})
			s.interactive(e.Payload, h)
		case "slash_commands":
			var p struct {
				UserID string `json:"user_id"`
				Text   string `json:"text"`
			}
			json.Unmarshal(e.Payload, &p)
			reply := "not allowed"
			if p.UserID == s.get("slack_user_id") {
				reply = h(chat.In{Text: p.Text})
			}
			c.WriteJSON(map[string]any{"envelope_id": e.EnvelopeID, "payload": map[string]any{"text": reply}})
		}
	}
}

// home is the App Home tab: Block Kit, the only markup Slack allows there.
var home = []any{
	map[string]any{"type": "header", "text": map[string]any{"type": "plain_text", "text": ":rocket: Conveyor"}},
	map[string]any{"type": "context", "elements": []any{map[string]any{"type": "mrkdwn", "text": "Your Conveyor tasks, from anywhere. Open the *Messages* tab to talk to it."}}},
	map[string]any{"type": "divider"},
	map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": chat.Help}},
	map[string]any{"type": "divider"},
	map[string]any{"type": "context", "elements": []any{map[string]any{"type": "mrkdwn", "text": ":bell: Task updates arrive as DMs, one thread per task. Conveyor must be running to answer."}}},
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
func (s *Slack) event(payload []byte, handle func(chat.In) string) {
	var p struct {
		Event struct {
			Type, Subtype, User, Text, Channel, TS string
			BotID                                  string `json:"bot_id"`
			ThreadTS                               string `json:"thread_ts"`
		} `json:"event"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	e := p.Event
	bot := s.token()
	if e.User != s.get("slack_user_id") {
		return
	}
	if e.Type != "message" || e.Subtype != "" || e.BotID != "" {
		return
	}
	if _, ours := mine.Load(e.TS); ours {
		return
	}
	// acknowledge at once; needs the reactions:write scope, best effort
	call(bot, "reactions.add", map[string]any{"channel": e.Channel, "timestamp": e.TS, "name": "eyes"})
	if e.ThreadTS == "" { // a plain DM is a command, so /conveyor is optional
		s.post(map[string]any{"channel": e.Channel, "text": handle(chat.In{Text: e.Text})})
		return
	}
	h, err := call(bot, "conversations.history", map[string]any{"channel": e.Channel, "latest": e.ThreadTS, "inclusive": true, "limit": 1})
	if err != nil {
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
	threads.LoadOrStore(key, e.ThreadTS) // the agent's answer comes back into this thread
	reply := ":speech_balloon: " + handle(chat.In{Key: key, Text: e.Text})
	s.post(map[string]any{"channel": e.Channel, "thread_ts": e.ThreadTS, "text": reply})
}

// interactive runs a button click (block_actions) as the matching command and answers in the
// message's thread. Link buttons (open_pr) also arrive here and are ignored.
func (s *Slack) interactive(payload []byte, handle func(chat.In) string) {
	var p struct {
		Type    string `json:"type"`
		User    struct{ ID string }
		Channel struct{ ID string }
		Message struct{ TS string }
		Actions []struct {
			ActionID string `json:"action_id"`
			Value    string
		}
	}
	if json.Unmarshal(payload, &p) != nil || p.Type != "block_actions" || p.User.ID != s.get("slack_user_id") {
		return
	}
	for _, a := range p.Actions {
		if status, ok := strings.CutPrefix(a.ActionID, "move_"); ok && a.Value != "" {
			s.post(map[string]any{"channel": p.Channel.ID, "thread_ts": p.Message.TS, "text": handle(chat.In{Text: "move " + a.Value + " " + status})})
		}
	}
}
