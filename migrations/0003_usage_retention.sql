CREATE TABLE request_daily_aggregates (
    day_start_ms INTEGER PRIMARY KEY CHECK (day_start_ms >= 0 AND day_start_ms % 86400000 = 0),
    request_count INTEGER NOT NULL CHECK (typeof(request_count) = 'integer' AND request_count >= 0),
    error_count INTEGER NOT NULL CHECK (typeof(error_count) = 'integer' AND error_count >= 0),
    streamed_count INTEGER NOT NULL CHECK (typeof(streamed_count) = 'integer' AND streamed_count >= 0),
    request_bytes INTEGER NOT NULL CHECK (typeof(request_bytes) = 'integer' AND request_bytes >= 0),
    response_bytes INTEGER NOT NULL CHECK (typeof(response_bytes) = 'integer' AND response_bytes >= 0),
    input_tokens INTEGER NOT NULL CHECK (typeof(input_tokens) = 'integer' AND input_tokens >= 0),
    output_tokens INTEGER NOT NULL CHECK (typeof(output_tokens) = 'integer' AND output_tokens >= 0),
    cache_read_tokens INTEGER NOT NULL CHECK (typeof(cache_read_tokens) = 'integer' AND cache_read_tokens >= 0),
    cache_write_tokens INTEGER NOT NULL CHECK (typeof(cache_write_tokens) = 'integer' AND cache_write_tokens >= 0),
    reasoning_tokens INTEGER NOT NULL CHECK (typeof(reasoning_tokens) = 'integer' AND reasoning_tokens >= 0),
    total_tokens INTEGER NOT NULL CHECK (typeof(total_tokens) = 'integer' AND total_tokens >= 0),
    updated_at_ms INTEGER NOT NULL
);
