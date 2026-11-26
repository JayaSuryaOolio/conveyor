package slack

// The App Home tab and its modals: a task board with buttons, built from Block Kit only (the
// one markup Home accepts). Every click maps to an existing chat command string, so no business
// logic lives here. There is no live refresh: the Refresh button republishes (decision CV-231 D1/D3).
//
// ponytail: blocks are built from map literals like the rest of this package; a tiny
// block-builder type would shorten them if more screens are added.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/chat"
)

type m = map[string]any

// perSection caps rows per status so Home stays under Slack's 100-block limit
// (4 sections * (header + 10 rows + "more") + header, actions and context blocks).
const perSection = 10

var esc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// clip cuts s to n runes (Slack caps text objects: 3000 for a section).
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func mrkdwn(t string) m  { return m{"type": "mrkdwn", "text": t} }
func plain(t string) m   { return m{"type": "plain_text", "text": t} }
func section(t string) m { return m{"type": "section", "text": mrkdwn(t)} }
func ctxLine(t string) m { return m{"type": "context", "elements": []any{mrkdwn(t)}} }
func button(text, id string, style ...string) m {
	b := m{"type": "button", "text": plain(text), "action_id": id}
	if len(style) > 0 {
		b["style"] = style[0]
	}
	return b
}

// homeSections are the board's groups in display order.
var homeSections = []struct {
	title, emoji string
	statuses     []string
}{
	{"Needs you", ":raising_hand:", []string{"needs_you", "review"}},
	{"Failed", ":x:", []string{"failed"}},
	{"Running", ":gear:", []string{"running"}},
	{"Todo", ":hourglass_flowing_sand:", []string{"todo"}},
}

// taskRow is one task with an overflow menu; values are the commands the click runs.
func taskRow(r chat.Row) m {
	t := fmt.Sprintf("*%s* %s  `%s`", r.Key, esc.Replace(clip(r.Title, 120)), r.Status)
	for _, p := range r.PRs[:min(len(r.PRs), 5)] { // a section's text caps at 3000 chars
		t += "\n:link: <" + p.URL + "|" + esc.Replace(p.Name) + " PR>"
	}
	opt := func(label, cmd string) m { return m{"text": plain(label), "value": cmd} }
	b := section(t)
	b["accessory"] = m{"type": "overflow", "action_id": "home_menu:" + r.Key, "options": []any{
		opt("View details", "view "+r.Key),
		opt("Mark done", "move "+r.Key+" done"),
		opt("Requeue (todo)", "move "+r.Key+" todo"),
		opt("Cancel task", "move "+r.Key+" cancelled"),
	}}
	return b
}

// homeView renders the Home tab for b.
func homeView(b chat.Board) []any {
	need := 0
	for _, r := range b.Rows {
		if r.Status == "needs_you" || r.Status == "review" {
			need++
		}
	}
	mute := button(":no_bell: Mute DMs", "home_mute")
	note := ":bell: Task updates arrive as DMs, one thread per task. Conveyor must be running to answer."
	if b.Muted {
		mute, note = button(":bell: Unmute DMs", "home_unmute", "danger"), ":no_bell: *Task DMs are muted.* Unmute to get updates here again."
	}
	out := []any{
		m{"type": "header", "text": plain("🚀 Conveyor")},
		ctxLine(fmt.Sprintf("%d need you · %d open · commands also work as DMs", need, len(b.Rows))),
		m{"type": "actions", "elements": []any{
			button("New task", "home_new", "primary"), button("View task", "home_view"),
			button("Refresh", "home_refresh"), mute, button("Help", "home_help"),
		}},
		m{"type": "divider"},
	}
	for _, sec := range homeSections {
		var rows []chat.Row
		for _, r := range b.Rows {
			for _, st := range sec.statuses {
				if r.Status == st {
					rows = append(rows, r)
				}
			}
		}
		if len(rows) == 0 {
			continue
		}
		out = append(out, section(fmt.Sprintf("%s *%s* (%d)", sec.emoji, sec.title, len(rows))))
		for i, r := range rows {
			if i == perSection {
				out = append(out, ctxLine(fmt.Sprintf("+%d more: send `list` or use View task", len(rows)-perSection)))
				break
			}
			out = append(out, taskRow(r))
		}
	}
	if len(b.Rows) == 0 {
		out = append(out, section("Nothing open. Hit *New task* to start one."))
	}
	return append(out, m{"type": "divider"}, ctxLine(note))
}

// ---- modals ----

func modal(callback, title string, blocks []any, submit string) m {
	v := m{"type": "modal", "callback_id": callback, "title": plain(clip(title, 24)), "close": plain("Close"), "blocks": blocks}
	if submit != "" {
		v["submit"] = plain(submit)
	}
	return v
}

func input(block, label string, el m) m {
	el["action_id"] = "v"
	return m{"type": "input", "block_id": block, "label": plain(label), "element": el}
}

func newModal(repos []string) m {
	var bs []any
	if len(repos) > 0 {
		var opts []any
		for _, r := range repos {
			opts = append(opts, m{"text": plain(clip(r, 75)), "value": r})
		}
		bs = append(bs, input("repo", "Repo", m{"type": "static_select", "options": opts, "initial_option": opts[0]}))
	}
	bs = append(bs, input("text", "What should the agent do?", m{"type": "plain_text_input", "multiline": true}))
	return modal("new_task", "New task", bs, "Create")
}

