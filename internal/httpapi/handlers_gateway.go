package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/clientauth"
	"github.com/AlexKris/sidervia/internal/gateway"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/usage"
)

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleGatewayCall(w, r, "openai", provider.EndpointChatCompletions, "", false)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	s.handleGatewayCall(w, r, "anthropic", provider.EndpointMessages, "", false)
}

func (s *Server) handleGeminiGenerate(w http.ResponseWriter, r *http.Request) {
	operation := r.PathValue("operation")
	var endpoint provider.Endpoint
	var model string
	switch {
	case strings.HasSuffix(operation, ":generateContent"):
		endpoint = provider.EndpointGenerateContent
		model = strings.TrimSuffix(operation, ":generateContent")
	case strings.HasSuffix(operation, ":streamGenerateContent"):
		endpoint = provider.EndpointStreamGenerateContent
		model = strings.TrimSuffix(operation, ":streamGenerateContent")
	default:
		writePublicProtocolError(w, r, "gemini", &gateway.Error{
			StatusCode: http.StatusNotFound, Code: "capability_not_supported", Message: "Gemini operation is not supported",
		})
		return
	}
	if !validPathModel(model) {
		writePublicProtocolError(w, r, "gemini", &gateway.Error{
			StatusCode: http.StatusBadRequest, Code: "invalid_request", Message: "model path is invalid",
		})
		return
	}
	s.handleGatewayCall(w, r, "gemini", endpoint, model, endpoint == provider.EndpointStreamGenerateContent)
}

func (s *Server) handleGatewayCall(w http.ResponseWriter, r *http.Request, protocol string, endpoint provider.Endpoint, model string, forceStream bool) {
	started := time.Now().UTC()
	if s.gateway == nil || s.clientAuth == nil {
		s.handleCapabilityUnavailable(w, r)
		return
	}
	rawKey, authError := extractClientKey(r, protocol)
	if authError != nil {
		writePublicProtocolError(w, r, protocol, &gateway.Error{
			StatusCode: http.StatusUnauthorized, Code: "authentication_failed", Message: "Client Key authentication failed",
		})
		return
	}
	identity, err := s.clientAuth.Authenticate(r.Context(), rawKey)
	if err != nil {
		if !errors.Is(err, clientauth.ErrUnauthorized) {
			s.loggerOrDefault().Error("Client Key authentication failed internally", "component", "clientauth", "event", "clientauth.failed", "request_id", requestID(r.Context()))
		}
		writePublicProtocolError(w, r, protocol, &gateway.Error{
			StatusCode: http.StatusUnauthorized, Code: "authentication_failed", Message: "Client Key authentication failed",
		})
		return
	}
	body, readError := readGatewayBody(w, r)
	if readError != nil {
		writePublicProtocolError(w, r, protocol, readError)
		s.recordPublicRequest(r, identity, protocol, string(endpoint), model, started, max(r.ContentLength, 0), 0, readError.StatusCode, readError.Code)
		return
	}
	headers := make(http.Header)
	if protocol == "anthropic" {
		headers.Set("Anthropic-Version", r.Header.Get("Anthropic-Version"))
		headers.Set("Anthropic-Beta", r.Header.Get("Anthropic-Beta"))
	}
	result := s.gateway.Execute(r.Context(), w, gateway.Invocation{
		Identity: identity, RequestID: requestID(r.Context()), Protocol: protocol, Endpoint: endpoint,
		PublicModelID: model, ForceStream: forceStream, Headers: headers, Body: body,
	})
	if result == nil || result.Committed {
		return
	}
	writePublicProtocolError(w, r, protocol, result)
}

func (s *Server) handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	protocol := "openai"
	if len(r.Header.Values("X-Api-Key")) > 0 {
		protocol = "anthropic"
	}
	s.handleModels(w, r, protocol)
}

