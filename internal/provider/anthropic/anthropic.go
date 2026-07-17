package anthropic

import (
	"errors"
	"net/http"
	"strings"

	"github.com/AlexKris/sidervia/internal/nativecodec"
	"github.com/AlexKris/sidervia/internal/provider"
)

const defaultVersion = "2023-06-01"

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: "anthropic", Version: "0.2.0", VerifiedAgainst: "2026-07-17",
		AuthMethods: []string{"api_key"}, Capabilities: []string{"text", "stream"},
		Endpoints: []provider.Endpoint{provider.EndpointMessages},
	}
}

func (Adapter) Prepare(input provider.PrepareInput) (provider.NativeRequest, error) {
	if input.Endpoint != provider.EndpointMessages {
		return provider.NativeRequest{}, &nativecodec.Error{Code: "capability_not_supported", Message: "Anthropic endpoint is not supported"}
	}
	if err := nativecodec.ValidateAnthropicTextRequest(input.Body); err != nil {
		return provider.NativeRequest{}, err
	}
	body, info, err := nativecodec.RewriteRequest(input.Body, messageFields, input.UpstreamModel, true)
	if err != nil {
		return provider.NativeRequest{}, err
	}
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	if info.Stream {
		header.Set("Accept", "text/event-stream")
	}
	version := strings.TrimSpace(input.Headers.Get("Anthropic-Version"))
	if version == "" {
		version = defaultVersion
	}
	if version != defaultVersion {
		return provider.NativeRequest{}, &nativecodec.Error{
			Code: "capability_not_supported", Message: "anthropic-version is not verified for the v0.2 text endpoint",
		}
	}
	header.Set("Anthropic-Version", version)
	if beta := strings.TrimSpace(input.Headers.Get("Anthropic-Beta")); beta != "" {
		return provider.NativeRequest{}, &nativecodec.Error{
			Code: "capability_not_supported", Message: "anthropic-beta is not verified for the v0.2 text endpoint",
		}
	}
	return provider.NativeRequest{Method: http.MethodPost, Path: "/v1/messages", Header: header, Body: body, Stream: info.Stream}, nil
}

func (Adapter) Probe() provider.NativeRequest {
	header := make(http.Header)
	header.Set("Accept", "application/json")
	header.Set("Anthropic-Version", defaultVersion)
	return provider.NativeRequest{Method: http.MethodGet, Path: "/v1/models", Header: header}
}

func (Adapter) Authorize(request *http.Request, credential provider.Credential) error {
	if credential.Kind() != provider.CredentialAPIKey {
		return errors.New("Anthropic adapter requires an API key")
	}
	return credential.SetHeader(request.Header, "X-Api-Key", "")
}

var messageFields = fieldSet(
	"max_tokens", "messages", "model", "stop_sequences", "stream", "system", "temperature", "top_k", "top_p",
)

func fieldSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
