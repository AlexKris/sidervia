package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/AlexKris/sidervia/internal/config"
)

func Healthcheck(ctx context.Context, cfg config.Config) error {
	host, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	} else if address, parseErr := netip.ParseAddr(host); parseErr == nil && address.IsUnspecified() {
		host = "127.0.0.1"
	}
	target := (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/readyz"}).String()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("readiness request failed: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return errors.New("Sidervia is not ready")
	}
	return nil
}
