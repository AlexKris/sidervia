package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/AlexKris/sidervia/internal/accountvalidate"
	"github.com/AlexKris/sidervia/internal/auth"
	"github.com/AlexKris/sidervia/internal/buildinfo"
	"github.com/AlexKris/sidervia/internal/clientauth"
	"github.com/AlexKris/sidervia/internal/clock"
	"github.com/AlexKris/sidervia/internal/config"
	"github.com/AlexKris/sidervia/internal/control"
	"github.com/AlexKris/sidervia/internal/cryptox"
	"github.com/AlexKris/sidervia/internal/egress"
	"github.com/AlexKris/sidervia/internal/gateway"
	"github.com/AlexKris/sidervia/internal/httpapi"
	"github.com/AlexKris/sidervia/internal/identifier"
	"github.com/AlexKris/sidervia/internal/metrics"
	"github.com/AlexKris/sidervia/internal/oauth"
	"github.com/AlexKris/sidervia/internal/processlock"
	"github.com/AlexKris/sidervia/internal/provider"
	"github.com/AlexKris/sidervia/internal/provider/anthropic"
	"github.com/AlexKris/sidervia/internal/provider/google"
	"github.com/AlexKris/sidervia/internal/provider/openai"
	"github.com/AlexKris/sidervia/internal/provider/xai"
	"github.com/AlexKris/sidervia/internal/routing"
	"github.com/AlexKris/sidervia/internal/store"
	"github.com/AlexKris/sidervia/internal/usage"
)

func Serve(ctx context.Context, cfg config.Config, assets http.Handler, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	masterKey, err := cryptox.LoadMasterKey(cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	cipher, err := cryptox.NewCipher(masterKey)
	if err != nil {
		return err
	}
	if err := store.PrepareDataDir(cfg.DataDir); err != nil {
		return err
	}
	lock, err := processlock.Acquire(filepath.Join(cfg.DataDir, "sidervia.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	database, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := database.VerifyOrCreateSentinel(ctx, cipher); err != nil {
		return err
	}
	ids := identifier.NewGenerator()
	authService := auth.NewService(database.DB(), cipher, clock.Real{}, ids, auth.NewPasswordHasher(), masterKey, cfg.PublicURL.Hostname())
	created, err := authService.BootstrapFromFile(ctx, cfg.BootstrapPasswordFile)
	if err != nil {
		return err
	}
	if created {
		logger.Warn("administrator bootstrapped; remove the bootstrap password file and mount", "component", "auth", "event", "admin.bootstrapped")
	}
	controlService := control.NewService(database.DB(), cipher, clock.Real{}, ids)
	providerRegistry, err := provider.NewRegistry(openai.New(), anthropic.New(), google.New(), xai.New())
	if err != nil {
		return err
	}
	clientAuthService := clientauth.New(database.DB(), clock.Real{})
	routingService := routing.New(database.DB(), cipher, clock.Real{})
	egressManager := egress.New(egress.Options{})
	defer egressManager.CloseIdleConnections()
	usageWriter := usage.NewWriter(database.DB())
	usageReader := usage.NewReader(database.DB())
	usageRetention := usage.NewRetention(database.DB(), clock.Real{}, controlService)
	defer func() {
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = usageWriter.Close(closeContext)
	}()
	oauthService := oauth.New(oauth.Options{
		DB: database.DB(), Cipher: cipher, Clock: clock.Real{}, IDs: ids, PublicURL: cfg.PublicURL,
		Control: controlService, Routing: routingService, Providers: providerRegistry, Transport: egressManager,
	})
	if err := oauthService.RecoverInterrupted(ctx); err != nil {
		return fmt.Errorf("recover interrupted runtime state: %w", err)
	}
	gatewayService := gateway.New(gateway.Options{
		Router: routingService, Providers: providerRegistry, Transport: egressManager,
		Recorder: usageWriter, Credentials: oauthService, Clock: clock.Real{}, Logger: logger,
	})
	accountValidator := accountvalidate.New(controlService, routingService, providerRegistry, egressManager)
	build := buildinfo.Current()
	registry := metrics.New(build)
	api := httpapi.New(httpapi.Options{
		Auth: authService, ClientAuth: clientAuthService, Control: controlService,
		AccountValidate: accountValidator, Gateway: gatewayService, Routing: routingService, OAuth: oauthService,
		UsageReader: usageReader, UsageRecorder: usageWriter,
		Store: database, Logger: logger, IDs: ids,
		PublicURL: cfg.PublicURL, TrustedProxies: cfg.TrustedProxies, SecureCookie: cfg.PublicURL.Scheme == "https",
		Assets: assets, Build: build, Metrics: registry,
	})

	applicationListener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.ListenAddr, err)
	}
	defer applicationListener.Close()
	applicationServer := &http.Server{
		Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute,
		MaxHeaderBytes: 32 << 10, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	var metricsServer *http.Server
	var metricsListener net.Listener
	if cfg.MetricsAddr != "" {
		metricsListener, err = net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			return fmt.Errorf("listen for metrics on %s: %w", cfg.MetricsAddr, err)
		}
		defer metricsListener.Close()
		metricsServer = &http.Server{
			Handler: registry.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
			MaxHeaderBytes: 8 << 10, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
		}
	}

	serveErrors := make(chan error, 2)
	runtimeContext, stopRuntime := context.WithCancel(ctx)
	refreshDone := make(chan struct{})
	retentionDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		oauthService.RunRefresher(runtimeContext)
	}()
	go func() {
		defer close(retentionDone)
		usageRetention.Run(runtimeContext, func(result usage.CleanupResult, err error) {
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					logger.Error("request metadata retention failed", "component", "usage", "event", "request.retention_failed")
				}
				return
			}
			if result.Deleted > 0 {
				logger.Info("expired request metadata aggregated and removed", "component", "usage",
					"event", "request.retention_completed", "deleted_count", result.Deleted,
					"aggregated_days", result.AggregatedDays, "cutoff", result.Cutoff.Format(time.RFC3339))
			}
		})
	}()
	go func() { serveErrors <- applicationServer.Serve(applicationListener) }()
	if metricsServer != nil {
		go func() { serveErrors <- metricsServer.Serve(metricsListener) }()
	}
	registry.SetReady(true)
	logger.Info("Sidervia is ready", "component", "app", "event", "server.ready", "listen_addr", cfg.ListenAddr, "version", build.Version)

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serveErrors:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	}
	stopRuntime()
	<-refreshDone
	<-retentionDone
	api.SetReady(false)
	registry.SetReady(false)
	shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	shutdownErr := applicationServer.Shutdown(shutdownContext)
	if metricsServer != nil {
		shutdownErr = errors.Join(shutdownErr, metricsServer.Shutdown(shutdownContext))
	}
	if shutdownErr != nil {
		_ = applicationServer.Close()
		if metricsServer != nil {
			_ = metricsServer.Close()
		}
	}
	usageContext, usageCancel := context.WithTimeout(context.Background(), 5*time.Second)
	usageErr := usageWriter.Close(usageContext)
	usageCancel()
	egressManager.CloseIdleConnections()
	if serveErr != nil {
		return fmt.Errorf("HTTP server stopped unexpectedly: %w", serveErr)
	}
	if shutdownErr != nil {
		return fmt.Errorf("graceful shutdown: %w", shutdownErr)
	}
	if usageErr != nil {
		return fmt.Errorf("flush request metadata: %w", usageErr)
	}
	logger.Info("Sidervia stopped", "component", "app", "event", "server.stopped")
	return nil
}
