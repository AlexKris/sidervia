package control

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Service) CreateUpstream(ctx context.Context, actor Actor, input UpstreamInput) (Upstream, error) {
	input.DefaultProxyID = validOptionalReference(input.DefaultProxyID)
	var err error
	input.Name, err = cleanName("name", input.Name)
	if err != nil {
		return Upstream{}, err
	}
	if !s.providerExists(input.ProviderID) {
		return Upstream{}, ValidationError{Field: "provider_id", Message: "is not a supported provider descriptor"}
	}
	input.BaseURL, err = normalizeBaseURL(input.BaseURL, input.AllowPrivateNetwork)
	if err != nil {
		return Upstream{}, err
	}
	publicID, err := s.ids.Object("up")
	if err != nil {
		return Upstream{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Upstream{}, err
	}
	defer tx.Rollback()
	proxyID, err := optionalInternalID(ctx, tx, "egress_proxies", input.DefaultProxyID)
	if err != nil {
		return Upstream{}, err
	}
	now := s.clock.Now().UnixMilli()
	_, err = tx.ExecContext(ctx, `INSERT INTO upstreams(public_id, provider_id, name, base_url,
        default_proxy_id, allow_private_network, enabled, created_at_ms, updated_at_ms)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, publicID, input.ProviderID, input.Name, input.BaseURL,
		proxyID, boolInt(input.AllowPrivateNetwork), boolInt(input.Enabled), now, now)
	if err != nil {
		return Upstream{}, mapSQLError(err)
	}
	if err := s.audit(ctx, tx, actor, "upstream.created", "upstream", publicID, map[string]any{"allow_private_network": input.AllowPrivateNetwork}); err != nil {
		return Upstream{}, err
	}
	if err := tx.Commit(); err != nil {
		return Upstream{}, err
	}
	return s.GetUpstream(ctx, publicID)
}

func (s *Service) GetUpstream(ctx context.Context, publicID string) (Upstream, error) {
	row := s.db.QueryRowContext(ctx, `SELECT u.public_id, u.provider_id, u.name, u.base_url,
        p.public_id, u.allow_private_network, u.enabled, u.version, u.created_at_ms, u.updated_at_ms
        FROM upstreams u LEFT JOIN egress_proxies p ON p.id = u.default_proxy_id WHERE u.public_id = ?`, publicID)
	return scanUpstream(row)
}

func (s *Service) ListUpstreams(ctx context.Context, limit int, after string) (Page[Upstream], error) {
	c, err := decodeCursor(after)
	if err != nil {
		return Page[Upstream]{}, ValidationError{Field: "cursor", Message: "is invalid"}
	}
	limit = normalizeLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT u.id, u.public_id, u.provider_id, u.name, u.base_url,
        p.public_id, u.allow_private_network, u.enabled, u.version, u.created_at_ms, u.updated_at_ms
        FROM upstreams u LEFT JOIN egress_proxies p ON p.id = u.default_proxy_id
        WHERE (u.created_at_ms < ? OR (u.created_at_ms = ? AND u.id < ?))
        ORDER BY u.created_at_ms DESC, u.id DESC LIMIT ?`, c.Timestamp, c.Timestamp, c.ID, limit+1)
	if err != nil {
		return Page[Upstream]{}, err
	}
	defer rows.Close()
	page := Page[Upstream]{Items: make([]Upstream, 0, limit)}
	var last cursor
	for rows.Next() {
		var internalID, created, updated int64
		var proxy sql.NullString
		var item Upstream
		if err := rows.Scan(&internalID, &item.ID, &item.ProviderID, &item.Name, &item.BaseURL, &proxy,
			&item.AllowPrivateNetwork, &item.Enabled, &item.Version, &created, &updated); err != nil {
			return Page[Upstream]{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(last)
			break
		}
		if proxy.Valid {
			item.DefaultProxyID = &proxy.String
		}
		item.CreatedAt, item.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
		page.Items = append(page.Items, item)
		last = cursor{Timestamp: created, ID: internalID}
	}
	return page, rows.Err()
}

func (s *Service) UpdateUpstream(ctx context.Context, actor Actor, publicID string, expectedVersion int64, input UpstreamInput) (Upstream, error) {
	input.DefaultProxyID = validOptionalReference(input.DefaultProxyID)
	var err error
	input.Name, err = cleanName("name", input.Name)
	if err != nil {
		return Upstream{}, err
	}
	if !s.providerExists(input.ProviderID) {
		return Upstream{}, ValidationError{Field: "provider_id", Message: "is not supported"}
	}
	input.BaseURL, err = normalizeBaseURL(input.BaseURL, input.AllowPrivateNetwork)
	if err != nil {
		return Upstream{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Upstream{}, err
	}
	defer tx.Rollback()
	var existingProviderID string
	if err := tx.QueryRowContext(ctx, "SELECT provider_id FROM upstreams WHERE public_id = ?", publicID).Scan(&existingProviderID); errors.Is(err, sql.ErrNoRows) {
		return Upstream{}, ErrNotFound
	} else if err != nil {
		return Upstream{}, err
	}
	if input.ProviderID != existingProviderID {
		return Upstream{}, ValidationError{Field: "provider_id", Message: "cannot be changed after upstream creation"}
	}
	proxyID, err := optionalInternalID(ctx, tx, "egress_proxies", input.DefaultProxyID)
	if err != nil {
		return Upstream{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE upstreams SET name = ?, base_url = ?,
        default_proxy_id = ?, allow_private_network = ?, enabled = ?, version = version + 1,
		updated_at_ms = ? WHERE public_id = ? AND version = ?`, input.Name, input.BaseURL,
		proxyID, boolInt(input.AllowPrivateNetwork), boolInt(input.Enabled), s.clock.Now().UnixMilli(), publicID, expectedVersion)
	if err != nil {
		return Upstream{}, mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Upstream{}, s.notFoundOrVersion(ctx, tx, "upstreams", publicID)
	}
	if err := s.audit(ctx, tx, actor, "upstream.updated", "upstream", publicID, map[string]any{"allow_private_network": input.AllowPrivateNetwork}); err != nil {
		return Upstream{}, err
	}
	if err := tx.Commit(); err != nil {
		return Upstream{}, err
	}
	return s.GetUpstream(ctx, publicID)
}

func (s *Service) DeleteUpstream(ctx context.Context, actor Actor, publicID string, expectedVersion int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM upstreams WHERE public_id = ? AND version = ?", publicID, expectedVersion)
	if err != nil {
		return mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return s.notFoundOrVersion(ctx, tx, "upstreams", publicID)
	}
	if err := s.audit(ctx, tx, actor, "upstream.deleted", "upstream", publicID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func optionalInternalID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table string, publicID *string) (any, error) {
	if publicID == nil {
		return nil, nil
	}
	id, err := lookupID(ctx, q, table, *publicID)
	if err != nil {
		return nil, err
	}
	return id, nil
}

func scanUpstream(row rowScanner) (Upstream, error) {
	var item Upstream
	var proxy sql.NullString
	var created, updated int64
	err := row.Scan(&item.ID, &item.ProviderID, &item.Name, &item.BaseURL, &proxy,
		&item.AllowPrivateNetwork, &item.Enabled, &item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Upstream{}, ErrNotFound
	}
	if err != nil {
		return Upstream{}, err
	}
	if proxy.Valid {
		item.DefaultProxyID = &proxy.String
	}
	item.CreatedAt, item.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return item, nil
}
