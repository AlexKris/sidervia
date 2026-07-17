package processlock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestExclusiveLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidervia.lock")
	first, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := Acquire(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
}
