# 005: Claude runner

**Wave 2** · depends on: 002 · branch `task/005-claude-runner`

## Goal
`internal/claude` wraps the `claude` CLI exactly as tech-spec §7 describes. This is pure command building plus exec. It has no DB access.

## API
```go
package claude

var (
    ErrNotFound  = errors.New("claude CLI not found")
    ErrUntrusted = errors.New("workspace not trusted by Claude Code")
)
var execCommand = exec.CommandContext // swapped in tests

type LaunchSpec struct {
    Cwd          string   // primary worktree
    AddDirs      []string // other worktrees
    Prompt       string
    Name         string   // task key
    SettingsPath string
    Model, Agent string   // optional
    MCPConfig    string   // optional; set if <Cwd>/.mcp.json exists (caller decides)
    Bypass       bool     // true for auto runs
}

// Args returns argv after "claude", with the prompt before --add-dir (spike finding).
func (s LaunchSpec) Args() []string

// Launch runs `claude --bg ...` with env CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1 and parses
// "backgrounded · <id>" from the combined output. Detects "Workspace not trusted" → ErrUntrusted
// (wrap it with the output).
func Launch(ctx context.Context, s LaunchSpec) (bgID string, err error)

// Resume: Stop(bgID) (errors ignored), then `claude --bg --resume <sessionID> <prompt>` with NO other flags,
// run with cwd. Returns the bg id printed (normally the same; if the output says "started a copy as X", return X).
func Resume(ctx context.Context, cwd, bgID, sessionID, prompt string) (string, error)

type Agent struct {
    ID, Name, Cwd, State, Status, SessionID string
    PID int
}
// Agents runs `claude agents --json --all`.
func Agents(ctx context.Context) ([]Agent, error)

func Stop(ctx context.Context, bgID string) error
func Remove(ctx context.Context, bgID string) error

// AttachCommand, ResumeCommand and FreshCommand return shell command strings for a terminal (task 008 uses them).
// ResumeCommand = `claude --resume <sid> --settings <path>`;
// FreshCommand  = `claude --settings <path> [--add-dir ...] "Read <briefPath> first, then wait for my instructions."`
// Both are interactive, so no bypass and no --bg.
func AttachCommand(bgID string) string
func ResumeCommand(sessionID, settingsPath string) string
func FreshCommand(settingsPath, briefPath string, addDirs []string) string

// WriteSettings writes <home>/claude-settings.json (tech-spec §7) atomically (tmp + rename) and returns its path.
// conveyorBin is the absolute CLI path; both paths are single-quoted in the commands.
func WriteSettings(home, conveyorBin string) (string, error)
```

## Tests
- A fake `claude` script on `PATH` records argv, cwd and env to a file and prints canned output for each subcommand.
- `Args()` ordering, especially the prompt before `--add-dir`, and the optional flags.
- `Launch` parses the id and maps an untrusted workspace to `ErrUntrusted`.
- `Resume` calls stop first, then resume with only `--bg --resume <sid> <prompt>`, and handles the "started a copy" output.
- `Agents` parses JSON that includes interactive sessions with no `id`.
- The `WriteSettings` JSON round-trips and contains `worktree.bgIsolation: "none"` and the three hooks.
- Command strings quote paths with spaces.
