package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AlexKris/sidervia/internal/clientauth"
	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/gateway"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/provider/anthropic"
	"github.com/AlexKris/sidervia/internal/provider/google"
	"github.com/AlexKris/sidervia/internal/provider/openai"
	"github.com/AlexKris/sidervia/internal/provider/xai"
	"github.com/AlexKris/sidervia/internal/routing"
	"github.com/AlexKris/sidervia/internal/store"
	"github.com/AlexKris/sidervia/internal/usage"
)

type integrationTransport struct {
	contentType string
	body        string
	native      provider.NativeRequest
	candidate   routing.Candidate
	calls       int
}

func (t *integrationTransport) Do(_ context.Context, candidate routing.Candidate, native provider.NativeRequest, _ provider.Adapter) (*http.Response, error) {
	t.calls++
	t.native = native
	t.candidate = candidate
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {t.contentType}},
		Body: io.NopCloser(strings.NewReader(t.body)),
	}, nil
}

type integrationRecorder struct{ events []usage.Event }

func (r *integrationRecorder) Enqueue(_ context.Context, event usage.Event) error {
	r.events = append(r.events, event)
	return nil
}

func TestFourProviderNativeJSONPaths(t *testing.T) {
	tests := []struct {
		name, providerID, protocol, publicPath, authHeader, authPrefix, requestBody, responseBody, nativePath string
	}{
		{
			name: "OpenAI", providerID: "openai", protocol: "openai", publicPath: "/v1/chat/completions",
			authHeader: "Authorization", authPrefix: "Bearer ", requestBody: `{"model":"public-model","messages":[]}`,
			responseBody: `{"id":"chat","model":"upstream-model","unknown":true}`, nativePath: "/v1/chat/completions",
		},
		{
			name: "Anthropic", providerID: "anthropic", protocol: "anthropic", publicPath: "/v1/messages",
			authHeader: "X-Api-Key", requestBody: `{"model":"public-model","max_tokens":10,"messages":[]}`,
			responseBody: `{"id":"msg","model":"upstream-model","type":"message","unknown":true}`, nativePath: "/v1/messages",
		},
		{
			name: "Gemini", providerID: "google", protocol: "gemini", publicPath: "/v1beta/models/public-model:generateContent",
			authHeader: "X-Goog-Api-Key", requestBody: `{"contents":[{"parts":[{"text":"hello"}]}]}`,
			responseBody: `{"candidates":[],"unknown":true}`, nativePath: "/v1beta/models/upstream-model:generateContent",
		},
		{
			name: "xAI through OpenAI ingress", providerID: "xai", protocol: "xai", publicPath: "/v1/chat/completions",
			authHeader: "Authorization", authPrefix: "Bearer ", requestBody: `{"model":"public-model","messages":[]}`,
			responseBody: `{"id":"chat","model":"upstream-model","unknown":true}`, nativePath: "/v1/chat/completions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPublicGatewayFixture(t, test.providerID, test.protocol, false, "application/json", test.responseBody)
			defer fixture.database.Close()
			request := httptest.NewRequest(http.MethodPost, test.publicPath, strings.NewReader(test.requestBody))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(test.authHeader, test.authPrefix+fixture.clientKey)
			request.Header.Set("Cookie", "downstream-cookie-must-not-pass")
			request.Header.Set("X-Client-Version", "official-looking-value-must-not-pass")
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if fixture.transport.native.Path != test.nativePath || fixture.transport.calls != 1 {
				t.Fatalf("native request=%#v calls=%d", fixture.transport.native, fixture.transport.calls)
			}
			for _, forbidden := range []string{"Authorization", "X-Api-Key", "X-Goog-Api-Key", "Cookie", "X-Client-Version", "User-Agent"} {
				if fixture.transport.native.Header.Get(forbidden) != "" {
					t.Fatalf("downstream header %s reached native transport", forbidden)
				}
			}
			if test.providerID != "google" {
				var body map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body["model"] != "public-model" || body["unknown"] != true {
					t.Fatalf("response=%#v", body)
				}
			}
			if len(fixture.recorder.events) != 1 || fixture.recorder.events[0].ProviderID != test.providerID {
				t.Fatalf("request events=%#v", fixture.recorder.events)
			}
		})
	}
}

func TestFourProviderNativeSSEPaths(t *testing.T) {
	tests := []struct {
		name, providerID, protocol, path, authHeader, authPrefix, requestBody string
	}{
		{"OpenAI", "openai", "openai", "/v1/chat/completions", "Authorization", "Bearer ", `{"model":"public-model","messages":[],"stream":true}`},
		{"Anthropic", "anthropic", "anthropic", "/v1/messages", "X-Api-Key", "", `{"model":"public-model","max_tokens":10,"messages":[],"stream":true}`},
		{"Gemini", "google", "gemini", "/v1beta/models/public-model:streamGenerateContent", "X-Goog-Api-Key", "", `{"contents":[]}`},
		{"xAI", "xai", "xai", "/v1/chat/completions", "Authorization", "Bearer ", `{"model":"public-model","messages":[],"stream":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			streamBody := "data: {\"model\":\"upstream-model\",\"unknown_event_field\":true}\n\n"
			if test.providerID == "google" {
				streamBody = "data: {\"candidates\":[],\"unknown_event_field\":true}\n\n"
			}
			fixture := newPublicGatewayFixture(t, test.providerID, test.protocol, true, "text/event-stream", streamBody)
			defer fixture.database.Close()
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.requestBody))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(test.authHeader, test.authPrefix+fixture.clientKey)
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("status/headers/body=%d %#v %s", response.Code, response.Header(), response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"unknown_event_field":true`) {
				t.Fatalf("unknown SSE event field was lost: %s", response.Body.String())
			}
			if test.providerID != "google" && !strings.Contains(response.Body.String(), `"model":"public-model"`) {
				t.Fatalf("stream model was not rewritten: %s", response.Body.String())
			}
			if len(fixture.recorder.events) != 1 || !fixture.recorder.events[0].Streamed {
				t.Fatalf("events=%#v", fixture.recorder.events)
			}
		})
	}
}

