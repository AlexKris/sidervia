package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/gateway"
)

func TestExtractClientKeyUsesOnlyProtocolAuthentication(t *testing.T) {
	const key = "sk-sdr_abcdefgh_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	tests := []struct {
		name     string
		protocol string
		headers  http.Header
		query    string
		want     string
		wantErr  bool
	}{
		{name: "openai bearer", protocol: "openai", headers: http.Header{"Authorization": {"Bearer " + key}}, want: key},
		{name: "anthropic api key", protocol: "anthropic", headers: http.Header{"X-Api-Key": {key}}, want: key},
		{name: "anthropic bearer fallback", protocol: "anthropic", headers: http.Header{"Authorization": {"Bearer " + key}}, want: key},
		{name: "anthropic conflicting methods", protocol: "anthropic", headers: http.Header{"Authorization": {"Bearer " + key}, "X-Api-Key": {key}}, wantErr: true},
		{name: "multiple bearer headers", protocol: "openai", headers: http.Header{"Authorization": {"Bearer " + key, "Bearer " + key}}, wantErr: true},
		{name: "gemini header", protocol: "gemini", headers: http.Header{"X-Goog-Api-Key": {key}}, want: key},
		{name: "gemini query disabled", protocol: "gemini", headers: http.Header{"X-Goog-Api-Key": {key}}, query: "key=" + key, wantErr: true},
		{name: "cookie is not public auth", protocol: "openai", headers: http.Header{"Cookie": {"sidervia_session=admin"}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://sidervia.test/v1/messages?"+test.query, nil)
			request.Header = test.headers.Clone()
			got, err := extractClientKey(request, test.protocol)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("got %q, err %v", got, err)
			}
		})
	}
}

func TestReadGatewayBodyLimitsAndRejectsCompression(t *testing.T) {
	compressed := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	compressed.Header.Set("Content-Type", "application/json")
	compressed.Header.Set("Content-Encoding", "gzip")
	if _, err := readGatewayBody(httptest.NewRecorder(), compressed); err == nil || err.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("compressed body error = %#v", err)
	}

	tooLarge := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"x":"`+strings.Repeat("a", gateway.MaxRequestBody)+`"}`))
	tooLarge.Header.Set("Content-Type", "application/json")
	if _, err := readGatewayBody(httptest.NewRecorder(), tooLarge); err == nil || err.Code != "request_too_large" {
		t.Fatalf("large body error = %#v", err)
	}
}

func TestWritePublicProtocolErrorUsesNativeShapes(t *testing.T) {
	tests := []struct {
		protocol string
		field    string
	}{
		{protocol: "openai", field: "request_id"},
		{protocol: "anthropic", field: "type"},
		{protocol: "gemini", field: "error"},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(context.WithValue(context.Background(), requestIDKey, "req_test"))
			recorder := httptest.NewRecorder()
			writePublicProtocolError(recorder, request, test.protocol, &gateway.Error{
				StatusCode: http.StatusTooManyRequests, Code: "quota_limited", Message: "temporarily limited", RetryAfter: 1500 * time.Millisecond,
			})
			if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "2" {
				t.Fatalf("status/headers = %d %#v", recorder.Code, recorder.Header())
			}
			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if _, exists := body[test.field]; !exists {
				t.Fatalf("body = %#v", body)
			}
		})
	}
}

func TestValidPathModel(t *testing.T) {
	if !validPathModel("gemini-2.5-pro") {
		t.Fatal("expected model to be valid")
	}
	for _, invalid := range []string{"", "models/gemini", "../gemini", "gemini?key=secret", " gemini"} {
		if validPathModel(invalid) {
			t.Fatalf("model %q should be invalid", invalid)
		}
	}
}
