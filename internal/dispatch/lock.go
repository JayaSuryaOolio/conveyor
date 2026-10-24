package dispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked means another process holds the dispatcher lock.
var ErrLocked = errors.New("dispatcher already running")

// Lock takes the exclusive flock on <home>/dispatcher.lock without blocking.
func Lock(home string) (release func(), err error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, fmt.Errorf("create home: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(home, "dispatcher.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open dispatcher lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("flock: %w", err)
	}
	return func() { f.Close() }, nil // closing releases the lock
}
