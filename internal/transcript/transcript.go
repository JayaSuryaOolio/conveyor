// Package transcript sums token usage from a Claude Code transcript JSONL.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
