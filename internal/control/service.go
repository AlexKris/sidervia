package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
)

type Service struct {
	db     *sql.DB
	cipher *cryptox.Cipher
	clock  clock.Clock
	ids    identifier.Generator
}

func NewService(db *sql.DB, cipher *cryptox.Cipher, c clock.Clock, ids identifier.Generator) *Service {
	if c == nil {
		c = clock.Real{}
	}
	return &Service{db: db, cipher: cipher, clock: c, ids: ids}
}

func (s *Service) Providers() []Provider {
	return []Provider{
		{ID: "openai", Name: "OpenAI", AuthMethods: []string{"api_key"}, Capabilities: []string{"text", "stream"}, ImplementationStatus: "beta"},
		{ID: "anthropic", Name: "Anthropic", AuthMethods: []string{"api_key"}, Capabilities: []string{"text", "stream"}, ImplementationStatus: "beta"},
		{ID: "google", Name: "Google Gemini", AuthMethods: []string{"api_key", "oauth_beta"}, Capabilities: []string{"text", "stream"}, ImplementationStatus: "beta"},
		{ID: "xai", Name: "xAI", AuthMethods: []string{"api_key"}, Capabilities: []string{"text", "stream"}, ImplementationStatus: "beta"},
		{ID: "openai-compatible", Name: "OpenAI-compatible", AuthMethods: []string{"api_key"}, Capabilities: []string{}, ImplementationStatus: "planned"},
	}
}

func (s *Service) providerExists(id string) bool {
	for _, provider := range s.Providers() {
		if provider.ID == id {
			return true
		}
	}
	return false
}

func (s *Service) audit(ctx context.Context, tx *sql.Tx, actor Actor, eventType, targetKind, targetID string, metadata map[string]any) error {
	return s.auditOutcome(ctx, tx, actor, eventType, targetKind, targetID, "success", metadata)
}

func (s *Service) RecordAudit(ctx context.Context, actor Actor, eventType, targetKind, targetID, outcome string, metadata map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.RecordAuditTx(ctx, tx, actor, eventType, targetKind, targetID, outcome, metadata); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) RecordAuditTx(ctx context.Context, tx *sql.Tx, actor Actor, eventType, targetKind, targetID, outcome string, metadata map[string]any) error {
	if tx == nil {
		return errors.New("audit transaction is required")
	}
	if outcome != "success" && outcome != "failure" {
		return errors.New("invalid audit outcome")
	}
	return s.auditOutcome(ctx, tx, actor, eventType, targetKind, targetID, outcome, metadata)
}

func (s *Service) auditOutcome(ctx context.Context, tx *sql.Tx, actor Actor, eventType, targetKind, targetID, outcome string, metadata map[string]any) error {
	id, err := s.ids.Object("audit")
	if err != nil {
		return err
	}
	if actor.Kind == "" {
		actor.Kind = "system"
	}
	if metadata == nil {
		metadata = map[string]any{"schema_version": 1}
	} else {
		metadata["schema_version"] = 1
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if len(body) > 4096 {
		return errors.New("audit metadata is too large")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(public_id, event_type, actor_kind, actor_id,
        target_kind, target_id, request_id, outcome, metadata_json, created_at_ms)
		VALUES(?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?)`,
		id, eventType, actor.Kind, actor.ID, targetKind, targetID, actor.RequestID, outcome, string(body), s.clock.Now().UnixMilli())
	return err
}

func cleanName(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 100 {
		return "", ValidationError{Field: field, Message: "must contain 1-100 characters"}
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", ValidationError{Field: field, Message: "contains control characters"}
		}
	}
	return value, nil
}

func mapSQLError(err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "foreign key constraint failed"):
		return ErrResourceInUse
	case strings.Contains(text, "unique constraint failed"):
		return ErrConflict
	default:
		return fmt.Errorf("database operation: %w", err)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableTimeMillis(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixMilli()
}

func timePtr(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := time.UnixMilli(value.Int64).UTC()
	return &t
}
