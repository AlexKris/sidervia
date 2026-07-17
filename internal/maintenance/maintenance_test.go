package maintenance

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/store"
)

func TestBackupVerifyAndKeyRotation(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	oldCipher := testCipher(t, 1)
	newCipher := testCipher(t, 2)
	if err := database.VerifyOrCreateSentinel(ctx, oldCipher); err != nil {
		t.Fatal(err)
	}
	service := control.NewService(database.DB(), oldCipher, clock.Real{}, identifier.NewGenerator())
	actor := control.Actor{Kind: "local_admin"}
	username, password := "backup-user-canary", "backup-password-canary"
	proxy, err := service.CreateProxy(ctx, actor, control.ProxyInput{
		Name: "Backup proxy", Scheme: "https", Host: "proxy.example.test", Port: 443,
		Username: &username, Password: &password, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := service.CreateUpstream(ctx, actor, control.UpstreamInput{
		ProviderID: "openai", Name: "Backup upstream", BaseURL: "https://api.example.test/v1",
		DefaultProxyID: &proxy.ID, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := "backup-api-key-canary"
	if _, err := service.CreateAccount(ctx, actor, control.AccountInput{
		UpstreamID: upstream.ID, Name: "Backup account", Credential: &credential,
		BillingKind: "metered", Status: "draft",
	}); err != nil {
		t.Fatal(err)
	}

	backupPath := filepath.Join(t.TempDir(), "sidervia-backup.db")
	created, err := CreateBackup(ctx, database, oldCipher, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if created.SchemaVersion != store.LatestSchemaVersion || created.EncryptedRows < 4 {
		t.Fatalf("unexpected backup report: %+v", created)
	}
	verified, err := VerifyBackup(ctx, backupPath, oldCipher)
	if err != nil || verified.SHA256 != created.SHA256 {
		t.Fatalf("verify report=%+v err=%v", verified, err)
	}

	rotated, err := RotateKey(ctx, database, oldCipher, newCipher)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.RowsRotated < 4 || rotated.OldKeyID == rotated.NewKeyID {
		t.Fatalf("unexpected rotation report: %+v", rotated)
	}
	if err := database.VerifySentinel(ctx, oldCipher); err == nil {
		t.Fatal("old key still verifies after rotation")
	}
	if err := database.VerifySentinel(ctx, newCipher); err != nil {
		t.Fatalf("new key does not verify: %v", err)
	}
	if _, err := VerifyBackup(ctx, backupPath, newCipher); err == nil {
		t.Fatal("backup unexpectedly verified with a key created after the backup")
	}
}

func TestBackupChecksumTamperIsRejected(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cipher := testCipher(t, 3)
	if err := database.VerifyOrCreateSentinel(ctx, cipher); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backup.db")
	if _, err := CreateBackup(ctx, database, cipher, path); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("tamper")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBackup(ctx, path, cipher); err == nil {
		t.Fatal("tampered backup passed verification")
	}
}

func testCipher(t *testing.T, value byte) *cryptox.Cipher {
	t.Helper()
	cipher, err := cryptox.NewCipher(bytes.Repeat([]byte{value}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}
