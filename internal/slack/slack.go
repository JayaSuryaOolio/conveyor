// Package slack is the Slack chat.Transport: it DMs task Msgs (with PR links) and delivers
// thread replies and /conveyor commands back, so the Slack mobile app is Conveyor's mobile
// client. It uses Socket Mode, so no public URL is needed.
//
// Settings: slack_bot_token (xoxb-), slack_user_token (xoxp-, optional: when set, every post and
// API call is made as you instead of the bot), slack_app_token (xapp-, scope connections:write;
// or a rotating xoxe.xapp- token plus slack_app_refresh_token, slack_client_id and slack_client_secret,
// which Conveyor refreshes itself) and
// slack_user_id (your member id: the only user the bridge talks to or listens to).
// Buttons need Interactivity enabled in the app (Socket Mode delivers the clicks).
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/chat"
	"github.com/gorilla/websocket"
)

// api is the Slack Web API base (swapped in tests).
var api = "https://slack.com/api/"

// mine holds the ts of messages we posted. With a user token our own posts come back as
// message events from slack_user_id, so they must not be handled as commands.
var mine sync.Map

// Thread <-> task links live in settings (no schema change; settings are never listed):
// slack_thread:CHANNEL:ROOT_TS -> KEY routes a reply to its task, slack_task:KEY -> CHANNEL:ROOT_TS
// keeps a task's notifications in one thread. Message text is only parsed (keyOf) for threads
// that predate this.
// ponytail: rows are never pruned (a few bytes per task); delete old ones if that matters.

// link records that the thread rooted at ts in channel belongs to key.
func (s *Slack) link(channel, ts, key string) {
	if s.set == nil || channel == "" || ts == "" || key == "" {
		return
	}
	s.set("slack_thread:"+channel+":"+ts, key)
	if s.get("slack_task:"+key) == "" {
		s.set("slack_task:"+key, channel+":"+ts)
	}
}

// taskOf is the task linked to a thread, or "".
func (s *Slack) taskOf(channel, ts string) string { return s.get("slack_thread:" + channel + ":" + ts) }

// rootOf is the thread root ts linked to a task in channel, or "".
func (s *Slack) rootOf(channel, key string) string {
	if ch, ts, ok := strings.Cut(s.get("slack_task:"+key), ":"); ok && ch == channel {
		return ts
	}
	return ""
}

// Slack is the transport; get reads settings.
type Slack struct {
	get func(string) string
	set func(k, v string) error
}

// New is a chat.Factory.
func New(get func(string) string, set func(k, v string) error) chat.Transport {
	return &Slack{get, set}
}

// token is the user token when set, else the bot token.
func (s *Slack) token() string {
	if t := s.get("slack_user_token"); t != "" {
		return t
	}
	return s.get("slack_bot_token")
}

