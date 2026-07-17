package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DataDir               string
	ListenAddr            string
	PublicURL             *url.URL
	MasterKeyFile         string
	BootstrapPasswordFile string
	TrustedProxies        []netip.Prefix
	LogLevel              slog.Level
	MetricsAddr           string
	ShutdownTimeout       time.Duration
	Dev                   bool
}

type Getter func(string) string

func Load(dev bool) (Config, error) { return LoadWith(dev, os.Getenv) }

func LoadWith(dev bool, get Getter) (Config, error) {
	cfg := Config{
		DataDir:               valueOr(get("SIDERVIA_DATA_DIR"), "/var/lib/sidervia"),
		ListenAddr:            valueOr(get("SIDERVIA_LISTEN_ADDR"), "127.0.0.1:8080"),
		MasterKeyFile:         strings.TrimSpace(get("SIDERVIA_MASTER_KEY_FILE")),
		BootstrapPasswordFile: strings.TrimSpace(get("SIDERVIA_BOOTSTRAP_PASSWORD_FILE")),
		MetricsAddr:           strings.TrimSpace(get("SIDERVIA_METRICS_ADDR")),
		ShutdownTimeout:       30 * time.Second,
		Dev:                   dev,
	}

	if raw := strings.TrimSpace(get("SIDERVIA_SHUTDOWN_TIMEOUT")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < time.Second || d > 10*time.Minute {
			return Config{}, fmt.Errorf("SIDERVIA_SHUTDOWN_TIMEOUT must be between 1s and 10m")
		}
		cfg.ShutdownTimeout = d
	}

	switch strings.ToLower(valueOr(get("SIDERVIA_LOG_LEVEL"), "info")) {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		return Config{}, errors.New("SIDERVIA_LOG_LEVEL must be debug, info, warn, or error")
	}

	publicRaw := strings.TrimSpace(get("SIDERVIA_PUBLIC_URL"))
	if publicRaw == "" && dev {
		publicRaw = "http://" + cfg.ListenAddr
	}
	if publicRaw == "" {
		return Config{}, errors.New("SIDERVIA_PUBLIC_URL is required")
	}
	publicURL, err := url.Parse(publicRaw)
	if err != nil || publicURL.Host == "" || publicURL.User != nil || publicURL.RawQuery != "" || publicURL.Fragment != "" || publicURL.Path != "" {
		return Config{}, errors.New("SIDERVIA_PUBLIC_URL must be an origin without path, query, userinfo, or fragment")
	}
	if (!dev && publicURL.Scheme != "https") || (dev && publicURL.Scheme != "http" && publicURL.Scheme != "https") {
		return Config{}, errors.New("SIDERVIA_PUBLIC_URL must use HTTPS outside development mode")
	}
	cfg.PublicURL = publicURL

	if err := validateListen(cfg.ListenAddr, dev); err != nil {
		return Config{}, fmt.Errorf("SIDERVIA_LISTEN_ADDR: %w", err)
	}
	if cfg.MetricsAddr != "" {
		if err := validateListen(cfg.MetricsAddr, false); err != nil {
			return Config{}, fmt.Errorf("SIDERVIA_METRICS_ADDR: %w", err)
		}
		if err := validateMetricsListen(cfg.MetricsAddr); err != nil {
			return Config{}, fmt.Errorf("SIDERVIA_METRICS_ADDR: %w", err)
		}
	}

	for _, raw := range splitCSV(get("SIDERVIA_TRUSTED_PROXIES")) {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid trusted proxy prefix %q", raw)
		}
		if prefix.Bits() == 0 {
			return Config{}, errors.New("SIDERVIA_TRUSTED_PROXIES cannot trust the entire Internet")
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, prefix.Masked())
	}

	if cfg.MasterKeyFile == "" {
		return Config{}, errors.New("SIDERVIA_MASTER_KEY_FILE is required")
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data directory: %w", err)
	}
	cfg.DataDir = abs
	return cfg, nil
}

func validateMetricsListen(value string) error {
	host, _, _ := net.SplitHostPort(value)
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	address, err := netip.ParseAddr(host)
	if err != nil || address.IsUnspecified() || (!address.IsLoopback() && !address.IsPrivate()) {
		return errors.New("must bind to a loopback or private IP address")
	}
	return nil
}

func validateListen(addr string, dev bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("must be host:port")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return errors.New("invalid port")
	}
	if dev {
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return errors.New("development mode may only listen on loopback")
		}
	}
	return nil
}

func splitCSV(raw string) []string {
	var result []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
