package control

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"net/url"
	"sort"
	"strings"
)

func validateHost(field, value string) (string, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, "."))
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "/?#@[] \t\r\n") {
		return "", ValidationError{Field: field, Message: "must be a DNS name or IP without scheme or port"}
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.String(), nil
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ValidationError{Field: field, Message: "contains an invalid DNS label"}
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return "", ValidationError{Field: field, Message: "contains an invalid DNS label"}
			}
		}
	}
	return strings.ToLower(value), nil
}

func normalizeBaseURL(raw string, allowPrivate bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ValidationError{Field: "base_url", Message: "must be an HTTPS URL without userinfo, query, or fragment"}
	}
	if u.RawPath != "" || strings.Contains(u.Path, "..") {
		return "", ValidationError{Field: "base_url", Message: "contains an unsafe path"}
	}
	host := u.Hostname()
	if host == "" {
		return "", ValidationError{Field: "base_url", Message: "must include a host"}
	}
	if !allowPrivate && isPrivateHost(host) {
		return "", ValidationError{Field: "base_url", Message: "private or loopback hosts require allow_private_network"}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func isPrivateHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified()
}

func validateProxyInput(in ProxyInput) (ProxyInput, error) {
	var err error
	in.Name, err = cleanName("name", in.Name)
	if err != nil {
		return ProxyInput{}, err
	}
	switch in.Scheme {
	case "http", "https", "socks5":
	default:
		return ProxyInput{}, ValidationError{Field: "scheme", Message: "must be http, https, or socks5"}
	}
	in.Host, err = validateHost("host", in.Host)
	if err != nil {
		return ProxyInput{}, err
	}
	if in.Port < 1 || in.Port > 65535 {
		return ProxyInput{}, ValidationError{Field: "port", Message: "must be between 1 and 65535"}
	}
	if in.TLSServerName != "" {
		in.TLSServerName, err = validateHost("tls_server_name", in.TLSServerName)
		if err != nil {
			return ProxyInput{}, err
		}
	}
	for field, value := range map[string]*string{"username": in.Username, "password": in.Password} {
		if value != nil && len(*value) > 1024 {
			return ProxyInput{}, ValidationError{Field: field, Message: "is too long"}
		}
	}
	return in, nil
}

func normalizeStringSet(field string, values []string, allowed map[string]bool) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 64 {
			return nil, ValidationError{Field: field, Message: "contains an invalid value"}
		}
		if allowed != nil && !allowed[value] {
			return nil, ValidationError{Field: field, Message: "contains an unsupported value"}
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func validOptionalReference(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	clean := strings.TrimSpace(*value)
	return &clean
}

func lookupID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, table, publicID string) (int64, error) {
	if !map[string]bool{"egress_proxies": true, "upstreams": true, "accounts": true}[table] {
		return 0, errors.New("unsupported lookup table")
	}
	var id int64
	err := q.QueryRowContext(ctx, "SELECT id FROM "+table+" WHERE public_id = ?", publicID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