func TestPublicModelListRequiresClientKeyAndUsesActiveRoutes(t *testing.T) {
	fixture := newPublicGatewayFixture(t, "openai", "openai", false, "application/json", `{}`)
	defer fixture.database.Close()
	unauthorized := httptest.NewRecorder()
	fixture.handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.clientKey)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"public-model"`) {
		t.Fatalf("status/body=%d %s", response.Code, response.Body.String())
	}
	if len(fixture.recorder.events) != 1 || fixture.recorder.events[0].EndpointKind != "models" || fixture.recorder.events[0].ResponseBytes == 0 {
		t.Fatalf("model-list event=%#v", fixture.recorder.events)
	}
}

func TestInboundRequestIDCannotCollapseUsageRecords(t *testing.T) {
	fixture := newPublicGatewayFixture(t, "openai", "openai", false, "application/json", `{}`)
	defer fixture.database.Close()
	responseIDs := make([]string, 0, 2)
	for range 2 {
		request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		request.Header.Set("Authorization", "Bearer "+fixture.clientKey)
		request.Header.Set("X-Request-ID", "reused-client-request-id")
		response := httptest.NewRecorder()
		fixture.handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status/body=%d %s", response.Code, response.Body.String())
		}
		responseIDs = append(responseIDs, response.Header().Get("X-Request-ID"))
	}
	if len(fixture.recorder.events) != 2 || fixture.recorder.events[0].RequestID == fixture.recorder.events[1].RequestID {
		t.Fatalf("request events collapsed: %#v", fixture.recorder.events)
	}
	if responseIDs[0] == "" || responseIDs[0] == responseIDs[1] {
		t.Fatalf("response request IDs = %#v", responseIDs)
	}
}

type publicGatewayFixture struct {
	handler   http.Handler
	database  *store.Store
	transport *integrationTransport
	recorder  *integrationRecorder
	clientKey string
}

func newPublicGatewayFixture(t *testing.T, providerID, protocol string, _ bool, contentType, responseBody string) publicGatewayFixture {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := cryptox.NewCipher(bytes.Repeat([]byte{4}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	ids := identifier.NewGenerator()
	controlService := control.NewService(database.DB(), cipher, clock.Real{}, ids)
	upstream, err := controlService.CreateUpstream(ctx, control.Actor{}, control.UpstreamInput{
		ProviderID: providerID, Name: "Provider", BaseURL: "https://provider.example", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret := "provider-secret"
	account, err := controlService.CreateAccount(ctx, control.Actor{}, control.AccountInput{
		UpstreamID: upstream.ID, Name: "Account", AuthKind: "api_key", Credential: &secret,
		BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	validating, err := controlService.BeginAccountValidation(ctx, control.Actor{}, account.ID, account.Version)
	if err != nil {
		t.Fatal(err)
	}
	account, err = controlService.FinishAccountValidation(ctx, control.Actor{}, account.ID, validating.Version, &control.AccountValidation{
		Identity:          map[string]any{"provider_id": providerID, "model_count": 1},
		CapabilityVersion: providerID + "/0.2.0@2026-07-17", Models: []string{"upstream-model"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	capabilities := []string{"text", "stream"}
	if _, err := controlService.CreateModelRoute(ctx, control.Actor{}, control.ModelRouteInput{
		PublicModelID: "public-model", Enabled: true, Candidates: []control.RouteCandidate{{
			AccountID: account.ID, UpstreamModelID: "upstream-model", Enabled: true,
			Protocols: []string{protocol}, Capabilities: capabilities,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	createdKey, err := controlService.CreateClientKey(ctx, control.Actor{}, "Client", nil)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := provider.NewRegistry(openai.New(), anthropic.New(), google.New(), xai.New())
	if err != nil {
		t.Fatal(err)
	}
	routingService := routing.New(database.DB(), cipher, clock.Real{})
	transport := &integrationTransport{contentType: contentType, body: responseBody}
	recorder := &integrationRecorder{}
	gatewayService := gateway.New(gateway.Options{
		Router: routingService, Providers: registry, Transport: transport, Recorder: recorder,
		Clock: clock.Real{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	server := New(Options{
		ClientAuth: clientauth.New(database.DB(), clock.Real{}), Control: controlService,
		Gateway: gatewayService, Routing: routingService, Store: database,
		UsageRecorder: recorder,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)), IDs: ids,
	})
	return publicGatewayFixture{
		handler: server.Handler(), database: database, transport: transport, recorder: recorder,
		clientKey: createdKey.Secret,
	}
}
