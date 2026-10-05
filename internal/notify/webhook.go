package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultWebhookTimeout = 10 * time.Second
	defaultWebhookRetries = 3
	maxWebhookBody        = 1 << 20
)

// WebhookNotifier POSTs a JSON alert to an HTTP endpoint. When a secret is
// configured it signs the body with HMAC-SHA256 and sets
// X-Guardian-Signature: sha256=<hex>.
type WebhookNotifier struct {
	ChannelName string
	URL         string
	Secret      string
	HTTP        *http.Client
	Retries     int
}

// NewWebhookNotifier builds a webhook notifier.
func NewWebhookNotifier(name, url, secret string, timeout time.Duration) *WebhookNotifier {
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	return &WebhookNotifier{
		ChannelName: name,
		URL:         strings.TrimSpace(url),
		Secret:      secret,
		HTTP:        &http.Client{Timeout: timeout},
		Retries:     defaultWebhookRetries,
	}
}

func (w *WebhookNotifier) Name() string {
	if strings.TrimSpace(w.ChannelName) != "" {
		return w.ChannelName
	}
	return "webhook"
}

// Send delivers the alert with bounded exponential-backoff retries.
func (w *WebhookNotifier) Send(ctx context.Context, alert Alert) error {
	if w == nil || strings.TrimSpace(w.URL) == "" {
		return fmt.Errorf("webhook URL is not configured")
	}
	payload, err := json.Marshal(alert)
	if err != nil {
		return err
	}
	retries := w.Retries
	if retries <= 0 {
		retries = defaultWebhookRetries
	}
	var lastErr error
	backoff := 500 * time.Millisecond
	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		lastErr = w.post(ctx, payload)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func (w *WebhookNotifier) post(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "guardian-notifier")
	if w.Secret != "" {
		mac := hmac.New(sha256.New, []byte(w.Secret))
		mac.Write(payload)
		req.Header.Set("X-Guardian-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxWebhookBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s returned status %d: %s", w.URL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
