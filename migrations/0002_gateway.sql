CREATE TABLE accounts_new (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    upstream_id INTEGER NOT NULL REFERENCES upstreams(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    auth_kind TEXT NOT NULL CHECK (auth_kind IN ('api_key', 'oauth')),
    billing_kind TEXT NOT NULL CHECK (billing_kind IN ('subscription', 'metered', 'custom')),
    credential_enc BLOB,
    credential_version INTEGER NOT NULL DEFAULT 1 CHECK (credential_version > 0),
    credential_expires_at_ms INTEGER,
    proxy_id INTEGER REFERENCES egress_proxies(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('draft', 'validating', 'active', 'invalid', 'reauth_required', 'disabled')),
    priority INTEGER NOT NULL CHECK (priority >= 0),
    max_concurrency INTEGER NOT NULL CHECK (max_concurrency BETWEEN 1 AND 1024),
    identity_json TEXT NOT NULL DEFAULT '{"schema_version":1}',
    capability_version TEXT,
    last_validated_at_ms INTEGER,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL,
    UNIQUE(upstream_id, name)
);

INSERT INTO accounts_new(
    id, public_id, upstream_id, name, auth_kind, billing_kind, credential_enc,
    credential_expires_at_ms, proxy_id, status, priority, max_concurrency,
    identity_json, capability_version, last_validated_at_ms, version,
    created_at_ms, updated_at_ms
)
SELECT
    id, public_id, upstream_id, name, auth_kind, billing_kind, credential_enc,
    credential_expires_at_ms, proxy_id, status, priority, max_concurrency,
    identity_json, capability_version, last_validated_at_ms, version,
    created_at_ms, updated_at_ms
FROM accounts;

CREATE TABLE route_candidates_new (
    id INTEGER PRIMARY KEY,
    model_route_id INTEGER NOT NULL REFERENCES model_routes(id) ON DELETE RESTRICT,
    account_id INTEGER NOT NULL REFERENCES accounts_new(id) ON DELETE RESTRICT,
    upstream_model_id TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    protocols_json TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    UNIQUE(model_route_id, account_id, upstream_model_id)
);

INSERT INTO route_candidates_new(
    id, model_route_id, account_id, upstream_model_id, enabled,
    protocols_json, capabilities_json, created_at_ms
)
SELECT
    id, model_route_id, account_id, upstream_model_id, enabled,
    protocols_json, capabilities_json, created_at_ms
FROM route_candidates;

DROP TABLE route_candidates;
DROP TABLE accounts;
ALTER TABLE accounts_new RENAME TO accounts;
ALTER TABLE route_candidates_new RENAME TO route_candidates;

CREATE TABLE account_runtime (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
    failure_streak INTEGER NOT NULL DEFAULT 0 CHECK (failure_streak >= 0),
    cooldown_until_ms INTEGER,
    quota_reset_at_ms INTEGER,
    last_success_at_ms INTEGER,
    last_error_at_ms INTEGER,
    last_error_code TEXT,
    quota_json TEXT NOT NULL DEFAULT '{"schema_version":1}',
    updated_at_ms INTEGER NOT NULL
);
CREATE INDEX account_runtime_cooldown_idx ON account_runtime(cooldown_until_ms);
CREATE INDEX account_runtime_quota_idx ON account_runtime(quota_reset_at_ms);

CREATE TABLE account_models (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    upstream_model_id TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('discovered', 'manual')),
    verified_at_ms INTEGER NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    UNIQUE(account_id, upstream_model_id)
);

CREATE TABLE provider_oauth_configs (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    provider_id TEXT NOT NULL UNIQUE CHECK (provider_id = 'google'),
    client_id TEXT NOT NULL,
    client_secret_enc BLOB NOT NULL,
    project_id TEXT NOT NULL,
    scopes_json TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL
);

CREATE TABLE oauth_attempts (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    admin_session_id INTEGER NOT NULL REFERENCES admin_sessions(id) ON DELETE CASCADE,
    provider_id TEXT NOT NULL CHECK (provider_id = 'google'),
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    flow_kind TEXT NOT NULL CHECK (flow_kind = 'authorization_code_pkce'),
    state_verifier TEXT NOT NULL UNIQUE,
    pkce_verifier_enc BLOB NOT NULL,
    egress_fingerprint TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'exchanging', 'consumed', 'failed', 'cancelled')),
    provider_payload_enc BLOB,
    created_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER NOT NULL,
    consumed_at_ms INTEGER
);
CREATE INDEX oauth_attempts_account_status_idx ON oauth_attempts(account_id, status);
CREATE INDEX oauth_attempts_expiry_idx ON oauth_attempts(expires_at_ms);

CREATE TABLE request_records (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    client_key_id INTEGER NOT NULL REFERENCES client_keys(id) ON DELETE RESTRICT,
    protocol TEXT NOT NULL,
    endpoint_kind TEXT NOT NULL,
    public_model_id TEXT NOT NULL,
    provider_id TEXT,
    upstream_id INTEGER REFERENCES upstreams(id) ON DELETE RESTRICT,
    account_id INTEGER REFERENCES accounts(id) ON DELETE RESTRICT,
    status_code INTEGER NOT NULL,
    error_code TEXT,
    streamed INTEGER NOT NULL CHECK (streamed IN (0, 1)),
    started_at_ms INTEGER NOT NULL,
    first_byte_at_ms INTEGER,
    completed_at_ms INTEGER NOT NULL,
    request_bytes INTEGER NOT NULL CHECK (request_bytes >= 0),
    response_bytes INTEGER NOT NULL CHECK (response_bytes >= 0),
    usage_json TEXT NOT NULL,
    routing_json TEXT NOT NULL
);
CREATE INDEX request_records_created_idx ON request_records(started_at_ms DESC, id DESC);
CREATE INDEX request_records_client_idx ON request_records(client_key_id, started_at_ms DESC);
CREATE INDEX request_records_account_idx ON request_records(account_id, started_at_ms DESC);
CREATE INDEX request_records_error_idx ON request_records(error_code, started_at_ms DESC);
