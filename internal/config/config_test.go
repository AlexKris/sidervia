package config

import (
	"path/filepath"
	"testing"
)

func TestLoadDevelopment(t *testing.T) {
	env := map[string]string{
		"SIDERVIA_DATA_DIR":        t.TempDir(),
		"SIDERVIA_LISTEN_ADDR":     "127.0.0.1:9000",
		"SIDERVIA_MASTER_KEY_FILE": filepath.Join(t.TempDir(), "master.key"),
	}
	cfg, err := LoadWith(true, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL.String() != "http://127.0.0.1:9000" || !cfg.Dev {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestProductionRequiresHTTPS(t *testing.T) {
	env := map[string]string{
		"SIDERVIA_PUBLIC_URL":      "http://example.com",
		"SIDERVIA_MASTER_KEY_FILE": "/tmp/master.key",
	}
	if _, err := LoadWith(false, func(k string) string { return env[k] }); err == nil {
		t.Fatal("expected HTTPS validation error")
	}
}

func TestRejectTrustAllProxy(t *testing.T) {
	env := map[string]string{
		"SIDERVIA_PUBLIC_URL":      "https://example.com",
		"SIDERVIA_MASTER_KEY_FILE": "/tmp/master.key",
		"SIDERVIA_TRUSTED_PROXIES": "0.0.0.0/0",
	}
	if _, err := LoadWith(false, func(k string) string { return env[k] }); err == nil {
		t.Fatal("expected trusted proxy validation error")
	}
}
