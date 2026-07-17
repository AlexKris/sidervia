package accountvalidate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/routing"
	"github.com/AlexKris/sidervia/internal/strictjson"
)

const maxProbeBody = 2 << 20

type Transport interface {
	Do(context.Context, routing.Candidate, provider.NativeRequest, provider.Adapter) (*http.Response, error)
}

type Service struct {
	control   *control.Service
	routing   *routing.Service
	providers *provider.Registry
	transport Transport
}

type ValidationError struct {
	Code string
}

func (e *ValidationError) Error() string { return e.Code }

func New(controlService *control.Service, routingService *routing.Service, providers *provider.Registry, transport Transport) *Service {
	return &Service{control: controlService, routing: routingService, providers: providers, transport: transport}
}

func (s *Service) Validate(ctx context.Context, actor control.Actor, publicID string, expectedVersion int64) (control.Account, error) {
	authKind, configured, err := s.control.AccountCredentialState(ctx, publicID)
	if err != nil {
		return control.Account{}, err
	}
	if authKind != "api_key" || !configured {
		return control.Account{}, &ValidationError{Code: "oauth_authorization_required"}
	}
	validating, err := s.control.BeginAccountValidation(ctx, actor, publicID, expectedVersion)
	if err != nil {
		return control.Account{}, err
	}
	fail := func(code string) (control.Account, error) {
		account, finishErr := s.control.FinishAccountValidation(ctx, actor, publicID, validating.Version, nil, code)
		if finishErr != nil {
			return control.Account{}, finishErr
		}
		return account, &ValidationError{Code: code}
	}
	config, err := s.routing.LoadAccount(ctx, publicID)
	if err != nil {
		return fail("configuration_invalid")
	}
	if !config.UpstreamEnabled || (config.HasProxy && !config.ProxyEnabled) {
		return fail("account_disabled")
	}
	adapter, ok := s.providers.Get(config.Candidate.ProviderID)
	if !ok {
		return fail("capability_not_supported")
	}
	response, err := s.transport.Do(ctx, config.Candidate, adapter.Probe(), adapter)
	if err != nil {
		return fail("upstream_unavailable")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProbeBody+1))
	if err != nil || len(body) > maxProbeBody {
		return fail("upstream_protocol_changed")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return fail("authentication_failed")
		}
		return fail("upstream_unavailable")
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || !strings.EqualFold(mediaType, "application/json") {
		return fail("upstream_protocol_changed")
	}
	models, err := ParseModels(config.Candidate.ProviderID, body)
	if err != nil {
		return fail("upstream_protocol_changed")
	}
	descriptor := adapter.Descriptor()
	capabilityVersion := fmt.Sprintf("%s/%s@%s", descriptor.ID, descriptor.Version, descriptor.VerifiedAgainst)
	return s.control.FinishAccountValidation(ctx, actor, publicID, validating.Version, &control.AccountValidation{
		Identity:          map[string]any{"provider_id": descriptor.ID, "model_count": len(models)},
		CapabilityVersion: capabilityVersion, Models: models,
	}, "")
}

func ParseModels(providerID string, body []byte) ([]string, error) {
	if err := strictjson.Validate(body, 32); err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return nil, errors.New("model response is invalid")
	}
	field := "data"
	if providerID == "google" {
		field = "models"
	}
	var values []map[string]json.RawMessage
	if json.Unmarshal(root[field], &values) != nil {
		return nil, errors.New("model list is invalid")
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		var id string
		field := "id"
		if providerID == "google" {
			field = "name"
		}
		if json.Unmarshal(value[field], &id) != nil {
			return nil, errors.New("model identifier is invalid")
		}
		if providerID == "google" {
			if !strings.HasPrefix(id, "models/") {
				return nil, errors.New("Google model identifier is invalid")
			}
			id = strings.TrimPrefix(id, "models/")
		}
		if id == "" || len(id) > 200 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\r\n\x00") {
			return nil, errors.New("model identifier is invalid")
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("model list is empty")
	}
	return result, nil
}

func IsValidationError(err error) (string, bool) {
	var validation *ValidationError
	if !errors.As(err, &validation) {
		return "", false
	}
	return validation.Code, true
}