func (s *Server) handleGeminiModels(w http.ResponseWriter, r *http.Request) {
	s.handleModels(w, r, "gemini")
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request, protocol string) {
	started := time.Now().UTC()
	if s.routing == nil || s.clientAuth == nil {
		s.handleCapabilityUnavailable(w, r)
		return
	}
	rawKey, authError := extractClientKey(r, protocol)
	if authError != nil {
		writePublicProtocolError(w, r, protocol, &gateway.Error{
			StatusCode: http.StatusUnauthorized, Code: "authentication_failed", Message: "Client Key authentication failed",
		})
		return
	}
	identity, err := s.clientAuth.Authenticate(r.Context(), rawKey)
	if err != nil {
		writePublicProtocolError(w, r, protocol, &gateway.Error{
			StatusCode: http.StatusUnauthorized, Code: "authentication_failed", Message: "Client Key authentication failed",
		})
		return
	}
	models, err := s.routing.ListModels(r.Context(), protocol)
	if err != nil {
		s.loggerOrDefault().Error("model listing failed", "component", "routing", "event", "models.list_failed", "request_id", requestID(r.Context()))
		writePublicProtocolError(w, r, protocol, &gateway.Error{
			StatusCode: http.StatusInternalServerError, Code: "internal_error", Message: "internal server error",
		})
		s.recordPublicRequest(r, identity, protocol, "models", "", started, 0, 0, http.StatusInternalServerError, "internal_error")
		return
	}
	var payload any
	switch protocol {
	case "anthropic":
		items := make([]map[string]any, 0, len(models))
		for _, model := range models {
			items = append(items, map[string]any{"id": model, "type": "model", "display_name": model})
		}
		payload = map[string]any{"data": items, "has_more": false}
	case "gemini":
		items := make([]map[string]any, 0, len(models))
		for _, model := range models {
			items = append(items, map[string]any{"name": "models/" + model, "displayName": model})
		}
		payload = map[string]any{"models": items}
	default:
		items := make([]map[string]any, 0, len(models))
		for _, model := range models {
			items = append(items, map[string]any{"id": model, "object": "model", "created": 0, "owned_by": "sidervia"})
		}
		payload = map[string]any{"object": "list", "data": items}
	}
	responseBytes := writeMeasuredJSON(w, http.StatusOK, payload)
	s.recordPublicRequest(r, identity, protocol, "models", "", started, 0, responseBytes, http.StatusOK, "")
}

func writeMeasuredJSON(w http.ResponseWriter, status int, value any) int64 {
	body, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	written, _ := w.Write(body)
	return int64(written)
}

