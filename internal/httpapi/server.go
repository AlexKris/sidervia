package httpapi

import (
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"sync/atomic"

	"github.com/AlexKris/sidervia/internal/auth"
	"github.com/AlexKris/sidervia/internal/buildinfo"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/metrics"
	"github.com/AlexKris/sidervia/internal/store"
)

type Server struct {
	auth           *auth.Service
	control        *control.Service
	store          *store.Store
	logger         *slog.Logger
	ids            identifier.Generator
	publicURL      *url.URL
	trustedProxies []netip.Prefix
	secureCookie   bool
	ready          atomic.Bool
	assets         http.Handler
	build          buildinfo.Info
	metrics        *metrics.Registry
}

type Options struct {
	Auth           *auth.Service
	Control        *control.Service
	Store          *store.Store
	Logger         *slog.Logger
	IDs            identifier.Generator
	PublicURL      *url.URL
	TrustedProxies []netip.Prefix
	SecureCookie   bool
	Assets         http.Handler
	Build          buildinfo.Info
	Metrics        *metrics.Registry
}

func New(options Options) *Server {
	s := &Server{
		auth: options.Auth, control: options.Control, store: options.Store, logger: options.Logger,
		ids: options.IDs, publicURL: options.PublicURL, trustedProxies: options.TrustedProxies,
		secureCookie: options.SecureCookie, assets: options.Assets, build: options.Build, metrics: options.Metrics,
	}
	s.ready.Store(true)
	return s
}

func (s *Server) SetReady(value bool) { s.ready.Store(value) }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("POST /api/admin/v1/auth/login", s.handleLogin)

	authenticated := http.NewServeMux()
	authenticated.HandleFunc("POST /api/admin/v1/auth/logout", s.handleLogout)
	authenticated.HandleFunc("GET /api/admin/v1/auth/session", s.handleSession)
	authenticated.HandleFunc("PUT /api/admin/v1/auth/password", s.handlePassword)
	authenticated.HandleFunc("POST /api/admin/v1/auth/totp/setup", s.handleTOTPSetup)
	authenticated.HandleFunc("POST /api/admin/v1/auth/totp/confirm", s.handleTOTPConfirm)
	authenticated.HandleFunc("DELETE /api/admin/v1/auth/totp", s.handleTOTPDisable)
	authenticated.HandleFunc("GET /api/admin/v1/providers", s.handleProviders)
	authenticated.HandleFunc("GET /api/admin/v1/dashboard", s.handleDashboard)
	authenticated.HandleFunc("GET /api/admin/v1/system/health", s.handleSystemHealth)
	authenticated.HandleFunc("GET /api/admin/v1/audit-events", s.handleAuditEvents)
	s.registerProxyRoutes(authenticated)
	s.registerUpstreamRoutes(authenticated)
	s.registerAccountRoutes(authenticated)
	s.registerModelRouteRoutes(authenticated)
	s.registerClientKeyRoutes(authenticated)
	mux.Handle("/api/admin/", s.requireSession(authenticated))
	mux.HandleFunc("/api/", s.handleAPINotFound)

	mux.HandleFunc("/v1", s.handleCapabilityUnavailable)
	mux.HandleFunc("/v1/", s.handleCapabilityUnavailable)
	mux.HandleFunc("/v1beta", s.handleCapabilityUnavailable)
	mux.HandleFunc("/v1beta/", s.handleCapabilityUnavailable)
	if s.assets != nil {
		mux.Handle("/", s.assets)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Sidervia web assets are not built", http.StatusServiceUnavailable)
		})
	}
	return s.baseMiddleware(mux)
}
