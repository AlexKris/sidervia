package store

import (
	"bytes"
	"context"
	"testing"

	"github.com/AlexKris/sidervia/internal/cryptox"
)

func TestOpenMigrateAndSentinel(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cipher, _ := cryptox.NewCipher(bytes.Repeat([]byte{1}, cryptox.KeySize))
	if err := s.VerifyOrCreateSentinel(ctx, cipher); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB().QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration count=%d err=%v", count, err)
	}
}

func TestWrongSentinelKeyFails(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, _ := cryptox.NewCipher(bytes.Repeat([]byte{1}, cryptox.KeySize))
	second, _ := cryptox.NewCipher(bytes.Repeat([]byte{2}, cryptox.KeySize))
	if err := s.VerifyOrCreateSentinel(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyOrCreateSentinel(ctx, second); err == nil {
		t.Fatal("expected wrong key failure")
	}
}
