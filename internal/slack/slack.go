// Package slack is the Slack chat.Transport: it DMs task Msgs (with PR links) and delivers
// thread replies and /conveyor commands back, so the Slack mobile app is Conveyor's mobile
// client. It uses Socket Mode, so no public URL is needed.
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
	"strings"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/chat"
	"github.com/gorilla/websocket"
)

const cursorKey = "slack_cursor"

// api is the Slack Web API base (swapped in tests).
var api = "https://slack.com/api/"

// Slack is the transport; get reads settings.
type Slack struct{ get func(string) string }

// New is a chat.Factory.
func New(get func(string) string) chat.Transport { return &Slack{get} }

func (s *Slack) Ready() bool {
	return s.get("slack_bot_token") != "" && s.get("slack_app_token") != "" && s.get("slack_user_id") != ""
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

func (s *Slack) Send(m chat.Msg) error {
	_, err := call(s.get("slack_bot_token"), "chat.postMessage", map[string]any{"channel": s.get("slack_user_id"), "text": message(m)})
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
			Type, Subtype, User, Text, Channel string
			BotID                              string `json:"bot_id"`
			ThreadTS                           string `json:"thread_ts"`
		} `json:"event"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	e := p.Event
	if e.Type != "message" || e.Subtype != "" || e.BotID != "" || e.ThreadTS == "" || e.User != s.get("slack_user_id") {
		return
	}
	bot := s.get("slack_bot_token")
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
	reply := ":speech_balloon: " + handle(chat.In{Key: key, Text: e.Text})
	call(bot, "chat.postMessage", map[string]any{"channel": e.Channel, "thread_ts": e.ThreadTS, "text": reply})
}
