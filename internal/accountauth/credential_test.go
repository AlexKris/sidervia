package accountauth

import (
	"bytes"
	"testing"

	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/provider"
)

func TestCredentialRoundTripAndAAD(t *testing.T) {
	cipher, err := cryptox.NewCipher(bytes.Repeat([]byte{9}, cryptox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Encrypt(cipher, "sdr_acct_a", Payload{SchemaVersion: 1, APIKey: "canary-secret"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := Decrypt(cipher, "sdr_acct_a", envelope)
	if err != nil || payload.APIKey != "canary-secret" {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
	if _, err := Decrypt(cipher, "sdr_acct_b", envelope); err == nil {
		t.Fatal("credential envelope was not bound to account AAD")
	}
	credential, err := ProviderCredential("api_key", "openai", payload)
	if err != nil || credential.Kind() != provider.CredentialAPIKey {
		t.Fatalf("kind=%q err=%v", credential.Kind(), err)
	}
}
