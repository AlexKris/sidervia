package oauth

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/AlexKris/sidervia/internal/accountauth"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/routing"
)

type refreshCall struct {
	done      chan struct{}
	candidate routing.Candidate
	err       error
}

func (s *Service) EnsureCredential(ctx context.Context, candidate routing.Candidate, force bool) (routing.Candidate, error) {
	if candidate.AuthKind != "oauth" {
		return candidate, nil
	}
	if candidate.ProviderID != ProviderGoogle {
		return routing.Candidate{}, &Error{Code: "capability_not_supported"}
	}
	if !force && !refreshDue(candidate.CredentialExpiresAtMS, s.clock.Now(), 0) {
		return candidate, nil
	}
	s.refreshMu.Lock()
	if existing := s.refreshes[candidate.AccountInternalID]; existing != nil {
		s.refreshMu.Unlock()
		select {
		case <-existing.done:
			return existing.candidate, existing.err
		case <-ctx.Done():
			return routing.Candidate{}, ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	s.refreshes[candidate.AccountInternalID] = call
	s.refreshMu.Unlock()
	call.candidate, call.err = s.refreshCredential(ctx, candidate, force)
	s.refreshMu.Lock()
	delete(s.refreshes, candidate.AccountInternalID)
	close(call.done)
	s.refreshMu.Unlock()
	return call.candidate, call.err
}

func (s *Service) refreshCredential(ctx context.Context, selected routing.Candidate, force bool) (routing.Candidate, error) {
	fresh, err := s.routing.LoadAccount(ctx, selected.AccountPublicID)
	if err != nil {
		return routing.Candidate{}, err
	}
	candidate := fresh.Candidate
	if candidate.AuthKind != "oauth" || fresh.Status != "active" {
		return routing.Candidate{}, &Error{Code: "reauth_required"}
	}
	if candidate.CredentialVersion != selected.CredentialVersion {
		return candidate, nil
	}
	var envelope []byte
	var credentialVersion int64
	err = s.db.QueryRowContext(ctx, `SELECT credential_enc, credential_version FROM accounts
		WHERE id = ? AND status = 'active' AND auth_kind = 'oauth'`, candidate.AccountInternalID).Scan(&envelope, &credentialVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return routing.Candidate{}, &Error{Code: "reauth_required"}
	}
	if err != nil {
		return routing.Candidate{}, err
	}
	payload, err := accountauth.Decrypt(s.cipher, candidate.AccountPublicID, envelope)
	if err != nil || payload.RefreshToken == "" {
		return routing.Candidate{}, &Error{Code: "reauth_required"}
	}
	issuedAt, _ := strconv.ParseInt(payload.ProviderFields["issued_at_ms"], 10, 64)
	if !force && !refreshDue(payload.ExpiresAtMS, s.clock.Now(), issuedAt) {
		return candidate, nil
	}
	config, secret, err := s.loadGoogleConfigSecret(ctx)
	if err != nil || !config.Enabled {
		if !force && candidate.CredentialExpiresAtMS > s.clock.Now().UnixMilli() {
			return candidate, nil
		}
		return routing.Candidate{}, &Error{Code: "oauth_config_invalid"}
	}
	form := make(url.Values)
	form.Set("client_id", config.ClientID)
	form.Set("client_secret", secret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", payload.RefreshToken)
	token, code, err := s.exchangeToken(ctx, candidate, form, false, payload.Scopes)
	if err != nil {
		if code == "reauth_required" {
			s.markReauthRequired(ctx, candidate, credentialVersion)
			return routing.Candidate{}, &Error{Code: "reauth_required"}
		}
		if !force && candidate.CredentialExpiresAtMS > s.clock.Now().UnixMilli() {
			return candidate, nil
		}
		return routing.Candidate{}, &Error{Code: "oauth_refresh_unavailable"}
	}
	if token.RefreshToken == "" {
		token.RefreshToken = payload.RefreshToken
	}
	updatedPayload := accountauth.Payload{
		SchemaVersion: 1, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken,
		TokenType: "Bearer", Scopes: token.Scopes, ExpiresAtMS: token.ExpiresAtMS,
		ProviderFields: map[string]string{
			"project_id":   config.ProjectID,
			"issued_at_ms": strconv.FormatInt(s.clock.Now().UnixMilli(), 10),
		},
	}
	updatedEnvelope, err := accountauth.Encrypt(s.cipher, candidate.AccountPublicID, updatedPayload)
	if err != nil {
		return routing.Candidate{}, err
	}
	now := s.clock.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE accounts SET credential_enc = ?, credential_expires_at_ms = ?,
		credential_version = credential_version + 1, version = version + 1, updated_at_ms = ?
		WHERE id = ? AND credential_version = ? AND status = 'active' AND auth_kind = 'oauth'`,
		updatedEnvelope, token.ExpiresAtMS, now, candidate.AccountInternalID, credentialVersion)
	if err != nil {
		return routing.Candidate{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return routing.Candidate{}, &Error{Code: "reauth_required"}
	}
	_ = s.control.RecordAudit(ctx, control.Actor{Kind: "system"}, "account.oauth_refreshed", "account", candidate.AccountPublicID, "success", nil)
	reloaded, err := s.routing.LoadAccount(ctx, candidate.AccountPublicID)
	if err != nil {
		return routing.Candidate{}, err
	}
	return reloaded.Candidate, nil
}

func (s *Service) markReauthRequired(ctx context.Context, candidate routing.Candidate, credentialVersion int64) {
	now := s.clock.Now().UnixMilli()
	_, _ = s.db.ExecContext(ctx, `UPDATE accounts SET status = 'reauth_required', version = version + 1,
		updated_at_ms = ? WHERE id = ? AND credential_version = ? AND status = 'active'`,
		now, candidate.AccountInternalID, credentialVersion)
	_ = s.control.RecordAudit(ctx, control.Actor{Kind: "system"}, "account.oauth_refresh_failed", "account", candidate.AccountPublicID, "failure", map[string]any{"reason": "reauth_required"})
}

func refreshDue(expiresAtMS int64, now time.Time, issuedAtMS int64) bool {
	if expiresAtMS <= 0 {
		return true
	}
	window := 5 * time.Minute
	if issuedAtMS > 0 && issuedAtMS < expiresAtMS {
		lifetimeWindow := time.Duration(expiresAtMS-issuedAtMS) * time.Millisecond / 10
		if lifetimeWindow < window {
			window = lifetimeWindow
		}
	}
	if window < time.Minute {
		window = time.Minute
	}
	return expiresAtMS <= now.Add(window).UnixMilli()
}

func (s *Service) RunRefresher(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.refreshSweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) refreshSweep(ctx context.Context) {
	_ = s.cleanupExpiredAttempts(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT public_id FROM accounts
		WHERE auth_kind = 'oauth' AND status = 'active'
		AND credential_expires_at_ms <= ? ORDER BY credential_expires_at_ms LIMIT 100`,
		s.clock.Now().Add(5*time.Minute).UnixMilli())
	if err != nil {
		return
	}
	var accounts []string
	for rows.Next() {
		var publicID string
		if rows.Scan(&publicID) == nil {
			accounts = append(accounts, publicID)
		}
	}
	_ = rows.Close()
	for _, publicID := range accounts {
		if ctx.Err() != nil {
			return
		}
		loaded, err := s.routing.LoadAccount(ctx, publicID)
		if err == nil {
			_, _ = s.EnsureCredential(ctx, loaded.Candidate, false)
		}
	}
}

func (s *Service) cleanupExpiredAttempts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_attempts WHERE expires_at_ms <= ?`, s.clock.Now().UnixMilli())
	return err
}
