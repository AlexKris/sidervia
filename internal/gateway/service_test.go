package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AlexKris/sidervia/internal/clientauth"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/provider/openai"
	"github.com/AlexKris/sidervia/internal/routing"
	"github.com/AlexKris/sidervia/internal/usage"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type fakeRouter struct {
	candidate routing.Candidate
	request   routing.Request
	planErr   error
	successes int
	failures  []routing.Failure
}

func (r *fakeRouter) Plan(_ context.Context, request routing.Request) (*routing.Lease, routing.Explanation, error) {
	r.request = request
	if r.planErr != nil {
		return nil, routing.Explanation{Filtered: map[string]int{}}, r.planErr
	}
	return &routing.Lease{Candidate: r.candidate}, routing.Explanation{CandidateCount: 1, Filtered: map[string]int{}}, nil
}

func (r *fakeRouter) RecordSuccess(context.Context, int64) error {
	r.successes++
	return nil
}

func (r *fakeRouter) RecordFailure(_ context.Context, _ routing.Candidate, failure routing.Failure) error {
	r.failures = append(r.failures, failure)
	return nil
}

type fakeTransport struct {
	response *http.Response
	err      error
	native   provider.NativeRequest
	calls    int
}

func (t *fakeTransport) Do(_ context.Context, _ routing.Candidate, native provider.NativeRequest, _ provider.Adapter) (*http.Response, error) {
	t.calls++
	t.native = native
	return t.response, t.err
}

type fakeRecorder struct{ events []usage.Event }

func (r *fakeRecorder) Enqueue(_ context.Context, event usage.Event) error {
	r.events = append(r.events, event)
	return nil
}

type fakeCredentialManager struct {
	err      error
	forceErr error
}

func (m fakeCredentialManager) EnsureCredential(_ context.Context, candidate routing.Candidate, force bool) (routing.Candidate, error) {
	if force && m.forceErr != nil {
		return routing.Candidate{}, m.forceErr
	}
	if m.err != nil {
		return routing.Candidate{}, m.err
	}
	return candidate, nil
}

type codedCredentialError string

func (e codedCredentialError) Error() string       { return string(e) }
func (e codedCredentialError) GatewayCode() string { return string(e) }

