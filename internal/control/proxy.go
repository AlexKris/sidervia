package control

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/AlexKris/sidervia/internal/cryptox"
)

func (s *Service) CreateProxy(ctx context.Context, actor Actor, input ProxyInput) (Proxy, error) {
	input, err := validateProxyInput(input)
	if err != nil {
		return Proxy{}, err
	}
	publicID, err := s.ids.Object("proxy")
	if err != nil {
		return Proxy{}, err
	}
	username, err := s.encryptOptional(input.Username, "egress_proxies", publicID, "username_enc")
	if err != nil {
		return Proxy{}, err
	}
	password, err := s.encryptOptional(input.Password, "egress_proxies", publicID, "password_enc")
	if err != nil {
		return Proxy{}, err
	}
	now := s.clock.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proxy{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO egress_proxies(public_id, name, scheme, host, port,
        username_enc, password_enc, tls_server_name, allow_insecure_tls, enabled, created_at_ms, updated_at_ms)
        VALUES(?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?)`, publicID, input.Name, input.Scheme,
		input.Host, input.Port, username, password, input.TLSServerName, boolInt(input.AllowInsecureTLS), boolInt(input.Enabled), now, now)
	if err != nil {
		return Proxy{}, mapSQLError(err)
	}
	if err := s.audit(ctx, tx, actor, "proxy.created", "proxy", publicID, map[string]any{"allow_insecure_tls": input.AllowInsecureTLS}); err != nil {
		return Proxy{}, err
	}
	if err := tx.Commit(); err != nil {
		return Proxy{}, err
	}
	return s.GetProxy(ctx, publicID)
}

func (s *Service) GetProxy(ctx context.Context, publicID string) (Proxy, error) {
	row := s.db.QueryRowContext(ctx, `SELECT public_id, name, scheme, host, port,
        username_enc IS NOT NULL, password_enc IS NOT NULL, COALESCE(tls_server_name, ''),
        allow_insecure_tls, enabled, version, created_at_ms, updated_at_ms
        FROM egress_proxies WHERE public_id = ?`, publicID)
	return scanProxy(row)
}

func (s *Service) ListProxies(ctx context.Context, limit int, after string) (Page[Proxy], error) {
	c, err := decodeCursor(after)
	if err != nil {
		return Page[Proxy]{}, ValidationError{Field: "cursor", Message: "is invalid"}
	}
	limit = normalizeLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, public_id, name, scheme, host, port,
        username_enc IS NOT NULL, password_enc IS NOT NULL, COALESCE(tls_server_name, ''),
        allow_insecure_tls, enabled, version, created_at_ms, updated_at_ms
        FROM egress_proxies WHERE (created_at_ms < ? OR (created_at_ms = ? AND id < ?))
        ORDER BY created_at_ms DESC, id DESC LIMIT ?`, c.Timestamp, c.Timestamp, c.ID, limit+1)
	if err != nil {
		return Page[Proxy]{}, err
	}
	defer rows.Close()
	page := Page[Proxy]{Items: make([]Proxy, 0, limit)}
	var last cursor
	for rows.Next() {
		var internalID, created, updated int64
		var item Proxy
		if err := rows.Scan(&internalID, &item.ID, &item.Name, &item.Scheme, &item.Host, &item.Port,
			&item.UsernameConfigured, &item.PasswordConfigured, &item.TLSServerName, &item.AllowInsecureTLS,
			&item.Enabled, &item.Version, &created, &updated); err != nil {
			return Page[Proxy]{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(last)
			break
		}
		item.CreatedAt, item.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
		page.Items = append(page.Items, item)
		last = cursor{Timestamp: created, ID: internalID}
	}
	return page, rows.Err()
}

func (s *Service) UpdateProxy(ctx context.Context, actor Actor, publicID string, expectedVersion int64, input ProxyInput) (Proxy, error) {
	input, err := validateProxyInput(input)
	if err != nil {
		return Proxy{}, err
	}
	var existingUsername, existingPassword []byte
	err = s.db.QueryRowContext(ctx, "SELECT username_enc, password_enc FROM egress_proxies WHERE public_id = ?", publicID).Scan(&existingUsername, &existingPassword)
	if errors.Is(err, sql.ErrNoRows) {
		return Proxy{}, ErrNotFound
	}
	if err != nil {
		return Proxy{}, err
	}
	username, password := existingUsername, existingPassword
	if input.Username != nil {
		username, err = s.encryptOptional(input.Username, "egress_proxies", publicID, "username_enc")
		if err != nil {
			return Proxy{}, err
		}
	}
	if input.Password != nil {
		password, err = s.encryptOptional(input.Password, "egress_proxies", publicID, "password_enc")
		if err != nil {
			return Proxy{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proxy{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE egress_proxies SET name = ?, scheme = ?, host = ?, port = ?,
        username_enc = ?, password_enc = ?, tls_server_name = NULLIF(?, ''), allow_insecure_tls = ?,
        enabled = ?, version = version + 1, updated_at_ms = ? WHERE public_id = ? AND version = ?`,
		input.Name, input.Scheme, input.Host, input.Port, username, password, input.TLSServerName,
		boolInt(input.AllowInsecureTLS), boolInt(input.Enabled), s.clock.Now().UnixMilli(), publicID, expectedVersion)
	if err != nil {
		return Proxy{}, mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Proxy{}, s.notFoundOrVersion(ctx, tx, "egress_proxies", publicID)
	}
	if err := s.audit(ctx, tx, actor, "proxy.updated", "proxy", publicID, map[string]any{"allow_insecure_tls": input.AllowInsecureTLS}); err != nil {
		return Proxy{}, err
	}
	if err := tx.Commit(); err != nil {
		return Proxy{}, err
	}
	return s.GetProxy(ctx, publicID)
}

func (s *Service) DeleteProxy(ctx context.Context, actor Actor, publicID string, expectedVersion int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM egress_proxies WHERE public_id = ? AND version = ?", publicID, expectedVersion)
	if err != nil {
		return mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return s.notFoundOrVersion(ctx, tx, "egress_proxies", publicID)
	}
	if err := s.audit(ctx, tx, actor, "proxy.deleted", "proxy", publicID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) encryptOptional(value *string, table, publicID, column string) ([]byte, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	return s.cipher.Seal([]byte(*value), cryptox.AAD(table, publicID, column))
}

func (s *Service) notFoundOrVersion(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table, publicID string) error {
	allowed := map[string]bool{"egress_proxies": true, "upstreams": true, "accounts": true, "model_routes": true, "client_keys": true}
	if !allowed[table] {
		return ErrVersion
	}
	var one int
	err := q.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE public_id = ?", publicID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrVersion
}

type rowScanner interface{ Scan(...any) error }

func scanProxy(row rowScanner) (Proxy, error) {
	var item Proxy
	var created, updated int64
	err := row.Scan(&item.ID, &item.Name, &item.Scheme, &item.Host, &item.Port,
		&item.UsernameConfigured, &item.PasswordConfigured, &item.TLSServerName, &item.AllowInsecureTLS,
		&item.Enabled, &item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Proxy{}, ErrNotFound
	}
	if err != nil {
		return Proxy{}, err
	}
	item.CreatedAt, item.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return item, nil
}
