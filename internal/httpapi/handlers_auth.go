package httpapi

import (
	"net/http"
	"time"

	"github.com/AlexKris/sidervia/internal/auth"
)

type loginRequest struct {
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type totpSetupRequest struct {
	Password string `json:"password"`
}

type totpCodeRequest struct {
	Code string `json:"code"`
}

type totpDisableRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

type sessionResponse struct {
	CSRFToken         string    `json:"csrf_token"`
	CreatedAt         time.Time `json:"created_at"`
	IdleExpiresAt     time.Time `json:"idle_expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
	TOTPEnabled       bool      `json:"totp_enabled"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.isSameOrigin(r) {
		writeError(w, r, http.StatusForbidden, "origin_rejected", "request origin is not allowed", nil)
		return
	}
	var body loginRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	session, err := s.auth.Login(r.Context(), body.Password, body.TOTPCode, s.clientIP(r), r.UserAgent())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setSessionCookie(w, session.Token, session.AbsoluteExpiresAt, s.secureCookie)
	writeJSON(w, http.StatusOK, toSessionResponse(session))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	if err := s.auth.Logout(r.Context(), session); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	clearSessionCookie(w, s.secureCookie)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	session, _ := sessionFrom(r.Context())
	writeJSON(w, http.StatusOK, toSessionResponse(session))
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var body passwordRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := auth.ValidatePassword(body.NewPassword); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_failed", "request validation failed", map[string]any{"field": "new_password", "message": err.Error()})
		return
	}
	current, _ := sessionFrom(r.Context())
	rotated, err := s.auth.ChangePassword(r.Context(), current, body.CurrentPassword, body.NewPassword, s.clientIP(r), r.UserAgent())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setSessionCookie(w, rotated.Token, rotated.AbsoluteExpiresAt, s.secureCookie)
	writeJSON(w, http.StatusOK, toSessionResponse(rotated))
}

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	var body totpSetupRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	session, _ := sessionFrom(r.Context())
	setup, err := s.auth.SetupTOTP(r.Context(), session, body.Password)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, setup)
}

func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	var body totpCodeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	current, _ := sessionFrom(r.Context())
	rotated, err := s.auth.ConfirmTOTP(r.Context(), current, body.Code, s.clientIP(r), r.UserAgent())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setSessionCookie(w, rotated.Token, rotated.AbsoluteExpiresAt, s.secureCookie)
	writeJSON(w, http.StatusOK, toSessionResponse(rotated))
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	var body totpDisableRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	current, _ := sessionFrom(r.Context())
	rotated, err := s.auth.DisableTOTP(r.Context(), current, body.Password, body.Code, s.clientIP(r), r.UserAgent())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	setSessionCookie(w, rotated.Token, rotated.AbsoluteExpiresAt, s.secureCookie)
	writeJSON(w, http.StatusOK, toSessionResponse(rotated))
}

func toSessionResponse(session auth.Session) sessionResponse {
	return sessionResponse{
		CSRFToken: session.CSRFToken, CreatedAt: session.CreatedAt, IdleExpiresAt: session.IdleExpiresAt,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt, TOTPEnabled: session.TOTPEnabled,
	}
}
