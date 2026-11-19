package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/gcpobserve"
	"github.com/radryc/guardian/internal/gcpread"
	"github.com/radryc/guardian/internal/tlsxutil"
)

type readAPIConfig struct {
	Address        string
	Token          string
	TokenFile      string
	Scopes         string
	TLSCert        string
	TLSKey         string
	TLSClientCA    string
	TLSEnabled     bool
	AllowPlaintext bool
	Pusher         string
	Project        string
	Region         string
	StateBucket    string
}

func validateReadAPIConfig(cfg readAPIConfig) error {
	loopback := isLoopbackListen(cfg.Address)
	hasToken := strings.TrimSpace(cfg.Token) != "" || strings.TrimSpace(cfg.TokenFile) != ""
	if !loopback && !hasToken {
		return fmt.Errorf("GCP read API is listening on %s without a bearer token; configure --read-token or --read-token-file", cfg.Address)
	}
	if (strings.TrimSpace(cfg.TLSCert) == "") != (strings.TrimSpace(cfg.TLSKey) == "") {
		return fmt.Errorf("GCP read API TLS requires both --read-tls-cert and --read-tls-key")
	}
	if !loopback && !cfg.TLSEnabled && strings.TrimSpace(cfg.TLSCert) == "" && !cfg.AllowPlaintext {
		return fmt.Errorf("GCP read API is listening on %s without TLS; configure --read-tls-cert/--read-tls-key, STRATA_TLS_* or pass --read-allow-plaintext for a trusted network", cfg.Address)
	}
	return nil
}

// startReadAPI starts the GCP read API in the background. Errors are logged and
// do not stop the task queue; the pusher stays useful without the read API.
func startReadAPI(ctx context.Context, cfg readAPIConfig) (*http.Server, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		return nil, nil
	}
	tlsCfg, tlsEnabled, err := tlsxutil.ServerTLSConfig(tlsxutil.LegacyServer{
		CertFile:          cfg.TLSCert,
		KeyFile:           cfg.TLSKey,
		CAFile:            cfg.TLSClientCA,
		RequireClientCert: strings.TrimSpace(cfg.TLSClientCA) != "",
	})
	if err != nil {
		return nil, fmt.Errorf("configure GCP read API TLS: %w", err)
	}
	cfg.TLSEnabled = tlsEnabled
	if err := validateReadAPIConfig(cfg); err != nil {
		return nil, err
	}

	reader, err := gcpread.New(gcpread.Config{
		Project:       cfg.Project,
		DefaultRegion: cfg.Region,
	})
	if err != nil {
		return nil, err
	}
	observer := gcpobserve.NewService(gcpobserve.NewCache(), gcpobserve.NewRefresher(cfg.StateBucket, cfg.Region))
	go refreshObserved(ctx, observer, cfg.Project, cfg.Region)

	handler := gcpread.NewHandler(reader, cfg.Token).
		WithPromQL(reader.PromQL()).
		WithIdentity(cfg.Pusher, cfg.Project, cfg.Region).
		WithScopes(splitCSV(cfg.Scopes)...).
		WithObserved(observer).
		WithRateLimit(50, 100).
		WithMaxRequestBodyBytes(1 << 20)
	if strings.TrimSpace(cfg.TokenFile) != "" {
		handler.WithTokenFile(cfg.TokenFile)
	}

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	go func() {
		log.Printf("GCP read API listening on %s (tls=%t)", cfg.Address, tlsCfg != nil)
		if tlsCfg == nil {
			if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				log.Printf("GCP read API stopped: %v", serveErr)
			}
			return
		}
		listener, listenErr := net.Listen("tcp", cfg.Address)
		if listenErr != nil {
			log.Printf("GCP read API stopped: %v", listenErr)
			return
		}
		if serveErr := server.Serve(tls.NewListener(listener, tlsCfg)); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("GCP read API stopped: %v", serveErr)
		}
	}()
	return server, nil
}

func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// readAPIUsesTLS reports whether the read API will terminate TLS, accounting
// for both the legacy flags and the shared STRATA_TLS_* contract.
func readAPIUsesTLS(certFile, keyFile, clientCAFile string) bool {
	_, enabled, err := tlsxutil.ServerConfig(tlsxutil.LegacyServer{
		CertFile:          certFile,
		KeyFile:           keyFile,
		CAFile:            clientCAFile,
		RequireClientCert: strings.TrimSpace(clientCAFile) != "",
	})
	return err == nil && enabled
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func refreshObserved(ctx context.Context, observer *gcpobserve.Service, project, region string) {
	refresh := func() {
		refreshCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if _, err := observer.Refresh(refreshCtx, project, region, nil); err != nil && ctx.Err() == nil {
			log.Printf("observed refresh failed: %v", err)
		}
	}
	refresh()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}