func (s *Server) recordPublicRequest(r *http.Request, identity clientauth.Identity, protocol, endpoint, model string, started time.Time, requestBytes, responseBytes int64, status int, errorCode string) {
	if s.usageRecorder == nil || identity.InternalID <= 0 {
		return
	}
	completed := time.Now().UTC()
	event := usage.Event{
		RequestID: requestID(r.Context()), ClientKeyID: identity.InternalID, Protocol: protocol,
		EndpointKind: endpoint, PublicModelID: model, StatusCode: status, ErrorCode: errorCode,
		StartedAtMS: started.UnixMilli(), CompletedAtMS: completed.UnixMilli(),
		RequestBytes: requestBytes, ResponseBytes: responseBytes,
		UsageJSON: `{"schema_version":1}`, RoutingJSON: `{"schema_version":1}`,
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
	defer cancel()
	if err := s.usageRecorder.Enqueue(ctx, event); err != nil {
		s.loggerOrDefault().Error("request metadata write failed", "component", "usage", "event", "request.record_failed", "request_id", event.RequestID)
	}
}

func extractClientKey(r *http.Request, protocol string) (string, error) {
	if r == nil {
		return "", errors.New("request is required")
	}
	switch protocol {
	case "anthropic":
		apiKeys := r.Header.Values("X-Api-Key")
		authorizations := r.Header.Values("Authorization")
		if len(apiKeys) > 0 && len(authorizations) > 0 {
			return "", errors.New("multiple authentication methods")
		}
		if len(apiKeys) == 1 && validSingleHeader(apiKeys[0]) {
			return apiKeys[0], nil
		}
		if len(apiKeys) != 0 {
			return "", errors.New("invalid x-api-key")
		}
		return bearerKey(authorizations)
	case "gemini":
		if _, present := r.URL.Query()["key"]; present {
			return "", errors.New("query Client Keys are disabled")
		}
		values := r.Header.Values("X-Goog-Api-Key")
		if len(values) != 1 || !validSingleHeader(values[0]) {
			return "", errors.New("invalid x-goog-api-key")
		}
		return values[0], nil
	default:
		return bearerKey(r.Header.Values("Authorization"))
	}
}

func bearerKey(values []string) (string, error) {
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return "", errors.New("invalid authorization")
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !validSingleHeader(parts[1]) {
		return "", errors.New("invalid authorization")
	}
	return parts[1], nil
}

func validSingleHeader(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00,")
}

func readGatewayBody(w http.ResponseWriter, r *http.Request) ([]byte, *gateway.Error) {
	if encoding := strings.TrimSpace(r.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil, &gateway.Error{StatusCode: http.StatusUnsupportedMediaType, Code: "invalid_request", Message: "compressed request bodies are not supported"}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return nil, &gateway.Error{StatusCode: http.StatusUnsupportedMediaType, Code: "invalid_request", Message: "content type must be application/json"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, gateway.MaxRequestBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, &gateway.Error{StatusCode: http.StatusRequestEntityTooLarge, Code: "request_too_large", Message: "request body exceeds the size limit"}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, &gateway.Error{StatusCode: http.StatusBadRequest, Code: "invalid_request", Message: "request body is required"}
	}
	return body, nil
}

func writePublicProtocolError(w http.ResponseWriter, r *http.Request, protocol string, value *gateway.Error) {
	if value == nil {
		value = &gateway.Error{StatusCode: http.StatusInternalServerError, Code: "internal_error", Message: "internal server error"}
	}
	if value.StatusCode < 400 || value.StatusCode > 599 {
		value.StatusCode = http.StatusInternalServerError
	}
	if value.RetryAfter > 0 {
		seconds := int64((value.RetryAfter + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	}
	w.Header().Set("Cache-Control", "no-store")
	switch protocol {
	case "anthropic":
		category := "api_error"
		switch value.StatusCode {
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			category = "invalid_request_error"
		case http.StatusUnauthorized:
			category = "authentication_error"
		case http.StatusNotFound:
			category = "not_found_error"
		case http.StatusTooManyRequests:
			category = "rate_limit_error"
		}
		writeJSON(w, value.StatusCode, map[string]any{
			"type": "error", "error": map[string]any{"type": category, "message": value.Message, "sidervia_code": value.Code},
			"request_id": requestID(r.Context()),
		})
	case "gemini":
		writeJSON(w, value.StatusCode, map[string]any{"error": map[string]any{
			"code": value.StatusCode, "message": value.Message, "status": googleErrorStatus(value.StatusCode),
			"details": []map[string]any{{"@type": "type.googleapis.com/sidervia.v1.ErrorInfo", "reason": value.Code, "requestId": requestID(r.Context())}},
		}})
	default:
		category := "server_error"
		if value.StatusCode < 500 {
			category = "invalid_request_error"
		}
		writeJSON(w, value.StatusCode, map[string]any{
			"error":      map[string]any{"message": value.Message, "type": category, "param": nil, "code": value.Code},
			"request_id": requestID(r.Context()),
		})
	}
}

func googleErrorStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusUnprocessableEntity:
		return "FAILED_PRECONDITION"
	default:
		return "UNAVAILABLE"
	}
}

func validPathModel(model string) bool {
	trimmed := strings.TrimSpace(model)
	return trimmed == model && model != "" && len(model) <= 200 && !strings.ContainsAny(model, "/\\\r\n\x00?#")
}
