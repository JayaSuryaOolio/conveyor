package hook

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

// QuestionItem holds one question and its options.
type QuestionItem struct {
	Question string
	Options  []string
}

// askPayload handles both Claude Code (AskUserQuestion) and agy (ask_question) PreToolUse payloads.
type askPayload struct {
	ToolName  string `json:"tool_name"`
	Cwd       string `json:"cwd"`
	ToolInput struct {
		Questions []struct {
			Question string `json:"question"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	} `json:"tool_input"`

	// agy format
	ToolCall struct {
		Name string `json:"name"`
		Args struct {
			Questions []struct {
				Question string            `json:"question"`
				Options  []json.RawMessage `json:"options"`
			} `json:"questions"`
		} `json:"args"`
	} `json:"toolCall"`
}

func (p askPayload) name() string {
	if p.ToolName != "" {
		return p.ToolName
	}
	return p.ToolCall.Name
}

func (p askPayload) questions() []QuestionItem {
	var items []QuestionItem
	if len(p.ToolInput.Questions) > 0 {
		for _, q := range p.ToolInput.Questions {
			var opts []string
			for _, o := range q.Options {
				if o.Description != "" {
					opts = append(opts, fmt.Sprintf("%s: %s", o.Label, o.Description))
				} else {
					opts = append(opts, o.Label)
				}
			}
			items = append(items, QuestionItem{Question: q.Question, Options: opts})
		}
		return items
	}
	if len(p.ToolCall.Args.Questions) > 0 {
		for _, q := range p.ToolCall.Args.Questions {
			var opts []string
			for _, raw := range q.Options {
				var s string
				if err := json.Unmarshal(raw, &s); err == nil {
					opts = append(opts, s)
					continue
				}
				var opt struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				}
				if err := json.Unmarshal(raw, &opt); err == nil {
					if opt.Description != "" {
						opts = append(opts, fmt.Sprintf("%s: %s", opt.Label, opt.Description))
					} else {
						opts = append(opts, opt.Label)
					}
				}
			}
			items = append(items, QuestionItem{Question: q.Question, Options: opts})
		}
		return items
	}
	return nil
}

// IsAsk reports whether a PreToolUse payload is an AskUserQuestion or ask_question call.
func IsAsk(raw []byte) bool {
	var p askPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return false
	}
	n := p.name()
	return n == "AskUserQuestion" || n == "ask_question" || strings.HasSuffix(n, ":ask_question")
}

// OnAsk posts the questions of an ask call to the task thread as an agent comment and
// transitions running tasks to needs_you so the user can answer in terminal.
func OnAsk(db *sql.DB, home string, raw []byte) error {
	var p askPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("parse ask payload: %w", err)
	}
	qs := p.questions()
	key := workspace.KeyFromCwd(home, p.Cwd)
	if key == "" || len(qs) == 0 {
		return nil
	}
	task, err := store.GetTaskByKey(db, key)
	if err != nil {
		return fmt.Errorf("hook ask: %w", err)
	}
	c := store.Comment{TaskID: task.ID, Author: store.AuthorAgent, Body: askBody(qs, p.name())}
	if run, err := store.LatestRun(db, task.ID); err == nil {
		c.RunID = &run.ID
	}
	if _, err := store.PostRunComment(db, c); err != nil {
		return err
	}
	// Claude gets its needs_you transition from the Notification hook.
	// Only transition here for agy toolCall payloads (ask_question).
	if p.ToolCall.Name != "" && task.Status == store.StatusRunning {
		_, err = store.UpdateTaskStatus(db, task.ID, store.StatusNeedsYou, store.ActorSystem)
	}
	return err
}

func askBody(qs []QuestionItem, toolName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Asked in the session (%s); answer there (attach) or reply here:\n", toolName)
	for i, q := range qs {
		fmt.Fprintf(&b, "\n%d. %s\n", i+1, q.Question)
		for _, o := range q.Options {
			fmt.Fprintf(&b, "   - %s\n", o)
		}
	}
	return strings.TrimSpace(b.String())
}
