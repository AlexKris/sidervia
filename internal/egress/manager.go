package egress

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/routing"
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type Options struct {
	Resolver Resolver
	RootCAs  *x509.CertPool
}

type Manager struct {
	resolver Resolver
	roots    *x509.CertPool

	mu      sync.Mutex
	clients map[string]*http.Client
}

const googleOAuthTokenURL = "https://oauth2.googleapis.com/token"

func New(options Options) *Manager {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Manager{resolver: resolver, roots: options.RootCAs, clients: make(map[string]*http.Client)}
}

func (m *Manager) Do(ctx context.Context, candidate routing.Candidate, native provider.NativeRequest, adapter provider.Adapter) (*http.Response, error) {
	target, err := buildTarget(candidate.BaseURL, native.Path, native.Query)
	if err != nil {
		return nil, err
	}
	if _, err := m.resolveAllowed(ctx, target.Hostname(), candidate.AllowPrivateNetwork); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, native.Method, target.String(), bytes.NewReader(native.Body))
	if err != nil {
		return nil, err
	}
	request.Header = native.Header.Clone()
	request.Header.Set("User-Agent", "Sidervia/0.2")
	if err := adapter.Authorize(request, candidate.Credential); err != nil {
		return nil, err
	}
	client, err := m.client(candidate, target.Hostname())
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

func (m *Manager) DoGoogleOAuthToken(ctx context.Context, candidate routing.Candidate, form url.Values) (*http.Response, error) {
	target, err := url.Parse(googleOAuthTokenURL)
	if err != nil {
		return nil, errors.New("Google OAuth token endpoint is invalid")
	}
	if _, err := m.resolveAllowed(ctx, target.Hostname(), false); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("build Google OAuth token request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Sidervia/0.2")
	client, err := m.client(candidate, target.Hostname())
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

func (m *Manager) CloseIdleConnections() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, client := range m.clients {
		client.CloseIdleConnections()
	}
}

func (m *Manager) client(candidate routing.Candidate, upstreamHost string) (*http.Client, error) {
	key := fmt.Sprintf("%d:%d:%s", candidate.AccountInternalID, candidate.UpstreamVersion, strings.ToLower(upstreamHost))
	if candidate.Proxy != nil {
		key += fmt.Sprintf(":%s:%d", candidate.Proxy.PublicID, candidate.Proxy.Version)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.clients[key]; existing != nil {
		return existing, nil
	}
	dial, err := m.dialer(candidate, upstreamHost)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: nil, DialContext: dial, ForceAttemptHTTP2: true, DisableCompression: true,
		MaxIdleConns: 64, MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 5 * time.Minute,
		ExpectContinueTimeout: time.Second, MaxResponseHeaderBytes: 64 << 10,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: m.roots},
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("upstream redirects are disabled")
		},
	}
	m.clients[key] = client
	return client, nil
}

func (m *Manager) dialer(candidate routing.Candidate, upstreamHost string) (func(context.Context, string, string) (net.Conn, error), error) {
	if candidate.Proxy == nil {
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(upstreamHost, ".")) {
				return nil, errors.New("outbound target does not match configured upstream")
			}
			addresses, err := m.resolveAllowed(ctx, host, candidate.AllowPrivateNetwork)
			if err != nil {
				return nil, err
			}
			return dialAddresses(ctx, network, port, addresses)
		}, nil
	}
	proxy := candidate.Proxy
	if proxy.AllowInsecureTLS && proxy.Scheme != "https" {
		return nil, errors.New("allow_insecure_tls is only valid for an HTTPS proxy")
	}
	switch proxy.Scheme {
	case "http", "https":
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(upstreamHost, ".")) {
				return nil, errors.New("outbound target does not match configured upstream")
			}
			targets, err := m.resolveAllowed(ctx, host, candidate.AllowPrivateNetwork)
			if err != nil {
				return nil, err
			}
			return m.connectHTTPProxy(ctx, network, proxy, targets[0], port)
		}, nil
	case "socks5":
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(upstreamHost, ".")) {
				return nil, errors.New("outbound target does not match configured upstream")
			}
			targets, err := m.resolveAllowed(ctx, host, candidate.AllowPrivateNetwork)
			if err != nil {
				return nil, err
			}
			return m.connectSOCKS5(ctx, network, proxy, targets[0], port)
		}, nil
	default:
		return nil, errors.New("proxy scheme is unsupported")
	}
}

