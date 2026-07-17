package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var allowedProtocols = map[string]bool{"openai": true, "anthropic": true, "gemini": true, "xai": true}
var allowedCapabilities = map[string]bool{
	"text": true, "stream": true, "tools": true, "structured_output": true,
	"reasoning": true, "image_input": true, "document_input": true,
}

func (s *Service) CreateModelRoute(ctx context.Context, actor Actor, input ModelRouteInput) (ModelRoute, error) {
	input, err := validateRouteInput(input)
	if err != nil {
		return ModelRoute{}, err
	}
	publicID, err := s.ids.Object("route")
	if err != nil {
		return ModelRoute{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ModelRoute{}, err
	}
	defer tx.Rollback()
	now := s.clock.Now().UnixMilli()
	var confirmed any
	if len(input.Candidates) > 1 {
		confirmed = now
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO model_routes(public_id, public_model_id, description,
        enabled, required_confirmation_at_ms, created_at_ms, updated_at_ms) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		publicID, input.PublicModelID, input.Description, boolInt(input.Enabled), confirmed, now, now)
	if err != nil {
		return ModelRoute{}, mapSQLError(err)
	}
	routeID, err := result.LastInsertId()
	if err != nil {
		return ModelRoute{}, err
	}
	if err := s.replaceCandidates(ctx, tx, routeID, input.Candidates, now); err != nil {
		return ModelRoute{}, err
	}
	if err := s.audit(ctx, tx, actor, "model_route.created", "model_route", publicID, map[string]any{"candidate_count": len(input.Candidates)}); err != nil {
		return ModelRoute{}, err
	}
	if err := tx.Commit(); err != nil {
		return ModelRoute{}, err
	}
	return s.GetModelRoute(ctx, publicID)
}

func (s *Service) GetModelRoute(ctx context.Context, publicID string) (ModelRoute, error) {
	var item ModelRoute
	var created, updated int64
	var confirmed sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT public_id, public_model_id, description, enabled,
        required_confirmation_at_ms, version, created_at_ms, updated_at_ms
        FROM model_routes WHERE public_id = ?`, publicID).Scan(&item.ID, &item.PublicModelID,
		&item.Description, &item.Enabled, &confirmed, &item.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelRoute{}, ErrNotFound
	}
	if err != nil {
		return ModelRoute{}, err
	}
	item.MultipleCandidatesConfirmed = confirmed.Valid
	item.CreatedAt, item.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	item.Candidates, err = s.loadCandidates(ctx, publicID)
	return item, err
}

func (s *Service) ListModelRoutes(ctx context.Context, limit int, after string) (Page[ModelRoute], error) {
	c, err := decodeCursor(after)
	if err != nil {
		return Page[ModelRoute]{}, ValidationError{Field: "cursor", Message: "is invalid"}
	}
	limit = normalizeLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, public_id, public_model_id, description, enabled,
        required_confirmation_at_ms, version, created_at_ms, updated_at_ms FROM model_routes
        WHERE (created_at_ms < ? OR (created_at_ms = ? AND id < ?))
        ORDER BY created_at_ms DESC, id DESC LIMIT ?`, c.Timestamp, c.Timestamp, c.ID, limit+1)
	if err != nil {
		return Page[ModelRoute]{}, err
	}
	defer rows.Close()
	type pending struct {
		item ModelRoute
		last cursor
	}
	values := make([]pending, 0, limit+1)
	for rows.Next() {
		var p pending
		var internalID, created, updated int64
		var confirmed sql.NullInt64
		if err := rows.Scan(&internalID, &p.item.ID, &p.item.PublicModelID, &p.item.Description,
			&p.item.Enabled, &confirmed, &p.item.Version, &created, &updated); err != nil {
			return Page[ModelRoute]{}, err
		}
		p.item.MultipleCandidatesConfirmed = confirmed.Valid
		p.item.CreatedAt, p.item.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
		p.last = cursor{Timestamp: created, ID: internalID}
		values = append(values, p)
	}
	if err := rows.Err(); err != nil {
		return Page[ModelRoute]{}, err
	}
	page := Page[ModelRoute]{Items: make([]ModelRoute, 0, limit)}
	for i, value := range values {
		if i == limit {
			page.NextCursor = encodeCursor(values[i-1].last)
			break
		}
		value.item.Candidates, err = s.loadCandidates(ctx, value.item.ID)
		if err != nil {
			return Page[ModelRoute]{}, err
		}
		page.Items = append(page.Items, value.item)
	}
	return page, nil
}

func (s *Service) UpdateModelRoute(ctx context.Context, actor Actor, publicID string, expectedVersion int64, input ModelRouteInput) (ModelRoute, error) {
	input, err := validateRouteInput(input)
	if err != nil {
		return ModelRoute{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ModelRoute{}, err
	}
	defer tx.Rollback()
	now := s.clock.Now().UnixMilli()
	var confirmed any
	if len(input.Candidates) > 1 {
		confirmed = now
	}
	result, err := tx.ExecContext(ctx, `UPDATE model_routes SET public_model_id = ?, description = ?,
        enabled = ?, required_confirmation_at_ms = ?, version = version + 1, updated_at_ms = ?
        WHERE public_id = ? AND version = ?`, input.PublicModelID, input.Description, boolInt(input.Enabled),
		confirmed, now, publicID, expectedVersion)
	if err != nil {
		return ModelRoute{}, mapSQLError(err)
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ModelRoute{}, s.notFoundOrVersion(ctx, tx, "model_routes", publicID)
	}
	var routeID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM model_routes WHERE public_id = ?", publicID).Scan(&routeID); err != nil {
		return ModelRoute{}, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM route_candidates WHERE model_route_id = ?", routeID); err != nil {
		return ModelRoute{}, err
	}
	if err := s.replaceCandidates(ctx, tx, routeID, input.Candidates, now); err != nil {
		return ModelRoute{}, err
	}
	if err := s.audit(ctx, tx, actor, "model_route.updated", "model_route", publicID, map[string]any{"candidate_count": len(input.Candidates)}); err != nil {
		return ModelRoute{}, err
	}
	if err := tx.Commit(); err != nil {
		return ModelRoute{}, err
	}
	return s.GetModelRoute(ctx, publicID)
}

