package usage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidCursor  = errors.New("invalid request cursor")
	ErrRecordNotFound = errors.New("request record not found")
)

type RequestRecord struct {
	ID            string           `json:"id"`
	ClientKeyID   string           `json:"client_key_id"`
	ClientKeyName string           `json:"client_key_name"`
	Protocol      string           `json:"protocol"`
	EndpointKind  string           `json:"endpoint_kind"`
	PublicModelID string           `json:"public_model_id"`
	ProviderID    *string          `json:"provider_id,omitempty"`
	UpstreamID    *string          `json:"upstream_id,omitempty"`
	AccountID     *string          `json:"account_id,omitempty"`
	StatusCode    int              `json:"status_code"`
	ErrorCode     *string          `json:"error_code,omitempty"`
	Streamed      bool             `json:"streamed"`
	StartedAt     time.Time        `json:"started_at"`
	FirstByteAt   *time.Time       `json:"first_byte_at,omitempty"`
	CompletedAt   time.Time        `json:"completed_at"`
	DurationMS    int64            `json:"duration_ms"`
	TTFTMS        *int64           `json:"ttft_ms,omitempty"`
	RequestBytes  int64            `json:"request_bytes"`
	ResponseBytes int64            `json:"response_bytes"`
	Usage         map[string]int64 `json:"usage"`
}

