package usage

import (
	"context"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/store"
)

type retentionClock struct{ now time.Time }

func (c retentionClock) Now() time.Time { return c.now }

func TestRetentionAggregatesThenDeletesExpiredRequestMetadata(t *testing.T) {
	ctx := context.Background()
	database, clientKeyID := retentionFixture(t)
	defer database.Close()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-RetentionPeriod)
	insertRequestRecord(t, database, clientKeyID, "sdr_req_expired_one", cutoff.Add(-35*time.Hour), 200, true,
		`{"schema_version":1,"input_tokens":12,"output_tokens":4,"total_tokens":16}`)
	insertRequestRecord(t, database, clientKeyID, "sdr_req_expired_two", cutoff.Add(-time.Hour), 500, false,
		`{"schema_version":1,"input_tokens":8,"output_tokens":3,"cache_read_tokens":2,"reasoning_tokens":1}`)
	insertRequestRecord(t, database, clientKeyID, "sdr_req_boundary", cutoff, 200, false, `{"schema_version":1}`)
	insertRequestRecord(t, database, clientKeyID, "sdr_req_current", now, 200, false, `{"schema_version":1}`)

	service := newRetentionForTest(database, retentionClock{now: now})
	result, err := service.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 2 || result.AggregatedDays != 2 || !result.Cutoff.Equal(cutoff) {
		t.Fatalf("cleanup result=%+v", result)
	}
	var remaining int
	if err := database.DB().QueryRowContext(ctx, "SELECT count(*) FROM request_records").Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("remaining=%d err=%v", remaining, err)
	}
	var requests, requestErrors, streamed, inputTokens, outputTokens, cacheTokens, reasoningTokens int64
	if err := database.DB().QueryRowContext(ctx, `SELECT sum(request_count), sum(error_count),
		sum(streamed_count), sum(input_tokens), sum(output_tokens), sum(cache_read_tokens),
		sum(reasoning_tokens) FROM request_daily_aggregates`).Scan(
		&requests, &requestErrors, &streamed, &inputTokens, &outputTokens, &cacheTokens, &reasoningTokens); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || requestErrors != 1 || streamed != 1 || inputTokens != 20 || outputTokens != 7 || cacheTokens != 2 || reasoningTokens != 1 {
		t.Fatalf("aggregate requests=%d errors=%d streamed=%d input=%d output=%d cache=%d reasoning=%d",
			requests, requestErrors, streamed, inputTokens, outputTokens, cacheTokens, reasoningTokens)
	}
	var auditCount int
	if err := database.DB().QueryRowContext(ctx, "SELECT count(*) FROM audit_events WHERE event_type = 'usage.request_metadata_expired'").Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}

	second, err := service.Cleanup(ctx)
	if err != nil || second.Deleted != 0 {
		t.Fatalf("second cleanup=%+v err=%v", second, err)
	}
	if err := database.DB().QueryRowContext(ctx, "SELECT sum(request_count) FROM request_daily_aggregates").Scan(&requests); err != nil || requests != 2 {
		t.Fatalf("aggregate changed after idempotent cleanup: requests=%d err=%v", requests, err)
	}
}

func TestRetentionRollsBackWhenAuditWriteFails(t *testing.T) {
	ctx := context.Background()
	database, clientKeyID := retentionFixture(t)
	defer database.Close()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	insertRequestRecord(t, database, clientKeyID, "sdr_req_rollback", now.Add(-RetentionPeriod-time.Hour), 200, false,
		`{"schema_version":1,"input_tokens":3}`)
	if _, err := database.DB().ExecContext(ctx, `CREATE TRIGGER fail_retention_audit
		BEFORE INSERT ON audit_events WHEN NEW.event_type = 'usage.request_metadata_expired'
		BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`); err != nil {
		t.Fatal(err)
	}

	if _, err := newRetentionForTest(database, retentionClock{now: now}).Cleanup(ctx); err == nil {
		t.Fatal("cleanup succeeded despite injected audit failure")
	}
	var records, aggregates int
	if err := database.DB().QueryRowContext(ctx, "SELECT count(*) FROM request_records").Scan(&records); err != nil {
		t.Fatal(err)
	}
	if err := database.DB().QueryRowContext(ctx, "SELECT count(*) FROM request_daily_aggregates").Scan(&aggregates); err != nil {
		t.Fatal(err)
	}
	if records != 1 || aggregates != 0 {
		t.Fatalf("cleanup was not atomic: records=%d aggregates=%d", records, aggregates)
	}
}

func TestRetentionStopsOnMalformedUsageWithoutDeleting(t *testing.T) {
	ctx := context.Background()
	database, clientKeyID := retentionFixture(t)
	defer database.Close()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	insertRequestRecord(t, database, clientKeyID, "sdr_req_invalid_usage", now.Add(-RetentionPeriod-time.Hour), 200, false,
		`{"schema_version":1,"input_tokens":"3"}`)

	if _, err := newRetentionForTest(database, retentionClock{now: now}).Cleanup(ctx); err == nil {
		t.Fatal("cleanup accepted malformed stored usage")
	}
	var records int
	if err := database.DB().QueryRowContext(ctx, "SELECT count(*) FROM request_records").Scan(&records); err != nil || records != 1 {
		t.Fatalf("malformed record was deleted: records=%d err=%v", records, err)
	}
}

func retentionFixture(t *testing.T) (*store.Store, int64) {
	t.Helper()
	database, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, err := database.DB().Exec(`INSERT INTO client_keys(
		public_id, name, prefix, secret_verifier, status, created_at_ms
	) VALUES('sdr_ckey_retention', 'retention', '87654321', 'retention-verifier', 'active', 1)`)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	clientKeyID, err := result.LastInsertId()
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	return database, clientKeyID
}

func newRetentionForTest(database *store.Store, c retentionClock) *Retention {
	audit := control.NewService(database.DB(), nil, c, identifier.NewGenerator())
	return NewRetention(database.DB(), c, audit)
}

func insertRequestRecord(t *testing.T, database *store.Store, clientKeyID int64, publicID string, started time.Time, status int, streamed bool, usageJSON string) {
	t.Helper()
	streamedValue := 0
	if streamed {
		streamedValue = 1
	}
	_, err := database.DB().Exec(`INSERT INTO request_records(
		public_id, client_key_id, protocol, endpoint_kind, public_model_id, status_code,
		streamed, started_at_ms, completed_at_ms, request_bytes, response_bytes, usage_json, routing_json
	) VALUES(?, ?, 'openai', 'chat_completions', 'model', ?, ?, ?, ?, 10, 20, ?, '{"schema_version":1}')`,
		publicID, clientKeyID, status, streamedValue, started.UnixMilli(), started.Add(time.Second).UnixMilli(), usageJSON)
	if err != nil {
		t.Fatal(err)
	}
}
