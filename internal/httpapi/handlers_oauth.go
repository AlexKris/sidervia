package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/AlexKris/sidervia/internal/oauth"
)

type googleOAuthConfigRequest struct {
	ClientID     string  `json:"client_id"`
	ClientSecret *string `json:"client_secret,omitempty"`
	ProjectID    string  `json:"project_id"`
	Enabled      bool    `json:"enabled"`
}

type oauthAttemptRequest struct {
	AccountID string `json:"account_id"`
}

type oauthCallbackRequest struct {
	CallbackURL string `json:"callback_url"`
}

func (s *Server) registerOAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/v1/oauth/providers/google", s.handleGetGoogleOAuthConfig)
	mux.HandleFunc("POST /api/admin/v1/oauth/providers/google", s.handleCreateGoogleOAuthConfig)
	mux.HandleFunc("PATCH /api/admin/v1/oauth/providers/google", s.handleUpdateGoogleOAuthConfig)
	mux.HandleFunc("POST /api/admin/v1/oauth-attempts", s.handleCreateOAuthAttempt)
	mux.HandleFunc("GET /api/admin/v1/oauth-attempts/{id}", s.handleGetOAuthAttempt)
	mux.HandleFunc("DELETE /api/admin/v1/oauth-attempts/{id}", s.handleCancelOAuthAttempt)
	mux.HandleFunc("POST /api/admin/v1/oauth-attempts/{id}/callback", s.handleManualOAuthCallback)
}

func (s *Server) handleGetGoogleOAuthConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireOAuth(w, r) {
		return
	}
	value, err := s.oauth.GetGoogleConfig(r.Context())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, value.Version)
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) handleCreateGoogleOAuthConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireOAuth(w, r) {
		return
	}
	var body googleOAuthConfigRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	value, err := s.oauth.CreateGoogleConfig(r.Context(), actor(r), body.configInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, value.Version)
	writeJSON(w, http.StatusCreated, value)
}

func (s *Server) handleUpdateGoogleOAuthConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireOAuth(w, r) {
		return
	}
	version, ok := s.requireVersion(w, r)
	if !ok {
		return
	}
	var body googleOAuthConfigRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	value, err := s.oauth.UpdateGoogleConfig(r.Context(), actor(r), version, body.configInput())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setETag(w, value.Version)
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) handleCreateOAuthAttempt(w http.ResponseWriter, r *http.Request) {
	if !s.requireOAuth(w, r) {
		return
	}
	var body oauthAttemptRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	session, _ := sessionFrom(r.Context())
	value, err := s.oauth.CreateAttempt(r.Context(), actor(r), session.PublicID, body.AccountID)
	if err != nil {
		s.writeOAuthError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/admin/v1/oauth-attempts/"+value.ID)
	writeJSON(w, http.StatusCreated, value)
}

func (s *Server) handleGetOAuthAttempt(w http.ResponseWriter, r *http.Request) {
	if !s.requireOAuth(w, r) {
		return
	}
	session, _ := sessionFrom(r.Context())
	value, err := s.oauth.GetAttempt(r.Context(), session.PublicID, r.PathValue("id"))
	if err != nil {
		s.writeOAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) handleCancelOAuthAttempt(w http.ResponseWriter, r *http.Request) {
	if !s.requireOAuth(w, r) {
		return
	}
	session, _ := sessionFrom(r.Context())
	if err := s.oauth.CancelAttempt(r.Context(), actor(r), session.PublicID, r.PathValue("id")); err != nil {
		s.writeOAuthError(w, r, err)
		return
	}
	writeNoContent(w)
}

func (s *Server) handleManualOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.requireOAuth(w, r) {
		return
	}
	var body oauthCallbackRequest
	if !s.decodeBody(w, r, &body) {
		return
	}
	session, _ := sessionFrom(r.Context())
	completion, err := s.oauth.CompleteAttemptCallbackURL(r.Context(), r.PathValue("id"), body.CallbackURL, session.PublicID, requestID(r.Context()))
	if err != nil {
		s.writeOAuthError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, completion)
}

func (s *Server) handleGoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		s.handleCapabilityUnavailable(w, r)
		return
	}
	completion, err := s.oauth.CompleteCallbackValues(r.Context(), r.URL.Query(), requestID(r.Context()))
	target := &url.URL{Path: "/accounts"}
	query := make(url.Values)
	if completion.AttemptID != "" {
		query.Set("oauth_attempt", completion.AttemptID)
	}
	if err != nil {
		query.Set("oauth_status", "failed")
	} else {
		query.Set("oauth_status", "success")
	}
	target.RawQuery = query.Encode()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func (s *Server) requireOAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.oauth != nil {
		return true
	}
	writeError(w, r, http.StatusNotImplemented, "capability_not_supported", "OAuth is not available", nil)
	return false
}

func (s *Server) writeOAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var oauthError *oauth.Error
	if errors.As(err, &oauthError) {
		status := http.StatusBadRequest
		switch oauthError.Code {
		case "admin_session_expired", "oauth_session_mismatch":
			status = http.StatusUnauthorized
		case "oauth_token_unavailable", "oauth_account_validation_failed":
			status = http.StatusBadGateway
		case "oauth_not_configured", "oauth_config_invalid":
			status = http.StatusServiceUnavailable
		}
		writeError(w, r, status, oauthError.Code, "OAuth operation failed", nil)
		return
	}
	s.writeServiceError(w, r, err)
}

func (body googleOAuthConfigRequest) configInput() oauth.GoogleConfigInput {
	return oauth.GoogleConfigInput{
		ClientID: body.ClientID, ClientSecret: body.ClientSecret, ProjectID: body.ProjectID, Enabled: body.Enabled,
	}
}
