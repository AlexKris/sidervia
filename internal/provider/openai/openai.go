package openai

import (
	"errors"
	"net/http"

	"github.com/AlexKris/sidervia/internal/nativecodec"
	"github.com/AlexKris/sidervia/internal/provider"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func (Adapter) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID: "openai", Version: "0.2.0", VerifiedAgainst: "2026-07-17",
		AuthMethods: []string{"api_key"}, Capabilities: []string{"text", "stream"},
		Endpoints: []provider.Endpoint{provider.EndpointChatCompletions},
	}
}

func (Adapter) Prepare(input provider.PrepareInput) (provider.NativeRequest, error) {
	if input.Endpoint != provider.EndpointChatCompletions {
		return provider.NativeRequest{}, &nativecodec.Error{Code: "capability_not_supported", Message: "OpenAI endpoint is not supported"}
	}
	if err := nativecodec.ValidateOpenAIChatTextRequest(input.Body); err != nil {
		return provider.NativeRequest{}, err
	}
	body, info, err := nativecodec.RewriteRequest(input.Body, chatFields, input.UpstreamModel, true)
	if err != nil {
		return provider.NativeRequest{}, err
	}
	return provider.NativeRequest{
		Method: http.MethodPost, Path: "/v1/chat/completions", Header: jsonHeaders(info.Stream), Body: body, Stream: info.Stream,
	}, nil
}

func (Adapter) Probe() provider.NativeRequest {
	return provider.NativeRequest{Method: http.MethodGet, Path: "/v1/models", Header: jsonHeaders(false)}
}

func (Adapter) Authorize(request *http.Request, credential provider.Credential) error {
	if credential.Kind() != provider.CredentialAPIKey {
		return errors.New("OpenAI adapter requires an API key")
	}
	return credential.SetHeader(request.Header, "Authorization", "Bearer ")
}

func jsonHeaders(stream bool) http.Header {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	if stream {
		header.Set("Accept", "text/event-stream")
	} else {
		header.Set("Accept", "application/json")
	}
	return header
}

var chatFields = fieldSet(
	"frequency_penalty", "max_completion_tokens", "max_tokens", "messages", "model", "n",
	"presence_penalty", "seed", "stop", "stream", "temperature", "top_p",
)

func fieldSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
