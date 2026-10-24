package core

import (
	"io"
	"os"
	"path/filepath"
)

// LogPath is the desktop app's log file under home.
func LogPath(home string) string { return filepath.Join(home, "conveyor.log") }

// AppLog is a chunk of the app log and the offset to read from next.
type AppLog struct {
	Text string `json:"text"`
	Next int64  `json:"next"`
}

const logTail = 64 << 10

// ReadAppLog returns the log from offset after. A negative after, or one past the end
// (the log was rotated), starts from the last 64 KB.
func ReadAppLog(home string, after int64) (AppLog, error) {
	f, err := os.Open(LogPath(home))
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
