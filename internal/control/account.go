package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/cryptox"
)

func (s *Service) CreateAccount(ctx context.Context, actor Actor, input AccountInput) (Account, error) {
	input, err := s.validateAccountInput(input, true)
	if err != nil {
		return Account{}, err
	}
	publicID, err := s.ids.Object("acct")
	if err != nil {
		return Account{}, err
	}
	credential, err := s.encryptCredential(publicID, *input.Credential)
	if err != nil {
		return Account{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	upstreamID, err := lookupID(ctx, tx, "upstreams", input.UpstreamID)
	if err != nil {
		return Account{}, err
	}
	proxyID, err := optionalInternalID(ctx, tx, "egress_proxies", validOptionalReference(input.ProxyID))
	if err != nil {
		return Account{}, err
	}
	now := s.clock.Now().UnixMilli()
	_, err = tx.ExecContext(ctx, `INSERT INTO accounts(public_id, upstream_id, name, auth_kind,
        billing_kind, credential_enc, credential_expires_at_ms, proxy_id, status, priority,
        max_concurrency, created_at_ms, updated_at_ms) VALUES(?, ?, ?, 'api_key', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		publicID, upstreamID, input.Name, input.BillingKind, credential, nullableTimeMillis(input.CredentialExpiresAt),
		proxyID, input.Status, *input.Priority, *input.MaxConcurrency, now, now)
	if err != nil {
		return Account{}, mapSQLError(err)
	}
	if err := s.audit(ctx, tx, actor, "account.created", "account", publicID, map[string]any{"billing_kind": input.BillingKind, "status": input.Status}); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, publicID)
}

func (s *Service) GetAccount(ctx context.Context, publicID string) (Account, error) {
	row := s.db.QueryRowContext(ctx, `SELECT a.public_id, u.public_id, a.name, a.auth_kind,
        a.billing_kind, a.credential_enc IS NOT NULL, a.credential_expires_at_ms, p.public_id,
        a.status, a.priority, a.max_concurrency, a.version, a.created_at_ms, a.updated_at_ms
        FROM accounts a JOIN upstreams u ON u.id = a.upstream_id
        LEFT JOIN egress_proxies p ON p.id = a.proxy_id WHERE a.public_id = ?`, publicID)
	return scanAccount(row)
}

func (s *Service) ListAccounts(ctx context.Context, limit int, after string) (Page[Account], error) {
	c, err := decodeCursor(after)
	if err != nil {
		return Page[Account]{}, ValidationError{Field: "cursor", Message: "is invalid"}
	}
	limit = normalizeLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.public_id, u.public_id, a.name, a.auth_kind,
        a.billing_kind, a.credential_enc IS NOT NULL, a.credential_expires_at_ms, p.public_id,
        a.status, a.priority, a.max_concurrency, a.version, a.created_at_ms, a.updated_at_ms
        FROM accounts a JOIN upstreams u ON u.id = a.upstream_id
        LEFT JOIN egress_proxies p ON p.id = a.proxy_id
        WHERE (a.created_at_ms < ? OR (a.created_at_ms = ? AND a.id < ?))
        ORDER BY a.created_at_ms DESC, a.id DESC LIMIT ?`, c.Timestamp, c.Timestamp, c.ID, limit+1)
	if err != nil {
		return Page[Account]{}, err
	}
	defer rows.Close()
	page := Page[Account]{Items: make([]Account, 0, limit)}
	var last cursor
	for rows.Next() {
		var internalID, created, updated int64
		var expiry sql.NullInt64
		var proxy sql.NullString
		var item Account
		if err := rows.Scan(&internalID, &item.ID, &item.UpstreamID, &item.Name, &item.AuthKind,
			&item.BillingKind, &item.CredentialConfigured, &expiry, &proxy, &item.Status, &item.Priority,
			&item.MaxConcurrency, &item.Version, &created, &updated); err != nil {
			return Page[Account]{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(last)
			break
		}
		item.CredentialExpiresAt = timePtr(expiry)
		if proxy.Valid {
			item.ProxyID = &proxy.String
		}
		item.CreatedAt, item.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
		page.Items = append(page.Items, item)
		last = cursor{Timestamp: created, ID: internalID}
	}
	return page, rows.Err()
}

func (s *Service) UpdateAccount(ctx context.Context, actor Actor, publicID string, expectedVersion int64, input AccountInput) (Account, error) {
	input, err := s.validateAccountInput(input, false)
	if err != nil {
		return Account{}, err
	}
	var existingCredential []byte
	err = s.db.QueryRowContext(ctx, "SELECT credential_enc FROM accounts WHERE public_id = ?", publicID).Scan(&existingCredential)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	credential := existingCredential
	if input.Credential != nil {
		credential, err = s.encryptCredential(publicID, *input.Credential)
		if err != nil {
			return Account{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	upstreamID, err := lookupID(ctx, tx, "upstreams", input.UpstreamID)
	if err != nil {
		return Account{}, err
	}
	proxyID, err := optionalInternalID(ctx, tx, "egress_proxies", validOptionalReference(input.ProxyID))
	if err != nil {
		return Account{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE accounts SET upstream_id = ?, name = ?, billing_kind = ?,
        credential_enc = ?, credential_expires_at_ms = ?, proxy_id = ?, status = ?, priority = ?,
        max_concurrency = ?, version = version + 1, updated_at_ms = ? WHERE public_id = ? AND version = ?`,
		upstreamID, input.Name, input.BillingKind, credential, nullableTimeMillis(input.CredentialExpiresAt),
		proxyID, input.Status, *input.Priority, *input.MaxConcurrency, s.clock.Now().UnixMilli(), publicID, expectedVersion)
	if err != nil {
		return Account{}, mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Account{}, s.notFoundOrVersion(ctx, tx, "accounts", publicID)
	}
	if err := s.audit(ctx, tx, actor, "account.updated", "account", publicID, map[string]any{"billing_kind": input.BillingKind, "status": input.Status}); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, publicID)
}

func (s *Service) DeleteAccount(ctx context.Context, actor Actor, publicID string, expectedVersion int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM accounts WHERE public_id = ? AND version = ?", publicID, expectedVersion)
	if err != nil {
		return mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return s.notFoundOrVersion(ctx, tx, "accounts", publicID)
	}
	if err := s.audit(ctx, tx, actor, "account.deleted", "account", publicID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) validateAccountInput(input AccountInput, create bool) (AccountInput, error) {
	var err error
	input.Name, err = cleanName("name", input.Name)
	if err != nil {
		return AccountInput{}, err
	}
	input.UpstreamID = strings.TrimSpace(input.UpstreamID)
	if input.UpstreamID == "" {
		return AccountInput{}, ValidationError{Field: "upstream_id", Message: "is required"}
	}
	if create && input.Credential == nil {
		return AccountInput{}, ValidationError{Field: "credential", Message: "is required"}
	}
	if input.Credential != nil {
		clean := strings.TrimSpace(*input.Credential)
		if clean == "" || len(clean) > 8192 || strings.ContainsAny(clean, "\r\n\x00") {
			return AccountInput{}, ValidationError{Field: "credential", Message: "is invalid"}
		}
		input.Credential = &clean
	}
	if input.BillingKind == "" {
		input.BillingKind = "subscription"
	}
	if input.BillingKind != "subscription" && input.BillingKind != "metered" && input.BillingKind != "custom" {
		return AccountInput{}, ValidationError{Field: "billing_kind", Message: "must be subscription, metered, or custom"}
	}
	if input.Status == "" {
		input.Status = "draft"
	}
	if input.Status != "draft" && input.Status != "disabled" {
		return AccountInput{}, ValidationError{Field: "status", Message: "v0.1 only supports draft or disabled"}
	}
	defaultPriority, defaultConcurrency := 20, 4
	if input.BillingKind == "subscription" {
		defaultPriority, defaultConcurrency = 10, 1
	}
	if input.Priority == nil {
		input.Priority = &defaultPriority
	}
	if input.MaxConcurrency == nil {
		input.MaxConcurrency = &defaultConcurrency
	}
	if *input.Priority < 0 || *input.Priority > 1_000_000 {
		return AccountInput{}, ValidationError{Field: "priority", Message: "must be between 0 and 1000000"}
	}
	if *input.MaxConcurrency < 1 || *input.MaxConcurrency > 1024 {
		return AccountInput{}, ValidationError{Field: "max_concurrency", Message: "must be between 1 and 1024"}
	}
	if input.CredentialExpiresAt != nil && input.CredentialExpiresAt.Before(s.clock.Now()) {
		return AccountInput{}, ValidationError{Field: "credential_expires_at", Message: "must be in the future"}
	}
	return input, nil
}

func (s *Service) encryptCredential(publicID, apiKey string) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"schema_version": 1, "api_key": apiKey})
	if err != nil {
		return nil, err
	}
	return s.cipher.Seal(body, cryptox.AAD("accounts", publicID, "credential_enc"))
}

func scanAccount(row rowScanner) (Account, error) {
	var item Account
	var expiry sql.NullInt64
	var proxy sql.NullString
	var created, updated int64
	err := row.Scan(&item.ID, &item.UpstreamID, &item.Name, &item.AuthKind, &item.BillingKind,
		&item.CredentialConfigured, &expiry, &proxy, &item.Status, &item.Priority, &item.MaxConcurrency,
		&item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	item.CredentialExpiresAt = timePtr(expiry)
	if proxy.Valid {
		item.ProxyID = &proxy.String
	}
	item.CreatedAt, item.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return item, nil
}
