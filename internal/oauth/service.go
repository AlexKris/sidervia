package oauth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/routing"
)

const (
	ProviderGoogle = "google"
	attemptTTL     = 10 * time.Minute
)

var googleScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/generative-language.retriever",
}

type Transport interface {
	Do(context.Context, routing.Candidate, provider.NativeRequest, provider.Adapter) (*http.Response, error)
	DoGoogleOAuthToken(context.Context, routing.Candidate, url.Values) (*http.Response, error)
}

type Service struct {
	db        *sql.DB
	cipher    *cryptox.Cipher
	clock     clock.Clock
	ids       identifier.Generator
	publicURL *url.URL
	control   *control.Service
	routing   *routing.Service
	providers *provider.Registry
	transport Transport

	refreshMu sync.Mutex
	refreshes map[int64]*refreshCall
}

type Options struct {
	DB        *sql.DB
	Cipher    *cryptox.Cipher
	Clock     clock.Clock
	IDs       identifier.Generator
	PublicURL *url.URL
	Control   *control.Service
	Routing   *routing.Service
	Providers *provider.Registry
	Transport Transport
}

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

func (e *Error) GatewayCode() string { return e.Code }

func New(options Options) *Service {
	c := options.Clock
	if c == nil {
		c = clock.Real{}
	}
	return &Service{
		db: options.DB, cipher: options.Cipher, clock: c, ids: options.IDs, publicURL: options.PublicURL,
		control: options.Control, routing: options.Routing, providers: options.Providers, transport: options.Transport,
		refreshes: make(map[int64]*refreshCall),
	}
}

func IsError(err error, code string) bool {
	var value *Error
	return errors.As(err, &value) && (code == "" || value.Code == code)
}

func (s *Service) redirectURI() string {
	if s.publicURL == nil {
		return ""
	}
	return s.publicURL.ResolveReference(&url.URL{Path: "/oauth/callback/google"}).String()
}

func (s *Service) RecoverInterrupted(ctx context.Context) error {
	now := s.clock.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx, `UPDATE oauth_attempts SET status = 'failed', consumed_at_ms = ?
		WHERE status = 'exchanging'`, now); err != nil {
		return err
	}
	if err := s.cleanupExpiredAttempts(ctx); err != nil {
		return err
	}
	_, err := s.control.RecoverInterruptedAccountValidations(ctx)
	return err
}
