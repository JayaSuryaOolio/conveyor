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
	s := &Slack{get: func(k string) string { return cfg[k] }, set: func(k, v string) error { cfg[k] = v; return nil }}
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
	if mk().rootOf("U1", "CV-8") != "40.0" {
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
	cfg := map[string]string{"slack_bot_token": "x", "slack_user_id": "U1", "slack_thread:U1:163.1": "CV-8"}
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
	if cfg["slack_thread:U1:163.5"] != "" {
		t.Fatal("closed task linked to thread")
	}
}

// CV-212: a reply in a thread with no link, where conversations.replies fails, still gets an
// answer in the thread; and the read call is form-encoded (Slack rejects JSON there).
func TestUnlinkedThreadRepliesFailureAnswers(t *testing.T) {
	var posts []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "conversations.replies") {
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.FormValue("ts") != "212.1" {
				t.Errorf("replies not form-encoded: %q %q", r.Header.Get("Content-Type"), r.FormValue("ts"))
			}
			fmt.Fprint(w, `{"ok":false,"error":"invalid_arguments"}`)
			return
		}
		var a map[string]any
		json.NewDecoder(r.Body).Decode(&a)
		posts = append(posts, a)
		fmt.Fprint(w, `{"ok":true,"ts":"212.9"}`)
	}))
	defer srv.Close()
	api = srv.URL + "/"
	s := &Slack{get: func(k string) string { return map[string]string{"slack_bot_token": "x", "slack_user_id": "U1"}[k] }}
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"hello?","channel":"D1","ts":"212.2","thread_ts":"212.1"}}`),
		func(chat.In) string { t.Fatal("must not run a command"); return "" })
	if len(posts) != 1 || posts[0]["thread_ts"] != "212.1" || !strings.Contains(fmt.Sprint(posts[0]["text"]), "closed or unknown") {
		t.Fatalf("posts %v", posts)
	}
}

func TestBackfillHandlesMissedMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"ts":"9.9","messages":[
			{"type":"message","user":"U1","text":"new msg","ts":"3.0","client_msg_id":"a"},
			{"type":"message","user":"U1","text":"our own post","ts":"2.0"},
			{"type":"message","user":"U1","text":"old","ts":"1.0","client_msg_id":"b"}]}`))
	}))
	defer srv.Close()
	old := api
	api = srv.URL + "/"
	defer func() { api = old }()
	cfg := map[string]string{"slack_bot_token": "x", "slack_user_id": "U1", "slack_cursor": "D1:1.0"}
	s := &Slack{get: func(k string) string { return cfg[k] }, set: func(k, v string) error { cfg[k] = v; return nil }}
	var got []string
	s.backfill(func(in chat.In) string { got = append(got, in.Text); return "ok" })
	if len(got) != 1 || got[0] != "new msg" || cfg["slack_cursor"] != "D1:3.0" {
		t.Fatalf("got %v cursor %q", got, cfg["slack_cursor"])
	}
}

// A task update is posted to the user id; the reply to it arrives on the DM channel id (D...).
// The thread link must match across that, or the reply is dropped.
func TestReplyToSentUpdateReachesTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "conversations.replies") {
			t.Error("a thread we posted must be found by link, not read back")
		}
		fmt.Fprint(w, `{"ok":true,"ts":"70.0"}`)
	}))
	defer srv.Close()
	api = srv.URL + "/"
	cfg := map[string]string{"slack_bot_token": "x", "slack_user_id": "U1"}
	s := &Slack{get: func(k string) string { return cfg[k] }, set: func(k, v string) error { cfg[k] = v; return nil }}
	if err := s.Send(chat.Msg{Key: "CV-7", Title: "t", Status: "needs_you"}); err != nil {
		t.Fatal(err)
	}
	var got chat.In
	s.event([]byte(`{"event":{"type":"message","user":"U1","text":"go on","channel":"D1","ts":"71.0","thread_ts":"70.0"}}`),
		func(in chat.In) string { got = in; return "ok" })
	if got.Key != "CV-7" || got.Text != "go on" {
		t.Fatalf("reply not routed to the task: %+v", got)
	}
}

func TestHomeViewStaysUnderBlockLimitAndOffersControls(t *testing.T) {
	var b chat.Board
	for i := range 60 {
		st := []string{"needs_you", "running", "todo", "failed", "review"}[i%5]
		b.Rows = append(b.Rows, chat.Row{Key: fmt.Sprint("CV-", i), Title: "t <&>", Status: st})
	}
	v := homeView(b)
	if len(v) > 100 {
		t.Fatalf("home has %d blocks; Slack allows 100", len(v))
	}
	js, _ := json.Marshal(v) // Marshal escapes & < > itself, hence the \\u0026 form below
	for _, want := range []string{"home_new", "home_view", "home_refresh", "home_mute", "home_menu:CV-0", "+", `\u0026lt;\u0026amp;\u0026gt;`} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("home lacks %q", want)
		}
	}
	b.Muted = true
	if js, _ := json.Marshal(homeView(b)); !strings.Contains(string(js), "home_unmute") {
		t.Fatal("muted home must offer unmute")
	}
}

