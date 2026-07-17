package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/app"
	"github.com/AlexKris/sidervia/internal/config"
	"github.com/AlexKris/sidervia/internal/safelog"
	webassets "github.com/AlexKris/sidervia/web"
)

func TestVersionWithoutConfiguration(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"version"}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"schema_max": 1`) {
		t.Fatalf("unexpected version output: %s", output.String())
	}
}

func TestHelpWithoutConfiguration(t *testing.T) {
	for _, arguments := range [][]string{{"--help"}, {"serve", "--help"}, {"backup", "create", "--help"}} {
		var output bytes.Buffer
		if err := run(arguments, &output, io.Discard); err != nil {
			t.Fatalf("run(%q): %v", arguments, err)
		}
		if !strings.Contains(output.String(), "Usage:") {
			t.Fatalf("run(%q) did not print usage: %s", arguments, output.String())
		}
	}
}

func TestServeBackupDoctorResetAndRotateLifecycle(t *testing.T) {
	directory := t.TempDir()
	dataDirectory := filepath.Join(directory, "data")
	masterKey := writeKey(t, directory, "master.key")
	newMasterKey := writeKey(t, directory, "master-new.key")
	bootstrap := writeSecret(t, directory, "bootstrap-password", "correct horse battery staple\n")
	resetPassword := writeSecret(t, directory, "reset-password", "another correct horse battery staple\n")
	listenAddress := availableAddress(t)
	publicURL, _ := url.Parse("http://" + listenAddress)
	cfg := config.Config{
		DataDir: dataDirectory, ListenAddr: listenAddress, PublicURL: publicURL,
		MasterKeyFile: masterKey, BootstrapPasswordFile: bootstrap, LogLevel: slog.LevelError,
		ShutdownTimeout: 5 * time.Second, Dev: true,
	}

	t.Setenv("SIDERVIA_DATA_DIR", dataDirectory)
	t.Setenv("SIDERVIA_LISTEN_ADDR", listenAddress)
	t.Setenv("SIDERVIA_PUBLIC_URL", publicURL.String())
	t.Setenv("SIDERVIA_MASTER_KEY_FILE", masterKey)
	t.Setenv("SIDERVIA_BOOTSTRAP_PASSWORD_FILE", bootstrap)
	t.Setenv("SIDERVIA_LOG_LEVEL", "error")

	serveContext, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- app.Serve(serveContext, cfg, webassets.Handler(), safelog.New(io.Discard, slog.LevelError))
	}()
	waitReady(t, cfg)

	var output bytes.Buffer
	if err := run([]string{"--dev", "doctor", "--healthcheck"}, &output, io.Discard); err != nil {
		t.Fatalf("healthcheck: %v", err)
	}
	output.Reset()
	if err := run([]string{"--dev", "doctor"}, &output, io.Discard); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(output.String(), `"status": "ok"`) {
		t.Fatalf("doctor output: %s", output.String())
	}

	backupPath := filepath.Join(directory, "backup.db")
	output.Reset()
	if err := run([]string{"--dev", "backup", "create", "--output", backupPath}, &output, io.Discard); err != nil {
		t.Fatalf("backup create while serving: %v", err)
	}
	if _, err := os.Stat(backupPath + ".sha256"); err != nil {
		t.Fatalf("backup checksum: %v", err)
	}
	if err := run([]string{"--dev", "backup", "verify", "--input", backupPath}, io.Discard, io.Discard); err != nil {
		t.Fatalf("backup verify: %v", err)
	}
	if err := run([]string{"--dev", "admin", "reset-password", "--password-file", resetPassword}, io.Discard, io.Discard); err == nil {
		t.Fatal("offline password reset succeeded while the server held the data lock")
	}

	cancel()
	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("serve shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
	if err := run([]string{"--dev", "admin", "reset-password", "--password-file", resetPassword, "--disable-totp"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("offline password reset: %v", err)
	}
	if err := run([]string{"--dev", "key", "rotate", "--new-key-file", newMasterKey, "--backup", backupPath}, io.Discard, io.Discard); err != nil {
		t.Fatalf("key rotate: %v", err)
	}
	if err := run([]string{"--dev", "doctor"}, io.Discard, io.Discard); err == nil {
		t.Fatal("old master key passed doctor after rotation")
	}
	t.Setenv("SIDERVIA_MASTER_KEY_FILE", newMasterKey)
	if err := run([]string{"--dev", "doctor"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("doctor with rotated key: %v", err)
	}
}

func waitReady(t *testing.T, cfg config.Config) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		err := app.Healthcheck(ctx, cfg)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("Sidervia did not become ready")
}

func availableAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func writeKey(t *testing.T, directory, name string) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return writeSecret(t, directory, name, base64.StdEncoding.EncodeToString(key)+"\n")
}

func writeSecret(t *testing.T, directory, name, value string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
