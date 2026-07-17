package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/store"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func newTestService(t *testing.T) (*Service, *store.Store, *fakeClock, string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{9}, cryptox.KeySize)
	cipher, err := cryptox.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.VerifyOrCreateSentinel(ctx, cipher); err != nil {
		t.Fatal(err)
	}
	c := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	hasher := testHasher()
	svc := NewService(db.DB(), cipher, c, identifier.NewGenerator(), hasher, key, "admin@example.test")
	password := "correct horse battery staple"
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte(password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := svc.BootstrapFromFile(ctx, path)
	if err != nil || !created {
		t.Fatalf("bootstrap created=%v err=%v", created, err)
	}
	return svc, db, c, password
}

func TestLoginSessionAndCSRF(t *testing.T) {
	svc, db, _, password := newTestService(t)
	defer db.Close()
	ctx := context.Background()
	session, err := svc.Login(ctx, password, "", "192.0.2.4", "test")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.Authenticate(ctx, session.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateCSRF(loaded, session.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateCSRF(loaded, "wrong"); !errors.Is(err, ErrInvalidCSRF) {
		t.Fatalf("expected CSRF error, got %v", err)
	}
	if err := svc.Logout(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestTOTPReplayAndRecovery(t *testing.T) {
	svc, db, c, password := newTestService(t)
	defer db.Close()
	ctx := context.Background()
	session, err := svc.Login(ctx, password, "", "192.0.2.4", "test")
	if err != nil {
		t.Fatal(err)
	}
	setup, err := svc.SetupTOTP(ctx, session, password)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := TOTPCode(setup.Secret, c.now)
	rotated, err := svc.ConfirmTOTP(ctx, session, code, "192.0.2.4", "test")
	if err != nil || !rotated.TOTPEnabled {
		t.Fatalf("confirm: %+v %v", rotated, err)
	}
	if _, err := svc.Login(ctx, password, code, "192.0.2.5", "test"); !errors.Is(err, ErrTOTPReplay) {
		t.Fatalf("expected replay error, got %v", err)
	}
	c.now = c.now.Add(30 * time.Second)
	nextCode, _ := TOTPCode(setup.Secret, c.now)
	if _, err := svc.Login(ctx, password, nextCode, "192.0.2.6", "test"); err != nil {
		t.Fatalf("next TOTP login: %v", err)
	}
}

func TestConcurrentTOTPConfirmationOnlySucceedsOnce(t *testing.T) {
	svc, db, c, password := newTestService(t)
	defer db.Close()
	ctx := context.Background()
	session, err := svc.Login(ctx, password, "", "192.0.2.4", "test")
	if err != nil {
		t.Fatal(err)
	}
	setup, err := svc.SetupTOTP(ctx, session, password)
	if err != nil {
		t.Fatal(err)
	}
	code, err := TOTPCode(setup.Secret, c.now)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, confirmErr := svc.ConfirmTOTP(ctx, session, code, "192.0.2.4", "test")
			results <- confirmErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	succeeded, rejected := 0, 0
	for result := range results {
		switch {
		case result == nil:
			succeeded++
		case errors.Is(result, ErrTOTPNotPending):
			rejected++
		default:
			t.Fatalf("unexpected concurrent confirmation error: %v", result)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("concurrent confirmations succeeded=%d rejected=%d, want 1 and 1", succeeded, rejected)
	}
}

func TestConcurrentLoginFailuresIncrementAtomically(t *testing.T) {
	svc, db, c, _ := newTestService(t)
	defer db.Close()

	const attempts = 8
	start := make(chan struct{})
	errors := make(chan error, attempts)
	var wait sync.WaitGroup
	for range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errors <- svc.recordFailure(context.Background(), c.now)
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("record failure: %v", err)
		}
	}

	var count int
	var lockedUntil sql.NullInt64
	if err := db.DB().QueryRow("SELECT failed_login_count, locked_until_ms FROM admin_user WHERE id = 1").Scan(&count, &lockedUntil); err != nil {
		t.Fatal(err)
	}
	if count != attempts {
		t.Fatalf("failed login count = %d, want %d", count, attempts)
	}
	if !lockedUntil.Valid || lockedUntil.Int64 <= c.now.UnixMilli() {
		t.Fatalf("locked_until_ms = %v, want a future lock", lockedUntil)
	}
	var failedAudits int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM audit_events WHERE event_type = 'admin.login_failed' AND outcome = 'failure'").Scan(&failedAudits); err != nil {
		t.Fatal(err)
	}
	if failedAudits != attempts {
		t.Fatalf("failure audit count = %d, want %d", failedAudits, attempts)
	}
}

func TestAttemptLimiterEnforcesPerIPAndGlobalLimits(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	perIP := newAttemptLimiter()
	for range perIPMinute {
		if !perIP.Allow("192.0.2.10", now) {
			t.Fatal("per-IP limiter rejected an allowed attempt")
		}
	}
	if perIP.Allow("192.0.2.10", now) {
		t.Fatal("per-IP limiter allowed an attempt above the minute limit")
	}

	global := newAttemptLimiter()
	for index := range globalMinute {
		if !global.Allow(fmt.Sprintf("192.0.2.%d", index+1), now) {
			t.Fatal("global limiter rejected an allowed attempt")
		}
	}
	if global.Allow("198.51.100.1", now) {
		t.Fatal("global limiter allowed an attempt above the minute limit")
	}
}

func TestAttemptLimiterRemovesExpiredKeys(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limiter := newAttemptLimiter()
	if !limiter.Allow("192.0.2.1", now) {
		t.Fatal("initial attempt was rejected")
	}
	if !limiter.Allow("192.0.2.2", now.Add(2*time.Hour)) {
		t.Fatal("attempt after expiry was rejected")
	}
	if _, exists := limiter.attempts["ip:192.0.2.1"]; exists {
		t.Fatal("expired IP key was retained")
	}
}

func TestRejectedIPAttemptsDoNotConsumeGlobalLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	limiter := newAttemptLimiter()
	for range perIPMinute {
		if !limiter.Allow("192.0.2.10", now) {
			t.Fatal("allowed per-IP attempt was rejected")
		}
	}
	for range globalMinute {
		if limiter.Allow("192.0.2.10", now) {
			t.Fatal("over-limit per-IP attempt was allowed")
		}
	}
	if !limiter.Allow("198.51.100.20", now) {
		t.Fatal("rejected attempts from one IP consumed the global allowance")
	}
}

func TestLoginPasswordWorkIsConcurrencyBounded(t *testing.T) {
	svc, db, c, password := newTestService(t)
	defer db.Close()
	for range passwordWorkLimit {
		if !svc.acquirePasswordWork() {
			t.Fatal("password work slot was unexpectedly unavailable")
		}
	}
	defer func() {
		for range passwordWorkLimit {
			svc.releasePasswordWork()
		}
	}()
	if svc.acquirePasswordWork() {
		svc.releasePasswordWork()
		t.Fatal("password work exceeded its concurrency bound")
	}
	if _, err := svc.Login(context.Background(), password, "", "192.0.2.20", "test"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("login with saturated password work returned %v", err)
	}
	if len(svc.limiter.attempts) != 0 {
		t.Fatalf("slot-rejected login consumed time-window allowance at %v", c.now)
	}
}
