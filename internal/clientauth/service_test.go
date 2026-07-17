package clientauth

import (
	"context"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/store"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestAuthenticateClientKey(t *testing.T) {
	database, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	key, prefix, verifier, err := identifier.NewGenerator().ClientKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	if _, err := database.DB().Exec(`INSERT INTO client_keys(
		public_id, name, prefix, secret_verifier, status, created_at_ms
	) VALUES('sdr_ckey_test', 'test', ?, ?, 'active', ?)`, prefix, verifier, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	service := New(database.DB(), fixedClock{now: now})
	identity, err := service.Authenticate(context.Background(), key)
	if err != nil || identity.PublicID != "sdr_ckey_test" {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
	var lastUsed int64
	if err := database.DB().QueryRow("SELECT last_used_at_ms FROM client_keys WHERE public_id = 'sdr_ckey_test'").Scan(&lastUsed); err != nil || lastUsed != now.UnixMilli() {
		t.Fatalf("last_used=%d err=%v", lastUsed, err)
	}

	for _, invalid := range []string{"", " " + key, key + " ", key[:len(key)-1], "sk-sdr_12345678_invalid"} {
		if _, err := service.Authenticate(context.Background(), invalid); !errorsIsUnauthorized(err) {
			t.Fatalf("invalid key %q returned %v", invalid, err)
		}
	}
}

func TestDisabledAndExpiredKeysFail(t *testing.T) {
	database, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Unix(1_800_000_000, 0).UTC()
	service := New(database.DB(), fixedClock{now: now})
	for index, status := range []string{"disabled", "revoked", "active"} {
		key, prefix, verifier, err := identifier.NewGenerator().ClientKey()
		if err != nil {
			t.Fatal(err)
		}
		expires := any(nil)
		if status == "active" {
			expires = now.Add(-time.Second).UnixMilli()
		}
		if _, err := database.DB().Exec(`INSERT INTO client_keys(
			public_id, name, prefix, secret_verifier, status, created_at_ms, expires_at_ms, revoked_at_ms
		) VALUES(?, 'test', ?, ?, ?, ?, ?, ?)`, "sdr_ckey_test_"+string(rune('a'+index)), prefix, verifier,
			status, now.UnixMilli(), expires, expires); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Authenticate(context.Background(), key); !errorsIsUnauthorized(err) {
			t.Fatalf("status=%s err=%v", status, err)
		}
	}
}

func errorsIsUnauthorized(err error) bool { return err == ErrUnauthorized }
