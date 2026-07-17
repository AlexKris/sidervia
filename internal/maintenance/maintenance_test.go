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
	oauthCiphertexts := createOAuthRotationFixture(t, database, service, oldCipher)

	backupPath := filepath.Join(t.TempDir(), "sidervia-backup.db")
	created, err := CreateBackup(ctx, database, oldCipher, backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if created.SchemaVersion != store.LatestSchemaVersion || created.EncryptedRows < 8 {
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
	if rotated.RowsRotated < 8 || rotated.OldKeyID == rotated.NewKeyID {
		t.Fatalf("unexpected rotation report: %+v", rotated)
	}
	for _, encrypted := range oauthCiphertexts {
		var envelope []byte
		if err := database.DB().QueryRow("SELECT "+encrypted.column+" FROM "+encrypted.table+" WHERE public_id = ?", encrypted.publicID).Scan(&envelope); err != nil {
			t.Fatal(err)
		}
		if _, err := newCipher.Open(envelope, cryptox.AAD(encrypted.table, encrypted.publicID, encrypted.column)); err != nil {
			t.Fatalf("%s.%s was not rotated: %v", encrypted.table, encrypted.column, err)
		}
		if _, err := oldCipher.Open(envelope, cryptox.AAD(encrypted.table, encrypted.publicID, encrypted.column)); err == nil {
			t.Fatalf("%s.%s still decrypts with the old key", encrypted.table, encrypted.column)
		}
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

type oauthCiphertext struct {
	table, publicID, column string
}

func createOAuthRotationFixture(t *testing.T, database *store.Store, service *control.Service, cipher *cryptox.Cipher) []oauthCiphertext {
	t.Helper()
	ctx := context.Background()
	googleUpstream, err := service.CreateUpstream(ctx, control.Actor{}, control.UpstreamInput{
		ProviderID: "google", Name: "OAuth rotation", BaseURL: "https://generativelanguage.googleapis.com", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := service.CreateAccount(ctx, control.Actor{}, control.AccountInput{
		UpstreamID: googleUpstream.ID, Name: "OAuth rotation", AuthKind: "oauth", BillingKind: "metered", Status: "draft",
	})
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "sdr_sess_rotation"
	csrf, err := cipher.Seal([]byte("csrf-canary"), cryptox.AAD("admin_sessions", sessionID, "csrf_token_enc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO admin_user(id, password_phc, created_at_ms, updated_at_ms)
		VALUES(1, 'password-hash', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO admin_sessions(public_id, token_verifier,
		csrf_token_enc, session_version, created_at_ms, last_seen_at_ms, idle_expires_at_ms,
		absolute_expires_at_ms, ip_prefix_hmac, user_agent_hmac)
		VALUES(?, 'token-verifier', ?, 1, 1, 1, 9999999999999, 9999999999999, 'ip', 'ua')`, sessionID, csrf); err != nil {
		t.Fatal(err)
	}
	const configID = "sdr_oauthcfg_rotation"
	clientSecret, err := cipher.Seal([]byte("oauth-client-secret-canary"), cryptox.AAD("provider_oauth_configs", configID, "client_secret_enc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO provider_oauth_configs(public_id, provider_id,
		client_id, client_secret_enc, project_id, scopes_json, enabled, created_at_ms, updated_at_ms)
		VALUES(?, 'google', 'client-id', ?, 'project-test1',
		'{"schema_version":1,"values":["https://www.googleapis.com/auth/cloud-platform","https://www.googleapis.com/auth/generative-language.retriever"]}', 1, 1, 1)`, configID, clientSecret); err != nil {
		t.Fatal(err)
	}
	const attemptID = "sdr_oauth_rotation"
	pkce, err := cipher.Seal([]byte("pkce-canary"), cryptox.AAD("oauth_attempts", attemptID, "pkce_verifier_enc"))
	if err != nil {
		t.Fatal(err)
	}
	providerPayload, err := cipher.Seal([]byte("provider-payload-canary"), cryptox.AAD("oauth_attempts", attemptID, "provider_payload_enc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO oauth_attempts(public_id, admin_session_id,
		provider_id, account_id, flow_kind, state_verifier, pkce_verifier_enc, egress_fingerprint,
		status, provider_payload_enc, created_at_ms, expires_at_ms)
		VALUES(?, (SELECT id FROM admin_sessions WHERE public_id = ?), 'google',
		(SELECT id FROM accounts WHERE public_id = ?), 'authorization_code_pkce', 'state-verifier',
		?, 'egress-fingerprint', 'pending', ?, 1, 9999999999999)`, attemptID, sessionID, account.ID, pkce, providerPayload); err != nil {
		t.Fatal(err)
	}
	return []oauthCiphertext{
		{table: "provider_oauth_configs", publicID: configID, column: "client_secret_enc"},
		{table: "oauth_attempts", publicID: attemptID, column: "pkce_verifier_enc"},
		{table: "oauth_attempts", publicID: attemptID, column: "provider_payload_enc"},
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
