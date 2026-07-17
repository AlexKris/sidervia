package provider

import (
	"errors"
	"net/http"
	"net/url"
)

type Endpoint string

const (
	EndpointChatCompletions       Endpoint = "chat_completions"
	EndpointMessages              Endpoint = "messages"
	EndpointGenerateContent       Endpoint = "generate_content"
	EndpointStreamGenerateContent Endpoint = "stream_generate_content"
)

type Descriptor struct {
	ID              string
	Version         string
	VerifiedAgainst string
	AuthMethods     []string
	Capabilities    []string
	Endpoints       []Endpoint
}

type PrepareInput struct {
	Endpoint      Endpoint
	Body          []byte
	UpstreamModel string
	Headers       http.Header
}

type NativeRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
	Stream bool
}

type Adapter interface {
	Descriptor() Descriptor
	Prepare(PrepareInput) (NativeRequest, error)
	Probe() NativeRequest
	Authorize(*http.Request, Credential) error
}

type Registry struct {
	byID map[string]Adapter
}

func NewRegistry(adapters ...Adapter) (*Registry, error) {
	result := &Registry{byID: make(map[string]Adapter, len(adapters))}
	for _, adapter := range adapters {
		if adapter == nil || adapter.Descriptor().ID == "" {
			return nil, errors.New("provider adapter has no identifier")
		}
		if _, exists := result.byID[adapter.Descriptor().ID]; exists {
			return nil, errors.New("duplicate provider adapter")
		}
		result.byID[adapter.Descriptor().ID] = adapter
	}
	return result, nil
}

func (r *Registry) Get(id string) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	adapter, ok := r.byID[id]
	return adapter, ok
}

func (r *Registry) Descriptors() []Descriptor {
	if r == nil {
		return nil
	}
	result := make([]Descriptor, 0, len(r.byID))
	for _, adapter := range r.byID {
		result = append(result, adapter.Descriptor())
	}
	return result
}
