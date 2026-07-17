package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/usage"
)

func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	if s.usageReader == nil {
		s.handleCapabilityUnavailable(w, r)
		return
	}
	limit, cursor, err := pagination(r)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	page, err := s.usageReader.ListRequests(r.Context(), limit, cursor)
	if errors.Is(err, usage.ErrInvalidCursor) {
		err = control.ValidationError{Field: "cursor", Message: "is invalid"}
	}
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if s.usageReader == nil {
		s.handleCapabilityUnavailable(w, r)
		return
	}
	item, err := s.usageReader.GetRequest(r.Context(), r.PathValue("id"))
	if errors.Is(err, usage.ErrRecordNotFound) {
		err = control.ErrNotFound
	}
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	if s.usageReader == nil {
		s.handleCapabilityUnavailable(w, r)
		return
	}
	value, err := s.usageReader.Summary24Hours(r.Context(), time.Now())
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