func TestHomeActionsAndSubmissions(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, strings.TrimPrefix(r.URL.Path, "/"))
		fmt.Fprint(w, `{"ok":true,"ts":"1.0"}`)
	}))
	defer srv.Close()
	api = srv.URL + "/"
	s := &Slack{get: func(k string) string { return map[string]string{"slack_bot_token": "x", "slack_user_id": "U1"}[k] }}
	s.UseSource(chat.Source{
		Board: func() chat.Board { return chat.Board{Repos: []string{"a"}} },
		Detail: func(k string) (chat.Detail, error) {
			return chat.Detail{Key: k, Title: "T", Status: "done", Description: "d"}, nil
		},
	})
	var cmds []string
	h := func(in chat.In) string { cmds = append(cmds, in.Text); return "created CV-9 x (backlog)" }

	s.interactive([]byte(`{"type":"block_actions","user":{"id":"U1"},"trigger_id":"T1","actions":[{"action_id":"home_mute"}]}`), h)
	s.interactive([]byte(`{"type":"block_actions","user":{"id":"U1"},"trigger_id":"T1","actions":[{"action_id":"home_menu:CV-1","selected_option":{"value":"move CV-1 done"}}]}`), h)
	s.interactive([]byte(`{"type":"block_actions","user":{"id":"U1"},"trigger_id":"T1","actions":[{"action_id":"home_new"},{"action_id":"home_menu:CV-1","selected_option":{"value":"view CV-1"}}]}`), h)
	if strings.Join(cmds, "|") != "mute|move CV-1 done" {
		t.Fatalf("commands %v", cmds)
	}
	if got := strings.Join(calls, ","); got != "views.publish,views.publish,views.open,views.open" {
		t.Fatalf("api calls %s", got)
	}

	sub := func(cb, vals string) (m, func(), bool) {
		return s.submit([]byte(`{"type":"view_submission","user":{"id":"U1"},"view":{"callback_id":"`+cb+`","state":{"values":`+vals+`}}}`), h)
	}
	resp, _, ok := sub("view_task", `{"key":{"v":{"value":" cv-5 "}}}`)
	if !ok || resp["response_action"] != "update" {
		t.Fatalf("view submit: %v", resp)
	}
	resp, after, _ := sub("new_task", `{"repo":{"v":{"selected_option":{"value":"a"}}},"text":{"v":{"value":"do it"}}}`)
	if resp["response_action"] != "clear" || after == nil || cmds[len(cmds)-1] != "new a do it" {
		t.Fatalf("new submit: %v %v", resp, cmds)
	}
	if resp, _, _ = sub("new_task", `{"text":{"v":{"value":"  "}}}`); resp["response_action"] != "errors" {
		t.Fatalf("empty text accepted: %v", resp)
	}
}

// TestListenAcksViewSubmissionWithResponse: a modal submission must be answered in the ack
// (response_action), otherwise Slack just closes the modal and nothing happens.
func TestListenAcksViewSubmissionWithResponse(t *testing.T) {
	acked := make(chan map[string]any, 1)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ok":true,"url":"ws%s/ws"}`, strings.TrimPrefix(srv.URL, "http"))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, _ := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		defer c.Close()
		c.WriteJSON(map[string]any{"type": "interactive", "envelope_id": "E2", "payload": map[string]any{
			"type": "view_submission", "user": map[string]any{"id": "U1"},
			"view": map[string]any{"callback_id": "view_task", "state": map[string]any{"values": map[string]any{"key": map[string]any{"v": map[string]any{"value": "CV-5"}}}}}}})
		var ack map[string]any
		c.ReadJSON(&ack)
		acked <- ack
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"ok":true,"ts":"1.0"}`) })
	api = srv.URL + "/"
	s := &Slack{get: func(k string) string {
		return map[string]string{"slack_app_token": "a", "slack_bot_token": "x", "slack_user_id": "U1"}[k]
	}}
	s.UseSource(chat.Source{Detail: func(k string) (chat.Detail, error) { return chat.Detail{Key: k}, nil }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Listen(ctx, func(chat.In) string { return "ok" })
	select {
	case ack := <-acked:
		p, _ := ack["payload"].(map[string]any)
		if ack["envelope_id"] != "E2" || p["response_action"] != "update" {
			t.Fatalf("ack %v", ack)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no ack")
	}
}
