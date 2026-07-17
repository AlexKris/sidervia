package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlexKris/sidervia/internal/auth"
	"github.com/AlexKris/sidervia/internal/buildinfo"
	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/store"
)

const testPassword = "correct horse battery staple"

type testAPI struct {
	handler http.Handler
	store   *store.Store
}

func newTestAPI(t *testing.T, assets http.Handler) testAPI {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, cryptox.KeySize)
	cipher, err := cryptox.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.VerifyOrCreateSentinel(ctx, cipher); err != nil {
		t.Fatal(err)
	}
	passwordFile := filepath.Join(t.TempDir(), "bootstrap-password")
	if err := os.WriteFile(passwordFile, []byte(testPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ids := identifier.NewGenerator()
	hasher := auth.PasswordHasher{Params: auth.PasswordParams{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}}
	authService := auth.NewService(database.DB(), cipher, clock.Real{}, ids, hasher, key, "admin@example.test")
	if created, err := authService.BootstrapFromFile(ctx, passwordFile); err != nil || !created {
		t.Fatalf("bootstrap created=%v err=%v", created, err)
	}
	publicURL, _ := url.Parse("https://sidervia.example.test")
	server := New(Options{
		Auth: authService, Control: control.NewService(database.DB(), cipher, clock.Real{}, ids),
		Store: database, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), IDs: ids,
		PublicURL: publicURL, SecureCookie: true, Assets: assets, Build: buildinfo.Current(),
	})
	t.Cleanup(func() { _ = database.Close() })
	return testAPI{handler: server.Handler(), store: database}
}

