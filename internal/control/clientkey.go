package control

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Service) CreateClientKey(ctx context.Context, actor Actor, name string, expiresAt *time.Time) (CreatedClientKey, error) {
	name, err := cleanName("name", name)
	if err != nil {
		return CreatedClientKey{}, err
	}
	if expiresAt != nil && !expiresAt.After(s.clock.Now()) {
		return CreatedClientKey{}, ValidationError{Field: "expires_at", Message: "must be in the future"}
	}
	publicID, err := s.ids.Object("ckey")
	if err != nil {
		return CreatedClientKey{}, err
	}
	full, prefix, verifier, err := s.ids.ClientKey()
	if err != nil {
		return CreatedClientKey{}, err
	}
	now := s.clock.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CreatedClientKey{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO client_keys(public_id, name, prefix, secret_verifier,
        status, created_at_ms, expires_at_ms) VALUES(?, ?, ?, ?, 'active', ?, ?)`,
		publicID, name, prefix, verifier, now, nullableTimeMillis(expiresAt))
	if err != nil {
		return CreatedClientKey{}, mapSQLError(err)
	}
	if err := s.audit(ctx, tx, actor, "client_key.created", "client_key", publicID, map[string]any{"prefix": prefix}); err != nil {
		return CreatedClientKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return CreatedClientKey{}, err
	}
	item, err := s.GetClientKey(ctx, publicID)
	if err != nil {
		return CreatedClientKey{}, err
	}
	return CreatedClientKey{ClientKey: item, Secret: full}, nil
}

func (s *Service) GetClientKey(ctx context.Context, publicID string) (ClientKey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT public_id, name, prefix, status, expires_at_ms,
        last_used_at_ms, version, created_at_ms FROM client_keys WHERE public_id = ?`, publicID)
	return scanClientKey(row)
}

func (s *Service) ListClientKeys(ctx context.Context, limit int, after string) (Page[ClientKey], error) {
	c, err := decodeCursor(after)
	if err != nil {
		return Page[ClientKey]{}, ValidationError{Field: "cursor", Message: "is invalid"}
	}
	limit = normalizeLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, public_id, name, prefix, status, expires_at_ms,
        last_used_at_ms, version, created_at_ms FROM client_keys
        WHERE (created_at_ms < ? OR (created_at_ms = ? AND id < ?))
        ORDER BY created_at_ms DESC, id DESC LIMIT ?`, c.Timestamp, c.Timestamp, c.ID, limit+1)
	if err != nil {
		return Page[ClientKey]{}, err
	}
	defer rows.Close()
	page := Page[ClientKey]{Items: make([]ClientKey, 0, limit)}
	var last cursor
	for rows.Next() {
		var internalID, created int64
		var expiry, lastUsed sql.NullInt64
		var item ClientKey
		if err := rows.Scan(&internalID, &item.ID, &item.Name, &item.Prefix, &item.Status,
			&expiry, &lastUsed, &item.Version, &created); err != nil {
			return Page[ClientKey]{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(last)
			break
		}
		item.ExpiresAt, item.LastUsedAt = timePtr(expiry), timePtr(lastUsed)
		item.CreatedAt = time.UnixMilli(created).UTC()
		page.Items = append(page.Items, item)
		last = cursor{Timestamp: created, ID: internalID}
	}
	return page, rows.Err()
}

func (s *Service) UpdateClientKey(ctx context.Context, actor Actor, publicID string, expectedVersion int64, name, status string, expiresAt *time.Time) (ClientKey, error) {
	name, err := cleanName("name", name)
	if err != nil {
		return ClientKey{}, err
	}
	status = strings.TrimSpace(status)
	if status != "active" && status != "disabled" {
		return ClientKey{}, ValidationError{Field: "status", Message: "must be active or disabled"}
	}
	if expiresAt != nil && !expiresAt.After(s.clock.Now()) {
		return ClientKey{}, ValidationError{Field: "expires_at", Message: "must be in the future"}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ClientKey{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE client_keys SET name = ?, status = ?, expires_at_ms = ?,
        version = version + 1 WHERE public_id = ? AND version = ? AND status <> 'revoked'`,
		name, status, nullableTimeMillis(expiresAt), publicID, expectedVersion)
	if err != nil {
		return ClientKey{}, mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ClientKey{}, s.notFoundOrVersion(ctx, tx, "client_keys", publicID)
	}
	if err := s.audit(ctx, tx, actor, "client_key.updated", "client_key", publicID, map[string]any{"status": status}); err != nil {
		return ClientKey{}, err
	}
	if err := tx.Commit(); err != nil {
		return ClientKey{}, err
	}
	return s.GetClientKey(ctx, publicID)
}

func (s *Service) RevokeClientKey(ctx context.Context, actor Actor, publicID string, expectedVersion int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.clock.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `UPDATE client_keys SET status = 'revoked', revoked_at_ms = ?,
        version = version + 1 WHERE public_id = ? AND version = ? AND status <> 'revoked'`, now, publicID, expectedVersion)
	if err != nil {
		return mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return s.notFoundOrVersion(ctx, tx, "client_keys", publicID)
	}
	if err := s.audit(ctx, tx, actor, "client_key.revoked", "client_key", publicID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func scanClientKey(row rowScanner) (ClientKey, error) {
	var item ClientKey
	var expiry, lastUsed sql.NullInt64
	var created int64
	err := row.Scan(&item.ID, &item.Name, &item.Prefix, &item.Status, &expiry, &lastUsed, &item.Version, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return ClientKey{}, ErrNotFound
	}
	if err != nil {
		return ClientKey{}, err
	}
	item.ExpiresAt, item.LastUsedAt = timePtr(expiry), timePtr(lastUsed)
	item.CreatedAt = time.UnixMilli(created).UTC()
	return item, nil
}
