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
