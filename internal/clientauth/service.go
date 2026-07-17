package clientauth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/identifier"
)

var ErrUnauthorized = errors.New("client key authentication failed")

type Identity struct {
	InternalID int64
	PublicID   string
}

type Service struct {
	db    *sql.DB
	clock clock.Clock
}

func New(db *sql.DB, c clock.Clock) *Service {
	if c == nil {
		c = clock.Real{}
	}
	return &Service{db: db, clock: c}
}

func (s *Service) Authenticate(ctx context.Context, raw string) (Identity, error) {
	prefix, secret, wellFormed := parse(raw)
	if !wellFormed {
		prefix = ""
		secret = "invalid-client-key"
	}

	var identity Identity
	var verifier, status string
	var expiresAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, public_id, secret_verifier, status, expires_at_ms
		FROM client_keys WHERE prefix = ?`, prefix).Scan(
		&identity.InternalID, &identity.PublicID, &verifier, &status, &expiresAt,
	)
	candidate := identifier.Verifier(secret)
	if errors.Is(err, sql.ErrNoRows) {
		constantCompare(candidate, identifier.Verifier("missing-client-key"))
		return Identity{}, ErrUnauthorized
	}
	if err != nil {
		return Identity{}, err
	}
	if !wellFormed || !constantCompare(candidate, verifier) || status != "active" {
		return Identity{}, ErrUnauthorized
	}
	now := s.clock.Now().UTC()
	if expiresAt.Valid && expiresAt.Int64 <= now.UnixMilli() {
		return Identity{}, ErrUnauthorized
	}
	_, err = s.db.ExecContext(ctx, `UPDATE client_keys SET last_used_at_ms = ?
		WHERE id = ? AND (last_used_at_ms IS NULL OR last_used_at_ms < ?)`,
		now.UnixMilli(), identity.InternalID, now.Add(-time.Minute).UnixMilli())
	if err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func parse(raw string) (prefix, secret string, ok bool) {
	if strings.TrimSpace(raw) != raw || !strings.HasPrefix(raw, "sk-sdr_") {
		return "", "", false
	}
	rest := strings.TrimPrefix(raw, "sk-sdr_")
	if len(rest) < 10 || rest[8] != '_' {
		return "", "", false
	}
	prefix, secret = rest[:8], rest[9:]
	decoded, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(decoded) != 32 {
		return "", "", false
	}
	for _, char := range prefix {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return "", "", false
		}
	}
	return prefix, secret, true
}

func constantCompare(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
