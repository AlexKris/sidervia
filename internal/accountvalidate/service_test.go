package accountvalidate

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/provider/openai"
	"github.com/AlexKris/sidervia/internal/routing"
	"github.com/AlexKris/sidervia/internal/store"
)

type fakeTransport struct {
	status      int
	body        string
	contentType string
	err         error
}

func (f fakeTransport) Do(context.Context, routing.Candidate, provider.NativeRequest, provider.Adapter) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	contentType := f.contentType
	if contentType == "" {
		contentType = "application/json"
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(bytes.NewBufferString(f.body)), Header: http.Header{"Content-Type": {contentType}}}, nil
}

func TestValidateActivatesAPIKeyAccountAndStoresModels(t *testing.T) {
	service, controlService, database, account := validationFixture(t, fakeTransport{
		status: http.StatusOK, body: `{"data":[{"id":"model-b"},{"id":"model-a"}]}`,
	})
	defer database.Close()
	validated, err := service.Validate(context.Background(), control.Actor{Kind: "admin"}, account.ID, account.Version)
	if err != nil || validated.Status != "active" || validated.Version != account.Version+2 {
		t.Fatalf("account=%+v err=%v", validated, err)
	}
	var count int
	if err := database.DB().QueryRow("SELECT count(*) FROM account_models WHERE account_id=(SELECT id FROM accounts WHERE public_id=?)", account.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("model count=%d err=%v", count, err)
	}
	loaded, err := controlService.GetAccount(context.Background(), account.ID)
	if err != nil || loaded.Status != "active" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestValidationFailureLeavesAccountInvalid(t *testing.T) {
	service, _, database, account := validationFixture(t, fakeTransport{status: http.StatusUnauthorized, body: `{"error":"secret details"}`})
	defer database.Close()
	validated, err := service.Validate(context.Background(), control.Actor{Kind: "admin"}, account.ID, account.Version)
	code, ok := IsValidationError(err)
	if !ok || code != "authentication_failed" || validated.Status != "invalid" {
		t.Fatalf("account=%+v code=%q ok=%v err=%v", validated, code, ok, err)
	}
}

func TestValidationRejectsMalformedSuccessfulModelResponse(t *testing.T) {
	service, _, database, account := validationFixture(t, fakeTransport{status: http.StatusOK, body: `{"data":{}}`})
	defer database.Close()
	validated, err := service.Validate(context.Background(), control.Actor{Kind: "admin"}, account.ID, account.Version)
	code, ok := IsValidationError(err)
	if !ok || code != "upstream_protocol_changed" || validated.Status != "invalid" {
		t.Fatalf("account=%+v code=%q ok=%v err=%v", validated, code, ok, err)
	}
}

func validationFixture(t *testing.T, transport Transport) (*Service, *control.Service, *store.Store, control.Account) {
	t.Helper()
	database, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := cryptox.NewCipher(bytes.Repeat([]byte{8}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	ids := identifier.NewGenerator()
	controlService := control.NewService(database.DB(), cipher, clock.Real{}, ids)
	upstream, err := controlService.CreateUpstream(context.Background(), control.Actor{}, control.UpstreamInput{
		ProviderID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.example/v1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "api-key-canary"
	account, err := controlService.CreateAccount(context.Background(), control.Actor{}, control.AccountInput{
		UpstreamID: upstream.ID, Name: "A", AuthKind: "api_key", Credential: &key,
		BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := provider.NewRegistry(openai.New())
	if err != nil {
		t.Fatal(err)
	}
	router := routing.New(database.DB(), cipher, clock.Real{})
	return New(controlService, router, registry, transport), controlService, database, account
}
