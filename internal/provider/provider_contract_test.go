package provider_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/AlexKris/sidervia/internal/nativecodec"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/provider/anthropic"
	"github.com/AlexKris/sidervia/internal/provider/google"
	"github.com/AlexKris/sidervia/internal/provider/openai"
	"github.com/AlexKris/sidervia/internal/provider/xai"
)

func TestOfficialProviderNativeContracts(t *testing.T) {
	tests := []struct {
		name         string
		adapter      provider.Adapter
		endpoint     provider.Endpoint
		body         string
		model        string
		path         string
		stream       bool
		authHeader   string
		authPrefix   string
		credential   func(t *testing.T) provider.Credential
		requestHeads http.Header
	}{
		{
			name: "openai", adapter: openai.New(), endpoint: provider.EndpointChatCompletions,
			body: `{"model":"public","messages":[]}`, model: "gpt-upstream", path: "/v1/chat/completions",
			authHeader: "Authorization", authPrefix: "Bearer ", credential: apiKey,
		},
		{
			name: "anthropic", adapter: anthropic.New(), endpoint: provider.EndpointMessages,
			body:  `{"model":"public","max_tokens":10,"messages":[],"stream":true}`,
			model: "claude-upstream", path: "/v1/messages", stream: true,
			authHeader: "X-Api-Key", credential: apiKey,
			requestHeads: http.Header{"Anthropic-Version": {"2023-06-01"}},
		},
		{
			name: "google API key", adapter: google.New(), endpoint: provider.EndpointGenerateContent,
			body: `{"contents":[{"parts":[{"text":"hello"}]}]}`, model: "gemini-upstream",
			path: "/v1beta/models/gemini-upstream:generateContent", authHeader: "X-Goog-Api-Key", credential: apiKey,
		},
		{
			name: "google OAuth", adapter: google.New(), endpoint: provider.EndpointStreamGenerateContent,
			body: `{"contents":[]}`, model: "gemini-stream", path: "/v1beta/models/gemini-stream:streamGenerateContent",
			stream: true, authHeader: "Authorization", authPrefix: "Bearer ", credential: oauthToken,
		},
		{
			name: "xai", adapter: xai.New(), endpoint: provider.EndpointChatCompletions,
			body: `{"model":"public","messages":[]}`, model: "grok-upstream", path: "/v1/chat/completions",
			authHeader: "Authorization", authPrefix: "Bearer ", credential: apiKey,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor := test.adapter.Descriptor()
			if descriptor.Version != "0.2.0" || descriptor.VerifiedAgainst != "2026-07-17" {
				t.Fatalf("descriptor = %#v", descriptor)
			}
			native, err := test.adapter.Prepare(provider.PrepareInput{
				Endpoint: test.endpoint, Body: []byte(test.body), UpstreamModel: test.model, Headers: test.requestHeads,
			})
			if err != nil {
				t.Fatal(err)
			}
			if native.Method != http.MethodPost || native.Path != test.path || native.Stream != test.stream {
				t.Fatalf("native request = %#v", native)
			}
			if test.name == "google OAuth" && native.Query.Get("alt") != "sse" {
				t.Fatalf("stream query = %v", native.Query)
			}
			if strings.Contains(string(native.Body), `"model":"public"`) {
				t.Fatalf("public model leaked upstream: %s", native.Body)
			}
			request, err := http.NewRequest(native.Method, "https://provider.example"+native.Path, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header = native.Header.Clone()
			if err := test.adapter.Authorize(request, test.credential(t)); err != nil {
				t.Fatal(err)
			}
			if got := request.Header.Get(test.authHeader); !strings.HasPrefix(got, test.authPrefix) || !strings.Contains(got, "provider-secret") {
				t.Fatalf("authorization header %s = %q", test.authHeader, got)
			}
			if test.name == "google OAuth" && request.Header.Get("X-Goog-User-Project") != "project-test" {
				t.Fatalf("Google project header = %q", request.Header.Get("X-Goog-User-Project"))
			}
			assertOnlyControlledHeaders(t, request.Header, test.authHeader)
		})
	}
}

