package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JayaSuryaOolio/conveyor/internal/chat"
	"github.com/gorilla/websocket"
)

func TestMessageKeyRoundTrip(t *testing.T) {
	pr := "https://github.com/o/r/pull/9"
	m := message(chat.Msg{Key: "CV-7", Title: "Fix it", Status: "needs_you", Last: "which index?\nbtree?", PRs: []chat.Link{{Name: "r", URL: pr}}})
	if keyOf(m) != "CV-7" || !strings.Contains(m, "<"+pr+"|r PR>") || !strings.Contains(m, ">btree?") {
		t.Fatalf("bad message:\n%s", m)
	}
	if keyOf("hello") != "" {
		t.Fatal("key from non-task text")
	}
}

func TestUserTokenAndOwnPostsIgnored(t *testing.T) {
	cfg := map[string]string{"slack_bot_token": "xoxb", "slack_user_token": "xoxp", "slack_app_token": "xapp", "slack_user_id": "U1"}
	s := &Slack{get: func(k string) string { return cfg[k] }}
	if s.token() != "xoxp" || !s.Ready() {
		t.Fatal("user token should win and satisfy Ready")
	}
	delete(cfg, "slack_user_token")
	delete(cfg, "slack_bot_token")
	if s.Ready() {
		t.Fatal("no token must not be ready")
	}
	mine.Store("1.1", true)
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"x","channel":"D1","ts":"1.1"}}`), func(chat.In) string {
		t.Fatal("own post handled as a command")
		return ""
	})
}

func TestButtonsAndInteractive(t *testing.T) {
	if n := len(buttons(chat.Msg{Key: "CV-7", Status: "review", PRs: []chat.Link{{Name: "r", URL: "u"}}})); n != 3 {
		t.Fatalf("review wants Done, Requeue, PR; got %d", n)
	}
	if n := len(buttons(chat.Msg{Key: "CV-7", Status: "done"})); n != 0 {
		t.Fatalf("done without PR wants no buttons; got %d", n)
	}
	s := &Slack{get: func(k string) string { return map[string]string{"slack_user_id": "U1"}[k] }}
	got := ""
	h := func(in chat.In) string { got = in.Text; return "ok" }
	s.interactive([]byte(`{"type":"block_actions","user":{"id":"U2"},"actions":[{"action_id":"move_done","value":"CV-7"}]}`), h)
	if got != "" {
		t.Fatal("click from another user obeyed")
	}
	api = "http://127.0.0.1:1/" // post fails fast; only the handled command matters
	s.interactive([]byte(`{"type":"block_actions","user":{"id":"U1"},"actions":[{"action_id":"move_done","value":"CV-7"}]}`), h)
	if got != "move CV-7 done" {
		t.Fatalf("got %q", got)
	}
}

func TestSendThreadsPerTask(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a map[string]any
		json.NewDecoder(r.Body).Decode(&a)
		got = append(got, a)
		fmt.Fprintf(w, `{"ok":true,"ts":"%d.0"}`, len(got))
	}))
	defer srv.Close()
	api = srv.URL + "/"
	cfg := map[string]string{"slack_bot_token": "x", "slack_user_id": "U1"}
	s := &Slack{get: func(k string) string { return cfg[k] }, set: func(k, v string) error { cfg[k] = v; return nil }}
	m := chat.Msg{Key: "CV-9", Title: "T", Status: "needs_you", Last: "**bold**"}
	s.Send(m)
	s.Send(m)
	if got[0]["thread_ts"] != nil || got[1]["thread_ts"] != "1.0" {
		t.Fatalf("second send not threaded under first: %v", got)
	}
	if b := got[0]["blocks"].([]any)[1].(map[string]any); b["type"] != "markdown" || b["text"] != "**bold**" {
		t.Fatalf("agent text not in a markdown block: %v", b)
	}
}

// TestListenRoutesInteractive drives the real Socket Mode loop against a fake Slack: a button
// click must be acked and run as a command (it was once silently dropped).
func TestListenRoutesInteractive(t *testing.T) {
	acked := make(chan string, 1)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ok":true,"url":"ws%s/ws"}`, strings.TrimPrefix(srv.URL, "http"))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, _ := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "interactive", "envelope_id": "E1", "payload": map[string]any{
			"type": "block_actions", "user": map[string]any{"id": "U1"}, "channel": map[string]any{"id": "D1"},
			"actions": []any{map[string]any{"action_id": "move_todo", "value": "CV-7"}}}})
		var ack map[string]any
		c.ReadJSON(&ack)
		acked <- fmt.Sprint(ack["envelope_id"])
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"ok":true,"ts":"1.0"}`) })
	api = srv.URL + "/"
	s := &Slack{get: func(k string) string {
		return map[string]string{"slack_app_token": "a", "slack_bot_token": "x", "slack_user_id": "U1"}[k]
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := make(chan string, 1)
	go s.Listen(ctx, func(in chat.In) string { cmd <- in.Text; return "ok" })
	got := map[string]bool{}
	for range 2 { // the ack and the command, in either order
		select {
		case v := <-acked:
			got[v] = true
		case v := <-cmd:
			got[v] = true
		case <-time.After(3 * time.Second):
			t.Fatalf("interactive envelope not routed, got %v", got)
		}
	}
	if !got["E1"] || !got["move CV-7 todo"] {
		t.Fatalf("want ack E1 and command 'move CV-7 todo', got %v", got)
	}
}

func TestOpenConnRotatesExpiredToken(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer new" {
			fmt.Fprint(w, `{"ok":true,"url":"wss://x"}`)
			return
		}
		fmt.Fprint(w, `{"ok":false,"error":"token_expired"}`)
	})
	mux.HandleFunc("/oauth.v2.access", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if f := r.Form; f.Get("grant_type") != "refresh_token" || f.Get("refresh_token") != "old-r" || f.Get("client_id") != "cid" || f.Get("client_secret") != "cs" {
			t.Errorf("form = %v", f)
		}
		fmt.Fprint(w, `{"ok":true,"access_token":"new","token_type":"app_level","refresh_token":"new-r"}`)
	})
	api = srv.URL + "/"
	cfg := map[string]string{"slack_app_token": "old", "slack_app_refresh_token": "old-r", "slack_client_id": "cid", "slack_client_secret": "cs"}
	s := &Slack{func(k string) string { return cfg[k] }, func(k, v string) error { cfg[k] = v; return nil }}
	r, err := s.openConn()
	if err != nil || r["url"] != "wss://x" || cfg["slack_app_token"] != "new" || cfg["slack_app_refresh_token"] != "new-r" {
		t.Fatalf("r=%v err=%v cfg=%v", r, err, cfg)
	}
}

func TestLookBackForward(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/img" {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("PNG"))
			return
		}
		fmt.Fprintf(w, `{"ok":true,"messages":[{"ts":"9.0","bot_id":"B1","text":"ours"},
			{"ts":"8.0","text":"look","attachments":[{"is_share":true,"text":"it crashes","author_name":"Bob",
			"files":[{"name":"shot","mimetype":"image/png","url_private_download":"%s/img"}]}]}]}`, "http://"+r.Host)
	}))
	defer srv.Close()
	api = srv.URL + "/"
	s := &Slack{get: func(k string) string { return map[string]string{"slack_bot_token": "x"}[k] }}
	q, fs := s.lookBack("D1", "")
	if !strings.Contains(q, "> look") || !strings.Contains(q, "> it crashes\n\n— Bob") || len(fs) != 1 || fs[0].Name != "shot.png" || string(fs[0].Data) != "PNG" {
		t.Fatalf("q=%q files=%+v", q, fs)
	}
}

// A reply in the thread of a forward (no task key in the root) is a command about the forward.
func TestThreadReplyUnderForward(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "conversations.replies") {
			fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"5.0","text":"/conveyor new x","attachments":[{"is_share":true,"text":"it crashes"}]}]}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"ts":"7.0"}`)
	}))
	defer srv.Close()
	api = srv.URL + "/"
	s := &Slack{get: func(k string) string { return map[string]string{"slack_bot_token": "x", "slack_user_id": "U1"}[k] }}
	var got chat.In
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"new B do it","channel":"D1","ts":"6.0","thread_ts":"5.0"}}`), func(in chat.In) string {
		got = in
		return "ok"
	})
	if got.Text != "new B do it" || got.Key != "" || got.Fwd == nil {
		t.Fatalf("not handled as a command: %+v", got)
	}
	if q, _ := got.Fwd(); q != "> it crashes" {
		t.Fatalf("fwd %q", q)
	}
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"thanks","channel":"D1","ts":"6.1","thread_ts":"5.0"}}`), func(chat.In) string {
		t.Fatal("plain chatter in a thread must not run a command")
		return ""
	})
}

