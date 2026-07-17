package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/routing"
)

type Attempt struct {
	ID               string    `json:"id"`
	ProviderID       string    `json:"provider_id"`
	AccountID        string    `json:"account_id"`
	FlowKind         string    `json:"flow_kind"`
	Status           string    `json:"status"`
	AuthorizationURL string    `json:"authorization_url,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
	CreatedAt        time.Time `json:"created_at"`
	Beta             bool      `json:"beta"`
}

type claimedAttempt struct {
	publicID          string
	accountPublicID   string
	adminSessionID    string
	accountVersion    int64
	pkceVerifier      string
	egressFingerprint string
	configPublicID    string
	clientID          string
	clientSecret      string
	projectID         string
	scopes            []string
	candidate         routing.Candidate
}

func (s *Service) CreateAttempt(ctx context.Context, actor control.Actor, adminSessionPublicID, accountPublicID string) (Attempt, error) {
	if adminSessionPublicID == "" || accountPublicID == "" {
		return Attempt{}, &Error{Code: "invalid_request"}
	}
	config, _, err := s.loadGoogleConfigSecret(ctx)
	if err != nil {
		return Attempt{}, err
	}
	if !config.Enabled {
		return Attempt{}, &Error{Code: "oauth_not_configured"}
	}
	account, err := s.routing.LoadAccount(ctx, accountPublicID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Attempt{}, control.ErrNotFound
		}
		return Attempt{}, err
	}
	if account.Candidate.ProviderID != ProviderGoogle || account.Candidate.AuthKind != "oauth" {
		return Attempt{}, control.ValidationError{Field: "account_id", Message: "must identify a Google OAuth account"}
	}
	if account.Status == "disabled" || account.Status == "validating" || account.Status == "active" {
		return Attempt{}, control.ValidationError{Field: "account_id", Message: "account is not awaiting OAuth authorization"}
	}
	state, err := s.ids.Token(32)
	if err != nil {
		return Attempt{}, err
	}
	pkce, err := s.ids.Token(32)
	if err != nil {
		return Attempt{}, err
	}
	publicID, err := s.ids.Object("oauth")
	if err != nil {
		return Attempt{}, err
	}
	pkceEnvelope, err := s.cipher.Seal([]byte(pkce), cryptox.AAD("oauth_attempts", publicID, "pkce_verifier_enc"))
	if err != nil {
		return Attempt{}, err
	}
	now := s.clock.Now().UTC()
	expires := now.Add(attemptTTL)
	fingerprint := egressFingerprint(account.Candidate)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback()
	var sessionID, accountID int64
	err = tx.QueryRowContext(ctx, `SELECT s.id FROM admin_sessions s JOIN admin_user a ON a.id = 1
		WHERE s.public_id = ? AND s.revoked_at_ms IS NULL AND s.session_version = a.session_version
		AND s.idle_expires_at_ms > ? AND s.absolute_expires_at_ms > ?`,
		adminSessionPublicID, now.UnixMilli(), now.UnixMilli()).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, &Error{Code: "admin_session_expired"}
	}
	if err != nil {
		return Attempt{}, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE public_id = ? AND version = ?
		AND auth_kind = 'oauth'`, accountPublicID, account.Candidate.AccountVersion).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, control.ErrVersion
	}
	if err != nil {
		return Attempt{}, err
	}
	var exchanging int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM oauth_attempts
		WHERE account_id = ? AND status = 'exchanging' AND expires_at_ms > ?`, accountID, now.UnixMilli()).Scan(&exchanging); err != nil {
		return Attempt{}, err
	}
	if exchanging > 0 {
		return Attempt{}, control.ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_attempts SET status = 'cancelled', consumed_at_ms = ?
		WHERE account_id = ? AND status = 'pending'`, now.UnixMilli(), accountID); err != nil {
		return Attempt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO oauth_attempts(
		public_id, admin_session_id, provider_id, account_id, flow_kind, state_verifier,
		pkce_verifier_enc, egress_fingerprint, status, created_at_ms, expires_at_ms
	) VALUES(?, ?, 'google', ?, 'authorization_code_pkce', ?, ?, ?, 'pending', ?, ?)`,
		publicID, sessionID, accountID, identifier.Verifier(state), pkceEnvelope, fingerprint,
		now.UnixMilli(), expires.UnixMilli())
	if err != nil {
		return Attempt{}, err
	}
	if err := s.control.RecordAuditTx(ctx, tx, actor, "oauth_attempt.created", "oauth_attempt", publicID, "success", map[string]any{
		"provider_id": "google", "account_id": accountPublicID,
	}); err != nil {
		return Attempt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Attempt{}, err
	}
	authorizationURL := googleAuthorizationURL(config.ClientID, s.redirectURI(), state, pkce, config.Scopes)
	return Attempt{
		ID: publicID, ProviderID: ProviderGoogle, AccountID: accountPublicID,
		FlowKind: "authorization_code_pkce", Status: "pending", AuthorizationURL: authorizationURL,
		ExpiresAt: expires, CreatedAt: now, Beta: true,
	}, nil
}

func (s *Service) GetAttempt(ctx context.Context, adminSessionPublicID, publicID string) (Attempt, error) {
	var result Attempt
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT oa.public_id, oa.provider_id, a.public_id,
		oa.flow_kind, oa.status, oa.created_at_ms, oa.expires_at_ms
		FROM oauth_attempts oa JOIN accounts a ON a.id = oa.account_id
		JOIN admin_sessions s ON s.id = oa.admin_session_id
		WHERE oa.public_id = ? AND s.public_id = ?`, publicID, adminSessionPublicID).Scan(
		&result.ID, &result.ProviderID, &result.AccountID, &result.FlowKind, &result.Status, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, control.ErrNotFound
	}
	if err != nil {
		return Attempt{}, err
	}
	result.CreatedAt, result.ExpiresAt = time.UnixMilli(created).UTC(), time.UnixMilli(expires).UTC()
	result.Beta = true
	return result, nil
}

