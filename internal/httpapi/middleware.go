package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime/debug"
	"strings"
	"time"
)

const sessionCookieName = "sidervia_session"
const sessionCookiePath = "/api/admin/"

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (s *Server) baseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		var observe func(int, time.Duration)
		if s.metrics != nil {
			observe = s.metrics.StartRequest()
		}
		requestID, err := s.ids.Object("req")
		if err != nil {
			http.Error(w, "request identifier unavailable", http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		r = r.WithContext(ctx)
		setSecurityHeaders(w, s.secureCookie)
		w.Header().Set("X-Request-ID", requestID)
		writer := &statusWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.loggerOrDefault().Error("http request panic", "component", "http", "event", "panic", "request_id", requestID)
				s.loggerOrDefault().Debug("http request panic stack", "stack", string(debug.Stack()))
				if writer.status == 0 {
					writeError(writer, r, http.StatusInternalServerError, "internal_error", "internal server error", nil)
				}
			}
			status := writer.status
			if status == 0 {
				status = http.StatusOK
			}
			if observe != nil {
				observe(status, time.Since(started))
			}
			s.loggerOrDefault().Info("http request completed",
				"component", "http", "event", "request.completed", "request_id", requestID,
				"method", r.Method, "route", safeLogRoute(r.Pattern), "status", status,
				"duration_ms", time.Since(started).Milliseconds())
		}()
		next.ServeHTTP(writer, r)
	})
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
			return
		}
		session, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			clearSessionCookie(w, s.secureCookie)
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
			return
		}
		if isUnsafeMethod(r.Method) {
			if !s.isSameOrigin(r) {
				writeError(w, r, http.StatusForbidden, "origin_rejected", "request origin is not allowed", nil)
				return
			}
			if err := s.auth.ValidateCSRF(session, r.Header.Get("X-CSRF-Token")); err != nil {
				writeError(w, r, http.StatusForbidden, "csrf_rejected", "CSRF validation failed", nil)
				return
			}
		}
		ctx := context.WithValue(r.Context(), sessionKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) isSameOrigin(r *http.Request) bool {
	if s.publicURL == nil {
		return false
	}
	if raw := strings.TrimSpace(r.Header.Get("Origin")); raw != "" {
		origin, err := url.Parse(raw)
		return err == nil && sameOrigin(origin, s.publicURL)
	}
	if raw := strings.TrimSpace(r.Header.Get("Referer")); raw != "" {
		referer, err := url.Parse(raw)
		return err == nil && sameOrigin(referer, s.publicURL)
	}
	return false
}

func sameOrigin(candidate, expected *url.URL) bool {
	if candidate == nil || expected == nil || candidate.User != nil || expected.User != nil {
		return false
	}
	if !strings.EqualFold(candidate.Scheme, expected.Scheme) || candidate.Hostname() == "" || expected.Hostname() == "" {
		return false
	}
	return strings.EqualFold(candidate.Hostname(), expected.Hostname()) && originPort(candidate) == originPort(expected)
}

func originPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return "unknown"
	}
	remote = remote.Unmap()
	if !s.isTrustedProxy(remote) {
		return remote.String()
	}
	raw := r.Header.Get("X-Forwarded-For")
	if raw == "" {
		return remote.String()
	}
	parts := strings.Split(raw, ",")
	chain := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		addr, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			return remote.String()
		}
		chain = append(chain, addr.Unmap())
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !s.isTrustedProxy(chain[index]) {
			return chain[index].String()
		}
	}
	if len(chain) > 0 {
		return chain[0].String()
	}
	return remote.String()
}

func (s *Server) isTrustedProxy(address netip.Addr) bool {
	for _, prefix := range s.trustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (s *Server) loggerOrDefault() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

func setSessionCookie(w http.ResponseWriter, sessionToken string, expires time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: sessionToken, Path: sessionCookiePath, HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: sessionCookiePath, HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func setSecurityHeaders(w http.ResponseWriter, https bool) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; object-src 'none'; img-src 'self' data:; connect-src 'self'; script-src 'self'; style-src 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	if https {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func safeLogRoute(pattern string) string {
	if pattern == "" {
		return "unmatched"
	}
	if len(pattern) > 256 {
		return pattern[:256]
	}
	return pattern
}
