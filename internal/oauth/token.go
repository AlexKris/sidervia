package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/accountauth"
	"github.com/AlexKris/sidervia/internal/accountvalidate"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/routing"
	"github.com/AlexKris/sidervia/internal/strictjson"
)

const maxOAuthResponse = 1 << 20

type Completion struct {
	AttemptID string          `json:"attempt_id"`
	Account   control.Account `json:"account"`
}

type tokenResponse struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scopes       []string
	ExpiresAtMS  int64
}

func (s *Service) CompleteAuthorization(ctx context.Context, state, code, providerError, requiredSessionPublicID, requestID string) (Completion, error) {
	return s.completeAuthorization(ctx, state, code, providerError, requiredSessionPublicID, "", requestID)
}

func (s *Service) completeAuthorization(ctx context.Context, state, code, providerError, requiredSessionPublicID, expectedAttemptID, requestID string) (Completion, error) {
	claimed, err := s.claimAttempt(ctx, state, requiredSessionPublicID, expectedAttemptID)
	if err != nil {
		return Completion{}, err
	}
	fail := func(code string) (Completion, error) {
		s.failAttempt(ctx, claimed.publicID, code)
		return Completion{AttemptID: claimed.publicID}, &Error{Code: code}
	}
	if providerError != "" {
		return fail("oauth_authorization_denied")
	}
	if code == "" || len(code) > 8192 || strings.TrimSpace(code) != code || strings.ContainsAny(code, "\r\n\x00") {
		return fail("oauth_code_invalid")
	}
	form := make(url.Values)
	form.Set("client_id", claimed.clientID)
	form.Set("client_secret", claimed.clientSecret)
	form.Set("code", code)
	form.Set("code_verifier", claimed.pkceVerifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", s.redirectURI())
	token, tokenCode, err := s.exchangeToken(ctx, claimed.candidate, form, true, claimed.scopes)
	if err != nil {
		return fail(tokenCode)
	}
	credential, err := provider.NewOAuthToken(token.AccessToken, claimed.projectID)
	if err != nil {
		return fail("oauth_token_invalid")
	}
	claimed.candidate.Credential = credential
	models, err := s.probeGoogle(ctx, claimed.candidate)
	if err != nil {
		return fail("oauth_account_validation_failed")
	}
	latest, err := s.routing.LoadAccount(ctx, claimed.accountPublicID)
	if err != nil || latest.Candidate.AccountVersion != claimed.accountVersion || egressFingerprint(latest.Candidate) != claimed.egressFingerprint {
		return fail("oauth_egress_changed")
	}
	descriptor, _ := s.providers.Get(ProviderGoogle)
	description := descriptor.Descriptor()
	payload := accountauth.Payload{
		SchemaVersion: 1, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
		TokenType: "Bearer", Scopes: token.Scopes, ExpiresAtMS: token.ExpiresAtMS,
		ProviderFields: map[string]string{
			"project_id":   claimed.projectID,
			"issued_at_ms": strconv.FormatInt(s.clock.Now().UnixMilli(), 10),
		},
	}
	account, err := s.control.CompleteOAuthAccount(ctx, control.Actor{
		Kind: "admin", ID: claimed.adminSessionID, RequestID: requestID,
	}, claimed.publicID, claimed.accountPublicID, claimed.accountVersion, payload, control.AccountValidation{
		Identity:          map[string]any{"provider_id": ProviderGoogle, "model_count": len(models)},
		CapabilityVersion: description.ID + "/" + description.Version + "@" + description.VerifiedAgainst,
		Models:            models,
	})
	if err != nil {
		s.failAttempt(ctx, claimed.publicID, "oauth_completion_failed")
		return Completion{AttemptID: claimed.publicID}, err
	}
	return Completion{AttemptID: claimed.publicID, Account: account}, nil
}

func (s *Service) exchangeToken(ctx context.Context, candidate routing.Candidate, form url.Values, requireRefresh bool, requiredScopes []string) (tokenResponse, string, error) {
	response, err := s.transport.DoGoogleOAuthToken(ctx, candidate, form)
	if err != nil {
		return tokenResponse{}, "oauth_token_unavailable", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxOAuthResponse+1))
	if err != nil || len(body) > maxOAuthResponse {
		return tokenResponse{}, "oauth_token_invalid", errors.New("OAuth token response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return tokenResponse{}, tokenErrorCode(body), errors.New("OAuth token exchange was rejected")
	}
	if err := strictjson.Validate(body, 32); err != nil {
		return tokenResponse{}, "oauth_token_invalid", err
	}
	var raw struct {
		AccessToken  string          `json:"access_token"`
		RefreshToken string          `json:"refresh_token"`
		TokenType    string          `json:"token_type"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
		Scope        string          `json:"scope"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return tokenResponse{}, "oauth_token_invalid", errors.New("OAuth token response fields are invalid")
	}
	invalidRefresh := raw.RefreshToken != "" && !safeToken(raw.RefreshToken)
	if !safeToken(raw.AccessToken) || invalidRefresh ||
		(requireRefresh && raw.RefreshToken == "") ||
		!strings.EqualFold(raw.TokenType, "Bearer") {
		return tokenResponse{}, "oauth_token_invalid", errors.New("OAuth token response fields are invalid")
	}
	var expiresNumber json.Number
	if json.Unmarshal(raw.ExpiresIn, &expiresNumber) != nil {
		return tokenResponse{}, "oauth_token_invalid", errors.New("OAuth token expiry is invalid")
	}
	expiresSeconds, err := strconv.ParseInt(expiresNumber.String(), 10, 64)
	if err != nil || expiresSeconds < 60 || expiresSeconds > int64((24*time.Hour)/time.Second) {
		return tokenResponse{}, "oauth_token_invalid", errors.New("OAuth token expiry is invalid")
	}
	scopes := strings.Fields(raw.Scope)
	if len(scopes) == 0 {
		scopes = append([]string(nil), requiredScopes...)
	}
	if !validScopes(scopes) || !sameScopeSet(scopes, requiredScopes) {
		return tokenResponse{}, "oauth_scope_mismatch", errors.New("OAuth token scopes are insufficient")
	}
	return tokenResponse{
		AccessToken: raw.AccessToken, RefreshToken: raw.RefreshToken, TokenType: "Bearer",
		Scopes: scopes, ExpiresAtMS: s.clock.Now().Add(time.Duration(expiresSeconds) * time.Second).UnixMilli(),
	}, "", nil
}

func (s *Service) probeGoogle(ctx context.Context, candidate routing.Candidate) ([]string, error) {
	adapter, ok := s.providers.Get(ProviderGoogle)
	if !ok {
		return nil, errors.New("Google provider adapter is unavailable")
	}
	response, err := s.transport.Do(ctx, candidate, adapter.Probe(), adapter)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxOAuthResponse+1))
	if err != nil || len(body) > maxOAuthResponse || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("Google account validation failed")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return nil, errors.New("Google account validation failed")
	}
	models, err := accountvalidate.ParseModels(ProviderGoogle, body)
	if err != nil {
		return nil, errors.New("Google account validation failed")
	}
	return models, nil
}

