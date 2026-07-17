package accountauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/provider"
)

type Payload struct {
	SchemaVersion  int               `json:"schema_version"`
	APIKey         string            `json:"api_key,omitempty"`
	AccessToken    string            `json:"access_token,omitempty"`
	RefreshToken   string            `json:"refresh_token,omitempty"`
	TokenType      string            `json:"token_type,omitempty"`
	Scopes         []string          `json:"scopes,omitempty"`
	ExpiresAtMS    int64             `json:"expires_at_ms,omitempty"`
	ProviderFields map[string]string `json:"provider_fields,omitempty"`
}

func Encrypt(cipher *cryptox.Cipher, accountPublicID string, payload Payload) ([]byte, error) {
	if cipher == nil || accountPublicID == "" || payload.SchemaVersion != 1 {
		return nil, errors.New("credential payload is invalid")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return cipher.Seal(body, cryptox.AAD("accounts", accountPublicID, "credential_enc"))
}

func Decrypt(cipher *cryptox.Cipher, accountPublicID string, envelope []byte) (Payload, error) {
	if cipher == nil || accountPublicID == "" || len(envelope) == 0 {
		return Payload{}, errors.New("credential is not configured")
	}
	body, err := cipher.Open(envelope, cryptox.AAD("accounts", accountPublicID, "credential_enc"))
	if err != nil {
		return Payload{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var payload Payload
	if err := decoder.Decode(&payload); err != nil || decoder.Decode(&struct{}{}) != io.EOF || payload.SchemaVersion != 1 {
		return Payload{}, errors.New("credential payload is invalid")
	}
	return payload, nil
}

func ProviderCredential(authKind, providerID string, payload Payload) (provider.Credential, error) {
	switch authKind {
	case "api_key":
		return provider.NewAPIKey(payload.APIKey)
	case "oauth":
		if providerID != "google" {
			return provider.Credential{}, errors.New("OAuth credential is not supported for this provider")
		}
		return provider.NewOAuthToken(payload.AccessToken, payload.ProviderFields["project_id"])
	default:
		return provider.Credential{}, errors.New("account authentication kind is unsupported")
	}
}
