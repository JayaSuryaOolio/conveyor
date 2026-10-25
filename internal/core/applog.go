package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LogPath is the desktop app's log file under home.
func LogPath(home string) string { return filepath.Join(home, "conveyor.log") }

// DispatcherLogPath and HookLogPath are the dispatcher's and the hook command's logs under home.
func DispatcherLogPath(home string) string { return filepath.Join(home, "logs", "dispatcher.log") }
func HookLogPath(home string) string       { return filepath.Join(home, "logs", "hook.log") }

// OpenLog appends to path, creating its directory, first moving it to path.1 once it passes maxLog.
// ponytail: rotates only on open; rotate on write if a single process ever logs that much.
func OpenLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLog {
		os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

const maxLog = 5 << 20

// logSources maps the fixed source names the UI may ask for to files under home.
var logSources = map[string]string{
	"app":        "conveyor.log",
	"dispatcher": "logs/dispatcher.log",
	"hooks":      "logs/hook.log",
}

// SourcePath is the log file for a source name; ok is false for an unknown name.
func SourcePath(home, source string) (string, bool) {
	rel, ok := logSources[source]
	return filepath.Join(home, rel), ok
}

// AppLog is a chunk of the app log and the offset to read from next.
type AppLog struct {
	Text string `json:"text"`
	Next int64  `json:"next"`
}

const logTail = 64 << 10

// ReadAppLog returns the source's log from offset after. A negative after, or one past the end
// (the log was rotated), starts from the last 64 KB.
func ReadAppLog(home, source string, after int64) (AppLog, error) {
	path, ok := SourcePath(home, source)
	if !ok {
		return AppLog{}, fmt.Errorf("unknown log source %q", source)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return AppLog{}, nil
	}
	if err != nil {
		return AppLog{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return AppLog{}, err
	}
	if after < 0 || after > fi.Size() {
		after = max(0, fi.Size()-logTail)
	}
	b, err := io.ReadAll(io.NewSectionReader(f, after, fi.Size()-after))
	return AppLog{Text: string(b), Next: after + int64(len(b))}, err
}
