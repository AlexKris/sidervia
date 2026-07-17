package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AlexKris/sidervia/internal/accountvalidate"
	"github.com/AlexKris/sidervia/internal/control"
)

type proxyRequest struct {
	Name             string  `json:"name"`
	Scheme           string  `json:"scheme"`
	Host             string  `json:"host"`
	Port             int     `json:"port"`
	Username         *string `json:"username,omitempty"`
	Password         *string `json:"password,omitempty"`
	TLSServerName    string  `json:"tls_server_name,omitempty"`
	AllowInsecureTLS bool    `json:"allow_insecure_tls"`
	Enabled          bool    `json:"enabled"`
}

type upstreamRequest struct {
	ProviderID          string  `json:"provider_id"`
	Name                string  `json:"name"`
	BaseURL             string  `json:"base_url"`
	DefaultProxyID      *string `json:"default_proxy_id,omitempty"`
	AllowPrivateNetwork bool    `json:"allow_private_network"`
	Enabled             bool    `json:"enabled"`
}

type accountRequest struct {
	UpstreamID          string     `json:"upstream_id"`
	Name                string     `json:"name"`
	AuthKind            string     `json:"auth_kind,omitempty"`
	Credential          *string    `json:"credential,omitempty"`
	CredentialExpiresAt *time.Time `json:"credential_expires_at,omitempty"`
	ProxyID             *string    `json:"proxy_id,omitempty"`
	BillingKind         string     `json:"billing_kind"`
	Status              string     `json:"status"`
	Priority            *int       `json:"priority,omitempty"`
	MaxConcurrency      *int       `json:"max_concurrency,omitempty"`
}

type modelRouteRequest struct {
	PublicModelID             string                   `json:"public_model_id"`
	Description               string                   `json:"description"`
	Enabled                   bool                     `json:"enabled"`
	ConfirmMultipleCandidates bool                     `json:"confirm_multiple_candidates"`
	Candidates                []control.RouteCandidate `json:"candidates"`
}

type clientKeyCreateRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type clientKeyUpdateRequest struct {
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) registerProxyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/v1/proxies", s.handleListProxies)
	mux.HandleFunc("POST /api/admin/v1/proxies", s.handleCreateProxy)
	mux.HandleFunc("GET /api/admin/v1/proxies/{id}", s.handleGetProxy)
	mux.HandleFunc("PATCH /api/admin/v1/proxies/{id}", s.handleUpdateProxy)
	mux.HandleFunc("DELETE /api/admin/v1/proxies/{id}", s.handleDeleteProxy)
}

func (s *Server) registerUpstreamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/v1/upstreams", s.handleListUpstreams)
	mux.HandleFunc("POST /api/admin/v1/upstreams", s.handleCreateUpstream)
	mux.HandleFunc("GET /api/admin/v1/upstreams/{id}", s.handleGetUpstream)
	mux.HandleFunc("PATCH /api/admin/v1/upstreams/{id}", s.handleUpdateUpstream)
	mux.HandleFunc("DELETE /api/admin/v1/upstreams/{id}", s.handleDeleteUpstream)
}

func (s *Server) registerAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/v1/accounts", s.handleListAccounts)
	mux.HandleFunc("POST /api/admin/v1/accounts", s.handleCreateAccount)
	mux.HandleFunc("GET /api/admin/v1/accounts/{id}", s.handleGetAccount)
	mux.HandleFunc("PATCH /api/admin/v1/accounts/{id}", s.handleUpdateAccount)
	mux.HandleFunc("DELETE /api/admin/v1/accounts/{id}", s.handleDeleteAccount)
	mux.HandleFunc("POST /api/admin/v1/accounts/{id}/validate", s.handleValidateAccount)
}

func (s *Server) registerModelRouteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/v1/model-routes", s.handleListModelRoutes)
	mux.HandleFunc("POST /api/admin/v1/model-routes", s.handleCreateModelRoute)
	mux.HandleFunc("GET /api/admin/v1/model-routes/{id}", s.handleGetModelRoute)
	mux.HandleFunc("PATCH /api/admin/v1/model-routes/{id}", s.handleUpdateModelRoute)
	mux.HandleFunc("DELETE /api/admin/v1/model-routes/{id}", s.handleDeleteModelRoute)
}

