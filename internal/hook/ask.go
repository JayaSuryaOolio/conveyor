package hook

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JayaSuryaOolio/conveyor/internal/store"
	"github.com/JayaSuryaOolio/conveyor/internal/workspace"
)

// askPayload is a PreToolUse call of AskUserQuestion.
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
}

// IsAsk reports whether a PreToolUse payload is an AskUserQuestion call.
func IsAsk(raw []byte) bool {
	var p askPayload
	return json.Unmarshal(raw, &p) == nil && p.ToolName == "AskUserQuestion"
}

// OnAsk posts the questions of an AskUserQuestion call to the task thread as an agent comment: a
// background run's dialog shows only in the session, so the thread would otherwise stay empty.
// Agent-authored, so it does not requeue the task.
func OnAsk(db *sql.DB, home string, raw []byte) error {
	var p askPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("parse AskUserQuestion payload: %w", err)
	}
	key := workspace.KeyFromCwd(home, p.Cwd)
	if key == "" || len(p.ToolInput.Questions) == 0 {
		return nil
	}
	task, err := store.GetTaskByKey(db, key)
	if err != nil {
		return fmt.Errorf("hook AskUserQuestion: %w", err)
	}
	_, err = store.AddComment(db, store.Comment{TaskID: task.ID, Author: store.AuthorAgent, Body: askBody(p)})
	return err
}

func askBody(p askPayload) string {
	var b strings.Builder
	b.WriteString("Asked in the session (AskUserQuestion); answer there (attach) or reply here:\n")
	for i, q := range p.ToolInput.Questions {
		fmt.Fprintf(&b, "\n%d. %s\n", i+1, q.Question)
		for _, o := range q.Options {
			if o.Description != "" {
				fmt.Fprintf(&b, "   - %s: %s\n", o.Label, o.Description)
			} else {
				fmt.Fprintf(&b, "   - %s\n", o.Label)
			}
		}
	}
	return strings.TrimSpace(b.String())
}
