package routing

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/AlexKris/sidervia/internal/accountauth"
	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/provider"
)

type Proxy struct {
	PublicID         string
	Version          int64
	Scheme           string
	Host             string
	Port             int
	Username         string
	Password         string
	TLSServerName    string
	AllowInsecureTLS bool
}

type Candidate struct {
	RouteInternalID       int64
	RoutePublicID         string
	AccountInternalID     int64
	AccountPublicID       string
	AccountVersion        int64
	CredentialVersion     int64
	CredentialExpiresAtMS int64
	AuthKind              string
	ProviderID            string
	UpstreamInternalID    int64
	UpstreamPublicID      string
	UpstreamVersion       int64
	BaseURL               string
	AllowPrivateNetwork   bool
	UpstreamModelID       string
	Priority              int
	MaxConcurrency        int
	Credential            provider.Credential
	Proxy                 *Proxy
	FailureStreak         int
}

type Request struct {
	PublicModelID        string
	Protocol             string
	RequiredCapabilities []string
}

type Explanation struct {
	CandidateCount          int            `json:"candidate_count"`
	Filtered                map[string]int `json:"filtered"`
	SelectedPriority        int            `json:"selected_priority,omitempty"`
	SelectedLoadNumerator   int            `json:"selected_load_numerator,omitempty"`
	SelectedLoadDenominator int            `json:"selected_load_denominator,omitempty"`
}

type RouteError struct {
	Code       string
	RetryAfter time.Duration
}

func (e *RouteError) Error() string { return e.Code }

type runtimeState struct {
	inFlight int
}

type Service struct {
	db     *sql.DB
	cipher *cryptox.Cipher
	clock  clock.Clock
	jitter func(time.Duration) time.Duration

	mu      sync.Mutex
	states  map[int64]*runtimeState
	cursors map[int64]uint64
}

func New(db *sql.DB, cipher *cryptox.Cipher, c clock.Clock) *Service {
	if c == nil {
		c = clock.Real{}
	}
	return &Service{
		db: db, cipher: cipher, clock: c, jitter: fullJitter,
		states: make(map[int64]*runtimeState), cursors: make(map[int64]uint64),
	}
}

