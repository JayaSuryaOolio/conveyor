// Package transcript sums token usage from a Claude Code transcript JSONL.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Usage is the token accounting for one run.
type Usage struct{ In, Out, CacheRead, CacheWrite, CtxPeak, Calls int }

type line struct {
	Type    string `json:"type"`
	Message struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			In         int `json:"input_tokens"`
			Out        int `json:"output_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// userText extracts the text of a user entry (string content or text blocks).
func userText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct{ Type, Text string }
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// Sum reads a Claude Code transcript JSONL. It counts assistant entries with message.usage,
// de-duplicating by message.id (the same message is written once per content block; the
// last copy wins). CtxPeak = max over calls of input+cache_read+cache_creation tokens.
// Only lines after the last "user" entry whose text equals promptText count toward the run
// (pass "" to count the whole file).
func Sum(path string, promptText string) (Usage, error) {
	f, err := os.Open(path)
	if err != nil {
		return Usage{}, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	want := strings.TrimSpace(promptText)
	var ls []line // entries since the last matching prompt
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // tolerate partial or garbage lines
		}
		if want != "" && l.Type == "user" && strings.TrimSpace(userText(l.Message.Content)) == want {
			ls = ls[:0]
			continue
		}
		if l.Type == "assistant" && l.Message.Usage != nil {
			ls = append(ls, l)
		}
	}
	if err := sc.Err(); err != nil {
		return Usage{}, fmt.Errorf("read transcript: %w", err)
	}
	last := map[string]int{} // message.id -> index of its last line
	for i, l := range ls {
		if l.Message.ID != "" {
			last[l.Message.ID] = i
		}
	}
	var u Usage
	for i, l := range ls {
		if l.Message.ID != "" && last[l.Message.ID] != i {
			continue
		}
		m := l.Message.Usage
		u.Calls++
		u.In += m.In
		u.Out += m.Out
		u.CacheRead += m.CacheRead
		u.CacheWrite += m.CacheWrite
		if c := m.In + m.CacheRead + m.CacheWrite; c > u.CtxPeak {
			u.CtxPeak = c
		}
	}
	return u, nil
}

// maxPendingAge bounds how long a launched background job keeps a task running, so a hung
// job cannot hold it forever.
const maxPendingAge = 2 * time.Hour

// BackgroundPending reports whether the run (entries after the last user entry equal to
// promptText) launched a Monitor or a Bash run_in_background call that has not yet delivered
// a <task-notification>. A last assistant entry flagged as an API error (e.g. an invalid model)
// never counts as pending: that run is stalled, not waiting.
func BackgroundPending(path, promptText string, now time.Time) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	want := strings.TrimSpace(promptText)
	var launched, done int
	var lastLaunch time.Time
	apiErr := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			APIError  bool   `json:"isApiErrorMessage"`
			Message   struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch l.Type {
		case "user":
			text := userText(l.Message.Content)
			if want != "" && strings.TrimSpace(text) == want {
				launched, done, apiErr = 0, 0, false
			} else if strings.Contains(text, "<task-notification>") {
				done++
			}
		case "assistant":
			apiErr = l.APIError
			var blocks []struct {
				Type  string `json:"type"`
				Name  string `json:"name"`
				Input struct {
					Background bool `json:"run_in_background"`
				} `json:"input"`
			}
			if json.Unmarshal(l.Message.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				if b.Type == "tool_use" && (b.Name == "Monitor" || b.Name == "Bash" && b.Input.Background) {
					launched++
					if t, err := time.Parse(time.RFC3339, l.Timestamp); err == nil {
						lastLaunch = t
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return false, fmt.Errorf("read transcript: %w", err)
	}
	return !apiErr && launched > done && now.Sub(lastLaunch) < maxPendingAge, nil
}
