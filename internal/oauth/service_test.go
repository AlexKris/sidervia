package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/accountauth"
	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/provider/google"
	"github.com/AlexKris/sidervia/internal/routing"
	"github.com/AlexKris/sidervia/internal/store"
)

type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

type fakeOAuthTransport struct {
	mu             sync.Mutex
	forms          []url.Values
	authorizeBody  string
	refreshBody    string
	tokenStatus    int
	probeBody      string
	probeStatus    int
	tokenCalls     atomic.Int64
	probeCalls     atomic.Int64
	tokenCandidate []routing.Candidate
	probeCandidate []routing.Candidate
}

func (f *fakeOAuthTransport) Do(_ context.Context, candidate routing.Candidate, _ provider.NativeRequest, _ provider.Adapter) (*http.Response, error) {
	f.probeCalls.Add(1)
	f.mu.Lock()
	f.probeCandidate = append(f.probeCandidate, candidate)
	f.mu.Unlock()
	status := f.probeStatus
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(f.probeBody))}, nil
}

func (f *fakeOAuthTransport) DoGoogleOAuthToken(_ context.Context, candidate routing.Candidate, form url.Values) (*http.Response, error) {
	f.tokenCalls.Add(1)
	f.mu.Lock()
	copyForm := make(url.Values, len(form))
	for key, values := range form {
		copyForm[key] = append([]string(nil), values...)
	}
	f.forms = append(f.forms, copyForm)
	f.tokenCandidate = append(f.tokenCandidate, candidate)
	f.mu.Unlock()
	body := f.authorizeBody
	if form.Get("grant_type") == "refresh_token" {
		body = f.refreshBody
	}
	status := f.tokenStatus
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(body))}, nil
}

type oauthFixture struct {
	service   *Service
	control   *control.Service
	routing   *routing.Service
	database  *store.Store
	cipher    *cryptox.Cipher
	transport *fakeOAuthTransport
	account   control.Account
	sessionID string
	clock     testClock
}

