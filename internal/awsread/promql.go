package awsread

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// PromQLProxier forwards signed requests to CloudWatch's Prometheus-compatible
// API. It is an interface so the HTTP handler can be tested without AWS.
type PromQLProxier interface {
	Do(ctx context.Context, region, path string, params url.Values) ([]byte, int, error)
}

// PromQLClient signs and proxies requests to
// https://monitoring.<region>.amazonaws.com/api/v1/<operation>.
type PromQLClient struct {
	defaultRegion string
	profile       string
	credentials   awssdk.CredentialsProvider
	httpClient    *http.Client
	signer        *v4.Signer

	mu   sync.Mutex
	cfgs map[string]awssdk.Config
}

// NewPromQLClient builds a SigV4-signed proxy for the CloudWatch PromQL API.
func NewPromQLClient(cfg Config) *PromQLClient {
	defaultRegion := strings.TrimSpace(cfg.DefaultRegion)
	if defaultRegion == "" {
		defaultRegion = "us-east-1"
	}
	return &PromQLClient{
		defaultRegion: defaultRegion,
		profile:       strings.TrimSpace(cfg.Profile),
		credentials:   cfg.Credentials,
		httpClient:    &http.Client{Timeout: 40 * time.Second},
		signer:        v4.NewSigner(),
		cfgs:          make(map[string]awssdk.Config),
	}
}

func (c *PromQLClient) loadConfig(ctx context.Context, region string) (awssdk.Config, error) {
	if region == "" {
		region = c.defaultRegion
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg, ok := c.cfgs[region]; ok {
		return cfg, nil
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if c.profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(c.profile))
	}
	if c.credentials != nil {
		opts = append(opts, awsconfig.WithCredentialsProvider(c.credentials))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return awssdk.Config{}, err
	}
	cfg.Credentials = awssdk.NewCredentialsCache(cfg.Credentials)
	c.cfgs[region] = cfg
	return cfg, nil
}

// CloudWatch QueryMetrics requires the PromQL expression to select a metric by
// name, either as {"metric"} or {__name__="metric"}. Grafana and Doctor expose
// the simpler metric{...} form, so normalize it before signing the request.
var simpleMetricSelector = regexp.MustCompile(`^([A-Za-z_:][A-Za-z0-9_:.]*)(\{(.*)\})?$`)
var simpleMetricName = regexp.MustCompile(`^[A-Za-z_:][A-Za-z0-9_:.]*$`)

func normalizeCloudWatchPromQL(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return query
	}
	if strings.HasPrefix(query, "{") {
		inner := strings.TrimPrefix(query, "{")
		trimmed := strings.TrimSpace(inner)
		if strings.HasPrefix(trimmed, "__name__") || strings.HasPrefix(trimmed, `"`) || strings.HasPrefix(trimmed, `'`) {
			return query
		}
		if end := strings.IndexAny(inner, ",}"); end > 0 {
			name := strings.TrimSpace(inner[:end])
			if simpleMetricName.MatchString(name) {
				rest := inner[end:]
				if strings.HasPrefix(rest, "}") {
					return `{__name__="` + name + `"}`
				}
				return `{__name__="` + name + `"` + rest
			}
		}
		return query
	}
	match := simpleMetricSelector.FindStringSubmatch(query)
	if match == nil {
		return query
	}
	labels := strings.TrimSpace(match[3])
	if labels == "" {
		return `{"` + match[1] + `"}`
	}
	return `{"` + match[1] + `", ` + labels + `}`
}

// Do signs and executes a form-encoded request against the CloudWatch PromQL API.
func (c *PromQLClient) Do(ctx context.Context, region, path string, params url.Values) ([]byte, int, error) {
	if query := params.Get("query"); query != "" {
		params.Set("query", normalizeCloudWatchPromQL(query))
	}
	if region == "" {
		region = c.defaultRegion
	}
	cfg, err := c.loadConfig(ctx, region)
	if err != nil {
		return nil, 0, err
	}
	credentials, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("retrieve aws credentials: %w", err)
	}

	body := params.Encode()
	endpoint := fmt.Sprintf("https://monitoring.%s.amazonaws.com%s", region, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	payloadHash := sha256Hex(body)
	if err := c.signer.SignHTTP(ctx, credentials, req, payloadHash, "monitoring", region, time.Now().UTC()); err != nil {
		return nil, 0, fmt.Errorf("sign request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return payload, resp.StatusCode, nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
