package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
)

var projectIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

type GoogleConfigInput struct {
	ClientID     string
	ClientSecret *string
	ProjectID    string
	Enabled      bool
}

type GoogleConfig struct {
	ID              string    `json:"id"`
	ProviderID      string    `json:"provider_id"`
	ClientID        string    `json:"client_id"`
	ClientSecretSet bool      `json:"client_secret_configured"`
	ProjectID       string    `json:"project_id"`
	Scopes          []string  `json:"scopes"`
	RedirectURI     string    `json:"redirect_uri"`
	Enabled         bool      `json:"enabled"`
	Beta            bool      `json:"beta"`
	Version         int64     `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (s *Service) CreateGoogleConfig(ctx context.Context, actor control.Actor, input GoogleConfigInput) (GoogleConfig, error) {
	input, err := validateGoogleConfig(input, true)
	if err != nil {
		return GoogleConfig{}, err
	}
	publicID, err := s.ids.Object("oauthcfg")
	if err != nil {
		return GoogleConfig{}, err
	}
	secret, err := s.cipher.Seal([]byte(*input.ClientSecret), cryptox.AAD("provider_oauth_configs", publicID, "client_secret_enc"))
	if err != nil {
		return GoogleConfig{}, err
	}
	scopes, _ := json.Marshal(map[string]any{"schema_version": 1, "values": googleScopes})
	now := s.clock.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return GoogleConfig{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO provider_oauth_configs(
		public_id, provider_id, client_id, client_secret_enc, project_id, scopes_json,
		enabled, created_at_ms, updated_at_ms
	) VALUES(?, 'google', ?, ?, ?, ?, ?, ?, ?)`,
		publicID, input.ClientID, secret, input.ProjectID, string(scopes), boolInt(input.Enabled), now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return GoogleConfig{}, control.ErrConflict
		}
		return GoogleConfig{}, err
	}
	if err := s.control.RecordAuditTx(ctx, tx, actor, "oauth_config.created", "oauth_config", publicID, "success", map[string]any{"provider_id": "google"}); err != nil {
		return GoogleConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return GoogleConfig{}, err
	}
	return s.GetGoogleConfig(ctx)
}

func (s *Service) UpdateGoogleConfig(ctx context.Context, actor control.Actor, expectedVersion int64, input GoogleConfigInput) (GoogleConfig, error) {
	input, err := validateGoogleConfig(input, false)
	if err != nil {
		return GoogleConfig{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return GoogleConfig{}, err
	}
	defer tx.Rollback()
	var publicID string
	var existingSecret []byte
	if err := tx.QueryRowContext(ctx, `SELECT public_id, client_secret_enc FROM provider_oauth_configs
		WHERE provider_id = 'google'`).Scan(&publicID, &existingSecret); errors.Is(err, sql.ErrNoRows) {
		return GoogleConfig{}, control.ErrNotFound
	} else if err != nil {
		return GoogleConfig{}, err
	}
	secret := existingSecret
	if input.ClientSecret != nil {
		secret, err = s.cipher.Seal([]byte(*input.ClientSecret), cryptox.AAD("provider_oauth_configs", publicID, "client_secret_enc"))
		if err != nil {
			return GoogleConfig{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE provider_oauth_configs SET client_id = ?, client_secret_enc = ?,
		project_id = ?, enabled = ?, version = version + 1, updated_at_ms = ?
		WHERE provider_id = 'google' AND version = ?`, input.ClientID, secret, input.ProjectID,
		boolInt(input.Enabled), s.clock.Now().UnixMilli(), expectedVersion)
	if err != nil {
		return GoogleConfig{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return GoogleConfig{}, control.ErrVersion
	}
	if err := s.control.RecordAuditTx(ctx, tx, actor, "oauth_config.updated", "oauth_config", publicID, "success", map[string]any{"provider_id": "google"}); err != nil {
		return GoogleConfig{}, err
	}
	if err := tx.Commit(); err != nil {
		return GoogleConfig{}, err
	}
	return s.GetGoogleConfig(ctx)
}

func (s *Service) GetGoogleConfig(ctx context.Context) (GoogleConfig, error) {
	var value GoogleConfig
	var scopesJSON []byte
	var created, updated int64
	err := s.db.QueryRowContext(ctx, `SELECT public_id, provider_id, client_id,
		client_secret_enc IS NOT NULL, project_id, scopes_json, enabled, version, created_at_ms, updated_at_ms
		FROM provider_oauth_configs WHERE provider_id = 'google'`).Scan(
		&value.ID, &value.ProviderID, &value.ClientID, &value.ClientSecretSet, &value.ProjectID,
		&scopesJSON, &value.Enabled, &value.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return GoogleConfig{}, control.ErrNotFound
	}
	if err != nil {
		return GoogleConfig{}, err
	}
	value.Scopes, err = decodeScopes(scopesJSON)
	if err != nil {
		return GoogleConfig{}, errors.New("OAuth scope configuration is invalid")
	}
	value.RedirectURI = s.redirectURI()
	value.Beta = true
	value.CreatedAt, value.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return value, nil
}

func validateGoogleConfig(input GoogleConfigInput, create bool) (GoogleConfigInput, error) {
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	if input.ClientID == "" || len(input.ClientID) > 512 || strings.ContainsAny(input.ClientID, "\r\n\x00") {
		return GoogleConfigInput{}, control.ValidationError{Field: "client_id", Message: "is invalid"}
	}
	if !projectIDPattern.MatchString(input.ProjectID) {
		return GoogleConfigInput{}, control.ValidationError{Field: "project_id", Message: "must be a valid Google Cloud project ID"}
	}
	if create && input.ClientSecret == nil {
		return GoogleConfigInput{}, control.ValidationError{Field: "client_secret", Message: "is required"}
	}
	if input.ClientSecret != nil {
		if *input.ClientSecret == "" || len(*input.ClientSecret) > 8192 || strings.TrimSpace(*input.ClientSecret) != *input.ClientSecret || strings.ContainsAny(*input.ClientSecret, "\r\n\x00") {
			return GoogleConfigInput{}, control.ValidationError{Field: "client_secret", Message: "is invalid"}
		}
	}
	return input, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
