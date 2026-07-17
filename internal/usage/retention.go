package usage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/strictjson"
)

const (
	RetentionPeriod    = 365 * 24 * time.Hour
	retentionBatchSize = 1000
	dayMilliseconds    = int64((24 * time.Hour) / time.Millisecond)
)

type CleanupResult struct {
	Cutoff         time.Time
	Deleted        int64
	AggregatedDays int
}

type auditRecorder interface {
	RecordAuditTx(context.Context, *sql.Tx, control.Actor, string, string, string, string, map[string]any) error
}

type Retention struct {
	db    *sql.DB
	clock clock.Clock
	audit auditRecorder
}

func NewRetention(db *sql.DB, c clock.Clock, audit auditRecorder) *Retention {
	if c == nil {
		c = clock.Real{}
	}
	return &Retention{db: db, clock: c, audit: audit}
}

func (r *Retention) Cleanup(ctx context.Context) (CleanupResult, error) {
	if r == nil || r.db == nil || r.audit == nil {
		return CleanupResult{}, errors.New("request metadata retention is unavailable")
	}
	result := CleanupResult{Cutoff: r.clock.Now().UTC().Add(-RetentionPeriod)}
	days := make(map[int64]struct{})
	for {
		deleted, batchDays, err := r.cleanupBatch(ctx, result.Cutoff)
		if err != nil {
			return result, err
		}
		result.Deleted += deleted
		for _, day := range batchDays {
			days[day] = struct{}{}
		}
		if deleted < retentionBatchSize {
			result.AggregatedDays = len(days)
			return result, nil
		}
	}
}