func (m *Manager) connectHTTPProxy(ctx context.Context, network string, proxy *routing.Proxy, target netip.Addr, targetPort string) (net.Conn, error) {
	proxyAddresses, err := m.resolveConfigured(ctx, proxy.Host)
	if err != nil {
		return nil, err
	}
	connection, err := dialAddresses(ctx, network, strconv.Itoa(proxy.Port), proxyAddresses)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = connection.Close()
		}
	}()
	if proxy.Scheme == "https" {
		serverName := proxy.TLSServerName
		if serverName == "" {
			serverName = proxy.Host
		}
		tlsConnection := tls.Client(connection, &tls.Config{
			MinVersion: tls.VersionTLS12, RootCAs: m.roots, ServerName: serverName,
			InsecureSkipVerify: proxy.AllowInsecureTLS,
		})
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("HTTPS proxy TLS handshake: %w", err)
		}
		connection = tlsConnection
	}
	deadline := time.Now().Add(15 * time.Second)
	_ = connection.SetDeadline(deadline)
	targetAddress := net.JoinHostPort(target.String(), targetPort)
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: targetAddress},
		Host:   targetAddress,
		Header: make(http.Header),
	}
	if proxy.Username != "" || proxy.Password != "" {
		token := base64.StdEncoding.EncodeToString([]byte(proxy.Username + ":" + proxy.Password))
		request.Header.Set("Proxy-Authorization", "Basic "+token)
	}
	if err := request.Write(connection); err != nil {
		return nil, fmt.Errorf("write proxy CONNECT: %w", err)
	}
	reader := bufio.NewReader(io.LimitReader(connection, 64<<10))
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, fmt.Errorf("read proxy CONNECT: %w", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT failed with status %d", response.StatusCode)
	}
	_ = connection.SetDeadline(time.Time{})
	keep = true
	return connection, nil
}

func (m *Manager) resolveAllowed(ctx context.Context, host string, allowPrivate bool) ([]netip.Addr, error) {
	addresses, err := m.resolveConfigured(ctx, host)
	if err != nil {
		return nil, err
	}
	if !allowPrivate {
		for _, address := range addresses {
			if prohibited(address) {
				return nil, errors.New("upstream resolved to a prohibited network address")
			}
		}
	}
	return addresses, nil
}

func (m *Manager) resolveConfigured(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address.Unmap()}, nil
	}
	addresses, err := m.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("configured host did not resolve")
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if address.IsValid() {
			result = append(result, address.Unmap())
		}
	}
	if len(result) == 0 {
		return nil, errors.New("configured host did not resolve to an IP address")
	}
	return result, nil
}

func prohibited(address netip.Addr) bool {
	address = address.Unmap()
	return !address.IsGlobalUnicast() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified()
}

func dialAddresses(ctx context.Context, network, port string, addresses []netip.Addr) (net.Conn, error) {
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var combined error
	for _, address := range addresses {
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if err == nil {
			return connection, nil
		}
		combined = errors.Join(combined, err)
	}
	return nil, combined
}

func buildTarget(baseRaw, nativePath string, query url.Values) (*url.URL, error) {
	base, err := url.Parse(baseRaw)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("configured upstream URL is invalid")
	}
	basePath := strings.TrimRight(base.Path, "/")
	path := nativePath
	for _, version := range []string{"/v1", "/v1beta"} {
		if strings.HasSuffix(basePath, version) && strings.HasPrefix(path, version+"/") {
			path = strings.TrimPrefix(path, version)
			break
		}
	}
	base.Path = basePath + "/" + strings.TrimLeft(path, "/")
	base.RawPath = ""
	base.RawQuery = query.Encode()
	return base, nil
}
