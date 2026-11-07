package transcript

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Entry is one line of a session chat log. Kind is user, text or tool.
type Entry struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// toolArgs are the input fields that best summarize a tool call, in preference order.
var toolArgs = []string{"command", "file_path", "pattern", "path", "url", "query", "description", "prompt"}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

// Log returns chat entries from the byte offset after, and the offset to pass next time.
// A trailing partial line is left for the next call.
func Log(path string, after int64) ([]Entry, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, after, err
	}
	defer f.Close()
	if _, err := f.Seek(after, io.SeekStart); err != nil {
		return nil, after, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, after, err
	}
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		b = b[:i+1]
	} else {
		b = nil
	}
	out := []Entry{}
	for _, raw := range bytes.Split(b, []byte{'\n'}) {
		var l struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		if l.Type == "user" || l.Type == "assistant" {
			switch l.Type {
			case "user":
				if t := strings.TrimSpace(userText(l.Message.Content)); t != "" && !strings.HasPrefix(t, "<") {
					out = append(out, Entry{"user", t, l.Timestamp})
				}
			case "assistant":
				var parts []struct {
					Type  string                     `json:"type"`
					Text  string                     `json:"text"`
					Name  string                     `json:"name"`
					Input map[string]json.RawMessage `json:"input"`
				}
				json.Unmarshal(l.Message.Content, &parts)
				for _, p := range parts {
					switch {
					case p.Type == "text" && strings.TrimSpace(p.Text) != "":
						out = append(out, Entry{"text", strings.TrimSpace(p.Text), l.Timestamp})
					case p.Type == "tool_use":
						arg := ""
						for _, k := range toolArgs {
							var s string
							if json.Unmarshal(p.Input[k], &s) == nil && s != "" {
								arg = s
								break
							}
						}
						var qs []struct {
							Question string `json:"question"`
						}
						if arg == "" && json.Unmarshal(p.Input["questions"], &qs) == nil && len(qs) > 0 { // AskUserQuestion
							arg = qs[0].Question
						}
						out = append(out, Entry{"tool", strings.TrimSpace(p.Name + ": " + oneLine(arg)), l.Timestamp})
					}
				}
			}
			continue
		}

		// Support Antigravity (agy) transcript.jsonl format
		var agyLine struct {
			Type      string  `json:"type"`
			CreatedAt string  `json:"created_at"`
			Content   *string `json:"content"`
			ToolCalls []struct {
				Name string                     `json:"name"`
				Args map[string]json.RawMessage `json:"args"`
			} `json:"tool_calls"`
		}
		if json.Unmarshal(raw, &agyLine) == nil {
			switch agyLine.Type {
			case "USER_INPUT":
				if agyLine.Content != nil && strings.TrimSpace(*agyLine.Content) != "" && !strings.HasPrefix(strings.TrimSpace(*agyLine.Content), "<") {
					out = append(out, Entry{"user", strings.TrimSpace(*agyLine.Content), agyLine.CreatedAt})
				}
			case "PLANNER_RESPONSE":
				if agyLine.Content != nil && strings.TrimSpace(*agyLine.Content) != "" {
					out = append(out, Entry{"text", strings.TrimSpace(*agyLine.Content), agyLine.CreatedAt})
				}
				for _, tc := range agyLine.ToolCalls {
					arg := ""
					for _, k := range toolArgs {
						var s string
						if json.Unmarshal(tc.Args[k], &s) == nil && s != "" {
							arg = s
							break
						}
					}
					out = append(out, Entry{"tool", strings.TrimSpace(tc.Name + ": " + oneLine(arg)), agyLine.CreatedAt})
				}
			}
		}
	}
	return out, after + int64(len(b)), nil
}