func TestAuthorizationCodePKCECompletesAndCannotReplay(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	ctx := context.Background()
	attempt, err := fixture.service.CreateAttempt(ctx, control.Actor{Kind: "admin", ID: fixture.sessionID}, fixture.sessionID, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	authorizationURL, err := url.Parse(attempt.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	state := authorizationURL.Query().Get("state")
	challenge := authorizationURL.Query().Get("code_challenge")
	if state == "" || challenge == "" || authorizationURL.Query().Get("code_challenge_method") != "S256" || authorizationURL.Query().Get("access_type") != "offline" {
		t.Fatalf("authorization URL = %s", attempt.AuthorizationURL)
	}
	var storedState string
	var pkceEnvelope []byte
	if err := fixture.database.DB().QueryRow(`SELECT state_verifier, pkce_verifier_enc FROM oauth_attempts WHERE public_id = ?`, attempt.ID).Scan(&storedState, &pkceEnvelope); err != nil {
		t.Fatal(err)
	}
	if storedState == state || storedState != identifier.Verifier(state) || bytes.Contains(pkceEnvelope, []byte(challenge)) {
		t.Fatal("state or PKCE was stored in plaintext")
	}
	pkce, err := fixture.cipher.Open(pkceEnvelope, cryptox.AAD("oauth_attempts", attempt.ID, "pkce_verifier_enc"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(pkce)
	if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
		t.Fatal("PKCE challenge does not match the encrypted verifier")
	}

	completed, err := fixture.service.CompleteAuthorization(ctx, state, "authorization-code-canary", "", "", "req_oauth")
	if err != nil {
		t.Fatal(err)
	}
	if completed.AttemptID != attempt.ID || completed.Account.Status != "active" || !completed.Account.CredentialConfigured {
		t.Fatalf("completion = %#v", completed)
	}
	if fixture.transport.tokenCalls.Load() != 1 || fixture.transport.probeCalls.Load() != 1 {
		t.Fatalf("token/probe calls = %d/%d", fixture.transport.tokenCalls.Load(), fixture.transport.probeCalls.Load())
	}
	fixture.transport.mu.Lock()
	form := fixture.transport.forms[0]
	tokenCandidate := fixture.transport.tokenCandidate[0]
	probeCandidate := fixture.transport.probeCandidate[0]
	fixture.transport.mu.Unlock()
	if form.Get("code_verifier") != string(pkce) || form.Get("code") != "authorization-code-canary" || form.Get("client_secret") != "client-secret-canary" {
		t.Fatalf("token form fields are incomplete")
	}
	if tokenCandidate.AccountInternalID != probeCandidate.AccountInternalID || tokenCandidate.Proxy != probeCandidate.Proxy {
		t.Fatal("token exchange and account validation did not use the same egress profile")
	}
	var credentialEnvelope []byte
	var status string
	if err := fixture.database.DB().QueryRow(`SELECT credential_enc, status FROM accounts WHERE public_id = ?`, fixture.account.ID).Scan(&credentialEnvelope, &status); err != nil {
		t.Fatal(err)
	}
	payload, err := accountauth.Decrypt(fixture.cipher, fixture.account.ID, credentialEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if status != "active" || payload.AccessToken != "access-token-canary" || payload.RefreshToken != "refresh-token-canary" || payload.ProviderFields["project_id"] != "project-test1" {
		t.Fatalf("credential payload/status = %#v %q", payload, status)
	}
	if bytes.Contains(credentialEnvelope, []byte("access-token-canary")) || bytes.Contains(credentialEnvelope, []byte("refresh-token-canary")) {
		t.Fatal("OAuth tokens were stored in plaintext")
	}
	if _, err := fixture.service.CompleteAuthorization(ctx, state, "authorization-code-canary", "", "", "req_replay"); !IsError(err, "oauth_state_invalid") {
		t.Fatalf("replay error = %v", err)
	}
}

func TestOAuthRefreshUsesSingleflightAndPreservesRefreshToken(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	ctx := context.Background()
	attempt, err := fixture.service.CreateAttempt(ctx, control.Actor{}, fixture.sessionID, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(attempt.AuthorizationURL)
	if _, err := fixture.service.CompleteAuthorization(ctx, parsed.Query().Get("state"), "code", "", "", "req"); err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.routing.LoadAccount(ctx, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.transport.tokenCalls.Store(0)
	fixture.transport.refreshBody = `{"access_token":"refreshed-access-token","token_type":"Bearer","expires_in":3600,"scope":"https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever"}`
	const workers = 100
	start := make(chan struct{})
	errorsCh := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := fixture.service.EnsureCredential(ctx, loaded.Candidate, true)
			errorsCh <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls := fixture.transport.tokenCalls.Load(); calls != 1 {
		t.Fatalf("refresh token calls = %d", calls)
	}
	var envelope []byte
	if err := fixture.database.DB().QueryRow(`SELECT credential_enc FROM accounts WHERE public_id = ?`, fixture.account.ID).Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	payload, err := accountauth.Decrypt(fixture.cipher, fixture.account.ID, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if payload.AccessToken != "refreshed-access-token" || payload.RefreshToken != "refresh-token-canary" {
		t.Fatalf("refreshed payload = %#v", payload)
	}
}

func TestTransientRefreshFailureUsesStillValidAccessToken(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	attempt, err := fixture.service.CreateAttempt(context.Background(), control.Actor{}, fixture.sessionID, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(attempt.AuthorizationURL)
	if _, err := fixture.service.CompleteAuthorization(context.Background(), parsed.Query().Get("state"), "code", "", "", "req"); err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.routing.LoadAccount(context.Background(), fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.clock = testClock{now: fixture.clock.now.Add(57 * time.Minute)}
	fixture.transport.tokenStatus = http.StatusServiceUnavailable

	candidate, err := fixture.service.EnsureCredential(context.Background(), loaded.Candidate, false)
	if err != nil || candidate.AccountInternalID != loaded.Candidate.AccountInternalID {
		t.Fatalf("still-valid credential was rejected: candidate=%#v err=%v", candidate, err)
	}
	fixture.service.clock = testClock{now: fixture.clock.now.Add(61 * time.Minute)}
	if _, err := fixture.service.EnsureCredential(context.Background(), loaded.Candidate, false); !IsError(err, "oauth_refresh_unavailable") {
		t.Fatalf("expired credential refresh error=%v", err)
	}
}

func TestManualCallbackRequiresBoundSession(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	attempt, err := fixture.service.CreateAttempt(context.Background(), control.Actor{}, fixture.sessionID, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(attempt.AuthorizationURL)
	callback := "https://sidervia.test/oauth/callback/google?code=code&state=" + url.QueryEscape(parsed.Query().Get("state"))
	if _, err := fixture.service.CompleteCallbackURL(context.Background(), callback, "another-session", "req"); !IsError(err, "oauth_session_mismatch") {
		t.Fatalf("session mismatch error = %v", err)
	}
}

func TestTokenResponseRejectsInvalidRotatedRefreshToken(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	fixture.transport.refreshBody = `{"access_token":"refreshed-access-token","refresh_token":"invalid\nrefresh-token","token_type":"Bearer","expires_in":3600,"scope":"https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever"}`

	_, code, err := fixture.service.exchangeToken(context.Background(), routing.Candidate{}, url.Values{
		"grant_type": {"refresh_token"},
	}, false, googleScopes)
	if err == nil || code != "oauth_token_invalid" {
		t.Fatalf("exchangeToken error/code = %v/%q", err, code)
	}
}

func TestValidateGoogleConfigRejectsInvalidProjectIDs(t *testing.T) {
	secret := "client-secret"
	for _, projectID := range []string{"short", "project-ending-", "project-id-that-is-more-than-thirty-characters"} {
		_, err := validateGoogleConfig(GoogleConfigInput{
			ClientID: "client-id.apps.googleusercontent.com", ClientSecret: &secret,
			ProjectID: projectID, Enabled: true,
		}, true)
		if err == nil {
			t.Fatalf("project ID %q was accepted", projectID)
		}
	}
	if _, err := validateGoogleConfig(GoogleConfigInput{
		ClientID: "client-id.apps.googleusercontent.com", ClientSecret: &secret,
		ProjectID: "valid-project-123", Enabled: true,
	}, true); err != nil {
		t.Fatalf("valid project ID rejected: %v", err)
	}
}

func TestDecodeScopesRejectsUnexpectedOAuthScope(t *testing.T) {
	_, err := decodeScopes([]byte(`{"schema_version":1,"values":["https://www.googleapis.com/auth/cloud-platform","https://www.googleapis.com/auth/generative-language.retriever","https://www.googleapis.com/auth/drive"]}`))
	if err == nil {
		t.Fatal("unexpected OAuth scope was accepted")
	}
}

func TestTokenResponseRejectsUnexpectedOAuthScope(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	fixture.transport.authorizeBody = `{"access_token":"access-token","refresh_token":"refresh-token","token_type":"Bearer","expires_in":3600,"scope":"https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever https://www.googleapis.com/auth/drive"}`

	_, code, err := fixture.service.exchangeToken(context.Background(), routing.Candidate{}, url.Values{
		"grant_type": {"authorization_code"},
	}, true, googleScopes)
	if err == nil || code != "oauth_scope_mismatch" {
		t.Fatalf("exchangeToken error/code = %v/%q", err, code)
	}
}

func TestCleanupExpiredAttemptsRemovesEncryptedFlowState(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	attempt, err := fixture.service.CreateAttempt(context.Background(), control.Actor{}, fixture.sessionID, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.service.clock = testClock{now: fixture.clock.now.Add(attemptTTL + time.Second)}
	if err := fixture.service.cleanupExpiredAttempts(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := fixture.database.DB().QueryRow(`SELECT count(*) FROM oauth_attempts WHERE public_id = ?`, attempt.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired attempt rows = %d", count)
	}
}

func TestAuthorizationRejectsMalformedSuccessfulModelProbe(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	fixture.transport.probeBody = `{"models":{}}`
	attempt, err := fixture.service.CreateAttempt(context.Background(), control.Actor{}, fixture.sessionID, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(attempt.AuthorizationURL)
	_, err = fixture.service.CompleteAuthorization(context.Background(), parsed.Query().Get("state"), "code", "", "", "req")
	if !IsError(err, "oauth_account_validation_failed") {
		t.Fatalf("malformed probe error=%v", err)
	}
}

func TestRecoverInterruptedOAuthExchange(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	attempt, err := fixture.service.CreateAttempt(context.Background(), control.Actor{}, fixture.sessionID, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.DB().Exec(`UPDATE oauth_attempts SET status = 'exchanging' WHERE public_id = ?`, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.RecoverInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := fixture.service.GetAttempt(context.Background(), fixture.sessionID, attempt.ID)
	if err != nil || recovered.Status != "failed" {
		t.Fatalf("attempt=%#v err=%v", recovered, err)
	}
}

func TestCreateGoogleConfigRollsBackWhenAuditWriteFails(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	if _, err := fixture.database.DB().Exec(`DELETE FROM provider_oauth_configs`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.DB().Exec(`CREATE TRIGGER reject_oauth_config_audit
		BEFORE INSERT ON audit_events WHEN NEW.event_type = 'oauth_config.created'
		BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	secret := "replacement-client-secret"
	_, err := fixture.service.CreateGoogleConfig(context.Background(), control.Actor{}, GoogleConfigInput{
		ClientID: "replacement.apps.googleusercontent.com", ClientSecret: &secret,
		ProjectID: "replacement-project", Enabled: true,
	})
	if err == nil {
		t.Fatal("OAuth config creation succeeded without its audit event")
	}
	var count int
	if err := fixture.database.DB().QueryRow(`SELECT count(*) FROM provider_oauth_configs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("OAuth configs after failed audit = %d", count)
	}
}

func TestCreateOAuthAttemptRollsBackWhenAuditWriteFails(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	if _, err := fixture.database.DB().Exec(`CREATE TRIGGER reject_oauth_attempt_create_audit
		BEFORE INSERT ON audit_events WHEN NEW.event_type = 'oauth_attempt.created'
		BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.CreateAttempt(context.Background(), control.Actor{}, fixture.sessionID, fixture.account.ID); err == nil {
		t.Fatal("OAuth attempt creation succeeded without its audit event")
	}
	var count int
	if err := fixture.database.DB().QueryRow(`SELECT count(*) FROM oauth_attempts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("OAuth attempts after failed audit = %d", count)
	}
}

func TestUpdateGoogleConfigRollsBackWhenAuditWriteFails(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	before, err := fixture.service.GetGoogleConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.DB().Exec(`CREATE TRIGGER reject_oauth_config_update_audit
		BEFORE INSERT ON audit_events WHEN NEW.event_type = 'oauth_config.updated'
		BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.UpdateGoogleConfig(context.Background(), control.Actor{}, before.Version, GoogleConfigInput{
		ClientID: "changed.apps.googleusercontent.com", ProjectID: "changed-project1", Enabled: false,
	})
	if err == nil {
		t.Fatal("OAuth config update succeeded without its audit event")
	}
	after, err := fixture.service.GetGoogleConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.ClientID != before.ClientID || after.ProjectID != before.ProjectID || after.Enabled != before.Enabled || after.Version != before.Version {
		t.Fatalf("OAuth config changed after failed audit: before=%#v after=%#v", before, after)
	}
}

func TestCancelOAuthAttemptRollsBackWhenAuditWriteFails(t *testing.T) {
	fixture := newOAuthFixture(t)
	defer fixture.database.Close()
	attempt, err := fixture.service.CreateAttempt(context.Background(), control.Actor{}, fixture.sessionID, fixture.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.DB().Exec(`CREATE TRIGGER reject_oauth_attempt_cancel_audit
		BEFORE INSERT ON audit_events WHEN NEW.event_type = 'oauth_attempt.cancelled'
		BEGIN SELECT RAISE(ABORT, 'forced audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.service.CancelAttempt(context.Background(), control.Actor{}, fixture.sessionID, attempt.ID); err == nil {
		t.Fatal("OAuth attempt cancellation succeeded without its audit event")
	}
	var status string
	if err := fixture.database.DB().QueryRow(`SELECT status FROM oauth_attempts WHERE public_id = ?`, attempt.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("OAuth attempt status after failed audit = %q", status)
	}
}

func newOAuthFixture(t *testing.T) oauthFixture {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := cryptox.NewCipher(bytes.Repeat([]byte{3}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	clockValue := testClock{now: now}
	ids := identifier.NewGenerator()
	controlService := control.NewService(database.DB(), cipher, clockValue, ids)
	sessionID := "sdr_sess_oauth_test"
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO admin_user(
		id, password_phc, created_at_ms, updated_at_ms
	) VALUES(1, 'test-password-hash', ?, ?)`, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO admin_sessions(
		public_id, token_verifier, csrf_token_enc, session_version, created_at_ms, last_seen_at_ms,
		idle_expires_at_ms, absolute_expires_at_ms, ip_prefix_hmac, user_agent_hmac
	) VALUES(?, 'verifier', X'01', 1, ?, ?, ?, ?, 'ip', 'ua')`, sessionID, now.UnixMilli(), now.UnixMilli(), now.Add(time.Hour).UnixMilli(), now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	upstream, err := controlService.CreateUpstream(ctx, control.Actor{}, control.UpstreamInput{
		ProviderID: "google", Name: "Google", BaseURL: "https://generativelanguage.googleapis.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := controlService.CreateAccount(ctx, control.Actor{}, control.AccountInput{
		UpstreamID: upstream.ID, Name: "Google OAuth", AuthKind: "oauth", BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := provider.NewRegistry(google.New())
	if err != nil {
		t.Fatal(err)
	}
	routingService := routing.New(database.DB(), cipher, clockValue)
	transport := &fakeOAuthTransport{
		authorizeBody: `{"access_token":"access-token-canary","refresh_token":"refresh-token-canary","token_type":"Bearer","expires_in":3600,"scope":"https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever"}`,
		refreshBody:   `{"access_token":"refreshed-access-token","token_type":"Bearer","expires_in":3600,"scope":"https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever"}`,
		probeBody:     `{"models":[{"name":"models/gemini-b"},{"name":"models/gemini-a"}]}`,
	}
	publicURL, _ := url.Parse("https://sidervia.test")
	service := New(Options{
		DB: database.DB(), Cipher: cipher, Clock: clockValue, IDs: ids, PublicURL: publicURL,
		Control: controlService, Routing: routingService, Providers: registry, Transport: transport,
	})
	secret := "client-secret-canary"
	if _, err := service.CreateGoogleConfig(ctx, control.Actor{}, GoogleConfigInput{
		ClientID: "client-id.apps.googleusercontent.com", ClientSecret: &secret, ProjectID: "project-test1", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	return oauthFixture{
		service: service, control: controlService, routing: routingService, database: database,
		cipher: cipher, transport: transport, account: account, sessionID: sessionID, clock: clockValue,
	}
}

var _ clock.Clock = testClock{}