func (s *Service) failAttempt(ctx context.Context, publicID, reason string) {
	now := s.clock.Now().UnixMilli()
	_, _ = s.db.ExecContext(ctx, `UPDATE oauth_attempts SET status = 'failed', consumed_at_ms = ?
		WHERE public_id = ? AND status IN ('pending', 'exchanging')`, now, publicID)
	_ = s.control.RecordAudit(ctx, control.Actor{Kind: "system"}, "oauth_attempt.failed", "oauth_attempt", publicID, "failure", map[string]any{"reason": reason})
}

func (s *Service) loadGoogleConfigSecret(ctx context.Context) (GoogleConfig, string, error) {
	config, err := s.GetGoogleConfig(ctx)
	if err != nil {
		return GoogleConfig{}, "", err
	}
	var envelope []byte
	if err := s.db.QueryRowContext(ctx, `SELECT client_secret_enc FROM provider_oauth_configs
		WHERE public_id = ? AND provider_id = 'google'`, config.ID).Scan(&envelope); err != nil {
		return GoogleConfig{}, "", err
	}
	secret, err := s.cipher.Open(envelope, cryptox.AAD("provider_oauth_configs", config.ID, "client_secret_enc"))
	if err != nil {
		return GoogleConfig{}, "", err
	}
	return config, string(secret), nil
}

