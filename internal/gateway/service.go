package gateway

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/clientauth"
	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/nativecodec"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/routing"
	"github.com/AlexKris/sidervia/internal/usage"
)

const (
	MaxRequestBody  = 8 << 20
	maxResponseBody = 32 << 20
	maxErrorBody    = 2 << 20
)

type Router interface {
	Plan(context.Context, routing.Request) (*routing.Lease, routing.Explanation, error)
	RecordSuccess(context.Context, int64) error
	RecordFailure(context.Context, routing.Candidate, routing.Failure) error
}

type Transport interface {
	Do(context.Context, routing.Candidate, provider.NativeRequest, provider.Adapter) (*http.Response, error)
}

type Recorder interface {
	Enqueue(context.Context, usage.Event) error
}

type CredentialManager interface {
	EnsureCredential(context.Context, routing.Candidate, bool) (routing.Candidate, error)
}

type Options struct {
	Router      Router
	Providers   *provider.Registry
	Transport   Transport
	Recorder    Recorder
	Credentials CredentialManager
	Clock       clock.Clock
	Logger      *slog.Logger
}

type Service struct {
	router      Router
	providers   *provider.Registry
	transport   Transport
	recorder    Recorder
	credentials CredentialManager
	clock       clock.Clock
	logger      *slog.Logger
}

type Invocation struct {
	Identity      clientauth.Identity
	RequestID     string
	Protocol      string
	Endpoint      provider.Endpoint
	PublicModelID string
	ForceStream   bool
	Headers       http.Header
	Body          []byte
}

type Error struct {
	StatusCode int
	Code       string
	Message    string
	RetryAfter time.Duration
	Committed  bool
}

func (e *Error) Error() string { return e.Code }

func New(options Options) *Service {
	c := options.Clock
	if c == nil {
		c = clock.Real{}
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		router: options.Router, providers: options.Providers, transport: options.Transport,
		recorder: options.Recorder, credentials: options.Credentials, clock: c, logger: logger,
	}
}

