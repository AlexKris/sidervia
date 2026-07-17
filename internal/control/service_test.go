package control

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/store"
)

type fixedClock struct{ value time.Time }

func (f fixedClock) Now() time.Time { return f.value }

func newControlService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	db, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{4}, cryptox.KeySize)
	cipher, err := cryptox.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.VerifyOrCreateSentinel(context.Background(), cipher); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.DB(), cipher, fixedClock{value: time.Unix(1_700_000_000, 0)}, identifier.NewGenerator())
	return svc, db
}

func TestControlPlaneGraphAndSecretHandling(t *testing.T) {
	ctx := context.Background()
	svc, db := newControlService(t)
	defer db.Close()
	actor := Actor{Kind: "admin_session", ID: "sdr_sess_test", RequestID: "req-test"}
	username, password := "proxy-user-canary", "proxy-password-canary"
	proxy, err := svc.CreateProxy(ctx, actor, ProxyInput{
		Name: "Primary", Scheme: "https", Host: "proxy.example.com", Port: 443,
		Username: &username, Password: &password, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := svc.CreateUpstream(ctx, actor, UpstreamInput{
		ProviderID: "openai", Name: "OpenAI", BaseURL: "https://api.example.com/v1",
		DefaultProxyID: &proxy.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := "api-key-canary-1234567890"
	account, err := svc.CreateAccount(ctx, actor, AccountInput{
		UpstreamID: upstream.ID, Name: "Account A", Credential: &credential,
		BillingKind: "subscription", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.Priority != 10 || account.MaxConcurrency != 1 || !account.CredentialConfigured {
		t.Fatalf("unexpected account defaults: %+v", account)
	}
	route, err := svc.CreateModelRoute(ctx, actor, ModelRouteInput{
		PublicModelID: "example-model", Enabled: true,
		Candidates: []RouteCandidate{{AccountID: account.ID, UpstreamModelID: "example-model", Enabled: true, Protocols: []string{"openai"}, Capabilities: []string{"text", "stream"}}},
	})
	if err != nil || len(route.Candidates) != 1 {
		t.Fatalf("route=%+v err=%v", route, err)
	}
	createdKey, err := svc.CreateClientKey(ctx, actor, "Local client", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix([]byte(createdKey.Secret), []byte("sk-sdr_")) {
		t.Fatal("client secret has unexpected format")
	}
	if _, err := svc.UpdateProxy(ctx, actor, proxy.ID, proxy.Version+1, ProxyInput{
		Name: proxy.Name, Scheme: proxy.Scheme, Host: proxy.Host, Port: proxy.Port, Enabled: true,
	}); !errors.Is(err, ErrVersion) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	if err := svc.DeleteProxy(ctx, actor, proxy.ID, proxy.Version); !errors.Is(err, ErrResourceInUse) {
		t.Fatalf("expected resource-in-use, got %v", err)
	}
	var auditCount int
	if err := db.DB().QueryRow("SELECT count(*) FROM audit_events").Scan(&auditCount); err != nil || auditCount < 5 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
	for _, path := range []string{db.Path(), db.Path() + "-wal"} {
		body, err := os.ReadFile(path)
		if err != nil && os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{username, password, credential, createdKey.Secret} {
			if bytes.Contains(body, []byte(secret)) {
				t.Fatalf("plaintext secret found in %s", path)
			}
		}
	}
}

func TestRejectPrivateUpstreamWithoutConfirmation(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	_, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "private", BaseURL: "https://127.0.0.1/v1", Enabled: true,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestDashboardReportsGatewayRequestsWithoutFoundationWarning(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	if _, err := db.DB().Exec(`INSERT INTO admin_user(id, password_phc, totp_enabled, created_at_ms, updated_at_ms)
		VALUES(1, 'test', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	dashboard, err := svc.Dashboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Counts["requests"] != 0 || len(dashboard.Warnings) != 0 {
		t.Fatalf("dashboard = %#v", dashboard)
	}
}

func TestRecoverInterruptedAccountValidation(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	upstream, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "OpenAI recovery", BaseURL: "https://api.example.test", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := "credential"
	account, err := svc.CreateAccount(context.Background(), Actor{}, AccountInput{
		UpstreamID: upstream.ID, Name: "Recovery", AuthKind: "api_key", Credential: &credential, BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	validating, err := svc.BeginAccountValidation(context.Background(), Actor{}, account.ID, account.Version)
	if err != nil {
		t.Fatal(err)
	}
	count, err := svc.RecoverInterruptedAccountValidations(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("recovered=%d err=%v", count, err)
	}
	recovered, err := svc.GetAccount(context.Background(), account.ID)
	if err != nil || recovered.Status != "invalid" || recovered.Version != validating.Version+1 {
		t.Fatalf("account=%#v err=%v", recovered, err)
	}
}

func TestOAuthAccountCannotMoveToNonGoogleUpstream(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	googleUpstream, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "google", Name: "Google OAuth", BaseURL: "https://generativelanguage.googleapis.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	openAIUpstream, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "OpenAI target", BaseURL: "https://api.openai.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(context.Background(), Actor{}, AccountInput{
		UpstreamID: googleUpstream.ID, Name: "OAuth account", AuthKind: "oauth", BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateAccount(context.Background(), Actor{}, account.ID, account.Version, AccountInput{
		UpstreamID: openAIUpstream.ID, Name: account.Name, BillingKind: account.BillingKind, Status: "draft",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("OAuth account moved to a non-Google upstream: %v", err)
	}
}

func TestUpstreamProviderCannotChangeAfterCreation(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	upstream, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "Immutable provider", BaseURL: "https://api.openai.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateUpstream(context.Background(), Actor{}, upstream.ID, upstream.Version, UpstreamInput{
		ProviderID: "anthropic", Name: upstream.Name, BaseURL: "https://api.anthropic.com", Enabled: true,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("upstream provider changed after creation: %v", err)
	}
}

func TestRouteRejectsProtocolThatDoesNotMatchAccountProvider(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	upstream, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "OpenAI route", BaseURL: "https://api.openai.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := "credential"
	account, err := svc.CreateAccount(context.Background(), Actor{}, AccountInput{
		UpstreamID: upstream.ID, Name: "OpenAI route account", AuthKind: "api_key", Credential: &credential,
		BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateModelRoute(context.Background(), Actor{}, ModelRouteInput{
		PublicModelID: "public-model", Enabled: true,
		Candidates: []RouteCandidate{{
			AccountID: account.ID, UpstreamModelID: "gpt-model", Enabled: true,
			Protocols: []string{"anthropic"}, Capabilities: []string{"text"},
		}},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("OpenAI account accepted an Anthropic route: %v", err)
	}
}

func TestActiveAccountOperationalUpdatePreservesValidation(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	upstream, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "OpenAI active", BaseURL: "https://api.openai.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := "credential"
	account, err := svc.CreateAccount(context.Background(), Actor{}, AccountInput{
		UpstreamID: upstream.ID, Name: "Active account", AuthKind: "api_key", Credential: &credential,
		BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`UPDATE accounts SET status = 'active' WHERE public_id = ?`, account.ID); err != nil {
		t.Fatal(err)
	}
	account, err = svc.GetAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority, concurrency := 7, 2
	updated, err := svc.UpdateAccount(context.Background(), Actor{}, account.ID, account.Version, AccountInput{
		UpstreamID: account.UpstreamID, Name: "Renamed active account", AuthKind: account.AuthKind,
		BillingKind: "custom", Status: "active", Priority: &priority, MaxConcurrency: &concurrency,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "active" || updated.Priority != priority || updated.MaxConcurrency != concurrency || updated.Name != "Renamed active account" {
		t.Fatalf("updated account = %#v", updated)
	}
}

func TestAccountCannotBypassValidationOrChangeEgressWhileActive(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	first, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "OpenAI first", BaseURL: "https://api.openai.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "OpenAI second", BaseURL: "https://api2.openai.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := "credential"
	account, err := svc.CreateAccount(context.Background(), Actor{}, AccountInput{
		UpstreamID: first.ID, Name: "Validation boundary", AuthKind: "api_key", Credential: &credential,
		BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	priority, concurrency := account.Priority, account.MaxConcurrency
	_, err = svc.UpdateAccount(context.Background(), Actor{}, account.ID, account.Version, AccountInput{
		UpstreamID: first.ID, Name: account.Name, AuthKind: account.AuthKind, BillingKind: account.BillingKind,
		Status: "active", Priority: &priority, MaxConcurrency: &concurrency,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("draft account bypassed validation: %v", err)
	}
	if _, err := db.DB().Exec(`UPDATE accounts SET status = 'active' WHERE public_id = ?`, account.ID); err != nil {
		t.Fatal(err)
	}
	account, err = svc.GetAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateAccount(context.Background(), Actor{}, account.ID, account.Version, AccountInput{
		UpstreamID: second.ID, Name: account.Name, AuthKind: account.AuthKind, BillingKind: account.BillingKind,
		Status: "active", Priority: &priority, MaxConcurrency: &concurrency,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("active account changed egress without revalidation: %v", err)
	}
}

func TestUpdatingAPIKeyIncrementsCredentialVersion(t *testing.T) {
	svc, db := newControlService(t)
	defer db.Close()
	upstream, err := svc.CreateUpstream(context.Background(), Actor{}, UpstreamInput{
		ProviderID: "openai", Name: "OpenAI credential", BaseURL: "https://api.openai.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := "credential-one"
	account, err := svc.CreateAccount(context.Background(), Actor{}, AccountInput{
		UpstreamID: upstream.ID, Name: "Credential version", AuthKind: "api_key", Credential: &credential,
		BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.DB().QueryRow(`SELECT credential_version FROM accounts WHERE public_id = ?`, account.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	replacement := "credential-two"
	priority, concurrency := account.Priority, account.MaxConcurrency
	if _, err := svc.UpdateAccount(context.Background(), Actor{}, account.ID, account.Version, AccountInput{
		UpstreamID: account.UpstreamID, Name: account.Name, AuthKind: account.AuthKind, Credential: &replacement,
		BillingKind: account.BillingKind, Status: "draft", Priority: &priority, MaxConcurrency: &concurrency,
	}); err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := db.DB().QueryRow(`SELECT credential_version FROM accounts WHERE public_id = ?`, account.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("credential version before=%d after=%d", before, after)
	}
}
