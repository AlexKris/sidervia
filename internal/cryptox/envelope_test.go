package cryptox

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvelopeBindsAAD(t *testing.T) {
	key := bytes.Repeat([]byte{7}, KeySize)
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal([]byte("secret"), AAD("accounts", "a", "credential_enc"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.Open(sealed, AAD("accounts", "a", "credential_enc"))
	if err != nil || string(plain) != "secret" {
		t.Fatalf("open: %q, %v", plain, err)
	}
	if _, err := c.Open(sealed, AAD("accounts", "b", "credential_enc")); err == nil {
		t.Fatal("expected AAD mismatch")
	}
}

func TestLoadMasterKeyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	value := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, KeySize))
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadMasterKey(path)
	if err != nil || len(key) != KeySize {
		t.Fatalf("load key: %v", err)
	}
}