func (s *Service) Execute(ctx context.Context, destination http.ResponseWriter, invocation Invocation) (result *Error) {
	if invocation.Identity.InternalID <= 0 || invocation.RequestID == "" || invocation.Protocol == "" || invocation.Endpoint == "" {
		return internalError()
	}
	started := s.clock.Now().UTC()
	event := usage.Event{
		RequestID: invocation.RequestID, ClientKeyID: invocation.Identity.InternalID,
		Protocol: invocation.Protocol, EndpointKind: string(invocation.Endpoint),
		PublicModelID: invocation.PublicModelID, StartedAtMS: started.UnixMilli(),
		RequestBytes: int64(len(invocation.Body)), UsageJSON: `{"schema_version":1}`,
		RoutingJSON: `{"schema_version":1}`,
	}
	defer func() {
		event.CompletedAtMS = s.clock.Now().UTC().UnixMilli()
		if result != nil {
			event.ErrorCode = result.Code
			if event.StatusCode == 0 {
				event.StatusCode = result.StatusCode
			}
		}
		if event.StatusCode == 0 {
			event.StatusCode = http.StatusOK
		}
		s.record(ctx, event)
	}()

	if s.router == nil || s.providers == nil || s.transport == nil || s.recorder == nil {
		return internalError()
	}
	info, err := nativecodec.InspectRequest(invocation.Body, invocation.PublicModelID == "")
	if err != nil {
		return codecError(err)
	}
	if invocation.PublicModelID == "" {
		invocation.PublicModelID = info.Model
		event.PublicModelID = info.Model
	}
	streamed := invocation.ForceStream || info.Stream
	event.Streamed = streamed
	required := []string{"text"}
	if streamed {
		required = append(required, "stream")
	}
	lease, explanation, err := s.router.Plan(ctx, routing.Request{
		PublicModelID: invocation.PublicModelID, Protocol: invocation.Protocol, RequiredCapabilities: required,
	})
	event.RoutingJSON = explanation.JSON()
	if err != nil {
		return routeError(err)
	}
	defer lease.Release()
	candidate := lease.Candidate
	event.ProviderID = candidate.ProviderID
	event.UpstreamID = candidate.UpstreamInternalID
	event.AccountID = candidate.AccountInternalID
	if s.credentials != nil {
		candidate, err = s.credentials.EnsureCredential(ctx, candidate, false)
		if err != nil {
			return credentialError(err)
		}
	}
	adapter, ok := s.providers.Get(candidate.ProviderID)
	if !ok {
		return publicError(http.StatusUnprocessableEntity, "capability_not_supported", "configured provider does not support this endpoint")
	}
	native, err := adapter.Prepare(provider.PrepareInput{
		Endpoint: invocation.Endpoint, Body: invocation.Body, UpstreamModel: candidate.UpstreamModelID,
		Headers: invocation.Headers,
	})
	if err != nil {
		return codecError(err)
	}
	if native.Stream != streamed {
		return publicError(http.StatusBadRequest, "invalid_request", "request stream mode does not match the endpoint")
	}

	response, err := s.transport.Do(ctx, candidate, native, adapter)
	if err != nil {
		s.recordFailure(ctx, candidate, routing.Failure{Code: "upstream_unavailable", Network: true})
		return publicError(http.StatusBadGateway, "upstream_unavailable", "upstream service is unavailable")
	}
	if response.StatusCode == http.StatusUnauthorized && candidate.AuthKind == "oauth" && s.credentials != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorBody))
		_ = response.Body.Close()
		selected := candidate
		refreshed, refreshErr := s.credentials.EnsureCredential(ctx, candidate, true)
		if refreshErr != nil {
			failure := routing.Failure{Code: "oauth_refresh_unavailable", Network: true}
			if credentialGatewayCode(refreshErr) == "reauth_required" {
				failure = routing.Failure{StatusCode: http.StatusUnauthorized, Code: "reauth_required"}
			}
			s.recordFailure(ctx, selected, failure)
			return credentialError(refreshErr)
		}
		candidate = refreshed
		response, err = s.transport.Do(ctx, candidate, native, adapter)
		if err != nil {
			s.recordFailure(ctx, candidate, routing.Failure{Code: "upstream_unavailable", Network: true})
			return publicError(http.StatusBadGateway, "upstream_unavailable", "upstream service is unavailable")
		}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return s.upstreamError(ctx, candidate, response)
	}
	if streamed {
		return s.stream(ctx, destination, response, candidate, invocation.PublicModelID, &event)
	}
	return s.nonStream(ctx, destination, response, candidate, invocation.PublicModelID, &event)
}

func (s *Service) nonStream(ctx context.Context, destination http.ResponseWriter, response *http.Response, candidate routing.Candidate, publicModel string, event *usage.Event) *Error {
	if !hasMediaType(response.Header, "application/json") {
		s.recordFailure(ctx, candidate, routing.Failure{Code: "upstream_protocol_changed"})
		return publicError(http.StatusBadGateway, "upstream_protocol_changed", "upstream returned an unexpected response type")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody+1))
	if err != nil || len(body) > maxResponseBody {
		s.recordFailure(ctx, candidate, routing.Failure{Code: "upstream_protocol_changed", Network: err != nil})
		return publicError(http.StatusBadGateway, "upstream_protocol_changed", "upstream response is incomplete or too large")
	}
	rewritten, err := nativecodec.RewriteProviderResponse(candidate.ProviderID, body, publicModel)
	if err != nil {
		s.recordFailure(ctx, candidate, routing.Failure{Code: "upstream_protocol_changed"})
		return publicError(http.StatusBadGateway, "upstream_protocol_changed", "upstream response does not match the verified protocol")
	}
	values := usage.Extract(candidate.ProviderID, body)
	event.UsageJSON = values.JSON()
	destination.Header().Set("Content-Type", "application/json; charset=utf-8")
	destination.Header().Set("Cache-Control", "no-store")
	destination.WriteHeader(response.StatusCode)
	tracked := &firstByteWriter{destination: destination, clock: s.clock, firstByteAtMS: &event.FirstByteAtMS}
	written, writeErr := tracked.Write(rewritten)
	event.StatusCode = response.StatusCode
	event.ResponseBytes = int64(written)
	s.recordSuccess(ctx, candidate.AccountInternalID)
	if writeErr != nil {
		return &Error{StatusCode: response.StatusCode, Code: "downstream_disconnected", Message: "downstream connection closed", Committed: true}
	}
	return nil
}