func TestExecuteNativeJSONRewritesOnlyModelAndRecordsMetadata(t *testing.T) {
	service, router, transport, recorder := newTestService(t, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl_upstream","model":"upstream-model","unknown":{"kept":true},
			"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}
		}`)),
	})
	destination := httptest.NewRecorder()
	result := service.Execute(context.Background(), destination, Invocation{
		Identity: clientauth.Identity{InternalID: 7, PublicID: "sdr_ckey_test"}, RequestID: "sdr_req_test",
		Protocol: "openai", Endpoint: provider.EndpointChatCompletions,
		Body: []byte(`{"model":"public-model","messages":[{"role":"user","content":"secret prompt"}]}`),
	})
	if result != nil {
		t.Fatalf("Execute returned error: %v", result)
	}
	if destination.Code != http.StatusOK {
		t.Fatalf("status = %d", destination.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(destination.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["model"] != "public-model" || response["unknown"].(map[string]any)["kept"] != true {
		t.Fatalf("response was not safely rewritten: %#v", response)
	}
	var upstream map[string]any
	if err := json.Unmarshal(transport.native.Body, &upstream); err != nil {
		t.Fatal(err)
	}
	if upstream["model"] != "upstream-model" || router.request.PublicModelID != "public-model" {
		t.Fatalf("upstream model or route mismatch: %#v %#v", upstream, router.request)
	}
	if router.successes != 1 || len(router.failures) != 0 {
		t.Fatalf("runtime state: successes=%d failures=%v", router.successes, router.failures)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d", len(recorder.events))
	}
	event := recorder.events[0]
	if event.ClientKeyID != 7 || event.ProviderID != "openai" || event.RequestBytes == 0 || event.ResponseBytes == 0 {
		t.Fatalf("event metadata = %#v", event)
	}
	if strings.Contains(event.UsageJSON, "secret prompt") || !strings.Contains(event.UsageJSON, `"input_tokens":2`) {
		t.Fatalf("unsafe or missing usage metadata: %s", event.UsageJSON)
	}
}

func TestExecuteRejectsUnknownRequestFieldBeforeEgress(t *testing.T) {
	service, _, transport, recorder := newTestService(t, nil)
	result := service.Execute(context.Background(), httptest.NewRecorder(), Invocation{
		Identity: clientauth.Identity{InternalID: 7}, RequestID: "sdr_req_unknown", Protocol: "openai",
		Endpoint: provider.EndpointChatCompletions,
		Body:     []byte(`{"model":"public-model","messages":[],"unverified_field":"must not pass"}`),
	})
	if result == nil || result.StatusCode != http.StatusBadRequest || result.Code != "unknown_request_field" {
		t.Fatalf("result = %#v", result)
	}
	if transport.calls != 0 {
		t.Fatalf("transport calls = %d", transport.calls)
	}
	if len(recorder.events) != 1 || recorder.events[0].ErrorCode != "unknown_request_field" {
		t.Fatalf("events = %#v", recorder.events)
	}
}

func TestExecuteSSEPreservesUnknownEventsAndAggregatesUsage(t *testing.T) {
	stream := "event: message\n" +
		`data: {"model":"upstream-model","choices":[{"delta":{"content":"hi"}}],"unknown_event_field":9}` + "\n\n" +
		`data: {"usage":{"prompt_tokens":4,"completion_tokens":2}}` + "\n\n" +
		"data: [DONE]\n\n"
	service, router, _, recorder := newTestService(t, &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
		Body: io.NopCloser(strings.NewReader(stream)),
	})
	destination := httptest.NewRecorder()
	result := service.Execute(context.Background(), destination, Invocation{
		Identity: clientauth.Identity{InternalID: 9}, RequestID: "sdr_req_stream", Protocol: "openai",
		Endpoint: provider.EndpointChatCompletions,
		Body:     []byte(`{"model":"public-model","messages":[],"stream":true}`),
	})
	if result != nil {
		t.Fatalf("Execute returned error: %v", result)
	}
	body := destination.Body.String()
	if !strings.Contains(body, `"model":"public-model"`) || !strings.Contains(body, `"unknown_event_field":9`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream response = %s", body)
	}
	if router.successes != 1 || len(recorder.events) != 1 || !recorder.events[0].Streamed {
		t.Fatalf("runtime/event state: %#v %#v", router, recorder.events)
	}
	if !strings.Contains(recorder.events[0].UsageJSON, `"input_tokens":4`) {
		t.Fatalf("usage = %s", recorder.events[0].UsageJSON)
	}
}

func TestExecuteNeverReflectsUpstreamErrorBody(t *testing.T) {
	secret := "upstream-body-secret-canary"
	service, router, _, recorder := newTestService(t, &http.Response{
		StatusCode: http.StatusUnauthorized, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"error":"` + secret + `"}`)),
	})
	destination := httptest.NewRecorder()
	result := service.Execute(context.Background(), destination, Invocation{
		Identity: clientauth.Identity{InternalID: 7}, RequestID: "sdr_req_error", Protocol: "openai",
		Endpoint: provider.EndpointChatCompletions, Body: []byte(`{"model":"public-model","messages":[]}`),
	})
	if result == nil || result.Code != "reauth_required" || strings.Contains(result.Message, secret) {
		t.Fatalf("result = %#v", result)
	}
	if destination.Body.Len() != 0 || len(router.failures) != 1 || router.failures[0].StatusCode != http.StatusUnauthorized {
		t.Fatalf("destination/failures = %q %#v", destination.Body.String(), router.failures)
	}
	if len(recorder.events) != 1 || strings.Contains(recorder.events[0].UsageJSON, secret) {
		t.Fatalf("events = %#v", recorder.events)
	}
}

func TestExecuteNetworkFailureIsSanitized(t *testing.T) {
	service, router, transport, _ := newTestService(t, nil)
	transport.err = errors.New("dial proxy user:password@private.invalid")
	result := service.Execute(context.Background(), httptest.NewRecorder(), Invocation{
		Identity: clientauth.Identity{InternalID: 7}, RequestID: "sdr_req_network", Protocol: "openai",
		Endpoint: provider.EndpointChatCompletions, Body: []byte(`{"model":"public-model","messages":[]}`),
	})
	if result == nil || result.Code != "upstream_unavailable" || strings.Contains(result.Message, "password") {
		t.Fatalf("result = %#v", result)
	}
	if len(router.failures) != 1 || !router.failures[0].Network {
		t.Fatalf("failures = %#v", router.failures)
	}
}

