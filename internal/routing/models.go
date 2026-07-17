package routing

import (
	"context"
	"encoding/json"
	"sort"
)

func (s *Service) ListModels(ctx context.Context, protocol string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.public_model_id, c.protocols_json, u.provider_id
		FROM model_routes r
		JOIN route_candidates c ON c.model_route_id = r.id
		JOIN accounts a ON a.id = c.account_id
		JOIN upstreams u ON u.id = a.upstream_id
		WHERE r.enabled = 1 AND c.enabled = 1 AND a.status = 'active' AND u.enabled = 1
		ORDER BY r.public_model_id, c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	models := make(map[string]struct{})
	for rows.Next() {
		var model, providerID string
		var protocolsJSON []byte
		if err := rows.Scan(&model, &protocolsJSON, &providerID); err != nil {
			return nil, err
		}
		var protocols struct {
			SchemaVersion int      `json:"schema_version"`
			Values        []string `json:"values"`
		}
		if json.Unmarshal(protocolsJSON, &protocols) != nil || protocols.SchemaVersion != 1 {
			continue
		}
		if protocolMatches(protocols.Values, protocol, providerID) {
			models[model] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(models))
	for model := range models {
		result = append(result, model)
	}
	sort.Strings(result)
	return result, nil
}