func TestOfficialAdaptersRejectUnknownTopLevelRequestFields(t *testing.T) {
	tests := []struct {
		adapter  provider.Adapter
		endpoint provider.Endpoint
		body     string
	}{
		{openai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[],"future_unknown":{"secret":true}}`},
		{anthropic.New(), provider.EndpointMessages, `{"model":"m","max_tokens":1,"messages":[],"future_unknown":true}`},
		{google.New(), provider.EndpointGenerateContent, `{"contents":[],"future_unknown":true}`},
		{xai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[],"future_unknown":true}`},
	}
	for _, test := range tests {
		_, err := test.adapter.Prepare(provider.PrepareInput{
			Endpoint: test.endpoint, Body: []byte(test.body), UpstreamModel: "upstream-model",
		})
		if !nativecodec.IsCode(err, "unknown_request_field") {
			t.Fatalf("adapter %s error = %v", test.adapter.Descriptor().ID, err)
		}
	}
}

func TestOfficialAdaptersRejectFieldsOutsideV02TextBoundary(t *testing.T) {
	tests := []struct {
		name     string
		adapter  provider.Adapter
		endpoint provider.Endpoint
		body     string
	}{
		{"OpenAI metadata", openai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[],"metadata":{"client":"official-cli"}}`},
		{"OpenAI nested status", openai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[{"role":"user","content":"hello","status":"completed"}]}`},
		{"Anthropic metadata", anthropic.New(), provider.EndpointMessages, `{"model":"m","max_tokens":1,"messages":[],"metadata":{"user_id":"client"}}`},
		{"Anthropic nested identifier", anthropic.New(), provider.EndpointMessages, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"hello","client_id":"cli"}]}`},
		{"Gemini tools", google.New(), provider.EndpointGenerateContent, `{"contents":[],"tools":[{"functionDeclarations":[]}]}`},
		{"Gemini nested status", google.New(), provider.EndpointGenerateContent, `{"contents":[{"parts":[{"text":"hello","status":"complete"}]}]}`},
		{"xAI user identifier", xai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[],"user":"official-cli"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.adapter.Prepare(provider.PrepareInput{
				Endpoint: test.endpoint, Body: []byte(test.body), UpstreamModel: "upstream-model",
			})
			if !nativecodec.IsCode(err, "unknown_request_field") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestOfficialAdaptersAcceptTextOnlyContentBlocks(t *testing.T) {
	tests := []struct {
		adapter  provider.Adapter
		endpoint provider.Endpoint
		body     string
	}{
		{openai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"temperature":0.2,"max_tokens":100,"stop":["END"]}`},
		{anthropic.New(), provider.EndpointMessages, `{"model":"m","max_tokens":100,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"top_k":40,"stop_sequences":["END"]}`},
		{google.New(), provider.EndpointGenerateContent, `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"temperature":0.2,"maxOutputTokens":100,"stopSequences":["END"]},"safetySettings":[{"category":"HARM_CATEGORY_HATE_SPEECH","threshold":"BLOCK_MEDIUM_AND_ABOVE"}]}`},
		{xai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"seed":7,"stream":false}`},
	}
	for _, test := range tests {
		if _, err := test.adapter.Prepare(provider.PrepareInput{
			Endpoint: test.endpoint, Body: []byte(test.body), UpstreamModel: "upstream-model",
		}); err != nil {
			t.Fatalf("adapter %s: %v", test.adapter.Descriptor().ID, err)
		}
	}
}

func TestOfficialAdaptersRejectObjectsHiddenInScalarControls(t *testing.T) {
	tests := []struct {
		name     string
		adapter  provider.Adapter
		endpoint provider.Endpoint
		body     string
	}{
		{"OpenAI temperature object", openai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[],"temperature":{"status":"official-client"}}`},
		{"OpenAI stop object", openai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[],"stop":[{"client_id":"official-client"}]}`},
		{"Anthropic top_k string", anthropic.New(), provider.EndpointMessages, `{"model":"m","max_tokens":1,"messages":[],"top_k":"40"}`},
		{"Anthropic stop object", anthropic.New(), provider.EndpointMessages, `{"model":"m","max_tokens":1,"messages":[],"stop_sequences":[{"status":"official-client"}]}`},
		{"Gemini generation control object", google.New(), provider.EndpointGenerateContent, `{"contents":[],"generationConfig":{"temperature":{"status":"official-client"}}}`},
		{"Gemini safety control object", google.New(), provider.EndpointGenerateContent, `{"contents":[],"safetySettings":[{"category":{"client_id":"official-client"},"threshold":"BLOCK_NONE"}]}`},
		{"xAI stream string", xai.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[],"stream":"true"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.adapter.Prepare(provider.PrepareInput{
				Endpoint: test.endpoint, Body: []byte(test.body), UpstreamModel: "upstream-model",
			})
			if !nativecodec.IsCode(err, "invalid_request") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAnthropicAdapterRejectsUnverifiedBetaHeader(t *testing.T) {
	_, err := anthropic.New().Prepare(provider.PrepareInput{
		Endpoint:      provider.EndpointMessages,
		Body:          []byte(`{"model":"m","max_tokens":1,"messages":[]}`),
		UpstreamModel: "upstream-model",
		Headers:       http.Header{"Anthropic-Beta": {"feature-test"}},
	})
	if !nativecodec.IsCode(err, "capability_not_supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnthropicAdapterRejectsUnverifiedProtocolVersion(t *testing.T) {
	_, err := anthropic.New().Prepare(provider.PrepareInput{
		Endpoint:      provider.EndpointMessages,
		Body:          []byte(`{"model":"m","max_tokens":1,"messages":[]}`),
		UpstreamModel: "upstream-model",
		Headers:       http.Header{"Anthropic-Version": {"2027-01-01"}},
	})
	if !nativecodec.IsCode(err, "capability_not_supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestOfficialAdaptersReturnCapabilityErrorForWrongEndpoint(t *testing.T) {
	tests := []struct {
		adapter  provider.Adapter
		endpoint provider.Endpoint
		body     string
	}{
		{openai.New(), provider.EndpointMessages, `{"model":"m","messages":[]}`},
		{anthropic.New(), provider.EndpointChatCompletions, `{"model":"m","messages":[]}`},
		{google.New(), provider.EndpointChatCompletions, `{"contents":[]}`},
		{xai.New(), provider.EndpointMessages, `{"model":"m","messages":[]}`},
	}
	for _, test := range tests {
		_, err := test.adapter.Prepare(provider.PrepareInput{
			Endpoint: test.endpoint, Body: []byte(test.body), UpstreamModel: "upstream-model",
		})
		if !nativecodec.IsCode(err, "capability_not_supported") {
			t.Fatalf("adapter %s error = %v", test.adapter.Descriptor().ID, err)
		}
	}
}

func TestNativeResponsePreservesUnknownFieldsSemantically(t *testing.T) {
	input := []byte(`{"id":"upstream","model":"provider-model","unknown":{"array":[1,null,"x"]},"usage":{"prompt_tokens":1}}`)
	output, err := nativecodec.RewriteProviderResponse("openai", input, "public-model")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["model"] != "public-model" || decoded["unknown"].(map[string]any)["array"] == nil {
		t.Fatalf("rewritten response = %#v", decoded)
	}
}

func apiKey(t *testing.T) provider.Credential {
	t.Helper()
	credential, err := provider.NewAPIKey("provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	return credential
}

func oauthToken(t *testing.T) provider.Credential {
	t.Helper()
	credential, err := provider.NewOAuthToken("provider-secret", "project-test")
	if err != nil {
		t.Fatal(err)
	}
	return credential
}

func assertOnlyControlledHeaders(t *testing.T, header http.Header, authHeader string) {
	t.Helper()
	allowed := map[string]bool{
		"Accept": true, "Content-Type": true, "Anthropic-Version": true,
		http.CanonicalHeaderKey(authHeader): true, "X-Goog-User-Project": true,
	}
	for name := range header {
		if !allowed[http.CanonicalHeaderKey(name)] {
			t.Fatalf("unexpected upstream header %q", name)
		}
	}
}
