package observed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/awsobserve"
)

const maxResponseBytes = 16 << 20

// Client talks to the pusher's observed-state read API.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient builds an observed API client. Timeout <= 0 uses 30s.
func NewClient(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:   strings.TrimSpace(token),
		HTTP:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) (bool, error) {
	if c == nil || c.BaseURL == "" {
		return false, fmt.Errorf("observed API URL is not configured")
	}
	endpoint := c.BaseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return false, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return false, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("observed API %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return false, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(data))
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			message = apiErr.Error
		}
		return false, fmt.Errorf("observed API %s %s: status %d: %s", method, path, resp.StatusCode, message)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return false, fmt.Errorf("decode observed API response: %w", err)
		}
	}
	return true, nil
}

// Stack fetches the latest observed state for one stack. found=false means the
// pusher has not observed it yet.
func (c *Client) Stack(ctx context.Context, account, region, stackName string) (awsobserve.StackState, bool, error) {
	query := url.Values{"region": {region}}
	if account != "" {
		query.Set("account", account)
	}
	var state awsobserve.StackState
	found, err := c.do(ctx, http.MethodGet, "/v1/aws/observed/stacks/"+url.PathEscape(stackName), query, nil, &state)
	if err != nil || !found {
		return awsobserve.StackState{}, found, err
	}
	return state, true, nil
}

// Template fetches the live CloudFormation template for one stack.
func (c *Client) Template(ctx context.Context, account, region, stackName string) (string, error) {
	query := url.Values{"region": {region}}
	if account != "" {
		query.Set("account", account)
	}
	var resp struct {
		StackName string `json:"stackName"`
		Template  string `json:"template"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/v1/aws/observed/stacks/"+url.PathEscape(stackName)+"/template", query, nil, &resp); err != nil {
		return "", err
	}
	return resp.Template, nil
}

// Refresh forces a live DescribeStacks (and optional drift detection) on the
// pusher for the given stacks, returning the refreshed state.
func (c *Client) Refresh(ctx context.Context, account, region string, stacks []string, detectDrift bool) ([]awsobserve.StackState, error) {
	var resp struct {
		Stacks []awsobserve.StackState `json:"stacks"`
	}
	body := map[string]any{
		"account":     account,
		"region":      region,
		"stacks":      stacks,
		"detectDrift": detectDrift,
	}
	if _, err := c.do(ctx, http.MethodPost, "/v1/aws/observed/refresh", nil, body, &resp); err != nil {
		return nil, err
	}
	return resp.Stacks, nil
}

// DiffSource asks the pusher to synthesize an in-memory CDK source tree and
// diff it against the live observed template.
func (c *Client) DiffSource(ctx context.Context, req awsobserve.SourceDiffRequest) (*awsobserve.SourceDiffResult, error) {
	var result awsobserve.SourceDiffResult
	if _, err := c.do(ctx, http.MethodPost, "/v1/aws/observed/diff", nil, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Events returns recent observed events for one stack.
func (c *Client) Events(ctx context.Context, account, region, stackName string, since time.Time, limit int) ([]awsobserve.Event, error) {
	query := url.Values{"region": {region}, "stack": {stackName}}
	if account != "" {
		query.Set("account", account)
	}
	if !since.IsZero() {
		query.Set("since", since.UTC().Format(time.RFC3339Nano))
	}
	if limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}
	var resp struct {
		Events []awsobserve.Event `json:"events"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/v1/aws/observed/events", query, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}
