package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenReadOnlyDoesNotMigrate(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	writable, err := Open(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}
	readOnly, err := OpenReadOnly(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	version, err := readOnly.SchemaVersion(ctx)
	if err != nil || version != LatestSchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	if _, err := readOnly.DB().ExecContext(ctx, "CREATE TABLE forbidden(value TEXT)"); err == nil {
		t.Fatal("read-only store accepted a write")
	}
}

func TestOpenReadOnlyRejectsDatabaseSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, "sidervia.db")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := OpenReadOnly(context.Background(), directory); err == nil {
		t.Fatal("expected symlink rejection")
	}
}