type RequestPage struct {
	Items      []RequestRecord `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type Summary struct {
	WindowStart     time.Time `json:"window_start"`
	Requests        int64     `json:"requests"`
	Errors          int64     `json:"errors"`
	Streamed        int64     `json:"streamed"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	CacheTokens     int64     `json:"cache_read_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
}

type Reader struct{ db *sql.DB }

func NewReader(db *sql.DB) *Reader { return &Reader{db: db} }

func (r *Reader) ListRequests(ctx context.Context, limit int, after string) (RequestPage, error) {
	cursor, err := decodeRequestCursor(after)
	if err != nil {
		return RequestPage{}, ErrInvalidCursor
	}
	limit = normalizeRequestLimit(limit)
	rows, err := r.db.QueryContext(ctx, requestSelect+`
		WHERE (rr.started_at_ms < ? OR (rr.started_at_ms = ? AND rr.id < ?))
		ORDER BY rr.started_at_ms DESC, rr.id DESC LIMIT ?`, cursor.timestamp, cursor.timestamp, cursor.id, limit+1)
	if err != nil {
		return RequestPage{}, err
	}
	defer rows.Close()
	page := RequestPage{Items: make([]RequestRecord, 0, limit)}
	var last requestCursor
	for rows.Next() {
		item, internalID, err := scanRequest(rows)
		if err != nil {
			return RequestPage{}, err
		}
		if len(page.Items) == limit {
			page.NextCursor = encodeRequestCursor(last)
			break
		}
		page.Items = append(page.Items, item)
		last = requestCursor{timestamp: item.StartedAt.UnixMilli(), id: internalID}
	}
	return page, rows.Err()
}

func (r *Reader) GetRequest(ctx context.Context, publicID string) (RequestRecord, error) {
	item, _, err := scanRequest(r.db.QueryRowContext(ctx, requestSelect+` WHERE rr.public_id = ?`, publicID))
	if errors.Is(err, sql.ErrNoRows) {
		return RequestRecord{}, ErrRecordNotFound
	}
	return item, err
}

func (r *Reader) Summary24Hours(ctx context.Context, now time.Time) (Summary, error) {
	start := now.UTC().Add(-24 * time.Hour)
	result := Summary{WindowStart: start}
	err := r.db.QueryRowContext(ctx, `SELECT count(*),
		COALESCE(sum(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
		COALESCE(sum(streamed), 0),
		COALESCE(sum(CAST(COALESCE(json_extract(usage_json, '$.input_tokens'), 0) AS INTEGER)), 0),
		COALESCE(sum(CAST(COALESCE(json_extract(usage_json, '$.output_tokens'), 0) AS INTEGER)), 0),
		COALESCE(sum(CAST(COALESCE(json_extract(usage_json, '$.cache_read_tokens'), 0) AS INTEGER)), 0),
		COALESCE(sum(CAST(COALESCE(json_extract(usage_json, '$.reasoning_tokens'), 0) AS INTEGER)), 0)
		FROM request_records WHERE started_at_ms >= ?`, start.UnixMilli()).Scan(
		&result.Requests, &result.Errors, &result.Streamed, &result.InputTokens,
		&result.OutputTokens, &result.CacheTokens, &result.ReasoningTokens)
	return result, err
}

const requestSelect = `SELECT rr.id, rr.public_id, ck.public_id, ck.name, rr.protocol,
	rr.endpoint_kind, rr.public_model_id, rr.provider_id, u.public_id, a.public_id,
	rr.status_code, rr.error_code, rr.streamed, rr.started_at_ms, rr.first_byte_at_ms,
	rr.completed_at_ms, rr.request_bytes, rr.response_bytes, rr.usage_json
	FROM request_records rr JOIN client_keys ck ON ck.id = rr.client_key_id
	LEFT JOIN upstreams u ON u.id = rr.upstream_id
	LEFT JOIN accounts a ON a.id = rr.account_id`

type rowScanner interface{ Scan(...any) error }

func scanRequest(row rowScanner) (RequestRecord, int64, error) {
	var item RequestRecord
	var internalID, started, completed int64
	var provider, upstream, account, errorCode sql.NullString
	var firstByte sql.NullInt64
	var streamed int
	var usageJSON []byte
	err := row.Scan(&internalID, &item.ID, &item.ClientKeyID, &item.ClientKeyName, &item.Protocol,
		&item.EndpointKind, &item.PublicModelID, &provider, &upstream, &account, &item.StatusCode,
		&errorCode, &streamed, &started, &firstByte, &completed, &item.RequestBytes,
		&item.ResponseBytes, &usageJSON)
	if err != nil {
		return RequestRecord{}, 0, err
	}
	item.ProviderID = nullableString(provider)
	item.UpstreamID = nullableString(upstream)
	item.AccountID = nullableString(account)
	item.ErrorCode = nullableString(errorCode)
	item.Streamed = streamed == 1
	item.StartedAt = time.UnixMilli(started).UTC()
	item.CompletedAt = time.UnixMilli(completed).UTC()
	item.DurationMS = max(completed-started, 0)
	if firstByte.Valid {
		value := time.UnixMilli(firstByte.Int64).UTC()
		item.FirstByteAt = &value
		ttft := max(firstByte.Int64-started, 0)
		item.TTFTMS = &ttft
	}
	if err := json.Unmarshal(usageJSON, &item.Usage); err != nil {
		return RequestRecord{}, 0, err
	}
	delete(item.Usage, "schema_version")
	return item, internalID, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

type requestCursor struct{ timestamp, id int64 }

func encodeRequestCursor(value requestCursor) string {
	raw := fmt.Sprintf("v1:%d:%d", value.timestamp, value.id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeRequestCursor(value string) (requestCursor, error) {
	if value == "" {
		return requestCursor{timestamp: 1<<63 - 1, id: 1<<63 - 1}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 128 {
		return requestCursor{}, ErrInvalidCursor
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 3 || parts[0] != "v1" {
		return requestCursor{}, ErrInvalidCursor
	}
	timestamp, timestampErr := strconv.ParseInt(parts[1], 10, 64)
	id, idErr := strconv.ParseInt(parts[2], 10, 64)
	if timestampErr != nil || idErr != nil || timestamp < 0 || id < 0 {
		return requestCursor{}, ErrInvalidCursor
	}
	return requestCursor{timestamp: timestamp, id: id}, nil
}

func normalizeRequestLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}
