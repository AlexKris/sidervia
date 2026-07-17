package securefile

import (
	"errors"
	"fmt"
	"os"
)

func Read(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect secret file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("secret path must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("secret file must not be accessible by group or other")
	}
	if err := checkOwner(info); err != nil {
		return nil, err
	}
	if info.Size() < 1 || info.Size() > maxBytes {
		return nil, fmt.Errorf("secret file size must be between 1 and %d bytes", maxBytes)
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secret file: %w", err)
	}
	return value, nil
}