func (s *Service) stream(ctx context.Context, destination http.ResponseWriter, response *http.Response, candidate routing.Candidate, publicModel string, event *usage.Event) *Error {
	if !hasMediaType(response.Header, "text/event-stream") {
		s.recordFailure(ctx, candidate, routing.Failure{Code: "upstream_protocol_changed"})
		return publicError(http.StatusBadGateway, "upstream_protocol_changed", "upstream did not return an SSE stream")
	}
	destination.Header().Set("Content-Type", "text/event-stream")
	destination.Header().Set("Cache-Control", "no-store")
	destination.Header().Set("X-Accel-Buffering", "no")
	destination.WriteHeader(response.StatusCode)
	tracked := &firstByteWriter{destination: destination, clock: s.clock, firstByteAtMS: &event.FirstByteAtMS}
	values := usage.Values{}
	written, err := nativecodec.CopySSE(tracked, response.Body, nativecodec.SSEOptions{
		RewriteData: func(data []byte) ([]byte, error) {
			return nativecodec.RewriteProviderResponse(candidate.ProviderID, data, publicModel)
		},
		ObserveData: func(data []byte) { values.Merge(usage.Extract(candidate.ProviderID, data)) },
		Flush:       func() error { return http.NewResponseController(destination).Flush() },
	})
	event.StatusCode = response.StatusCode
	event.ResponseBytes = written
	event.UsageJSON = values.JSON()
	if err == nil {
		s.recordSuccess(ctx, candidate.AccountInternalID)
		return nil
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return &Error{StatusCode: response.StatusCode, Code: "downstream_disconnected", Message: "downstream connection closed", Committed: true}
	}
	s.recordFailure(ctx, candidate, routing.Failure{Code: "upstream_protocol_changed", Network: true})
	return &Error{StatusCode: response.StatusCode, Code: "upstream_protocol_changed", Message: "upstream stream ended unexpectedly", Committed: true}
}

func (s *Service) upstreamError(ctx context.Context, candidate routing.Candidate, response *http.Response) *Error {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorBody))
	retryAfter := parseRetryAfter(response.Header.Get("Retry-After"), s.clock.Now())
	failure := routing.Failure{StatusCode: response.StatusCode, Code: "upstream_unavailable", RetryAfter: retryAfter}
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		failure.StatusCode = http.StatusUnauthorized
		failure.Code = "reauth_required"
		s.recordFailure(ctx, candidate, failure)
		return publicError(http.StatusServiceUnavailable, "reauth_required", "configured upstream account requires authentication")
	case http.StatusTooManyRequests:
		failure.Code = "quota_limited"
		s.recordFailure(ctx, candidate, failure)
		result := publicError(http.StatusTooManyRequests, "quota_limited", "upstream quota is temporarily limited")
		result.RetryAfter = retryAfter
		return result
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		failure.Network = true
		s.recordFailure(ctx, candidate, failure)
		return publicError(http.StatusBadGateway, "upstream_unavailable", "upstream service is unavailable")
	default:
		if response.StatusCode >= 500 {
			s.recordFailure(ctx, candidate, failure)
			return publicError(http.StatusBadGateway, "upstream_unavailable", "upstream service is unavailable")
		}
		return publicError(http.StatusBadRequest, "invalid_request", "upstream rejected the request")
	}
}