func (s *Service) CancelAttempt(ctx context.Context, actor control.Actor, adminSessionPublicID, publicID string) error {
	now := s.clock.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE oauth_attempts SET status = 'cancelled', consumed_at_ms = ?
		WHERE public_id = ? AND status = 'pending' AND admin_session_id = (
			SELECT id FROM admin_sessions WHERE public_id = ?
		)`, now, publicID, adminSessionPublicID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return control.ErrNotFound
	}
	if err := s.control.RecordAuditTx(ctx, tx, actor, "oauth_attempt.cancelled", "oauth_attempt", publicID, "success", nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) claimAttempt(ctx context.Context, state, requiredSessionPublicID, expectedAttemptID string) (claimedAttempt, error) {
	if state == "" || len(state) > 1024 || strings.TrimSpace(state) != state || strings.ContainsAny(state, "\r\n\x00") {
		return claimedAttempt{}, &Error{Code: "oauth_state_invalid"}
	}
	verifier := identifier.Verifier(state)
	now := s.clock.Now().UnixMilli()
	var claimed claimedAttempt
	var pkceEnvelope, clientSecretEnvelope, scopesJSON []byte
	var sessionVersion, adminVersion int64
	var revoked sql.NullInt64
	var idleExpires, absoluteExpires int64
	err := s.db.QueryRowContext(ctx, `SELECT oa.public_id, a.public_id, a.version,
		s.public_id, s.session_version, au.session_version, s.revoked_at_ms,
		s.idle_expires_at_ms, s.absolute_expires_at_ms,
		oa.pkce_verifier_enc, oa.egress_fingerprint,
		cfg.public_id, cfg.client_id, cfg.client_secret_enc, cfg.project_id, cfg.scopes_json
		FROM oauth_attempts oa
		JOIN accounts a ON a.id = oa.account_id
		JOIN admin_sessions s ON s.id = oa.admin_session_id
		JOIN admin_user au ON au.id = 1
		JOIN provider_oauth_configs cfg ON cfg.provider_id = oa.provider_id AND cfg.enabled = 1
		WHERE oa.state_verifier = ? AND oa.status = 'pending' AND oa.expires_at_ms > ?
		AND (? = '' OR oa.public_id = ?)`,
		verifier, now, expectedAttemptID, expectedAttemptID).Scan(
		&claimed.publicID, &claimed.accountPublicID, &claimed.accountVersion,
		&claimed.adminSessionID, &sessionVersion, &adminVersion, &revoked,
		&idleExpires, &absoluteExpires, &pkceEnvelope, &claimed.egressFingerprint,
		&claimed.configPublicID, &claimed.clientID, &clientSecretEnvelope, &claimed.projectID, &scopesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		constantStateCompare(verifier)
		return claimedAttempt{}, &Error{Code: "oauth_state_invalid"}
	}
	if err != nil {
		return claimedAttempt{}, err
	}
	if revoked.Valid || sessionVersion != adminVersion || idleExpires <= now || absoluteExpires <= now {
		return claimedAttempt{}, &Error{Code: "admin_session_expired"}
	}
	if requiredSessionPublicID != "" && subtle.ConstantTimeCompare([]byte(requiredSessionPublicID), []byte(claimed.adminSessionID)) != 1 {
		return claimedAttempt{}, &Error{Code: "oauth_session_mismatch"}
	}
	account, err := s.routing.LoadAccount(ctx, claimed.accountPublicID)
	if err != nil || account.Candidate.AccountVersion != claimed.accountVersion || egressFingerprint(account.Candidate) != claimed.egressFingerprint {
		return claimedAttempt{}, &Error{Code: "oauth_egress_changed"}
	}
	claimed.candidate = account.Candidate
	pkce, err := s.cipher.Open(pkceEnvelope, cryptox.AAD("oauth_attempts", claimed.publicID, "pkce_verifier_enc"))
	if err != nil {
		return claimedAttempt{}, &Error{Code: "oauth_attempt_invalid"}
	}
	secret, err := s.cipher.Open(clientSecretEnvelope, cryptox.AAD("provider_oauth_configs", claimed.configPublicID, "client_secret_enc"))
	if err != nil {
		return claimedAttempt{}, &Error{Code: "oauth_config_invalid"}
	}
	claimed.pkceVerifier, claimed.clientSecret = string(pkce), string(secret)
	claimed.scopes, err = decodeScopes(scopesJSON)
	if err != nil {
		return claimedAttempt{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE oauth_attempts SET status = 'exchanging'
		WHERE public_id = ? AND status = 'pending' AND expires_at_ms > ?
		AND admin_session_id IN (SELECT s.id FROM admin_sessions s JOIN admin_user a ON a.id = 1
			WHERE s.public_id = ? AND s.revoked_at_ms IS NULL AND s.session_version = a.session_version
			AND s.idle_expires_at_ms > ? AND s.absolute_expires_at_ms > ?)`,
		claimed.publicID, now, claimed.adminSessionID, now, now)
	if err != nil {
		return claimedAttempt{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return claimedAttempt{}, &Error{Code: "oauth_state_invalid"}
	}
	return claimed, nil
}

func googleAuthorizationURL(clientID, redirectURI, state, pkce string, scopes []string) string {
	challenge := sha256.Sum256([]byte(pkce))
	query := make(url.Values)
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", strings.Join(scopes, " "))
	query.Set("state", state)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	query.Set("access_type", "offline")
	query.Set("prompt", "consent")
	return "https://accounts.google.com/o/oauth2/v2/auth?" + query.Encode()
}

func egressFingerprint(candidate routing.Candidate) string {
	proxy := "direct"
	if candidate.Proxy != nil {
		proxy = fmt.Sprintf("%s:%d", candidate.Proxy.PublicID, candidate.Proxy.Version)
	}
	value := fmt.Sprintf("v1:%s:%d:%s:%d:%s", candidate.AccountPublicID, candidate.AccountVersion,
		candidate.UpstreamPublicID, candidate.UpstreamVersion, proxy)
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func constantStateCompare(value string) {
	expected := identifier.Verifier("invalid-oauth-state")
	_ = subtle.ConstantTimeCompare([]byte(value), []byte(expected))
}
