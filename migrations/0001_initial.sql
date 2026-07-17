CREATE TABLE crypto_sentinel (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    key_id TEXT NOT NULL,
    ciphertext BLOB NOT NULL
);

CREATE TABLE system_settings (
    key TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at_ms INTEGER NOT NULL
);

CREATE TABLE admin_user (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_phc TEXT NOT NULL,
    totp_secret_enc BLOB,
    totp_pending_secret_enc BLOB,
    totp_pending_expires_at_ms INTEGER,
    totp_enabled INTEGER NOT NULL DEFAULT 0 CHECK (totp_enabled IN (0, 1)),
    totp_last_used_step INTEGER,
    session_version INTEGER NOT NULL DEFAULT 1 CHECK (session_version > 0),
    failed_login_count INTEGER NOT NULL DEFAULT 0 CHECK (failed_login_count >= 0),
    locked_until_ms INTEGER,
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL
);

CREATE TABLE admin_sessions (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    token_verifier TEXT NOT NULL UNIQUE,
    csrf_token_enc BLOB NOT NULL,
    session_version INTEGER NOT NULL,
    created_at_ms INTEGER NOT NULL,
    last_seen_at_ms INTEGER NOT NULL,
    idle_expires_at_ms INTEGER NOT NULL,
    absolute_expires_at_ms INTEGER NOT NULL,
    ip_prefix_hmac TEXT NOT NULL,
    user_agent_hmac TEXT NOT NULL,
    revoked_at_ms INTEGER
);
CREATE INDEX admin_sessions_expiry_idx ON admin_sessions(absolute_expires_at_ms);
CREATE INDEX admin_sessions_revoked_idx ON admin_sessions(revoked_at_ms);

CREATE TABLE client_keys (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL UNIQUE,
    secret_verifier TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled', 'revoked')),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at_ms INTEGER NOT NULL,
    expires_at_ms INTEGER,
    last_used_at_ms INTEGER,
    revoked_at_ms INTEGER
);
CREATE INDEX client_keys_status_idx ON client_keys(status);

CREATE TABLE egress_proxies (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL UNIQUE,
    scheme TEXT NOT NULL CHECK (scheme IN ('http', 'https', 'socks5')),
    host TEXT NOT NULL,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    username_enc BLOB,
    password_enc BLOB,
    tls_server_name TEXT,
    allow_insecure_tls INTEGER NOT NULL DEFAULT 0 CHECK (allow_insecure_tls IN (0, 1)),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL
);

CREATE TABLE upstreams (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    provider_id TEXT NOT NULL,
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    default_proxy_id INTEGER REFERENCES egress_proxies(id) ON DELETE RESTRICT,
    allow_private_network INTEGER NOT NULL DEFAULT 0 CHECK (allow_private_network IN (0, 1)),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    config_json TEXT NOT NULL DEFAULT '{"schema_version":1}',
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL,
    UNIQUE(provider_id, name)
);

CREATE TABLE accounts (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    upstream_id INTEGER NOT NULL REFERENCES upstreams(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    auth_kind TEXT NOT NULL CHECK (auth_kind = 'api_key'),
    billing_kind TEXT NOT NULL CHECK (billing_kind IN ('subscription', 'metered', 'custom')),
    credential_enc BLOB NOT NULL,
    credential_expires_at_ms INTEGER,
    proxy_id INTEGER REFERENCES egress_proxies(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('draft', 'disabled')),
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

CREATE TABLE model_routes (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    public_model_id TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    required_confirmation_at_ms INTEGER,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at_ms INTEGER NOT NULL,
    updated_at_ms INTEGER NOT NULL
);

CREATE TABLE route_candidates (
    id INTEGER PRIMARY KEY,
    model_route_id INTEGER NOT NULL REFERENCES model_routes(id) ON DELETE RESTRICT,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    upstream_model_id TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    protocols_json TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL,
    UNIQUE(model_route_id, account_id, upstream_model_id)
);

CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    actor_kind TEXT NOT NULL,
    actor_id TEXT,
    target_kind TEXT,
    target_id TEXT,
    request_id TEXT,
    outcome TEXT NOT NULL,
    metadata_json TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL
);
CREATE INDEX audit_events_created_idx ON audit_events(created_at_ms DESC, id DESC);