func (s *Service) Plan(ctx context.Context, request Request) (*Lease, Explanation, error) {
	rows, err := s.db.QueryContext(ctx, routeQuery, strings.TrimSpace(request.PublicModelID))
	if err != nil {
		return nil, Explanation{}, err
	}
	defer rows.Close()
	explanation := Explanation{Filtered: make(map[string]int)}
	now := s.clock.Now().UnixMilli()
	var candidates []Candidate
	var routeFound bool
	var routeID int64
	var quotaRetryAt, cooldownRetryAt int64
	for rows.Next() {
		loaded, flags, err := s.scanRouteCandidate(rows)
		if err != nil {
			return nil, explanation, err
		}
		routeFound = true
		routeID = loaded.candidate.RouteInternalID
		explanation.CandidateCount++
		reason := s.filter(loaded, request, now, flags)
		if reason != "" {
			explanation.Filtered[reason]++
			switch reason {
			case "quota_limited":
				quotaRetryAt = earlierPositive(quotaRetryAt, loaded.quotaResetAt.Int64)
			case "account_cooldown":
				cooldownRetryAt = earlierPositive(cooldownRetryAt, loaded.cooldownUntil.Int64)
			}
			continue
		}
		candidates = append(candidates, loaded.candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, explanation, err
	}
	if !routeFound {
		return nil, explanation, &RouteError{Code: "model_not_configured"}
	}
	if len(candidates) == 0 {
		return nil, explanation, noCandidateError(explanation.Filtered, now, quotaRetryAt, cooldownRetryAt)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	withCapacity := candidates[:0]
	for _, candidate := range candidates {
		if s.stateLocked(candidate.AccountInternalID).inFlight >= candidate.MaxConcurrency {
			explanation.Filtered["all_accounts_saturated"]++
			continue
		}
		withCapacity = append(withCapacity, candidate)
	}
	if len(withCapacity) == 0 {
		return nil, explanation, &RouteError{Code: "all_accounts_saturated"}
	}
	minimumPriority := withCapacity[0].Priority
	for _, candidate := range withCapacity[1:] {
		if candidate.Priority < minimumPriority {
			minimumPriority = candidate.Priority
		}
	}
	filtered := withCapacity[:0]
	for _, candidate := range withCapacity {
		if candidate.Priority == minimumPriority {
			filtered = append(filtered, candidate)
		} else {
			explanation.Filtered["lower_priority"]++
		}
	}
	available := make([]Candidate, 0, len(filtered))
	bestNumerator, bestDenominator := 0, 1
	haveBest := false
	for _, candidate := range filtered {
		state := s.stateLocked(candidate.AccountInternalID)
		if !haveBest || state.inFlight*bestDenominator < bestNumerator*candidate.MaxConcurrency {
			bestNumerator, bestDenominator = state.inFlight, candidate.MaxConcurrency
			haveBest = true
			available = available[:0]
			available = append(available, candidate)
		} else if state.inFlight*bestDenominator == bestNumerator*candidate.MaxConcurrency {
			available = append(available, candidate)
		}
	}
	cursor := s.cursors[routeID]
	selected := available[cursor%uint64(len(available))]
	s.cursors[routeID] = cursor + 1
	state := s.stateLocked(selected.AccountInternalID)
	state.inFlight++
	explanation.SelectedPriority = selected.Priority
	explanation.SelectedLoadNumerator = state.inFlight - 1
	explanation.SelectedLoadDenominator = selected.MaxConcurrency
	return &Lease{Candidate: selected, service: s}, explanation, nil
}

type Lease struct {
	Candidate Candidate
	service   *Service
	once      sync.Once
}

func (l *Lease) Release() {
	if l == nil || l.service == nil {
		return
	}
	l.once.Do(func() {
		l.service.mu.Lock()
		defer l.service.mu.Unlock()
		state := l.service.stateLocked(l.Candidate.AccountInternalID)
		if state.inFlight > 0 {
			state.inFlight--
		}
	})
}

func (s *Service) InFlight(accountID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked(accountID).inFlight
}

func (s *Service) stateLocked(accountID int64) *runtimeState {
	state := s.states[accountID]
	if state == nil {
		state = &runtimeState{}
		s.states[accountID] = state
	}
	return state
}

type loadedCandidate struct {
	candidate        Candidate
	routeEnabled     bool
	candidateEnabled bool
	accountStatus    string
	credentialExpiry sql.NullInt64
	hasCredential    bool
	upstreamEnabled  bool
	proxyEnabled     sql.NullBool
	protocols        []string
	capabilities     []string
	cooldownUntil    sql.NullInt64
	quotaResetAt     sql.NullInt64
}

type scanFlags struct{}

func (s *Service) scanRouteCandidate(rows *sql.Rows) (loadedCandidate, scanFlags, error) {
	var loaded loadedCandidate
	var credential []byte
	var protocolsJSON, capabilitiesJSON []byte
	var proxyPublicID, proxyScheme, proxyHost, proxyTLSName sql.NullString
	var proxyInternalID, proxyPort, proxyVersion sql.NullInt64
	var proxyUsername, proxyPassword []byte
	var proxyInsecure sql.NullBool
	var failureStreak sql.NullInt64
	err := rows.Scan(
		&loaded.candidate.RouteInternalID, &loaded.candidate.RoutePublicID, &loaded.routeEnabled,
		&loaded.candidateEnabled, &loaded.candidate.UpstreamModelID, &protocolsJSON, &capabilitiesJSON,
		&loaded.candidate.AccountInternalID, &loaded.candidate.AccountPublicID, &loaded.candidate.AuthKind,
		&credential, &loaded.candidate.CredentialVersion, &loaded.credentialExpiry, &loaded.accountStatus,
		&loaded.candidate.Priority, &loaded.candidate.MaxConcurrency, &loaded.candidate.AccountVersion,
		&loaded.candidate.UpstreamInternalID, &loaded.candidate.UpstreamPublicID, &loaded.candidate.ProviderID,
		&loaded.candidate.BaseURL, &loaded.candidate.AllowPrivateNetwork, &loaded.upstreamEnabled, &loaded.candidate.UpstreamVersion,
		&proxyInternalID, &proxyPublicID, &proxyScheme, &proxyHost, &proxyPort, &proxyUsername,
		&proxyPassword, &proxyTLSName, &proxyInsecure, &loaded.proxyEnabled, &proxyVersion,
		&failureStreak, &loaded.cooldownUntil, &loaded.quotaResetAt,
	)
	if err != nil {
		return loadedCandidate{}, scanFlags{}, err
	}
	loaded.protocols, err = decodeStringSet(protocolsJSON)
	if err != nil {
		return loadedCandidate{}, scanFlags{}, err
	}
	loaded.capabilities, err = decodeStringSet(capabilitiesJSON)
	if err != nil {
		return loadedCandidate{}, scanFlags{}, err
	}
	if failureStreak.Valid {
		loaded.candidate.FailureStreak = int(failureStreak.Int64)
	}
	if len(credential) > 0 {
		loaded.hasCredential = true
		payload, err := accountauth.Decrypt(s.cipher, loaded.candidate.AccountPublicID, credential)
		if err != nil {
			return loadedCandidate{}, scanFlags{}, err
		}
		loaded.candidate.Credential, err = accountauth.ProviderCredential(loaded.candidate.AuthKind, loaded.candidate.ProviderID, payload)
		if err != nil {
			return loadedCandidate{}, scanFlags{}, err
		}
	}
	if loaded.credentialExpiry.Valid {
		loaded.candidate.CredentialExpiresAtMS = loaded.credentialExpiry.Int64
	}
	if proxyInternalID.Valid {
		proxy := &Proxy{
			PublicID: proxyPublicID.String, Version: proxyVersion.Int64, Scheme: proxyScheme.String,
			Host: proxyHost.String, Port: int(proxyPort.Int64), TLSServerName: proxyTLSName.String,
			AllowInsecureTLS: proxyInsecure.Bool,
		}
		if len(proxyUsername) > 0 {
			plain, err := s.cipher.Open(proxyUsername, cryptox.AAD("egress_proxies", proxy.PublicID, "username_enc"))
			if err != nil {
				return loadedCandidate{}, scanFlags{}, err
			}
			proxy.Username = string(plain)
		}
		if len(proxyPassword) > 0 {
			plain, err := s.cipher.Open(proxyPassword, cryptox.AAD("egress_proxies", proxy.PublicID, "password_enc"))
			if err != nil {
				return loadedCandidate{}, scanFlags{}, err
			}
			proxy.Password = string(plain)
		}
		loaded.candidate.Proxy = proxy
	}
	return loaded, scanFlags{}, nil
}

func (s *Service) filter(loaded loadedCandidate, request Request, now int64, _ scanFlags) string {
	if !loaded.routeEnabled || !loaded.candidateEnabled || !loaded.upstreamEnabled {
		return "upstream_unavailable"
	}
	if loaded.proxyEnabled.Valid && !loaded.proxyEnabled.Bool {
		return "upstream_unavailable"
	}
	if !protocolMatches(loaded.protocols, request.Protocol, loaded.candidate.ProviderID) || !containsAll(loaded.capabilities, request.RequiredCapabilities) {
		return "capability_not_supported"
	}
	switch loaded.accountStatus {
	case "reauth_required", "invalid":
		return "reauth_required"
	case "active":
	default:
		return "account_disabled"
	}
	if !loaded.hasCredential {
		return "reauth_required"
	}
	if loaded.credentialExpiry.Valid && loaded.credentialExpiry.Int64 <= now && loaded.candidate.AuthKind != "oauth" {
		return "reauth_required"
	}
	if loaded.quotaResetAt.Valid && loaded.quotaResetAt.Int64 > now {
		return "quota_limited"
	}
	if loaded.cooldownUntil.Valid && loaded.cooldownUntil.Int64 > now {
		return "account_cooldown"
	}
	return ""
}

func noCandidateError(filtered map[string]int, now, quotaRetryAt, cooldownRetryAt int64) error {
	for _, code := range []string{"reauth_required", "capability_not_supported", "quota_limited", "all_accounts_saturated", "account_cooldown", "account_disabled"} {
		if filtered[code] > 0 {
			result := &RouteError{Code: code}
			switch code {
			case "quota_limited":
				result.RetryAfter = time.Duration(max(quotaRetryAt-now, 0)) * time.Millisecond
			case "account_cooldown":
				result.RetryAfter = time.Duration(max(cooldownRetryAt-now, 0)) * time.Millisecond
			}
			return result
		}
	}
	return &RouteError{Code: "upstream_unavailable"}
}

func earlierPositive(current, candidate int64) int64 {
	if candidate <= 0 || (current > 0 && current <= candidate) {
		return current
	}
	return candidate
}

func protocolMatches(values []string, requested, providerID string) bool {
	if contains(values, requested) {
		return true
	}
	return requested == "openai" && providerID == "xai" && contains(values, "xai")
}

func containsAll(values, required []string) bool {
	for _, requirement := range required {
		if !contains(values, requirement) {
			return false
		}
	}
	return true
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func decodeStringSet(body []byte) ([]string, error) {
	var value struct {
		SchemaVersion int      `json:"schema_version"`
		Values        []string `json:"values"`
	}
	if err := json.Unmarshal(body, &value); err != nil || value.SchemaVersion != 1 {
		return nil, errors.New("route candidate set is invalid")
	}
	return value.Values, nil
}

func fullJitter(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	var body [8]byte
	if _, err := rand.Read(body[:]); err != nil {
		return maximum / 2
	}
	return time.Duration(binary.LittleEndian.Uint64(body[:]) % uint64(maximum))
}

const routeQuery = `SELECT
	r.id, r.public_id, r.enabled,
	c.enabled, c.upstream_model_id, c.protocols_json, c.capabilities_json,
	a.id, a.public_id, a.auth_kind, a.credential_enc, a.credential_version,
	a.credential_expires_at_ms, a.status, a.priority, a.max_concurrency, a.version,
	u.id, u.public_id, u.provider_id, u.base_url, u.allow_private_network, u.enabled, u.version,
	p.id, p.public_id, p.scheme, p.host, p.port, p.username_enc, p.password_enc,
	p.tls_server_name, p.allow_insecure_tls, p.enabled, p.version,
	ar.failure_streak, ar.cooldown_until_ms, ar.quota_reset_at_ms
FROM model_routes r
JOIN route_candidates c ON c.model_route_id = r.id
JOIN accounts a ON a.id = c.account_id
JOIN upstreams u ON u.id = a.upstream_id
LEFT JOIN egress_proxies p ON p.id = COALESCE(a.proxy_id, u.default_proxy_id)
LEFT JOIN account_runtime ar ON ar.account_id = a.id
WHERE r.public_model_id = ?
ORDER BY c.id`

func (s *Service) RecordSuccess(ctx context.Context, accountID int64) error {
	now := s.clock.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO account_runtime(
		account_id, failure_streak, last_success_at_ms, quota_json, updated_at_ms
	) VALUES(?, 0, ?, '{"schema_version":1}', ?)
	ON CONFLICT(account_id) DO UPDATE SET
		failure_streak = 0, cooldown_until_ms = NULL, last_success_at_ms = excluded.last_success_at_ms,
		last_error_code = NULL, updated_at_ms = excluded.updated_at_ms`, accountID, now, now)
	return err
}

type Failure struct {
	StatusCode int
	Code       string
	RetryAfter time.Duration
	Network    bool
}

func (s *Service) RecordFailure(ctx context.Context, candidate Candidate, failure Failure) error {
	now := s.clock.Now()
	if failure.StatusCode == httpUnauthorized {
		_, err := s.db.ExecContext(ctx, `UPDATE accounts SET status = 'reauth_required',
			version = version + 1, updated_at_ms = ? WHERE id = ? AND status = 'active'`,
			now.UnixMilli(), candidate.AccountInternalID)
		if err != nil {
			return err
		}
		return s.recordRuntimeError(ctx, candidate.AccountInternalID, failure.Code, nil, nil)
	}
	if failure.StatusCode == httpTooManyRequests && failure.RetryAfter > 0 {
		delay := clamp(failure.RetryAfter, time.Second, 24*time.Hour)
		until := now.Add(delay).UnixMilli()
		return s.recordRuntimeError(ctx, candidate.AccountInternalID, failure.Code, nil, &until)
	}
	if failure.StatusCode == httpTooManyRequests {
		return s.recordBackoff(ctx, candidate.AccountInternalID, failure.Code, 30*time.Second, 15*time.Minute)
	}
	if failure.Network || failure.StatusCode >= 500 {
		return s.recordBackoff(ctx, candidate.AccountInternalID, failure.Code, 2*time.Second, 2*time.Minute)
	}
	return nil
}

func (s *Service) recordBackoff(ctx context.Context, accountID int64, code string, base, maximum time.Duration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.clock.Now()
	var streak int
	err = tx.QueryRowContext(ctx, `INSERT INTO account_runtime(
		account_id, failure_streak, last_error_at_ms, last_error_code, quota_json, updated_at_ms
	) VALUES(?, 1, ?, ?, '{"schema_version":1}', ?)
	ON CONFLICT(account_id) DO UPDATE SET
		failure_streak = account_runtime.failure_streak + 1,
		last_error_at_ms = excluded.last_error_at_ms,
		last_error_code = excluded.last_error_code,
		updated_at_ms = excluded.updated_at_ms
	RETURNING failure_streak`, accountID, now.UnixMilli(), code, now.UnixMilli()).Scan(&streak)
	if err != nil {
		return err
	}
	capDuration := base
	for step := 1; step < streak && capDuration < maximum; step++ {
		if capDuration > maximum/2 {
			capDuration = maximum
			break
		}
		capDuration *= 2
	}
	until := now.Add(s.jitter(capDuration)).UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE account_runtime SET cooldown_until_ms = ?, updated_at_ms = ? WHERE account_id = ?`, until, now.UnixMilli(), accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) recordRuntimeError(ctx context.Context, accountID int64, code string, cooldown, quota *int64) error {
	now := s.clock.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO account_runtime(
		account_id, failure_streak, cooldown_until_ms, quota_reset_at_ms,
		last_error_at_ms, last_error_code, quota_json, updated_at_ms
	) VALUES(?, 1, ?, ?, ?, ?, '{"schema_version":1}', ?)
	ON CONFLICT(account_id) DO UPDATE SET
		failure_streak = account_runtime.failure_streak + 1,
		cooldown_until_ms = COALESCE(excluded.cooldown_until_ms, account_runtime.cooldown_until_ms),
		quota_reset_at_ms = COALESCE(excluded.quota_reset_at_ms, account_runtime.quota_reset_at_ms),
		last_error_at_ms = excluded.last_error_at_ms,
		last_error_code = excluded.last_error_code,
		updated_at_ms = excluded.updated_at_ms`, accountID, cooldown, quota, now, code, now)
	return err
}

func clamp(value, minimum, maximum time.Duration) time.Duration {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

const (
	httpUnauthorized    = 401
	httpTooManyRequests = 429
)

func (s *Service) SetJitterForTest(jitter func(time.Duration) time.Duration) {
	if jitter != nil {
		s.jitter = jitter
	}
}

func (e Explanation) JSON() string {
	body, err := json.Marshal(struct {
		SchemaVersion           int            `json:"schema_version"`
		CandidateCount          int            `json:"candidate_count"`
		Filtered                map[string]int `json:"filtered"`
		SelectedPriority        int            `json:"selected_priority,omitempty"`
		SelectedLoadNumerator   int            `json:"selected_load_numerator,omitempty"`
		SelectedLoadDenominator int            `json:"selected_load_denominator,omitempty"`
	}{1, e.CandidateCount, e.Filtered, e.SelectedPriority, e.SelectedLoadNumerator, e.SelectedLoadDenominator})
	if err != nil {
		return `{"schema_version":1}`
	}
	return string(body)
}