// CV-105: a reply to the "created KEY" confirmation (threaded under the user's "new" DM) comments
// on KEY, even after a restart; a redelivered event is handled once; a top-level command is
// answered in its own thread.
func TestReplyToCreatedConfirmation(t *testing.T) {
	var posts []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var a map[string]any
		json.NewDecoder(r.Body).Decode(&a)
		switch {
		case strings.HasSuffix(r.URL.Path, "conversations.replies"):
			fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"20.0","text":"new fix login"},{"ts":"20.1","text":"created CV-104 fix login (backlog; move CV-104 todo to start it)"}]}`)
		case strings.HasSuffix(r.URL.Path, "chat.postMessage"):
			posts = append(posts, a)
			fmt.Fprint(w, `{"ok":true,"ts":"99.0"}`)
		default:
			fmt.Fprint(w, `{"ok":true}`)
		}
	}))
	defer srv.Close()
	api = srv.URL + "/"
	s := &Slack{get: func(k string) string { return map[string]string{"slack_bot_token": "x", "slack_user_id": "U1"}[k] }}
	var got []chat.In
	h := func(in chat.In) string { got = append(got, in); return "comment added to CV-104" }
	ev := []byte(`{"event":{"type":"message","user":"U1","text":"use the v2 API","channel":"D1","ts":"21.0","thread_ts":"20.0"}}`)
	s.event(ev, h)
	s.event(ev, h) // redelivered
	if len(got) != 1 || got[0].Key != "CV-104" || got[0].Text != "use the v2 API" {
		t.Fatalf("want one comment on CV-104, got %+v", got)
	}
	s.event([]byte(`{"event":{"type":"message","subtype":"thread_broadcast","user":"U1","text":"also this","channel":"D1","ts":"21.1","thread_ts":"20.0"}}`), h)
	if len(got) != 2 || got[1].Key != "CV-104" {
		t.Fatalf("thread_broadcast reply not a comment: %+v", got)
	}
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"list","channel":"D1","ts":"22.0"}}`), h)
	if p := posts[len(posts)-1]; p["thread_ts"] != "22.0" {
		t.Fatalf("top-level command not answered in its thread: %v", p)
	}
}