func (s *Service) record(ctx context.Context, event usage.Event) {
	background, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.recorder.Enqueue(background, event); err != nil {
		s.logger.Error("request metadata write failed", "component", "usage", "event", "request.record_failed", "request_id", event.RequestID)
	}
}

func (s *Service) recordSuccess(ctx context.Context, accountID int64) {
	background, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.router.RecordSuccess(background, accountID); err != nil {
		s.logger.Error("account success state write failed", "component", "routing", "event", "account.success_write_failed")
	}
}

func (s *Service) recordFailure(ctx context.Context, candidate routing.Candidate, failure routing.Failure) {
	background, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.router.RecordFailure(background, candidate, failure); err != nil {
		s.logger.Error("account failure state write failed", "component", "routing", "event", "account.failure_write_failed", "error_code", failure.Code)
	}
}

type firstByteWriter struct {
	destination   io.Writer
	clock         clock.Clock
	firstByteAtMS **int64
	written       bool
}

func (w *firstByteWriter) Write(body []byte) (int, error) {
	n, err := w.destination.Write(body)
	if n > 0 && !w.written {
		w.written = true
		now := w.clock.Now().UTC().UnixMilli()
		*w.firstByteAtMS = &now
	}
	return n, err
}

func routeError(err error) *Error {
	var route *routing.RouteError
	if !errors.As(err, &route) {
		return internalError()
	}
	status := http.StatusServiceUnavailable
	message := "no upstream account is currently available"
	switch route.Code {
	case "model_not_configured":
		status, message = http.StatusNotFound, "model route is not configured"
	case "capability_not_supported":
		status, message = http.StatusUnprocessableEntity, "model route does not support this capability"
	case "quota_limited":
		status, message = http.StatusTooManyRequests, "all eligible upstream accounts are quota limited"
	case "all_accounts_saturated":
		status, message = http.StatusTooManyRequests, "all eligible upstream accounts are busy"
	case "reauth_required":
		message = "all eligible upstream accounts require authentication"
	case "account_cooldown":
		message = "all eligible upstream accounts are cooling down"
	case "account_disabled":
		message = "all eligible upstream accounts are disabled"
	case "upstream_unavailable":
	default:
		return internalError()
	}
	result := publicError(status, route.Code, message)
	result.RetryAfter = route.RetryAfter
	return result
}

func codecError(err error) *Error {
	var codec *nativecodec.Error
	if errors.As(err, &codec) {
		status := http.StatusBadRequest
		if codec.Code == "capability_not_supported" {
			status = http.StatusUnprocessableEntity
		}
		return publicError(status, codec.Code, codec.Message)
	}
	return internalError()
}

func credentialError(err error) *Error {
	if err == nil {
		return nil
	}
	if code := credentialGatewayCode(err); code != "" {
		switch code {
		case "reauth_required":
			return publicError(http.StatusServiceUnavailable, "reauth_required", "configured upstream account requires authentication")
		case "capability_not_supported":
			return publicError(http.StatusUnprocessableEntity, "capability_not_supported", "account authentication is not supported")
		case "oauth_config_invalid":
			return publicError(http.StatusServiceUnavailable, "reauth_required", "OAuth provider configuration is unavailable")
		default:
			return publicError(http.StatusBadGateway, "upstream_unavailable", "OAuth credential refresh is temporarily unavailable")
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return publicError(http.StatusGatewayTimeout, "upstream_timeout", "OAuth credential refresh timed out")
	}
	return internalError()
}

func credentialGatewayCode(err error) string {
	var coded interface{ GatewayCode() string }
	if errors.As(err, &coded) {
		return coded.GatewayCode()
	}
	return ""
}

func publicError(status int, code, message string) *Error {
	return &Error{StatusCode: status, Code: code, Message: message}
}

func internalError() *Error {
	return publicError(http.StatusInternalServerError, "internal_error", "internal server error")
}

func hasMediaType(header http.Header, expected string) bool {
	value := header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, expected)
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(now) {
		return parsed.Sub(now)
	}
	return 0
}
