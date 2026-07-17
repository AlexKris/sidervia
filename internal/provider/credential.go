package provider

import (
	"errors"
	"net/http"
	"strings"
)

type CredentialKind string

const (
	CredentialAPIKey     CredentialKind = "api_key"
	CredentialOAuthToken CredentialKind = "oauth_token"
)

type Credential struct {
	kind      CredentialKind
	secret    []byte
	projectID string
}

func NewAPIKey(value string) (Credential, error) {
	return newCredential(CredentialAPIKey, value, "")
}

func NewOAuthToken(value, projectID string) (Credential, error) {
	return newCredential(CredentialOAuthToken, value, projectID)
}

func newCredential(kind CredentialKind, value, projectID string) (Credential, error) {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n\x00") {
		return Credential{}, errors.New("credential is invalid")
	}
	return Credential{kind: kind, secret: append([]byte(nil), value...), projectID: strings.TrimSpace(projectID)}, nil
}

func (c Credential) Kind() CredentialKind { return c.kind }

func (c Credential) SetHeader(header http.Header, name, prefix string) error {
	if len(c.secret) == 0 {
		return errors.New("credential is empty")
	}
	header.Set(name, prefix+string(c.secret))
	return nil
}

func (c Credential) SetGoogleProject(header http.Header) {
	if c.projectID != "" {
		header.Set("X-Goog-User-Project", c.projectID)
	}
}
