package probes

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (r *Runner) execute(ctx context.Context, target Target) Result {
	started := time.Now()
	result := Result{
		ProbeID:   target.ID,
		Service:   target.Service,
		FullName:  target.FullName,
		Pusher:    r.cfg.Pusher,
		Cluster:   r.cfg.Cluster,
		Partition: target.Partition,
		Intent:    target.Intent,
		Asset:     target.Asset,
		Name:      target.Name,
		Type:      target.Type,
		Mode:      target.Mode,
		Target:    targetValue(target),
		CheckedAt: time.Now().UTC(),
		Labels:    target.Labels,
	}
	timeout := target.Timeout
	if timeout <= 0 {
		timeout = r.cfg.Timeout
	}
	retries := target.Retries
	if retries < 0 {
		retries = 0
	}
	var lastErr error
	var lastStatus int
	var lastPath string
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				goto done
			case <-time.After(250 * time.Millisecond):
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		statusCode, path, err := r.probeOnce(attemptCtx, target)
		cancel()
		lastStatus = statusCode
		lastPath = path
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
	}
done:
	result.DurationMs = time.Since(started).Milliseconds()
	result.StatusCode = lastStatus
	result.Path = lastPath
	if lastErr != nil {
		result.Success = false
		result.Status = "down"
		result.Error = lastErr.Error()
	} else {
		result.Success = true
		result.Status = "up"
	}
	return result
}

func (r *Runner) probeOnce(ctx context.Context, target Target) (int, string, error) {
	switch strings.ToLower(target.Type) {
	case "http", "https":
		return r.probeHTTP(ctx, target)
	case "dns":
		return 0, "", probeDNS(ctx, target)
	case "tls":
		return 0, "", probeTLS(ctx, target)
	case "grpc":
		return probeTCP(ctx, target)
	default:
		return probeTCP(ctx, target)
	}
}

func (r *Runner) probeHTTP(ctx context.Context, target Target) (int, string, error) {
	paths := []string{target.Path}
	if strings.TrimSpace(target.Path) == "" {
		paths = r.cfg.HTTPPaths
	}
	client := &http.Client{
		Timeout: target.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: target.Insecure}, //nolint:gosec
		},
	}
	var lastErr error
	var lastStatus int
	for _, path := range paths {
		requestURL := httpURL(target, path)
		if requestURL == "" {
			continue
		}
		method := strings.ToUpper(firstNonEmpty(target.Method, http.MethodGet))
		req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "guardian-probe/1")
		for key, value := range target.Headers {
			req.Header.Set(key, value)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		lastStatus = resp.StatusCode
		if !expectedStatus(resp.StatusCode, target.ExpectStatus) {
			lastErr = fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
			continue
		}
		if target.ExpectBody != "" && !strings.Contains(string(body), target.ExpectBody) {
			lastErr = fmt.Errorf("response body does not contain %q", target.ExpectBody)
			continue
		}
		return resp.StatusCode, path, nil
	}
	return lastStatus, "", firstNonNil(lastErr, fmt.Errorf("no HTTP path succeeded"))
}

func probeTCP(ctx context.Context, target Target) (int, string, error) {
	address := target.Address
	if address == "" && target.URL != "" {
		if parsed, err := url.Parse(target.URL); err == nil {
			address = parsed.Host
		}
	}
	if target.Port > 0 && !strings.Contains(address, ":") {
		address = fmt.Sprintf("%s:%d", address, target.Port)
	}
	if !strings.Contains(address, ":") {
		address = net.JoinHostPort(address, "80")
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return 0, "", err
	}
	_ = conn.Close()
	return 0, "", nil
}

func probeDNS(ctx context.Context, target Target) error {
	name := target.Address
	if name == "" && target.URL != "" {
		if parsed, err := url.Parse(target.URL); err == nil {
			name = parsed.Hostname()
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("dns probe requires an address")
	}
	addresses, err := net.DefaultResolver.LookupHost(ctx, name)
	if err != nil {
		return err
	}
	if len(target.ExpectIPs) > 0 {
		allowed := map[string]bool{}
		for _, value := range target.ExpectIPs {
			allowed[value] = true
		}
		for _, address := range addresses {
			if allowed[address] {
				return nil
			}
		}
		return fmt.Errorf("dns %s did not resolve to an expected address (%s)", name, strings.Join(addresses, ","))
	}
	return nil
}

func probeTLS(ctx context.Context, target Target) error {
	address := target.Address
	if address == "" && target.URL != "" {
		if parsed, err := url.Parse(target.URL); err == nil {
			address = parsed.Host
		}
	}
	if target.Port > 0 && !strings.Contains(address, ":") {
		address = fmt.Sprintf("%s:%d", address, target.Port)
	}
	if !strings.Contains(address, ":") {
		address = net.JoinHostPort(address, "443")
	}
	dialer := &net.Dialer{}
	conn, err := tls.DialWithDialer(dialer, "tcp", address, &tls.Config{
		InsecureSkipVerify: target.Insecure, //nolint:gosec
		ServerName:         hostOnly(address),
	})
	if err != nil {
		return err
	}
	defer conn.Close()
	return nil
}

func httpURL(target Target, path string) string {
	if target.URL != "" {
		parsed, err := url.Parse(target.URL)
		if err != nil {
			return ""
		}
		if path != "" {
			parsed.Path = path
		}
		return parsed.String()
	}
	address := target.Address
	if address == "" {
		return ""
	}
	scheme := "http"
	if strings.EqualFold(target.Type, "https") {
		scheme = "https"
	}
	if target.Port > 0 && !strings.Contains(address, ":") {
		address = fmt.Sprintf("%s:%d", address, target.Port)
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return scheme + "://" + address + path
}

func expectedStatus(status int, expected []int) bool {
	if len(expected) == 0 {
		return status >= 200 && status < 400
	}
	for _, value := range expected {
		if value == status {
			return true
		}
	}
	return false
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func hostOnly(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	return host
}
