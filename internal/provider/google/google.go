package google

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/AlexKris/sidervia/internal/nativecodec"
	"github.com/AlexKris/sidervia/internal/provider"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: "google", Version: "0.2.0", VerifiedAgainst: "2026-07-17",
		AuthMethods: []string{"api_key", "oauth_beta"}, Capabilities: []string{"text", "stream"},
		Endpoints: []provider.Endpoint{provider.EndpointGenerateContent, provider.EndpointStreamGenerateContent},
	}
}

func (Adapter) Prepare(input provider.PrepareInput) (provider.NativeRequest, error) {
	stream := input.Endpoint == provider.EndpointStreamGenerateContent
	if input.Endpoint != provider.EndpointGenerateContent && !stream {
		return provider.NativeRequest{}, &nativecodec.Error{Code: "capability_not_supported", Message: "Google endpoint is not supported"}
	}
	if err := nativecodec.ValidateGeminiTextRequest(input.Body); err != nil {
		return provider.NativeRequest{}, err
	}
	body, _, err := nativecodec.RewriteRequest(input.Body, generateFields, "", false)
	if err != nil {
		return provider.NativeRequest{}, err
	}
	if input.UpstreamModel == "" || len(input.UpstreamModel) > 200 || strings.ContainsAny(input.UpstreamModel, "/\\%?#\r\n\x00") {
		return provider.NativeRequest{}, errors.New("configured Google model identifier is invalid")
	}
	action := ":generateContent"
	query := make(url.Values)
	if stream {
		action = ":streamGenerateContent"
		query.Set("alt", "sse")
	}
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	if stream {
		header.Set("Accept", "text/event-stream")
	}
	return provider.NativeRequest{
		Method: http.MethodPost, Path: "/v1beta/models/" + input.UpstreamModel + action,
		Query: query, Header: header, Body: body, Stream: stream,
	}, nil
}

func (Adapter) Probe() provider.NativeRequest {
	header := make(http.Header)
	header.Set("Accept", "application/json")
	return provider.NativeRequest{Method: http.MethodGet, Path: "/v1beta/models", Header: header}
}

func (Adapter) Authorize(request *http.Request, credential provider.Credential) error {
	switch credential.Kind() {
	case provider.CredentialAPIKey:
		return credential.SetHeader(request.Header, "X-Goog-Api-Key", "")
	case provider.CredentialOAuthToken:
		if err := credential.SetHeader(request.Header, "Authorization", "Bearer "); err != nil {
			return err
		}
		credential.SetGoogleProject(request.Header)
		return nil
	default:
		return errors.New("Google adapter credential kind is unsupported")
	}
}

var generateFields = fieldSet(
	"contents", "generationConfig", "safetySettings", "systemInstruction",
)

func fieldSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
