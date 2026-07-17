package routing

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/store"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestSchedulerFiltersCapacityBeforePriorityAndRoundRobins(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cipher, err := cryptox.NewCipher(bytes.Repeat([]byte{5}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	controlService := control.NewService(database.DB(), cipher, fixedClock{now: now}, identifier.NewGenerator())
	upstream, err := controlService.CreateUpstream(ctx, control.Actor{}, control.UpstreamInput{
		ProviderID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.example/v1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	keyA, keyB := "key-a-canary", "key-b-canary"
	priorityA, priorityB, concurrency := 10, 20, 1
	accountA, err := controlService.CreateAccount(ctx, control.Actor{}, control.AccountInput{
		UpstreamID: upstream.ID, Name: "A", Credential: &keyA, BillingKind: "metered", Status: "draft",
		Priority: &priorityA, MaxConcurrency: &concurrency,
	})
	if err != nil {
		t.Fatal(err)
	}
	accountB, err := controlService.CreateAccount(ctx, control.Actor{}, control.AccountInput{
		UpstreamID: upstream.ID, Name: "B", Credential: &keyB, BillingKind: "metered", Status: "draft",
		Priority: &priorityB, MaxConcurrency: &concurrency,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB().Exec("UPDATE accounts SET status='active'"); err != nil {
		t.Fatal(err)
	}
	_, err = controlService.CreateModelRoute(ctx, control.Actor{}, control.ModelRouteInput{
		PublicModelID: "public-model", Enabled: true, ConfirmMultipleCandidates: true,
		Candidates: []control.RouteCandidate{
			{AccountID: accountA.ID, UpstreamModelID: "upstream-a", Enabled: true, Protocols: []string{"openai"}, Capabilities: []string{"text", "stream"}},
			{AccountID: accountB.ID, UpstreamModelID: "upstream-b", Enabled: true, Protocols: []string{"openai"}, Capabilities: []string{"text", "stream"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	scheduler := New(database.DB(), cipher, fixedClock{now: now})
	request := Request{PublicModelID: "public-model", Protocol: "openai", RequiredCapabilities: []string{"text"}}
	first, _, err := scheduler.Plan(ctx, request)
	if err != nil || first.Candidate.AccountPublicID != accountA.ID {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, explanation, err := scheduler.Plan(ctx, request)
	if err != nil || second.Candidate.AccountPublicID != accountB.ID || explanation.Filtered["all_accounts_saturated"] != 1 {
		t.Fatalf("second=%+v explanation=%+v err=%v", second, explanation, err)
	}
	if _, _, err := scheduler.Plan(ctx, request); !routeErrorIs(err, "all_accounts_saturated") {
		t.Fatalf("expected saturation, got %v", err)
	}
	first.Release()
	second.Release()

	if _, err := database.DB().Exec("UPDATE accounts SET priority=10 WHERE public_id=?", accountB.ID); err != nil {
		t.Fatal(err)
	}
	one, _, err := scheduler.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	one.Release()
	two, _, err := scheduler.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	two.Release()
	if one.Candidate.AccountPublicID == two.Candidate.AccountPublicID {
		t.Fatalf("round-robin selected %s twice", one.Candidate.AccountPublicID)
	}
}

func TestSchedulerCapabilityAndCooldownReasons(t *testing.T) {
	scheduler, database, cipher, accountID := singleCandidateScheduler(t)
	defer database.Close()
	ctx := context.Background()
	_, _, err := scheduler.Plan(ctx, Request{PublicModelID: "model", Protocol: "openai", RequiredCapabilities: []string{"tools"}})
	if !routeErrorIs(err, "capability_not_supported") {
		t.Fatalf("capability error=%v", err)
	}
	lease, _, err := scheduler.Plan(ctx, Request{PublicModelID: "model", Protocol: "openai", RequiredCapabilities: []string{"text"}})
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if err := scheduler.RecordFailure(ctx, lease.Candidate, Failure{StatusCode: 429, Code: "rate_limit", RetryAfter: time.Minute}); err != nil {
		t.Fatal(err)
	}
	_, _, err = scheduler.Plan(ctx, Request{PublicModelID: "model", Protocol: "openai", RequiredCapabilities: []string{"text"}})
	if !routeErrorIs(err, "quota_limited") {
		t.Fatalf("quota error=%v", err)
	}
	var quotaError *RouteError
	if !errors.As(err, &quotaError) || quotaError.RetryAfter != time.Minute {
		t.Fatalf("quota retry-after=%v", quotaError)
	}
	if err := scheduler.RecordSuccess(ctx, accountID); err != nil {
		t.Fatal(err)
	}
	var streak int
	if err := database.DB().QueryRow("SELECT failure_streak FROM account_runtime WHERE account_id=?", accountID).Scan(&streak); err != nil || streak != 0 {
		t.Fatalf("streak=%d err=%v", streak, err)
	}
	_ = cipher
}

func singleCandidateScheduler(t *testing.T) (*Service, *store.Store, *cryptox.Cipher, int64) {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := cryptox.NewCipher(bytes.Repeat([]byte{6}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	controlService := control.NewService(database.DB(), cipher, fixedClock{now: now}, identifier.NewGenerator())
	upstream, err := controlService.CreateUpstream(ctx, control.Actor{}, control.UpstreamInput{
		ProviderID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.example/v1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "key-canary"
	account, err := controlService.CreateAccount(ctx, control.Actor{}, control.AccountInput{
		UpstreamID: upstream.ID, Name: "A", Credential: &key, BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	var internalID int64
	if err := database.DB().QueryRow("UPDATE accounts SET status='active' WHERE public_id=? RETURNING id", account.ID).Scan(&internalID); err != nil {
		t.Fatal(err)
	}
	_, err = controlService.CreateModelRoute(ctx, control.Actor{}, control.ModelRouteInput{
		PublicModelID: "model", Enabled: true,
		Candidates: []control.RouteCandidate{{AccountID: account.ID, UpstreamModelID: "model", Enabled: true, Protocols: []string{"openai"}, Capabilities: []string{"text"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return New(database.DB(), cipher, fixedClock{now: now}), database, cipher, internalID
}

func routeErrorIs(err error, code string) bool {
	var routeErr *RouteError
	return errors.As(err, &routeErr) && routeErr.Code == code
}