func viewAskModal() m {
	return modal("view_task", "View task", []any{input("key", "Task key", m{"type": "plain_text_input", "placeholder": plain("CV-12")})}, "VIEW")
}

func noticeModal(title, text string) m {
	return modal("notice", title, []any{section(clip(text, 2900))}, "")
}

func detailModal(d chat.Detail) m {
	bs := []any{section(fmt.Sprintf("*%s* %s\nStatus: `%s`", d.Key, esc.Replace(clip(d.Title, 200)), d.Status))}
	if len(d.Repos) > 0 {
		bs = append(bs, ctxLine(":file_folder: "+esc.Replace(strings.Join(d.Repos, ", "))))
	}
	for _, p := range d.PRs {
		bs = append(bs, section(":link: <"+p.URL+"|"+esc.Replace(p.Name)+" PR>"))
	}
	run := "never run"
	if d.LastRun != "" {
		run = d.LastRun
	}
	bs = append(bs, section("*Last run:* "+esc.Replace(run)))
	if d.Summary != "" {
		bs = append(bs, section("*Summary*\n"+esc.Replace(clip(d.Summary, 1200))))
	}
	if d.Next != "" {
		bs = append(bs, section("*Next*\n"+esc.Replace(clip(d.Next, 600))))
	}
	// description is Markdown; mrkdwn shows most of it fine. ponytail: clipped to fit one section.
	if d.Description != "" {
		bs = append(bs, m{"type": "divider"}, section("*Description*\n"+esc.Replace(clip(d.Description, 2500))))
	}
	return modal("task_detail", d.Key, bs, "")
}

// ---- publishing and opening ----

// UseSource implements chat.Boarded.
func (s *Slack) UseSource(src chat.Source) { s.src = src }

func (s *Slack) board() chat.Board {
	if s.src.Board == nil {
		return chat.Board{}
	}
	return s.src.Board()
}

// publishHome (re)draws the Home tab. Called on connect and after Home actions, never on a timer.
func (s *Slack) publishHome() {
	_, err := call(s.token(), "views.publish", m{"user_id": s.get("slack_user_id"), "view": m{"type": "home", "blocks": homeView(s.board())}})
	if err != nil {
		log.Printf("chat: slack home: %v", err)
	}
}

// open shows a modal for a click (trigger ids expire after 3s, so no slow work before this).
func (s *Slack) open(trigger string, view m) {
	if _, err := call(s.token(), "views.open", m{"trigger_id": trigger, "view": view}); err != nil {
		log.Printf("chat: slack modal: %v", err)
	}
}

// detailView is the detail modal for key, or a notice carrying the error.
func (s *Slack) detailView(key string) m {
	if s.src.Detail == nil {
		return noticeModal("View task", "Not available.")
	}
	d, err := s.src.Detail(key)
	if err != nil {
		return noticeModal("View task", err.Error())
	}
	return detailModal(d)
}

// submit answers a modal submission. It runs inside the read loop because Slack wants the
// response in the ack (3s); commands here are local DB work. after, if set, runs on the worker.
func (s *Slack) submit(payload []byte, handle func(chat.In) string) (resp m, after func(), ok bool) {
	var p struct {
		Type string
		User struct{ ID string }
		View struct {
			CallbackID string `json:"callback_id"`
			State      struct {
				Values map[string]map[string]struct {
					Value          string
					SelectedOption struct{ Value string } `json:"selected_option"`
				}
			}
		}
	}
	if json.Unmarshal(payload, &p) != nil || p.Type != "view_submission" {
		return nil, nil, false
	}
	if p.User.ID != s.get("slack_user_id") {
		return m{"response_action": "clear"}, nil, true
	}
	val := func(block string) (v, sel string) {
		x := p.View.State.Values[block]["v"]
		return strings.TrimSpace(x.Value), x.SelectedOption.Value
	}
	fail := func(block, msg string) m {
		return m{"response_action": "errors", "errors": m{block: msg}}
	}
	switch p.View.CallbackID {
	case "view_task":
		key, _ := val("key")
		if s.src.Detail == nil {
			return fail("key", "not available"), nil, true
		}
		d, err := s.src.Detail(key)
		if err != nil {
			return fail("key", err.Error()), nil, true
		}
		return m{"response_action": "update", "view": detailModal(d)}, nil, true
	case "new_task":
		text, _ := val("text")
		_, repo := val("repo")
		if text == "" {
			return fail("text", "Say what the task is."), nil, true
		}
		out := handle(chat.In{Text: strings.TrimSpace("new " + repo + " " + text)})
		if !strings.HasPrefix(out, "created ") {
			return fail("text", clip(strings.TrimPrefix(out, "warning: "), 300)), nil, true
		}
		// confirm in the DM like a typed "new" does, and thread later updates under it
		return m{"response_action": "clear"}, func() {
			dm := s.get("slack_user_id")
			ts, _ := s.post(m{"channel": dm, "text": out})
			if c := createdRe.FindStringSubmatch(out); c != nil {
				s.link(dm, ts, c[1])
			}
		}, true
	}
	return m{"response_action": "clear"}, nil, true
}
