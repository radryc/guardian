package awsread

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return awssdk.Config{}, err
	}
	cfg.Credentials = awssdk.NewCredentialsCache(cfg.Credentials)
	c.cfgs[region] = cfg
	return cfg, nil
}

// Do signs and executes a form-encoded request against the CloudWatch PromQL API.
func (c *PromQLClient) Do(ctx context.Context, region, path string, params url.Values) ([]byte, int, error) {
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
