package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	result := Dashboard{DatabaseReady: true, Counts: make(map[string]int), Warnings: []string{"providers_not_implemented"}}
	if err := s.db.PingContext(ctx); err != nil {
		result.DatabaseReady = false
		return result, err
	}
	if err := s.db.QueryRowContext(ctx, "SELECT totp_enabled FROM admin_user WHERE id = 1").Scan(&result.AdminTOTPEnabled); err != nil {
		return Dashboard{}, err
	}
	if !result.AdminTOTPEnabled {
		result.Warnings = append(result.Warnings, "admin_totp_disabled")
	}
	for name, table := range map[string]string{
		"proxies": "egress_proxies", "upstreams": "upstreams", "accounts": "accounts",
		"model_routes": "model_routes", "client_keys": "client_keys",
	} {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return Dashboard{}, err
		}
		result.Counts[name] = count
	}
	return result, nil
}

func (s *Service) ListAuditEvents(ctx context.Context, limit int, after string) (Page[AuditEvent], error) {
	c, err := decodeCursor(after)
	if err != nil {
		return Page[AuditEvent]{}, ValidationError{Field: "cursor", Message: "is invalid"}
	}
	limit = normalizeLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, public_id, event_type, actor_kind, actor_id,
        target_kind, target_id, request_id, outcome, metadata_json, created_at_ms FROM audit_events
        WHERE (created_at_ms < ? OR (created_at_ms = ? AND id < ?))
        ORDER BY created_at_ms DESC, id DESC LIMIT ?`, c.Timestamp, c.Timestamp, c.ID, limit+1)
	if err != nil {
		return Page[AuditEvent]{}, err
	}
	defer rows.Close()
	page := Page[AuditEvent]{Items: make([]AuditEvent, 0, limit)}
	var last cursor
	for rows.Next() {
		var internalID, created int64
		var actorID, targetKind, targetID, requestID sql.NullString
		var metadata []byte
		var item AuditEvent
		if err := rows.Scan(&internalID, &item.ID, &item.EventType, &item.ActorKind, &actorID,
			&targetKind, &targetID, &requestID, &item.Outcome, &metadata, &created); err != nil {
			return Page[AuditEvent]{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeCursor(last)
			break
		}
		item.ActorID, item.TargetKind, item.TargetID, item.RequestID = nullStringPtr(actorID), nullStringPtr(targetKind), nullStringPtr(targetID), nullStringPtr(requestID)
		if err := json.Unmarshal(metadata, &item.Metadata); err != nil {
			return Page[AuditEvent]{}, err
		}
		item.CreatedAt = time.UnixMilli(created).UTC()
		page.Items = append(page.Items, item)
		last = cursor{Timestamp: created, ID: internalID}
	}
	return page, rows.Err()
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
