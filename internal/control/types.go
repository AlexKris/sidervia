package control

import "time"

type Provider struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	AuthMethods          []string `json:"auth_methods"`
	Capabilities         []string `json:"capabilities"`
	ImplementationStatus string   `json:"implementation_status"`
}

type Proxy struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Scheme             string    `json:"scheme"`
	Host               string    `json:"host"`
	Port               int       `json:"port"`
	UsernameConfigured bool      `json:"username_configured"`
	PasswordConfigured bool      `json:"password_configured"`
	TLSServerName      string    `json:"tls_server_name,omitempty"`
	AllowInsecureTLS   bool      `json:"allow_insecure_tls"`
	Enabled            bool      `json:"enabled"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ProxyInput struct {
	Name             string
	Scheme           string
	Host             string
	Port             int
	Username         *string
	Password         *string
	TLSServerName    string
	AllowInsecureTLS bool
	Enabled          bool
}

type Upstream struct {
	ID                  string    `json:"id"`
	ProviderID          string    `json:"provider_id"`
	Name                string    `json:"name"`
	BaseURL             string    `json:"base_url"`
	DefaultProxyID      *string   `json:"default_proxy_id,omitempty"`
	AllowPrivateNetwork bool      `json:"allow_private_network"`
	Enabled             bool      `json:"enabled"`
	Version             int64     `json:"version"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type UpstreamInput struct {
	ProviderID          string
	Name                string
	BaseURL             string
	DefaultProxyID      *string
	AllowPrivateNetwork bool
	Enabled             bool
}

type Account struct {
	ID                   string     `json:"id"`
	UpstreamID           string     `json:"upstream_id"`
	Name                 string     `json:"name"`
	AuthKind             string     `json:"auth_kind"`
	BillingKind          string     `json:"billing_kind"`
	CredentialConfigured bool       `json:"credential_configured"`
	CredentialExpiresAt  *time.Time `json:"credential_expires_at,omitempty"`
	ProxyID              *string    `json:"proxy_id,omitempty"`
	Status               string     `json:"status"`
	Priority             int        `json:"priority"`
	MaxConcurrency       int        `json:"max_concurrency"`
	Version              int64      `json:"version"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type AccountInput struct {
	UpstreamID          string
	Name                string
	AuthKind            string
	Credential          *string
	CredentialExpiresAt *time.Time
	ProxyID             *string
	BillingKind         string
	Status              string
	Priority            *int
	MaxConcurrency      *int
}

type AccountValidation struct {
	Identity          map[string]any
	CapabilityVersion string
	Models            []string
}

type RouteCandidate struct {
	AccountID       string   `json:"account_id"`
	UpstreamModelID string   `json:"upstream_model_id"`
	Enabled         bool     `json:"enabled"`
	Protocols       []string `json:"protocols"`
	Capabilities    []string `json:"capabilities"`
}

type ModelRoute struct {
	ID                          string           `json:"id"`
	PublicModelID               string           `json:"public_model_id"`
	Description                 string           `json:"description"`
	Enabled                     bool             `json:"enabled"`
	MultipleCandidatesConfirmed bool             `json:"multiple_candidates_confirmed"`
	Candidates                  []RouteCandidate `json:"candidates"`
	Version                     int64            `json:"version"`
	CreatedAt                   time.Time        `json:"created_at"`
	UpdatedAt                   time.Time        `json:"updated_at"`
}

type ModelRouteInput struct {
	PublicModelID             string
	Description               string
	Enabled                   bool
	ConfirmMultipleCandidates bool
	Candidates                []RouteCandidate
}

type ClientKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Version    int64      `json:"version"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreatedClientKey struct {
	ClientKey ClientKey `json:"client_key"`
	Secret    string    `json:"secret"`
}

type AuditEvent struct {
	ID         string         `json:"id"`
	EventType  string         `json:"event_type"`
	ActorKind  string         `json:"actor_kind"`
	ActorID    *string        `json:"actor_id,omitempty"`
	TargetKind *string        `json:"target_kind,omitempty"`
	TargetID   *string        `json:"target_id,omitempty"`
	RequestID  *string        `json:"request_id,omitempty"`
	Outcome    string         `json:"outcome"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Actor struct {
	Kind      string
	ID        string
	RequestID string
}

type Dashboard struct {
	DatabaseReady    bool           `json:"database_ready"`
	AdminTOTPEnabled bool           `json:"admin_totp_enabled"`
	Counts           map[string]int `json:"counts"`
	Warnings         []string       `json:"warnings"`
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}
