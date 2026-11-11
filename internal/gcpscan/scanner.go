// Package gcpscan implements the Guardian scan interface for GCP using Cloud
// Asset Inventory. It reuses the AWS scan request/result schema so the Guardian
// UI and queue protocol do not need a new kind.
package gcpscan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/radryc/guardian/internal/awsscan"
)

const maxPages = 20

// Scanner lists GCP assets with Cloud Asset Inventory.
type Scanner struct {
	Project     string
	Region      string
	AssetURL    string
	HTTPClient  *http.Client
	TokenSource oauth2.TokenSource
	Now         func() time.Time
}

// NewScanner builds a scanner for one project.
func NewScanner(project, region string) *Scanner {
	return &Scanner{Project: strings.TrimSpace(project), Region: strings.TrimSpace(region)}
}

type searchResult struct {
	Name        string          `json:"name"`
	AssetType   string          `json:"assetType"`
	Project     string          `json:"project"`
	Location    string          `json:"location"`
	DisplayName string          `json:"displayName"`
	State       string          `json:"state"`
	Raw         json.RawMessage `json:"-"`
}

// Scan implements awsscan.Scanner.
func (s *Scanner) Scan(ctx context.Context, req *awsscan.ScanRequest) *awsscan.ScanResult {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	started := now().UTC()
	result := &awsscan.ScanResult{
		APIVersion: awsscan.APIVersion,
		Kind:       awsscan.ResultKind,
		ScanID:     req.ScanID,
		Account:    firstNonEmpty(req.Account, s.Project),
		Status:     awsscan.ScanStatusRunning,
		StartedAt:  started,
		Request:    req,
		Regions:    append([]string(nil), req.Regions...),
		Inventory:  map[string]map[string][]awsscan.InventoryResource{},
	}

	resources, err := s.listResources(ctx)
	if err != nil {
		result.Errors = append(result.Errors, awsscan.ScanError{Message: err.Error()})
		result.Status = awsscan.ScanStatusFailed
		result.FinishedAt = now().UTC()
		result.Summary = summarize(result)
		return result
	}

	for _, resource := range resources {
		if !includeResource(resource.AssetType, req) {
			continue
		}
		if !includeLocation(resource.Location, req.Regions) {
			continue
		}
		location := resource.Location
		if location == "" {
			location = s.Region
		}
		raw, _ := json.Marshal(resource)
		byLocation := result.Inventory[resource.AssetType]
		if byLocation == nil {
			byLocation = map[string][]awsscan.InventoryResource{}
			result.Inventory[resource.AssetType] = byLocation
		}
		byLocation[location] = append(byLocation[location], awsscan.InventoryResource{
			Identifier: resource.Name,
			Properties: raw,
		})
	}

	result.Status = awsscan.ScanStatusSucceeded
	result.FinishedAt = now().UTC()
	result.Summary = summarize(result)
	return result
}

func summarize(result *awsscan.ScanResult) awsscan.ScanSummary {
	summary := awsscan.ScanSummary{
		RegionCount:    len(result.Regions),
		InventoryCount: result.CountInventory(),
		ErrorCount:     len(result.Errors),
	}
	return summary
}

func includeResource(assetType string, req *awsscan.ScanRequest) bool {
	assetType = strings.TrimSpace(assetType)
	for _, excluded := range req.ExcludeResourceTypes {
		if strings.EqualFold(excluded, assetType) {
			return false
		}
	}
	if len(req.IncludeResourceTypes) == 0 {
		return true
	}
	for _, included := range req.IncludeResourceTypes {
		if strings.EqualFold(included, assetType) {
			return true
		}
	}
	return false
}

// includeLocation restricts a scan to the requested regions. Multi-region
// locations (for example "EU" or "US") and resources without a location are
// always kept: they cannot be attributed to one region.
func includeLocation(location string, regions []string) bool {
	location = strings.TrimSpace(location)
	if len(regions) == 0 || location == "" {
		return true
	}
	multiRegion := true
	for _, r := range location {
		if r >= '0' && r <= '9' {
			multiRegion = false
			break
		}
	}
	if multiRegion {
		return true
	}
	for _, region := range regions {
		region = strings.TrimSpace(region)
		if region == "" {
			continue
		}
		if strings.EqualFold(location, region) || strings.HasPrefix(strings.ToLower(location), strings.ToLower(region)+"-") {
			return true
		}
	}
	return false
}

func (s *Scanner) listResources(ctx context.Context) ([]searchResult, error) {
	client, err := s.httpClient(ctx)
	if err != nil {
		return nil, err
	}
	base := firstNonEmpty(s.AssetURL, "https://cloudasset.googleapis.com")
	pageToken := ""
	out := make([]searchResult, 0)
	for page := 0; page < maxPages; page++ {
		params := url.Values{}
		params.Set("pageSize", "500")
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		endpoint := fmt.Sprintf("%s/v1/projects/%s:searchAllResources?%s", base, url.PathEscape(s.Project), params.Encode())
		var response struct {
			Results []searchResult `json:"results"`
			Next    string         `json:"nextPageToken"`
		}
		if err := getJSON(ctx, client, endpoint, &response); err != nil {
			return nil, err
		}
		out = append(out, response.Results...)
		if response.Next == "" {
			break
		}
		pageToken = response.Next
	}
	return out, nil
}

func (s *Scanner) httpClient(ctx context.Context) (*http.Client, error) {
	if s.HTTPClient != nil {
		return s.HTTPClient, nil
	}
	tokens := s.TokenSource
	if tokens == nil {
		var err error
		tokens, err = google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, fmt.Errorf("application default credentials: %w", err)
		}
	}
	return oauth2.NewClient(ctx, tokens), nil
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gcpscan: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return json.Unmarshal(payload, out)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