func decodeScopes(body []byte) ([]string, error) {
	var value struct {
		SchemaVersion int      `json:"schema_version"`
		Values        []string `json:"values"`
	}
	if json.Unmarshal(body, &value) != nil || value.SchemaVersion != 1 || !validScopes(value.Values) || !sameScopes(value.Values, googleScopes) {
		return nil, errors.New("OAuth scopes are invalid")
	}
	return value.Values, nil
}

func tokenErrorCode(body []byte) string {
	if strictjson.Validate(body, 16) != nil {
		return "oauth_token_rejected"
	}
	var value struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &value) != nil {
		return "oauth_token_rejected"
	}
	switch value.Error {
	case "invalid_grant":
		return "reauth_required"
	case "invalid_client":
		return "oauth_config_invalid"
	default:
		return "oauth_token_rejected"
	}
}

func safeToken(value string) bool {
	return value != "" && len(value) <= 8192 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

func sameScopeSet(actual, required []string) bool {
	if len(actual) != len(required) {
		return false
	}
	set := make(map[string]struct{}, len(required))
	for _, value := range required {
		set[value] = struct{}{}
	}
	for _, value := range actual {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}

func validScopes(scopes []string) bool {
	if len(scopes) == 0 || len(scopes) > 16 {
		return false
	}
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope == "" || len(scope) > 512 || strings.TrimSpace(scope) != scope || strings.ContainsAny(scope, " \t\r\n\x00") {
			return false
		}
		if _, exists := seen[scope]; exists {
			return false
		}
		seen[scope] = struct{}{}
	}
	return true
}

func sameScopes(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateCallbackValues(values url.Values) (state, code, providerError string, err error) {
	allowed := map[string]bool{"state": true, "code": true, "error": true, "error_description": true, "error_uri": true, "scope": true, "authuser": true, "prompt": true, "hd": true}
	for key, list := range values {
		if !allowed[key] || len(list) != 1 {
			return "", "", "", &Error{Code: "oauth_callback_invalid"}
		}
	}
	state = values.Get("state")
	code = values.Get("code")
	providerError = values.Get("error")
	if state == "" || (code == "") == (providerError == "") {
		return "", "", "", &Error{Code: "oauth_callback_invalid"}
	}
	if providerError != "" && (len(providerError) > 128 || strings.ContainsAny(providerError, "\r\n\x00")) {
		return "", "", "", &Error{Code: "oauth_callback_invalid"}
	}
	return state, code, providerError, nil
}

func (s *Service) CompleteCallbackURL(ctx context.Context, callbackURL, requiredSessionPublicID, requestID string) (Completion, error) {
	return s.CompleteAttemptCallbackURL(ctx, "", callbackURL, requiredSessionPublicID, requestID)
}

func (s *Service) CompleteAttemptCallbackURL(ctx context.Context, expectedAttemptID, callbackURL, requiredSessionPublicID, requestID string) (Completion, error) {
	if callbackURL == "" || len(callbackURL) > 16<<10 {
		return Completion{}, &Error{Code: "oauth_callback_invalid"}
	}
	parsed, err := url.Parse(callbackURL)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != s.publicURL.Scheme || parsed.Host != s.publicURL.Host || parsed.Path != "/oauth/callback/google" {
		return Completion{}, &Error{Code: "oauth_callback_invalid"}
	}
	state, code, providerError, err := validateCallbackValues(parsed.Query())
	if err != nil {
		return Completion{}, err
	}
	return s.completeAuthorization(ctx, state, code, providerError, requiredSessionPublicID, expectedAttemptID, requestID)
}

func (s *Service) CompleteCallbackValues(ctx context.Context, values url.Values, requestID string) (Completion, error) {
	state, code, providerError, err := validateCallbackValues(values)
	if err != nil {
		return Completion{}, err
	}
	return s.CompleteAuthorization(ctx, state, code, providerError, "", requestID)
}
