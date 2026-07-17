package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/AlexKris/sidervia/internal/accountauth"
)

func (s *Service) BeginAccountValidation(ctx context.Context, actor Actor, publicID string, expectedVersion int64) (Account, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	now := s.clock.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `UPDATE accounts SET status = 'validating', version = version + 1,
		updated_at_ms = ? WHERE public_id = ? AND version = ? AND status <> 'validating'`,
		now, publicID, expectedVersion)
	if err != nil {
		return Account{}, err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Account{}, s.notFoundOrVersion(ctx, tx, "accounts", publicID)
	}
	if err := s.audit(ctx, tx, actor, "account.validation_started", "account", publicID, nil); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, publicID)
}

func (s *Service) FinishAccountValidation(ctx context.Context, actor Actor, publicID string, expectedVersion int64, validation *AccountValidation, failureCode string) (Account, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	now := s.clock.Now().UnixMilli()
	status := "invalid"
	outcome := "failure"
	identityJSON := `{"schema_version":1}`
	capabilityVersion := any(nil)
	models := []string(nil)
	if validation != nil {
		status, outcome = "active", "success"
		identity := map[string]any{"schema_version": 1}
		for key, value := range validation.Identity {
			if key == "provider_id" || key == "model_count" {
				identity[key] = value
			}
		}
		body, err := json.Marshal(identity)
		if err != nil || len(body) > 4096 {
			return Account{}, errors.New("account identity metadata is invalid")
		}
		identityJSON = string(body)
		if validation.CapabilityVersion == "" || len(validation.CapabilityVersion) > 200 {
			return Account{}, errors.New("account capability version is invalid")
		}
		capabilityVersion = validation.CapabilityVersion
		models, err = normalizeModels(validation.Models)
		if err != nil {
			return Account{}, err
		}
	} else if !safeReasonCode(failureCode) {
		return Account{}, errors.New("account validation failure code is invalid")
	}
	result, err := tx.ExecContext(ctx, `UPDATE accounts SET status = ?, identity_json = ?,
		capability_version = ?, last_validated_at_ms = ?, version = version + 1, updated_at_ms = ?
		WHERE public_id = ? AND version = ? AND status = 'validating'
		AND (? <> 'active' OR credential_enc IS NOT NULL)`,
		status, identityJSON, capabilityVersion, now, now, publicID, expectedVersion, status)
	if err != nil {
		return Account{}, err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Account{}, s.notFoundOrVersion(ctx, tx, "accounts", publicID)
	}
	var accountID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM accounts WHERE public_id = ?", publicID).Scan(&accountID); err != nil {
		return Account{}, err
	}
	if validation != nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM account_models WHERE account_id = ?", accountID); err != nil {
			return Account{}, err
		}
		capabilities := `{"schema_version":1,"values":["stream","text"]}`
		for _, model := range models {
			if _, err := tx.ExecContext(ctx, `INSERT INTO account_models(
				account_id, upstream_model_id, capabilities_json, source, verified_at_ms, enabled
			) VALUES(?, ?, ?, 'discovered', ?, 1)`, accountID, model, capabilities, now); err != nil {
				return Account{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_runtime(
			account_id, failure_streak, last_success_at_ms, quota_json, updated_at_ms
		) VALUES(?, 0, ?, '{"schema_version":1}', ?)
		ON CONFLICT(account_id) DO UPDATE SET failure_streak = 0, cooldown_until_ms = NULL,
			quota_reset_at_ms = NULL, last_error_code = NULL, updated_at_ms = excluded.updated_at_ms`,
			accountID, now, now); err != nil {
			return Account{}, err
		}
	}
	metadata := map[string]any{"model_count": len(models)}
	if failureCode != "" {
		metadata["reason"] = failureCode
	}
	if err := s.auditOutcome(ctx, tx, actor, "account.validation_completed", "account", publicID, outcome, metadata); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, publicID)
}

func normalizeModels(models []string) ([]string, error) {
	if len(models) > 1000 {
		return nil, errors.New("provider returned too many models")
	}
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 200 || strings.ContainsAny(model, "\r\n\x00") {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	sort.Strings(result)
	return result, nil
}

func safeReasonCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func (s *Service) AccountCredentialState(ctx context.Context, publicID string) (authKind string, configured bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT auth_kind, credential_enc IS NOT NULL FROM accounts WHERE public_id = ?", publicID).Scan(&authKind, &configured)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, ErrNotFound
	}
	return authKind, configured, err
}

func (s *Service) RecoverInterruptedAccountValidations(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT public_id FROM accounts WHERE status = 'validating' ORDER BY id`)
	if err != nil {
		return 0, err
	}
	var accountIDs []string
	for rows.Next() {
		var publicID string
		if err := rows.Scan(&publicID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		accountIDs = append(accountIDs, publicID)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	now := s.clock.Now().UnixMilli()
	for _, publicID := range accountIDs {
		result, err := tx.ExecContext(ctx, `UPDATE accounts SET status = 'invalid', last_validated_at_ms = ?,
			version = version + 1, updated_at_ms = ? WHERE public_id = ? AND status = 'validating'`, now, now, publicID)
		if err != nil {
			return 0, err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return 0, ErrConflict
		}
		if err := s.auditOutcome(ctx, tx, Actor{Kind: "system"}, "account.validation_recovered", "account", publicID, "failure", map[string]any{"reason": "process_interrupted"}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(accountIDs), nil
}

func (s *Service) CompleteOAuthAccount(ctx context.Context, actor Actor, attemptPublicID, accountPublicID string, expectedAccountVersion int64, payload accountauth.Payload, validation AccountValidation) (Account, error) {
	if payload.SchemaVersion != 1 || payload.AccessToken == "" || payload.RefreshToken == "" || payload.ExpiresAtMS <= s.clock.Now().UnixMilli() {
		return Account{}, errors.New("OAuth credential payload is invalid")
	}
	models, err := normalizeModels(validation.Models)
	if err != nil {
		return Account{}, err
	}
	if validation.CapabilityVersion == "" || len(validation.CapabilityVersion) > 200 {
		return Account{}, errors.New("account capability version is invalid")
	}
	identity := map[string]any{"schema_version": 1}
	for key, value := range validation.Identity {
		if key == "provider_id" || key == "model_count" {
			identity[key] = value
		}
	}
	identityBody, err := json.Marshal(identity)
	if err != nil || len(identityBody) > 4096 {
		return Account{}, errors.New("account identity metadata is invalid")
	}
	credential, err := accountauth.Encrypt(s.cipher, accountPublicID, payload)
	if err != nil {
		return Account{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	now := s.clock.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `UPDATE accounts SET credential_enc = ?, credential_expires_at_ms = ?,
		credential_version = credential_version + 1, status = 'active', identity_json = ?, capability_version = ?,
		last_validated_at_ms = ?, version = version + 1, updated_at_ms = ?
		WHERE public_id = ? AND version = ? AND auth_kind = 'oauth' AND status <> 'disabled'`,
		credential, payload.ExpiresAtMS, string(identityBody), validation.CapabilityVersion,
		now, now, accountPublicID, expectedAccountVersion)
	if err != nil {
		return Account{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return Account{}, s.notFoundOrVersion(ctx, tx, "accounts", accountPublicID)
	}
	var accountID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM accounts WHERE public_id = ?", accountPublicID).Scan(&accountID); err != nil {
		return Account{}, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM account_models WHERE account_id = ?", accountID); err != nil {
		return Account{}, err
	}
	capabilities := `{"schema_version":1,"values":["stream","text"]}`
	for _, model := range models {
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_models(
			account_id, upstream_model_id, capabilities_json, source, verified_at_ms, enabled
		) VALUES(?, ?, ?, 'discovered', ?, 1)`, accountID, model, capabilities, now); err != nil {
			return Account{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_runtime(
		account_id, failure_streak, last_success_at_ms, quota_json, updated_at_ms
	) VALUES(?, 0, ?, '{"schema_version":1}', ?)
	ON CONFLICT(account_id) DO UPDATE SET failure_streak = 0, cooldown_until_ms = NULL,
		quota_reset_at_ms = NULL, last_error_code = NULL, updated_at_ms = excluded.updated_at_ms`,
		accountID, now, now); err != nil {
		return Account{}, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE oauth_attempts SET status = 'consumed', consumed_at_ms = ?
		WHERE public_id = ? AND account_id = ? AND status = 'exchanging' AND expires_at_ms > ?`,
		now, attemptPublicID, accountID, now)
	if err != nil {
		return Account{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return Account{}, ErrConflict
	}
	if err := s.audit(ctx, tx, actor, "account.oauth_connected", "account", accountPublicID, map[string]any{
		"provider_id": "google", "model_count": len(models), "attempt_id": attemptPublicID,
	}); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, accountPublicID)
}