// Thread->task links are stored, so routing needs neither message parsing nor an API read, and
// survives a restart (a new Slack over the same settings).
func TestThreadLinksPersist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "conversations.replies") {
			t.Error("linked thread must not read the thread")
		}
		fmt.Fprint(w, `{"ok":true,"ts":"50.0"}`)
	}))
	defer srv.Close()
	api = srv.URL + "/"
	cfg := map[string]string{"slack_bot_token": "x", "slack_user_id": "U1"}
	mk := func() *Slack {
		return &Slack{get: func(k string) string { return cfg[k] }, set: func(k, v string) error { cfg[k] = v; return nil }}
	}
	mk().event([]byte(`{"event":{"type":"message","user":"U1","text":"new B x","channel":"D1","ts":"40.0"}}`),
		func(chat.In) string { return "created CV-8 x (backlog)" })
	var got chat.In
	mk().event([]byte(`{"event":{"type":"message","user":"U1","text":"more","channel":"D1","ts":"41.0","thread_ts":"40.0"}}`),
		func(in chat.In) string { got = in; return "ok" })
	if got.Key != "CV-8" || got.Text != "more" {
		t.Fatalf("got %+v", got)
	}
	if mk().rootOf("D1", "CV-8") != "40.0" {
		t.Fatalf("task not linked to its thread: %v", cfg)
	}
}

// CV-163: a reply to a closed or missing task is answered with the error, unprefixed, and an
// unlinked thread is not linked to it.
func TestClosedTaskReplyIsError(t *testing.T) {
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "conversations.replies") {
			fmt.Fprint(w, `{"ok":true,"messages":[{"ts":"163.5","text":"*CV-9* done"}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "chat.postMessage") {
			posts = append(posts, string(b))
		}
		fmt.Fprint(w, `{"ok":true,"ts":"163.7"}`)
	}))
	defer srv.Close()
	api = srv.URL + "/"
	cfg := map[string]string{"slack_bot_token": "x", "slack_user_id": "U1", "slack_thread:D1:163.1": "CV-8"}
	s := &Slack{get: func(k string) string { return cfg[k] }, set: func(k, v string) error { cfg[k] = v; return nil }}
	h := func(in chat.In) string { return chat.Closed + in.Key + " is closed." }
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"hi","channel":"D1","ts":"163.2","thread_ts":"163.1"}}`), h)
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"hi","channel":"D1","ts":"163.6","thread_ts":"163.5"}}`), h)
	if len(posts) != 2 || !strings.Contains(posts[0], "CV-8 is closed") || !strings.Contains(posts[1], "CV-9 is closed") {
		t.Fatalf("posts %v", posts)
	}
	for _, p := range posts {
		if strings.Contains(p, "speech_balloon") {
			t.Fatalf("closed reply shown as a comment: %s", p)
		}
	}
	if cfg["slack_thread:D1:163.5"] != "" {
		t.Fatal("closed task linked to thread")
	}
}
