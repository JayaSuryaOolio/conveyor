package slack

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestImpersonateToggleAndOwnPostsIgnored(t *testing.T) {
	cfg := map[string]string{"slack_bot_token": "xoxb", "slack_user_token": "xoxp", "slack_app_token": "xapp", "slack_user_id": "U1"}
	s := &Slack{func(k string) string { return cfg[k] }}
	if s.token() != "xoxb" {
		t.Fatal("user token used while impersonation is off")
	}
	cfg["slack_impersonate"] = "on"
	if s.token() != "xoxp" || !s.Ready() {
		t.Fatal("user token should win when impersonating and satisfy Ready")
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
	s := &Slack{func(k string) string { return map[string]string{"slack_user_id": "U1"}[k] }}
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
	s := &Slack{func(k string) string { return map[string]string{"slack_bot_token": "x", "slack_user_id": "U1"}[k] }}
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
	s := &Slack{func(k string) string {
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