func (r *Retention) Run(ctx context.Context, observe func(CleanupResult, error)) {
	for {
		result, err := r.Cleanup(ctx)
		if observe != nil {
			observe(result, err)
		}
		if ctx.Err() != nil {
			return
		}
		now := r.clock.Now().UTC()
		timer := time.NewTimer(nextRetentionSweep(now).Sub(now))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

type dailyAggregate struct {
	requests, errors, streamed                                   int64
	requestBytes, responseBytes                                  int64
	inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64
	reasoningTokens, totalTokens                                 int64
}

func (r *Retention) cleanupBatch(ctx context.Context, cutoff time.Time) (int64, []int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, started_at_ms, status_code, streamed,
		request_bytes, response_bytes, usage_json FROM request_records
		WHERE started_at_ms < ? ORDER BY started_at_ms, id LIMIT ?`, cutoff.UnixMilli(), retentionBatchSize)
	if err != nil {
		return 0, nil, err
	}
	aggregates := make(map[int64]*dailyAggregate)
	var count, lastID, lastStarted int64
	for rows.Next() {
		var id, started, statusCode, streamed, requestBytes, responseBytes int64
		var usageJSON []byte
		if err := rows.Scan(&id, &started, &statusCode, &streamed, &requestBytes, &responseBytes, &usageJSON); err != nil {
			_ = rows.Close()
			return 0, nil, err
		}
		if started < 0 || statusCode < 100 || statusCode > 599 || (streamed != 0 && streamed != 1) || requestBytes < 0 || responseBytes < 0 {
			_ = rows.Close()
			return 0, nil, errors.New("request metadata contains an invalid retained value")
		}
		values, err := decodeStoredUsage(usageJSON)
		if err != nil {
			_ = rows.Close()
			return 0, nil, fmt.Errorf("decode retained request usage: %w", err)
		}
		day := started - started%dayMilliseconds
		aggregate := aggregates[day]
		if aggregate == nil {
			aggregate = &dailyAggregate{}
			aggregates[day] = aggregate
		}
		if err := aggregate.add(statusCode, streamed == 1, requestBytes, responseBytes, values); err != nil {
			_ = rows.Close()
			return 0, nil, err
		}
		count++
		lastID, lastStarted = id, started
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, nil, err
	}
	if err := rows.Close(); err != nil {
		return 0, nil, err
	}
	if count == 0 {
		return 0, nil, tx.Commit()
	}

	now := r.clock.Now().UTC().UnixMilli()
	days := make([]int64, 0, len(aggregates))
	for day, aggregate := range aggregates {
		if err := upsertDailyAggregate(ctx, tx, day, now, aggregate); err != nil {
			return 0, nil, err
		}
		days = append(days, day)
	}
	deleted, err := tx.ExecContext(ctx, `DELETE FROM request_records WHERE started_at_ms < ?
		AND (started_at_ms < ? OR (started_at_ms = ? AND id <= ?))`,
		cutoff.UnixMilli(), lastStarted, lastStarted, lastID)
	if err != nil {
		return 0, nil, err
	}
	deletedCount, err := deleted.RowsAffected()
	if err != nil || deletedCount != count {
		return 0, nil, errors.New("request metadata retention selection changed during cleanup")
	}
	if err := r.audit.RecordAuditTx(ctx, tx, control.Actor{Kind: "system"},
		"usage.request_metadata_expired", "request_metadata", "retention", "success", map[string]any{
			"cutoff_ms": cutoff.UnixMilli(), "deleted_count": deletedCount, "aggregated_days": len(days),
		}); err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	return deletedCount, days, nil
}

func (a *dailyAggregate) add(statusCode int64, streamed bool, requestBytes, responseBytes int64, values Values) error {
	if err := addChecked(&a.requests, 1); err != nil {
		return err
	}
	if statusCode >= 400 {
		if err := addChecked(&a.errors, 1); err != nil {
			return err
		}
	}
	if streamed {
		if err := addChecked(&a.streamed, 1); err != nil {
			return err
		}
	}
	fields := []struct {
		destination *int64
		value       int64
	}{
		{&a.requestBytes, requestBytes}, {&a.responseBytes, responseBytes},
		{&a.inputTokens, values["input_tokens"]}, {&a.outputTokens, values["output_tokens"]},
		{&a.cacheReadTokens, values["cache_read_tokens"]}, {&a.cacheWriteTokens, values["cache_write_tokens"]},
		{&a.reasoningTokens, values["reasoning_tokens"]}, {&a.totalTokens, values["total_tokens"]},
	}
	for _, field := range fields {
		if err := addChecked(field.destination, field.value); err != nil {
			return err
		}
	}
	return nil
}

func upsertDailyAggregate(ctx context.Context, tx *sql.Tx, day, now int64, value *dailyAggregate) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO request_daily_aggregates(
		day_start_ms, request_count, error_count, streamed_count, request_bytes, response_bytes,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
		total_tokens, updated_at_ms
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(day_start_ms) DO UPDATE SET
		request_count = request_count + excluded.request_count,
		error_count = error_count + excluded.error_count,
		streamed_count = streamed_count + excluded.streamed_count,
		request_bytes = request_bytes + excluded.request_bytes,
		response_bytes = response_bytes + excluded.response_bytes,
		input_tokens = input_tokens + excluded.input_tokens,
		output_tokens = output_tokens + excluded.output_tokens,
		cache_read_tokens = cache_read_tokens + excluded.cache_read_tokens,
		cache_write_tokens = cache_write_tokens + excluded.cache_write_tokens,
		reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
		total_tokens = total_tokens + excluded.total_tokens,
		updated_at_ms = excluded.updated_at_ms`,
		day, value.requests, value.errors, value.streamed, value.requestBytes, value.responseBytes,
		value.inputTokens, value.outputTokens, value.cacheReadTokens, value.cacheWriteTokens,
		value.reasoningTokens, value.totalTokens, now)
	return err
}

func decodeStoredUsage(body []byte) (Values, error) {
	if err := strictjson.Validate(body, 4); err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, err
	}
	version, err := nonnegativeInteger(object["schema_version"])
	if err != nil || version != 1 {
		return nil, errors.New("usage schema version is invalid")
	}
	result := Values{}
	for _, name := range []string{"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "reasoning_tokens", "total_tokens"} {
		raw, exists := object[name]
		if !exists {
			continue
		}
		value, err := nonnegativeInteger(raw)
		if err != nil {
			return nil, fmt.Errorf("usage field %s is invalid", name)
		}
		result[name] = value
	}
	return result, nil
}

func nonnegativeInteger(raw json.RawMessage) (int64, error) {
	var value int64
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value < 0 {
		return 0, errors.New("value must be a non-negative integer")
	}
	return value, nil
}

func addChecked(destination *int64, value int64) error {
	if value < 0 || *destination > math.MaxInt64-value {
		return errors.New("request metadata aggregate overflow")
	}
	*destination += value
	return nil
}

func nextRetentionSweep(now time.Time) time.Time {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 10, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
