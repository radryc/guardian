package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/gcpobserve"
	"github.com/radryc/guardian/internal/gcpread"
)

type readAPIConfig struct {
	Address        string
	Token          string
	TokenFile      string
	Scopes         string
	TLSCert        string
	TLSKey         string
	TLSClientCA    string
	AllowPlaintext bool
	Pusher         string
	Project        string
	Region         string
	StateBucket    string
}

// startReadAPI starts the GCP read API in the background. Errors are logged and
// do not stop the task queue; the pusher stays useful without the read API.
func startReadAPI(ctx context.Context, cfg readAPIConfig) (*http.Server, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		return nil, nil
	}
	loopback := isLoopbackListen(cfg.Address)
	hasToken := strings.TrimSpace(cfg.Token) != "" || strings.TrimSpace(cfg.TokenFile) != ""
	if !loopback && !hasToken {
		return nil, fmt.Errorf("GCP read API is listening on %s without a bearer token; configure --read-token or --read-token-file", cfg.Address)
	}
	if (strings.TrimSpace(cfg.TLSCert) == "") != (strings.TrimSpace(cfg.TLSKey) == "") {
		return nil, fmt.Errorf("GCP read API TLS requires both --read-tls-cert and --read-tls-key")
	}
	// A bearer token is enough to expose the read API on a non-loopback
	// address (the token is checked on every request). TLS is required only
	// when the endpoint is unauthenticated.
	if !loopback && strings.TrimSpace(cfg.TLSCert) == "" && !cfg.AllowPlaintext && !hasToken {
		return nil, fmt.Errorf("GCP read API is listening on %s without TLS or a bearer token; configure --read-tls-cert/--read-tls-key, --read-token, or pass --read-allow-plaintext", cfg.Address)
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
		WithObserved(observer)
	if strings.TrimSpace(cfg.TokenFile) != "" {
		handler.WithTokenFile(cfg.TokenFile)
	}

	tlsCfg, err := readTLSConfig(cfg.TLSCert, cfg.TLSKey, cfg.TLSClientCA)
	if err != nil {
		return nil, fmt.Errorf("configure GCP read API TLS: %w", err)
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
		var serveErr error
		if tlsCfg != nil {
			serveErr = server.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			serveErr = server.ListenAndServe()
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
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

func readTLSConfig(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if strings.TrimSpace(certFile) == "" && strings.TrimSpace(keyFile) == "" {
		return nil, nil
	}
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return nil, fmt.Errorf("load certificate: %w", err)
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(clientCAFile) != "" {
		content, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(content) {
			return nil, fmt.Errorf("parse client CA bundle")
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
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
