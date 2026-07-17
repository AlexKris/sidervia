package routing

import (
	"context"
	"database/sql"
	"errors"

	"github.com/AlexKris/sidervia/internal/accountauth"
	"github.com/AlexKris/sidervia/internal/cryptox"
)

type AccountConfig struct {
	Candidate       Candidate
	Status          string
	UpstreamEnabled bool
	ProxyEnabled    bool
	HasProxy        bool
}

func (s *Service) LoadAccount(ctx context.Context, publicID string) (AccountConfig, error) {
	var result AccountConfig
	var credential []byte
	var credentialExpiry sql.NullInt64
	var proxyInternalID, proxyPort, proxyVersion sql.NullInt64
	var proxyPublicID, proxyScheme, proxyHost, proxyTLSName sql.NullString
	var proxyUsername, proxyPassword []byte
	var proxyInsecure, proxyEnabled sql.NullBool
	err := s.db.QueryRowContext(ctx, accountQuery, publicID).Scan(
		&result.Candidate.AccountInternalID, &result.Candidate.AccountPublicID, &result.Candidate.AuthKind,
		&credential, &result.Candidate.CredentialVersion, &credentialExpiry, &result.Status,
		&result.Candidate.Priority, &result.Candidate.MaxConcurrency, &result.Candidate.AccountVersion,
		&result.Candidate.UpstreamInternalID, &result.Candidate.UpstreamPublicID, &result.Candidate.ProviderID,
		&result.Candidate.BaseURL, &result.Candidate.AllowPrivateNetwork, &result.UpstreamEnabled, &result.Candidate.UpstreamVersion,
		&proxyInternalID, &proxyPublicID, &proxyScheme, &proxyHost, &proxyPort, &proxyUsername,
		&proxyPassword, &proxyTLSName, &proxyInsecure, &proxyEnabled, &proxyVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountConfig{}, sql.ErrNoRows
	}
	if err != nil {
		return AccountConfig{}, err
	}
	if len(credential) > 0 {
		payload, err := accountauth.Decrypt(s.cipher, result.Candidate.AccountPublicID, credential)
		if err != nil {
			return AccountConfig{}, err
		}
		result.Candidate.Credential, err = accountauth.ProviderCredential(result.Candidate.AuthKind, result.Candidate.ProviderID, payload)
		if err != nil {
			return AccountConfig{}, err
		}
	}
	if credentialExpiry.Valid {
		result.Candidate.CredentialExpiresAtMS = credentialExpiry.Int64
	}
	if proxyInternalID.Valid {
		result.HasProxy = true
		result.ProxyEnabled = proxyEnabled.Bool
		proxy := &Proxy{
			PublicID: proxyPublicID.String, Version: proxyVersion.Int64, Scheme: proxyScheme.String,
			Host: proxyHost.String, Port: int(proxyPort.Int64), TLSServerName: proxyTLSName.String,
			AllowInsecureTLS: proxyInsecure.Bool,
		}
		if len(proxyUsername) > 0 {
			plain, err := s.cipher.Open(proxyUsername, cryptox.AAD("egress_proxies", proxy.PublicID, "username_enc"))
			if err != nil {
				return AccountConfig{}, err
			}
			proxy.Username = string(plain)
		}
		if len(proxyPassword) > 0 {
			plain, err := s.cipher.Open(proxyPassword, cryptox.AAD("egress_proxies", proxy.PublicID, "password_enc"))
			if err != nil {
				return AccountConfig{}, err
			}
			proxy.Password = string(plain)
		}
		result.Candidate.Proxy = proxy
	} else {
		result.ProxyEnabled = true
	}
	return result, nil
}

const accountQuery = `SELECT
	a.id, a.public_id, a.auth_kind, a.credential_enc, a.credential_version,
	a.credential_expires_at_ms, a.status, a.priority, a.max_concurrency, a.version,
	u.id, u.public_id, u.provider_id, u.base_url, u.allow_private_network, u.enabled, u.version,
	p.id, p.public_id, p.scheme, p.host, p.port, p.username_enc, p.password_enc,
	p.tls_server_name, p.allow_insecure_tls, p.enabled, p.version
FROM accounts a
JOIN upstreams u ON u.id = a.upstream_id
LEFT JOIN egress_proxies p ON p.id = COALESCE(a.proxy_id, u.default_proxy_id)
WHERE a.public_id = ?`