// openConn calls apps.connections.open. A rotating app token expires after 12h: on token_expired
// it is refreshed with slack_app_refresh_token (both new tokens are saved) and the call retried once.
// App-level tokens refresh through oauth.v2.access with the app's client credentials;
// tooling.tokens.rotate only takes configuration-token refresh tokens (invalid_refresh_token).
func (s *Slack) openConn() (map[string]any, error) {
	r, err := call(strings.TrimSpace(s.get("slack_app_token")), "apps.connections.open", struct{}{})
	if err == nil || !strings.HasSuffix(err.Error(), "token_expired") || s.get("slack_app_refresh_token") == "" {
		return r, err
	}
	log.Printf("chat: slack app token expired, refreshing")
	id, secret := strings.TrimSpace(s.get("slack_client_id")), strings.TrimSpace(s.get("slack_client_secret"))
	if id == "" || secret == "" {
		return nil, fmt.Errorf("slack app token expired: set slack_client_id and slack_client_secret to refresh it")
	}
	resp, err := http.PostForm(api+"oauth.v2.access", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {strings.TrimSpace(s.get("slack_app_refresh_token"))},
		"client_id":     {id},
		"client_secret": {secret},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		OK           bool   `json:"ok"`
		Error        string `json:"error"`
		Token        string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK || out.Token == "" || out.RefreshToken == "" {
		return nil, fmt.Errorf("slack oauth.v2.access: %v", out.Error)
	}
	// the old refresh token is dead once used, so save both before using the new token
	if err := s.set("slack_app_refresh_token", out.RefreshToken); err != nil {
		return nil, err
	}
	if err := s.set("slack_app_token", out.Token); err != nil {
		return nil, err
	}
	log.Printf("chat: slack app token refreshed")
	return call(out.Token, "apps.connections.open", struct{}{})
}

// post sends chat.postMessage and remembers its ts (see mine).
func (s *Slack) post(args map[string]any) (string, error) {
	r, err := call(s.token(), "chat.postMessage", args)
	ts, _ := r["ts"].(string)
	if err == nil && ts != "" {
		mine.Store(ts, true)
	}
	if err != nil {
		log.Printf("chat: slack post: %v", err)
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
	for i, r := range m.PRs {
		// action_ids must be unique within a message or Slack answers invalid_blocks
		els = append(els, map[string]any{"type": "button", "text": map[string]any{"type": "plain_text", "text": "Open " + r.Name + " PR"}, "action_id": fmt.Sprintf("open_pr_%d", i), "url": r.URL})
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
	root := s.rootOf(s.get("slack_user_id"), m.Key)
	threaded := root != ""
	if threaded {
		args["thread_ts"] = root
	}
	ts, err := s.post(args)
	if err != nil && strings.HasSuffix(err.Error(), "invalid_blocks") {
		delete(args, "blocks") // plain text still delivers the message
		ts, err = s.post(args)
	}
	if err == nil && !threaded {
		s.link(s.get("slack_user_id"), ts, m.Key)
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
	r, err := s.openConn()
	if err != nil {
		return err
	}
	u, _ := r["url"].(string)
	c, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	log.Printf("chat: slack connected")
	// the Home tab keeps its content, so publishing the guide once per connection is enough
	call(s.token(), "views.publish", map[string]any{"user_id": s.get("slack_user_id"), "view": map[string]any{"type": "home", "blocks": home}})
	go func() { <-ctx.Done(); c.Close() }()
	// Handling an event does Web API calls and downloads, so it runs on one worker (in order)
	// instead of the read loop: Slack redelivers an envelope not acked within 3s and drops a
	// connection that keeps missing acks.
	work := make(chan func(), 64)
	defer close(work)
	go func() {
		for f := range work {
			f()
		}
	}()
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
			work <- func() { s.event(e.Payload, h) }
		case "interactive": // button clicks
			c.WriteJSON(map[string]any{"envelope_id": e.EnvelopeID})
			work <- func() { s.interactive(e.Payload, h) }
		case "slash_commands":
			var p struct {
				UserID      string `json:"user_id"`
				Text        string `json:"text"`
				ChannelID   string `json:"channel_id"`
				ResponseURL string `json:"response_url"`
			}
			json.Unmarshal(e.Payload, &p)
			if p.UserID != s.get("slack_user_id") {
				c.WriteJSON(map[string]any{"envelope_id": e.EnvelopeID, "payload": map[string]any{"text": "not allowed"}})
				continue
			}
			// ack now and answer via response_url: "new" may download images past Slack's 3s ack limit
			c.WriteJSON(map[string]any{"envelope_id": e.EnvelopeID})
			go func() {
				in := chat.In{Text: p.Text, Fwd: func() (string, []chat.File) { return s.lookBack(p.ChannelID, "") }}
				body, _ := json.Marshal(map[string]any{"text": h(in)})
				if r, err := http.Post(p.ResponseURL, "application/json", strings.NewReader(string(body))); err == nil {
					r.Body.Close()
				}
			}()
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
	var raw struct{ Event map[string]any }
	if json.Unmarshal(payload, &p) != nil || json.Unmarshal(payload, &raw) != nil {
		return
	}
	e := p.Event
	bot := s.token()
	if e.User != s.get("slack_user_id") {
		return
	}
	// thread_broadcast is a thread reply also sent to the conversation: still a reply
	if e.Type != "message" || (e.Subtype != "" && e.Subtype != "file_share" && e.Subtype != "thread_broadcast") || e.BotID != "" {
		return
	}
	if _, ours := mine.Load(e.TS); ours {
		return
	}
	// Slack redelivers an event it thinks went unacked; a second copy must not comment twice.
	// ponytail: grows by one short string per message; fine for one user's DMs.
	if _, dup := seen.LoadOrStore(e.Channel+"/"+e.TS, true); dup {
		return
	}
	// acknowledge at once; needs the reactions:write scope, best effort
	call(bot, "reactions.add", map[string]any{"channel": e.Channel, "timestamp": e.TS, "name": "eyes"})
	// reply answers in thread (the command's own thread for a top-level DM) and keeps a created
	// task's later notifications in that thread too
	reply := func(thread, text string) {
		ts, _ := s.post(map[string]any{"channel": e.Channel, "thread_ts": thread, "text": text})
		if m := createdRe.FindStringSubmatch(text); m != nil { // our own answer to "new"
			s.link(e.Channel, thread, m[1])
			s.link(e.Channel, ts, m[1]) // and the confirmation itself, should it be replied to as a root
		}
	}
	if e.ThreadTS == "" { // a plain DM is a command, so /conveyor is optional
		quote, files := content(raw.Event, false)
		if strings.TrimSpace(e.Text) == "" && (quote != "" || len(files) > 0) { // a bare forward: keep it for "new"
			reply(e.TS, "Got it. Reply `new [REPO] your comment` here (or send `/conveyor new …`) to make a task from it.")
			return
		}
		fwd := func() (string, []chat.File) { // forwarded with the comment, else the message just before
			if quote == "" && len(files) == 0 {
				return s.lookBack(e.Channel, e.TS)
			}
			return s.fetch(quote, files)
		}
		reply(e.TS, handle(chat.In{Text: e.Text, Fwd: fwd}))
		return
	}
	if key := s.taskOf(e.Channel, e.ThreadTS); key != "" { // the usual path: no API call, no parsing
		reply(e.ThreadTS, commented(handle(chat.In{Key: key, Text: e.Text})))
		return
	}
	// unlinked thread: a forward to run "new" under, or one from before links were stored
	h, err := call(bot, "conversations.replies", map[string]any{"channel": e.Channel, "ts": e.ThreadTS, "limit": 200})
	if err != nil {
		log.Printf("chat: slack thread %s: %v", e.ThreadTS, err)
		return
	}
	msgs, _ := h["messages"].([]any)
	if len(msgs) == 0 {
		return
	}
	root, _ := msgs[0].(map[string]any)
	rootText, _ := root["text"].(string)
	key := keyOf(rootText)
	if f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(e.Text), "/conveyor")); key == "" && len(f) > 0 && strings.EqualFold(f[0], "new") {
		// "new ..." under a non-task message (e.g. a forward) makes a task from that message
		quote, files := content(root, false)
		fwd := func() (string, []chat.File) { return s.fetch(quote, files) }
		reply(e.ThreadTS, handle(chat.In{Text: e.Text, Fwd: fwd}))
		return
	}
	if key == "" { // a thread where "new" made a task: the newest "created KEY" names it
		for _, m := range msgs {
			m, _ := m.(map[string]any)
			t, _ := m["text"].(string)
			if c := createdRe.FindStringSubmatch(t); c != nil {
				key = c[1]
			}
		}
	}
	if key == "" { // not a thread we can route: say so rather than leave only the :eyes:
		reply(e.ThreadTS, "I can't tell which task this thread is about. Start the message with the task key (`CV-12 your comment`) or send a command like `list`.")
		return
	}
	out := handle(chat.In{Key: key, Text: e.Text})
	if !strings.HasPrefix(out, chat.Closed) {
		s.link(e.Channel, e.ThreadTS, key) // the agent's answer comes back into this thread
	}
	reply(e.ThreadTS, commented(out))
}

// commented marks a successful comment; a closed-task error is shown as is.
func commented(out string) string {
	if strings.HasPrefix(out, chat.Closed) {
		return out
	}
	return ":speech_balloon: " + out
}

// createdRe reads the task key from the "created KEY ..." reply to "new".
var createdRe = regexp.MustCompile(`^created ([A-Za-z0-9]+-[A-Za-z0-9]+) `)

// seen holds channel/ts of handled messages (see event).
var seen sync.Map

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

// ---- forwarded messages for "new" ----

// content reads a message's forwarded/shared attachments as a Markdown quote and collects
// its files (the message's own and the shared ones). withText also quotes the message's
// own text, which for the command message itself is the command, not content.
func content(m map[string]any, withText bool) (string, []map[string]any) {
	var parts []string
	var files []map[string]any
	add := func(fs any) {
		l, _ := fs.([]any)
		for _, f := range l {
			if f, ok := f.(map[string]any); ok {
				files = append(files, f)
			}
		}
	}
	quote := func(t string) string { return "> " + strings.ReplaceAll(strings.TrimSpace(t), "\n", "\n> ") }
	if t, _ := m["text"].(string); withText && strings.TrimSpace(t) != "" {
		parts = append(parts, quote(t))
	}
	add(m["files"])
	atts, _ := m["attachments"].([]any)
	for _, a := range atts {
		a, _ := a.(map[string]any)
		t, _ := a["text"].(string)
		if t == "" {
			t, _ = a["fallback"].(string)
		}
		if strings.TrimSpace(t) != "" {
			q := quote(t)
			author, _ := a["author_name"].(string)
			link, _ := a["from_url"].(string)
			if by := strings.TrimSpace(author + " " + link); by != "" {
				q += "\n\n— " + by
			}
			parts = append(parts, q)
		}
		add(a["files"])
	}
	return strings.Join(parts, "\n\n"), files
}

// lookBack finds the message just before the command in channel (before ts, or now when
// ts is empty), skipping our own posts, and returns it when it carries any content.
func (s *Slack) lookBack(channel, ts string) (string, []chat.File) {
	args := map[string]any{"channel": channel, "limit": 10}
	if ts != "" {
		args["latest"] = ts // exclusive by default
	}
	h, err := call(s.token(), "conversations.history", args)
	if err != nil {
		return "(could not read the forwarded message: " + err.Error() + ")", nil
	}
	msgs, _ := h["messages"].([]any)
	for _, m := range msgs { // newest first
		m, _ := m.(map[string]any)
		mts, _ := m["ts"].(string)
		if _, ours := mine.Load(mts); ours || m["bot_id"] != nil {
			continue
		}
		if q, fs := content(m, false); q == "" && len(fs) == 0 {
			return "", nil // the message before is not a forward
		}
		return s.fetch(content(m, true))
	}
	return "", nil
}

var imgExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}

// fetch downloads the image files (needs the files:read scope); other files and failed
// downloads are listed in the quote instead.
func (s *Slack) fetch(quote string, files []map[string]any) (string, []chat.File) {
	var out []chat.File
	for _, f := range files {
		name, _ := f["name"].(string)
		mime, _ := f["mimetype"].(string)
		link, _ := f["permalink"].(string)
		u, _ := f["url_private_download"].(string)
		if u == "" {
			u, _ = f["url_private"].(string)
		}
		ext, img := imgExt[mime]
		var data []byte
		var err error
		if img {
			data, err = s.download(u)
		}
		if img && err == nil {
			if !strings.HasSuffix(strings.ToLower(name), ext) {
				name += ext
			}
			out = append(out, chat.File{Name: name, Data: data})
			continue
		}
		note := "\n\n(file " + name + " " + link
		if err != nil {
			note += ", not downloaded: " + err.Error()
		}
		quote += note + ")"
	}
	return quote, out
}

func (s *Slack) download(u string) ([]byte, error) {
	if u == "" {
		return nil, fmt.Errorf("no url")
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token())
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// without files:read Slack answers 200 with its HTML login page
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return nil, fmt.Errorf("HTTP %d %s (does the app have files:read?)", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20+1))
}