func TestAuthenticationCSRFAndControlCRUD(t *testing.T) {
	api := newTestAPI(t, nil)
	login := doJSON(t, api.handler, http.MethodPost, "/api/admin/v1/auth/login", `{"password":"`+testPassword+`"}`, "", "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/api/admin/" {
		t.Fatalf("unexpected session cookie: %+v", cookies)
	}
	var session sessionResponse
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil || session.CSRFToken == "" {
		t.Fatalf("session response: %+v err=%v", session, err)
	}

	invalidPassword := doJSON(t, api.handler, http.MethodPut, "/api/admin/v1/auth/password", `{"current_password":"`+testPassword+`","new_password":"short"}`, cookies[0].Value, session.CSRFToken)
	if invalidPassword.Code != http.StatusBadRequest || !strings.Contains(invalidPassword.Body.String(), `"code":"validation_failed"`) {
		t.Fatalf("invalid password response=%d %s", invalidPassword.Code, invalidPassword.Body.String())
	}

	missingCSRF := doJSON(t, api.handler, http.MethodPost, "/api/admin/v1/proxies", `{"name":"proxy","scheme":"https","host":"proxy.example.test","port":443,"allow_insecure_tls":false,"enabled":true}`, cookies[0].Value, "")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("expected CSRF rejection, got %d", missingCSRF.Code)
	}

	created := doJSON(t, api.handler, http.MethodPost, "/api/admin/v1/proxies", `{"name":"proxy","scheme":"https","host":"proxy.example.test","port":443,"username":"canary-user","password":"canary-password","allow_insecure_tls":false,"enabled":true}`, cookies[0].Value, session.CSRFToken)
	if created.Code != http.StatusCreated || created.Header().Get("ETag") != `"v1"` {
		t.Fatalf("create status=%d etag=%q body=%s", created.Code, created.Header().Get("ETag"), created.Body.String())
	}
	var proxy control.Proxy
	if err := json.Unmarshal(created.Body.Bytes(), &proxy); err != nil || proxy.ID == "" {
		t.Fatalf("proxy=%+v err=%v", proxy, err)
	}
	if strings.Contains(created.Body.String(), "canary-user") || strings.Contains(created.Body.String(), "canary-password") {
		t.Fatal("write-only proxy secret was returned")
	}

	missingVersion := doJSON(t, api.handler, http.MethodPatch, "/api/admin/v1/proxies/"+proxy.ID, `{"name":"proxy","scheme":"https","host":"proxy.example.test","port":443,"allow_insecure_tls":false,"enabled":false}`, cookies[0].Value, session.CSRFToken)
	if missingVersion.Code != http.StatusConflict || !strings.Contains(missingVersion.Body.String(), "version_conflict") {
		t.Fatalf("missing If-Match response=%d %s", missingVersion.Code, missingVersion.Body.String())
	}

	request := newJSONRequest(t, http.MethodPatch, "/api/admin/v1/proxies/"+proxy.ID, `{"name":"proxy","scheme":"https","host":"proxy.example.test","port":443,"allow_insecure_tls":false,"enabled":false}`, cookies[0].Value, session.CSRFToken)
	request.Header.Set("If-Match", `"v1"`)
	updated := httptest.NewRecorder()
	api.handler.ServeHTTP(updated, request)
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"v2"` {
		t.Fatalf("update status=%d etag=%q body=%s", updated.Code, updated.Header().Get("ETag"), updated.Body.String())
	}

	recoveredRequest := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/session", nil)
	recoveredRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookies[0].Value})
	recovered := httptest.NewRecorder()
	api.handler.ServeHTTP(recovered, recoveredRequest)
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), session.CSRFToken) {
		t.Fatalf("session recovery status=%d body=%s", recovered.Code, recovered.Body.String())
	}
}

func TestStrictJSONOriginAndProtocolBoundary(t *testing.T) {
	assets := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("spa")) })
	api := newTestAPI(t, assets)

	duplicate := doJSON(t, api.handler, http.MethodPost, "/api/admin/v1/auth/login", `{"password":"one","password":"two"}`, "", "")
	if duplicate.Code != http.StatusBadRequest || !strings.Contains(duplicate.Body.String(), "duplicate") {
		t.Fatalf("duplicate JSON status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	wrongOrigin := newJSONRequest(t, http.MethodPost, "/api/admin/v1/auth/login", `{"password":"`+testPassword+`"}`, "", "")
	wrongOrigin.Header.Set("Origin", "https://attacker.example")
	wrongOriginResult := httptest.NewRecorder()
	api.handler.ServeHTTP(wrongOriginResult, wrongOrigin)
	if wrongOriginResult.Code != http.StatusForbidden {
		t.Fatalf("wrong origin status=%d", wrongOriginResult.Code)
	}

	for _, path := range []string{"/api/unknown", "/v1/chat/completions", "/v1beta/models/example:generateContent"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		result := httptest.NewRecorder()
		api.handler.ServeHTTP(result, request)
		if result.Body.String() == "spa" {
			t.Fatalf("protected protocol path %s fell back to SPA", path)
		}
		if path == "/api/unknown" && result.Code != http.StatusNotFound {
			t.Fatalf("API status=%d", result.Code)
		}
		if path != "/api/unknown" && result.Code != http.StatusNotImplemented {
			t.Fatalf("protocol status=%d for %s", result.Code, path)
		}
		if path != "/api/unknown" && !strings.Contains(result.Body.String(), `"code":"capability_not_supported"`) {
			t.Fatalf("protocol error code for %s: %s", path, result.Body.String())
		}
	}

	page := httptest.NewRecorder()
	api.handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if page.Code != http.StatusOK || page.Body.String() != "spa" {
		t.Fatalf("SPA status=%d body=%s", page.Code, page.Body.String())
	}
}

func TestTrustedProxyClientIP(t *testing.T) {
	publicURL, _ := url.Parse("https://sidervia.example.test")
	s := &Server{publicURL: publicURL, trustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.1.2.3:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.10, 10.9.8.7")
	if got := s.clientIP(request); got != "198.51.100.10" {
		t.Fatalf("client IP=%q", got)
	}
	request.RemoteAddr = "192.0.2.44:1234"
	if got := s.clientIP(request); got != "192.0.2.44" {
		t.Fatalf("untrusted proxy affected client IP: %q", got)
	}
}

func TestSameOriginNormalizesDefaultPorts(t *testing.T) {
	tests := []struct {
		candidate string
		expected  string
		want      bool
	}{
		{candidate: "https://sidervia.example.test", expected: "https://sidervia.example.test:443", want: true},
		{candidate: "http://localhost:80/path", expected: "http://localhost", want: true},
		{candidate: "https://sidervia.example.test:444", expected: "https://sidervia.example.test", want: false},
		{candidate: "https://user@sidervia.example.test", expected: "https://sidervia.example.test", want: false},
	}
	for _, test := range tests {
		candidate, err := url.Parse(test.candidate)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := url.Parse(test.expected)
		if err != nil {
			t.Fatal(err)
		}
		if got := sameOrigin(candidate, expected); got != test.want {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v", test.candidate, test.expected, got, test.want)
		}
	}
}

func doJSON(t *testing.T, handler http.Handler, method, path, body, token, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, newJSONRequest(t, method, path, body, token, csrf))
	return result
}

func newJSONRequest(t *testing.T, method, path, body, token, csrf string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://sidervia.example.test")
	request.RemoteAddr = "192.0.2.10:4321"
	if token != "" {
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	return request
}