func TestExecuteRecordsSelectedAccountWhenCredentialRefreshFails(t *testing.T) {
	service, router, transport, recorder := newTestService(t, nil)
	router.candidate.AuthKind = "oauth"
	service.credentials = fakeCredentialManager{err: codedCredentialError("oauth_refresh_unavailable")}

	result := service.Execute(context.Background(), httptest.NewRecorder(), Invocation{
		Identity: clientauth.Identity{InternalID: 7}, RequestID: "sdr_req_refresh", Protocol: "openai",
		Endpoint: provider.EndpointChatCompletions, Body: []byte(`{"model":"public-model","messages":[]}`),
	})
	if result == nil || result.Code != "upstream_unavailable" {
		t.Fatalf("result = %#v", result)
	}
	if transport.calls != 0 || len(recorder.events) != 1 {
		t.Fatalf("transport/events = %d/%#v", transport.calls, recorder.events)
	}
	event := recorder.events[0]
	if event.ProviderID != router.candidate.ProviderID || event.UpstreamID != router.candidate.UpstreamInternalID || event.AccountID != router.candidate.AccountInternalID {
		t.Fatalf("selected account metadata was lost: %#v", event)
	}
}

func TestOAuth401WithTransientRefreshFailureDoesNotRequireReauthorization(t *testing.T) {
	service, router, _, _ := newTestService(t, &http.Response{
		StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"expired"}`)),
	})
	router.candidate.AuthKind = "oauth"
	service.credentials = fakeCredentialManager{forceErr: codedCredentialError("oauth_refresh_unavailable")}

	result := service.Execute(context.Background(), httptest.NewRecorder(), Invocation{
		Identity: clientauth.Identity{InternalID: 7}, RequestID: "sdr_req_refresh_transient", Protocol: "openai",
		Endpoint: provider.EndpointChatCompletions, Body: []byte(`{"model":"public-model","messages":[]}`),
	})
	if result == nil || result.Code != "upstream_unavailable" {
		t.Fatalf("result = %#v", result)
	}
	if len(router.failures) != 1 || router.failures[0].StatusCode == http.StatusUnauthorized || !router.failures[0].Network {
		t.Fatalf("transient refresh failure was classified as reauthorization: %#v", router.failures)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("15", now); got != 15*time.Second {
		t.Fatalf("seconds retry-after = %s", got)
	}
	if got := parseRetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now); got != time.Minute {
		t.Fatalf("date retry-after = %s", got)
	}
	if got := parseRetryAfter("invalid", now); got != 0 {
		t.Fatalf("invalid retry-after = %s", got)
	}
}

func TestRouteErrorPreservesRetryAfter(t *testing.T) {
	result := routeError(&routing.RouteError{Code: "quota_limited", RetryAfter: 45 * time.Second})
	if result.StatusCode != http.StatusTooManyRequests || result.RetryAfter != 45*time.Second {
		t.Fatalf("gateway route error = %#v", result)
	}
}

func newTestService(t *testing.T, response *http.Response) (*Service, *fakeRouter, *fakeTransport, *fakeRecorder) {
	t.Helper()
	registry, err := provider.NewRegistry(openai.New())
	if err != nil {
		t.Fatal(err)
	}
	credential, err := provider.NewAPIKey("upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	router := &fakeRouter{candidate: routing.Candidate{
		AccountInternalID: 11, UpstreamInternalID: 13, ProviderID: "openai",
		UpstreamModelID: "upstream-model", Credential: credential,
	}}
	transport := &fakeTransport{response: response}
	recorder := &fakeRecorder{}
	service := New(Options{
		Router: router, Providers: registry, Transport: transport, Recorder: recorder,
		Clock: fixedClock{now: time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)},
	})
	return service, router, transport, recorder
}

func TestNoRequestOrResponseBodyFieldExistsInUsageEvent(t *testing.T) {
	typeOfEvent, err := json.Marshal(usage.Event{RequestID: "req"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(typeOfEvent, []byte("request_body")) || bytes.Contains(typeOfEvent, []byte("response_body")) {
		t.Fatalf("usage event unexpectedly serializes body fields: %s", typeOfEvent)
	}
}
