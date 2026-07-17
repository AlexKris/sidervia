//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd

package processlock

import "errors"

var ErrLocked = errors.New("data directory is already locked")

type Lock struct{}

func Acquire(string) (*Lock, error) {
	return nil, errors.New("process locking is unsupported on this platform")
}
func (*Lock) Close() error { return nil }
