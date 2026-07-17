//go:build darwin || linux || freebsd || openbsd || netbsd

package processlock

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var ErrLocked = errors.New("data directory is already locked")

type Lock struct {
	file *os.File
}

func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock data directory: %w", err)
	}
	if err := f.Truncate(0); err == nil {
		_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	}
	return &Lock{file: f}, nil
}

func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	errUnlock := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	errClose := l.file.Close()
	if errUnlock != nil {
		return errUnlock
	}
	return errClose
}