func (s *Service) DeleteModelRoute(ctx context.Context, actor Actor, publicID string, expectedVersion int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var routeID, version int64
	err = tx.QueryRowContext(ctx, "SELECT id, version FROM model_routes WHERE public_id = ?", publicID).Scan(&routeID, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if version != expectedVersion {
		return ErrVersion
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM route_candidates WHERE model_route_id = ?", routeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM model_routes WHERE id = ?", routeID); err != nil {
		return mapSQLError(err)
	}
	if err := s.audit(ctx, tx, actor, "model_route.deleted", "model_route", publicID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func validateRouteInput(input ModelRouteInput) (ModelRouteInput, error) {
	input.PublicModelID = strings.TrimSpace(input.PublicModelID)
	if input.PublicModelID == "" || len(input.PublicModelID) > 200 || strings.ContainsAny(input.PublicModelID, "\r\n\x00") {
		return ModelRouteInput{}, ValidationError{Field: "public_model_id", Message: "must contain 1-200 safe characters"}
	}
	input.Description = strings.TrimSpace(input.Description)
	if len(input.Description) > 500 {
		return ModelRouteInput{}, ValidationError{Field: "description", Message: "must be at most 500 characters"}
	}
	if len(input.Candidates) == 0 || len(input.Candidates) > 100 {
		return ModelRouteInput{}, ValidationError{Field: "candidates", Message: "must contain 1-100 candidates"}
	}
	if len(input.Candidates) > 1 && !input.ConfirmMultipleCandidates {
		return ModelRouteInput{}, ValidationError{Field: "confirm_multiple_candidates", Message: "must be true for multiple candidates"}
	}
	seen := make(map[string]struct{})
	for i := range input.Candidates {
		candidate := &input.Candidates[i]
		candidate.AccountID = strings.TrimSpace(candidate.AccountID)
		candidate.UpstreamModelID = strings.TrimSpace(candidate.UpstreamModelID)
		if candidate.AccountID == "" || candidate.UpstreamModelID == "" || len(candidate.UpstreamModelID) > 200 {
			return ModelRouteInput{}, ValidationError{Field: "candidates", Message: "contains an invalid account or model"}
		}
		key := candidate.AccountID + "\x00" + candidate.UpstreamModelID
		if _, ok := seen[key]; ok {
			return ModelRouteInput{}, ValidationError{Field: "candidates", Message: "contains a duplicate candidate"}
		}
		seen[key] = struct{}{}
		var err error
		candidate.Protocols, err = normalizeStringSet("protocols", candidate.Protocols, allowedProtocols)
		if err != nil || len(candidate.Protocols) == 0 {
			return ModelRouteInput{}, ValidationError{Field: "protocols", Message: "must contain at least one supported protocol"}
		}
		candidate.Capabilities, err = normalizeStringSet("capabilities", candidate.Capabilities, allowedCapabilities)
		if err != nil {
			return ModelRouteInput{}, err
		}
	}
	return input, nil
}

func (s *Service) replaceCandidates(ctx context.Context, tx *sql.Tx, routeID int64, candidates []RouteCandidate, now int64) error {
	for _, candidate := range candidates {
		accountID, err := lookupID(ctx, tx, "accounts", candidate.AccountID)
		if err != nil {
			return err
		}
		protocols, _ := json.Marshal(map[string]any{"schema_version": 1, "values": candidate.Protocols})
		capabilities, _ := json.Marshal(map[string]any{"schema_version": 1, "values": candidate.Capabilities})
		_, err = tx.ExecContext(ctx, `INSERT INTO route_candidates(model_route_id, account_id,
            upstream_model_id, enabled, protocols_json, capabilities_json, created_at_ms)
            VALUES(?, ?, ?, ?, ?, ?, ?)`, routeID, accountID, candidate.UpstreamModelID,
			boolInt(candidate.Enabled), string(protocols), string(capabilities), now)
		if err != nil {
			return mapSQLError(err)
		}
	}
	return nil
}

func (s *Service) loadCandidates(ctx context.Context, routePublicID string) ([]RouteCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.public_id, c.upstream_model_id, c.enabled,
        c.protocols_json, c.capabilities_json FROM route_candidates c
        JOIN model_routes r ON r.id = c.model_route_id JOIN accounts a ON a.id = c.account_id
        WHERE r.public_id = ? ORDER BY c.id`, routePublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RouteCandidate, 0)
	for rows.Next() {
		var candidate RouteCandidate
		var protocols, capabilities []byte
		if err := rows.Scan(&candidate.AccountID, &candidate.UpstreamModelID, &candidate.Enabled, &protocols, &capabilities); err != nil {
			return nil, err
		}
		var p, c struct {
			Values []string `json:"values"`
		}
		if err := json.Unmarshal(protocols, &p); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(capabilities, &c); err != nil {
			return nil, err
		}
		candidate.Protocols, candidate.Capabilities = p.Values, c.Values
		result = append(result, candidate)
	}
	return result, rows.Err()
}
