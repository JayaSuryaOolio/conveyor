# 004: Hook handler, status-block parser, transcript usage

**Wave 2** · depends on: 002 · branch `task/004-hooks`

## Goal
Implement tech-spec §8 (hooks → state) and §9 (status block), plus the token accounting.

## API
```go
package statusblock
type Block struct {
    Status   string   // "done" | "failed"
    Summary, State, Next string
    Criteria []Criterion
    Learnings, Spawn []string
}
type Criterion struct{ Met bool; Text string }
// Parse returns the last valid block and the message with that block removed (trimmed).
// ok=false when there is no valid block (missing, or bad status).
func Parse(msg string) (b Block, rest string, ok bool)

package transcript
type Usage struct{ In, Out, CacheRead, CacheWrite, CtxPeak, Calls int }
// Sum reads a Claude Code transcript JSONL. It counts assistant entries with message.usage,
// de-duplicating by message.id (the same message is written once per content block).
// CtxPeak = max over calls of input_tokens+cache_read_input_tokens+cache_creation_input_tokens.
// Only lines after the last "user" entry whose text equals promptText count toward the run
// (pass "" to count the whole file).
func Sum(path string, promptText string) (Usage, error)

package hook
// Handle processes one hook event. It never returns an error to the caller that should fail Claude;
// the CLI logs it and exits 0.
func Handle(db *sql.DB, home, event string, payload []byte) error
```
- `cmd/conveyor/cmd_hook.go`: `conveyor hook <Event>` reads stdin with a 1 MB cap, calls `hook.Handle`, appends errors to `<home>/logs/hook.log`, and **always exits 0**.
- `Handle` follows tech-spec §8 exactly, including run resolution and the task transitions for each event.
  - Use `workspace.KeyFromCwd` if task 003 has merged. Otherwise implement an unexported copy with the same semantics and leave a `// TODO(003)` note; the dispatcher task will dedupe.
  - On `Stop` for an auto run, insert one learnings row per `Block.Learnings` entry, with status proposed and repo_id = the primary repo. Insert a `spawn` event per spawn line; task 011 or later turns those into tasks.
  - Pass the run's prompt as `promptText` so a resumed session's usage counts only this run.
- Payload fields used:
  - `session_id`, `cwd`, `transcript_path`, `hook_event_name`
  - `last_assistant_message` (Stop)
  - `prompt` (UserPromptSubmit)
  - `message` and `notification_type` (Notification)

## Fixtures
`internal/transcript/testdata/` holds a small hand-written JSONL. Its shape:
- `{"type":"assistant","message":{"id":"m1","usage":{...},"content":[...]}}`
- repeated `m1` lines, which must be de-duplicated
- user lines

## Tests
- **statusblock**: a full block; a missing block; an invalid status; two blocks (the last wins); criteria with `[x]` and `[ ]`; multiple learnings; key case-insensitivity; `rest` excludes the block.
- **transcript**: de-dup, peak and the prompt split.
- **hook**: table tests on a temp DB with a task and runs, covering:
  - a UserPromptSubmit that sets the session_id
  - Stop with done → review
  - Stop with no block → needs_you
  - Stop with failed → todo, then failed at the max
  - Stop with unconsumed comments → todo
  - an interactive Stop that doesn't change the task
  - a cwd outside the workspaces → ignored
  - malformed JSON → error but no panic
