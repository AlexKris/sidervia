package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/migrations"
)

func TestOpenMigrateAndSentinel(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cipher, _ := cryptox.NewCipher(bytes.Repeat([]byte{1}, cryptox.KeySize))
	if err := s.VerifyOrCreateSentinel(ctx, cipher); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.DB().QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != LatestSchemaVersion {
		t.Fatalf("migration count=%d err=%v", count, err)
	}
}

func TestWrongSentinelKeyFails(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, _ := cryptox.NewCipher(bytes.Repeat([]byte{1}, cryptox.KeySize))
	second, _ := cryptox.NewCipher(bytes.Repeat([]byte{2}, cryptox.KeySize))
	if err := s.VerifyOrCreateSentinel(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyOrCreateSentinel(ctx, second); err == nil {
		t.Fatal("expected wrong key failure")
	}
}

func TestMigrationTwoPreservesVersionOneAccountsAndRoutes(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "sidervia.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	versionOne, err := migrations.FS.ReadFile("0001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at_ms INTEGER NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(versionOne)); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(versionOne)
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, checksum, applied_at_ms)
		VALUES(1, '0001_initial.sql', ?, 1)`, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO upstreams(
		id, public_id, provider_id, name, base_url, enabled, created_at_ms, updated_at_ms
	) VALUES(1, 'sdr_up_v1', 'openai', 'OpenAI', 'https://api.example/v1', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts(
		id, public_id, upstream_id, name, auth_kind, billing_kind, credential_enc,
		status, priority, max_concurrency, created_at_ms, updated_at_ms
	) VALUES(1, 'sdr_acct_v1', 1, 'Account', 'api_key', 'metered', X'010203',
		'draft', 20, 4, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO model_routes(
		id, public_id, public_model_id, description, enabled, created_at_ms, updated_at_ms
	) VALUES(1, 'sdr_route_v1', 'public-model', '', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO route_candidates(
		id, model_route_id, account_id, upstream_model_id, enabled, protocols_json, capabilities_json, created_at_ms
	) VALUES(1, 1, 1, 'upstream-model', 1,
		'{"schema_version":1,"values":["openai"]}',
		'{"schema_version":1,"values":["text"]}', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var authKind, status, publicModel, upstreamModel string
	var credentialVersion int
	if err := upgraded.DB().QueryRow(`SELECT auth_kind, status, credential_version FROM accounts
		WHERE public_id = 'sdr_acct_v1'`).Scan(&authKind, &status, &credentialVersion); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.DB().QueryRow(`SELECT r.public_model_id, c.upstream_model_id
		FROM route_candidates c JOIN model_routes r ON r.id = c.model_route_id
		JOIN accounts a ON a.id = c.account_id WHERE a.public_id = 'sdr_acct_v1'`).Scan(&publicModel, &upstreamModel); err != nil {
		t.Fatal(err)
	}
	if authKind != "api_key" || status != "draft" || credentialVersion != 1 || publicModel != "public-model" || upstreamModel != "upstream-model" {
		t.Fatalf("upgrade result: auth=%q status=%q credential_version=%d route=%q upstream=%q",
			authKind, status, credentialVersion, publicModel, upstreamModel)
	}
	var foreignKeyViolations int
	if err := upgraded.DB().QueryRow("SELECT count(*) FROM pragma_foreign_key_check").Scan(&foreignKeyViolations); err != nil || foreignKeyViolations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", foreignKeyViolations, err)
	}
}