func (s *Server) registerClientKeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/v1/client-keys", s.handleListClientKeys)
	mux.HandleFunc("POST /api/admin/v1/client-keys", s.handleCreateClientKey)
	mux.HandleFunc("GET /api/admin/v1/client-keys/{id}", s.handleGetClientKey)
	mux.HandleFunc("PATCH /api/admin/v1/client-keys/{id}", s.handleUpdateClientKey)
	mux.HandleFunc("DELETE /api/admin/v1/client-keys/{id}", s.handleRevokeClientKey)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() || s.store.Ready(r.Context()) != nil {
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "service is not ready", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleCapabilityUnavailable(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotImplemented, "capability_not_supported", "requested provider capability is not implemented in this release", nil)
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, "not_found", "API endpoint not found", nil)
}

func (s *Server) handleProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.control.Providers()})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	value, err := s.control.Dashboard(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	databaseReady := s.store.Ready(r.Context()) == nil
	writeJSON(w, http.StatusOK, map[string]any{
		"ready": s.ready.Load() && databaseReady, "database_ready": databaseReady, "build": s.build,
	})
}

func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	page, err := s.control.ListAuditEvents(r.Context(), limit, cursor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleListProxies(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	page, err := s.control.ListProxies(r.Context(), limit, cursor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleCreateProxy(w http.ResponseWriter, r *http.Request) {
	var body proxyRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.CreateProxy(r.Context(), actor(r), body.proxyInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/v1/proxies/"+item.ID)
	setETag(w, item.Version)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleGetProxy(w http.ResponseWriter, r *http.Request) {
	item, err := s.control.GetProxy(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleUpdateProxy(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	var body proxyRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.UpdateProxy(r.Context(), actor(r), r.PathValue("id"), version, body.proxyInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteProxy(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	if err := s.control.DeleteProxy(r.Context(), actor(r), r.PathValue("id"), version); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeNoContent(w)
}

func (s *Server) handleListUpstreams(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	page, err := s.control.ListUpstreams(r.Context(), limit, cursor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleCreateUpstream(w http.ResponseWriter, r *http.Request) {
	var body upstreamRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.CreateUpstream(r.Context(), actor(r), body.upstreamInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/v1/upstreams/"+item.ID)
	setETag(w, item.Version)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleGetUpstream(w http.ResponseWriter, r *http.Request) {
	item, err := s.control.GetUpstream(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleUpdateUpstream(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	var body upstreamRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.UpdateUpstream(r.Context(), actor(r), r.PathValue("id"), version, body.upstreamInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteUpstream(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	if err := s.control.DeleteUpstream(r.Context(), actor(r), r.PathValue("id"), version); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeNoContent(w)
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	page, err := s.control.ListAccounts(r.Context(), limit, cursor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var body accountRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.CreateAccount(r.Context(), actor(r), body.accountInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/v1/accounts/"+item.ID)
	setETag(w, item.Version)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	item, err := s.control.GetAccount(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	var body accountRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.UpdateAccount(r.Context(), actor(r), r.PathValue("id"), version, body.accountInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	if err := s.control.DeleteAccount(r.Context(), actor(r), r.PathValue("id"), version); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeNoContent(w)
}

func (s *Server) handleValidateAccount(w http.ResponseWriter, r *http.Request) {
	if s.accountValidate == nil {
		writeError(w, r, http.StatusNotImplemented, "capability_not_supported", "account validation is not available", nil)
		return
	}
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	item, err := s.accountValidate.Validate(r.Context(), actor(r), r.PathValue("id"), version)
	if err != nil {
		if code, validation := accountvalidate.IsValidationError(err); validation {
			status := http.StatusBadGateway
			if code == "oauth_authorization_required" || code == "account_disabled" {
				status = http.StatusUnprocessableEntity
			}
			writeError(w, r, status, "account_validation_failed", "account validation failed", map[string]any{"reason": code})
			return
		}
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleListModelRoutes(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	page, err := s.control.ListModelRoutes(r.Context(), limit, cursor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleCreateModelRoute(w http.ResponseWriter, r *http.Request) {
	var body modelRouteRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.CreateModelRoute(r.Context(), actor(r), body.modelRouteInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/v1/model-routes/"+item.ID)
	setETag(w, item.Version)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleGetModelRoute(w http.ResponseWriter, r *http.Request) {
	item, err := s.control.GetModelRoute(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleUpdateModelRoute(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	var body modelRouteRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.UpdateModelRoute(r.Context(), actor(r), r.PathValue("id"), version, body.modelRouteInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteModelRoute(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	if err := s.control.DeleteModelRoute(r.Context(), actor(r), r.PathValue("id"), version); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeNoContent(w)
}

func (s *Server) handleListClientKeys(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pagination(r)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	page, err := s.control.ListClientKeys(r.Context(), limit, cursor)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleCreateClientKey(w http.ResponseWriter, r *http.Request) {
	var body clientKeyCreateRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.CreateClientKey(r.Context(), actor(r), body.Name, body.ExpiresAt)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/v1/client-keys/"+item.ClientKey.ID)
	setETag(w, item.ClientKey.Version)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleGetClientKey(w http.ResponseWriter, r *http.Request) {
	item, err := s.control.GetClientKey(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleUpdateClientKey(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	var body clientKeyUpdateRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	item, err := s.control.UpdateClientKey(r.Context(), actor(r), r.PathValue("id"), version, body.Name, body.Status, body.ExpiresAt)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, item.Version)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleRevokeClientKey(w http.ResponseWriter, r *http.Request) {
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	if err := s.control.RevokeClientKey(r.Context(), actor(r), r.PathValue("id"), version); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeNoContent(w)
}

func (s *Server) decodeBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := decodeJSON(w, r, target); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return false
	}
	return true
}

func (s *Server) requireVersion(w http.ResponseWriter, r *http.Request) (int64, bool) {
	version, err := parseETag(r.Header.Get("If-Match"))
	if err != nil {
		s.writeServiceError(w, r, control.ErrVersion)
		return 0, false
	}
	return version, true
}

func pagination(r *http.Request) (int, string, error) {
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			return 0, "", control.ValidationError{Field: "limit", Message: "must be between 1 and 200"}
		}
		limit = value
	}
	return limit, r.URL.Query().Get("cursor"), nil
}

func parseETag(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if len(value) < 4 || !strings.HasPrefix(value, "\"v") || !strings.HasSuffix(value, "\"") {
		return 0, errors.New("invalid ETag")
	}
	version, err := strconv.ParseInt(value[2:len(value)-1], 10, 64)
	if err != nil || version < 1 {
		return 0, errors.New("invalid ETag")
	}
	return version, nil
}

func setETag(w http.ResponseWriter, version int64) {
	w.Header().Set("ETag", fmt.Sprintf("\"v%d\"", version))
}

func actor(r *http.Request) control.Actor {
	session, _ := sessionFrom(r.Context())
	return control.Actor{Kind: "admin", ID: session.PublicID, RequestID: requestID(r.Context())}
}

func writeNoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (body proxyRequest) proxyInput() control.ProxyInput {
	return control.ProxyInput{
		Name: body.Name, Scheme: body.Scheme, Host: body.Host, Port: body.Port, Username: body.Username,
		Password: body.Password, TLSServerName: body.TLSServerName, AllowInsecureTLS: body.AllowInsecureTLS, Enabled: body.Enabled,
	}
}

func (body upstreamRequest) upstreamInput() control.UpstreamInput {
	return control.UpstreamInput{
		ProviderID: body.ProviderID, Name: body.Name, BaseURL: body.BaseURL, DefaultProxyID: body.DefaultProxyID,
		AllowPrivateNetwork: body.AllowPrivateNetwork, Enabled: body.Enabled,
	}
}

func (body accountRequest) accountInput() control.AccountInput {
	return control.AccountInput{
		UpstreamID: body.UpstreamID, Name: body.Name, AuthKind: body.AuthKind, Credential: body.Credential,
		CredentialExpiresAt: body.CredentialExpiresAt, ProxyID: body.ProxyID, BillingKind: body.BillingKind,
		Status: body.Status, Priority: body.Priority, MaxConcurrency: body.MaxConcurrency,
	}
}

func (body modelRouteRequest) modelRouteInput() control.ModelRouteInput {
	return control.ModelRouteInput{
		PublicModelID: body.PublicModelID, Description: body.Description, Enabled: body.Enabled,
		ConfirmMultipleCandidates: body.ConfirmMultipleCandidates, Candidates: body.Candidates,
	}
}
