package httpapi

import (
	"errors"
	"net/http"

	"github.com/AlexKris/sidervia/internal/auth"
	"github.com/AlexKris/sidervia/internal/control"
)

func (s *Server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var validation control.ValidationError
	switch {
	case errors.As(err, &validation):
		writeError(w, r, http.StatusBadRequest, "validation_failed", "request validation failed", map[string]any{"field": validation.Field, "message": validation.Message})
	case errors.Is(err, control.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "resource not found", nil)
	case errors.Is(err, control.ErrVersion):
		writeError(w, r, http.StatusConflict, "version_conflict", "resource version does not match", nil)
	case errors.Is(err, control.ErrResourceInUse):
		writeError(w, r, http.StatusConflict, "resource_in_use", "resource is still in use", nil)
	case errors.Is(err, control.ErrConflict):
		writeError(w, r, http.StatusConflict, "resource_conflict", "resource conflicts with an existing object", nil)
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "30")
		writeError(w, r, http.StatusTooManyRequests, "authentication_limited", "authentication temporarily limited", nil)
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrTOTPReplay):
		writeError(w, r, http.StatusUnauthorized, "authentication_failed", "authentication failed", nil)
	case errors.Is(err, auth.ErrTOTPNotPending):
		writeError(w, r, http.StatusConflict, "totp_not_pending", "TOTP setup is not pending", nil)
	case errors.Is(err, auth.ErrUnauthorized):
		writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
	default:
		s.loggerOrDefault().Error("request failed", "component", "http", "event", "request.failed", "request_id", requestID(r.Context()), "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}
