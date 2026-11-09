// Package gcpsetup verifies that a GCP project is ready for the Guardian GCP
// pusher. It performs read-only checks with Application Default Credentials and
// reports the exact remediation for anything missing. Provisioning itself lives
// in scripts/gcp-bootstrap.sh (or the customer's own Terraform).
package gcpsetup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const defaultStateBucketSuffix = "-tofu-state"

// Config describes one target project.
type Config struct {
	Project        string
	Region         string
	ServiceAccount string
	StateBucket    string
	KMSKey         string
	TokenSource    oauth2.TokenSource
	HTTPClient     *http.Client
}

// WithDefaults fills derived values.
func (c Config) WithDefaults() Config {
	c.Project = strings.TrimSpace(c.Project)
	c.Region = strings.TrimSpace(c.Region)
	if c.Region == "" {
		c.Region = "europe-west1"
	}
	c.ServiceAccount = strings.TrimSpace(c.ServiceAccount)
	if c.ServiceAccount == "" && c.Project != "" {
		c.ServiceAccount = "guardian-deployer@" + c.Project + ".iam.gserviceaccount.com"
	}
	c.StateBucket = strings.TrimSpace(c.StateBucket)
	if c.StateBucket == "" && c.Project != "" {
		c.StateBucket = c.Project + defaultStateBucketSuffix
	}
	c.KMSKey = strings.TrimSpace(c.KMSKey)
	return c
}

// Validate checks required fields.
func (c Config) Validate() error {
	if c.Project == "" {
		return fmt.Errorf("project is required")
	}
	if c.ServiceAccount == "" {
		return fmt.Errorf("service account is required")
	}
	if c.StateBucket == "" {
		return fmt.Errorf("state bucket is required")
	}
	return nil
}

// Severity classifies a check.
type Severity string

const (
	SeverityOK       Severity = "ok"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Check is one verified prerequisite.
type Check struct {
	Name        string   `json:"name"`
	Severity    Severity `json:"severity"`
	Detail      string   `json:"detail,omitempty"`
	Remediation string   `json:"remediation,omitempty"`
}

// Report is the aggregate result.
type Report struct {
	Project string  `json:"project"`
	Region  string  `json:"region"`
	Checks  []Check `json:"checks"`
}

// OK reports whether no check is critical.
func (r Report) OK() bool {
	for _, check := range r.Checks {
		if check.Severity == SeverityCritical {
			return false
		}
	}
	return true
}

// RequiredAPIs are the APIs the pusher and read API need.
var RequiredAPIs = []string{
	"serviceusage.googleapis.com",
	"cloudresourcemanager.googleapis.com",
	"iam.googleapis.com",
	"iamcredentials.googleapis.com",
	"sts.googleapis.com",
	"compute.googleapis.com",
	"run.googleapis.com",
	"storage.googleapis.com",
	"sqladmin.googleapis.com",
	"file.googleapis.com",
	"secretmanager.googleapis.com",
	"monitoring.googleapis.com",
	"logging.googleapis.com",
	"cloudtrace.googleapis.com",
	"cloudasset.googleapis.com",
	"pubsub.googleapis.com",
	"cloudkms.googleapis.com",
	"servicenetworking.googleapis.com",
	"artifactregistry.googleapis.com",
}

type checker struct {
	cfg    Config
	client *http.Client
}

// Run verifies the target project. The returned error is only for transport or
// credential failures; prerequisite problems are reported as checks.
func Run(ctx context.Context, cfg Config) (Report, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return Report{}, err
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		tokens := cfg.TokenSource
		if tokens == nil {
			var err error
			tokens, err = google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
			if err != nil {
				return Report{}, fmt.Errorf("application default credentials: %w (run gcloud auth application-default login)", err)
			}
		}
		httpClient = oauth2.NewClient(ctx, tokens)
	}
	c := &checker{cfg: cfg, client: httpClient}
	report := Report{Project: cfg.Project, Region: cfg.Region}

	report.Checks = append(report.Checks, c.checkProject(ctx))
	report.Checks = append(report.Checks, c.checkAPIs(ctx)...)
	report.Checks = append(report.Checks, c.checkServiceAccount(ctx))
	report.Checks = append(report.Checks, c.checkBucket(ctx))
	report.Checks = append(report.Checks, c.checkKMS(ctx))
	return report, nil
}

func (c *checker) getJSON(ctx context.Context, endpoint string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if out != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.Unmarshal(payload, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (c *checker) checkProject(ctx context.Context) Check {
	var response struct {
		LifecycleState string `json:"lifecycleState"`
	}
	status, err := c.getJSON(ctx, "https://cloudresourcemanager.googleapis.com/v1/projects/"+url.PathEscape(c.cfg.Project), &response)
	check := Check{
		Name:     "project",
		Severity: SeverityOK,
		Detail:   c.cfg.Project + " ACTIVE",
	}
	if err != nil {
		return Check{Name: "project", Severity: SeverityCritical, Detail: err.Error()}
	}
	if status == http.StatusForbidden || status == http.StatusNotFound {
		check.Severity = SeverityCritical
		check.Detail = fmt.Sprintf("project not readable (HTTP %d)", status)
		check.Remediation = fmt.Sprintf("gcloud projects create %s --name=%s", c.cfg.Project, c.cfg.Project)
		return check
	}
	if response.LifecycleState != "" && !strings.EqualFold(response.LifecycleState, "ACTIVE") {
		check.Severity = SeverityCritical
		check.Detail = "lifecycleState=" + response.LifecycleState
	}
	return check
}

func (c *checker) checkAPIs(ctx context.Context) []Check {
	checks := make([]Check, 0, len(RequiredAPIs))
	missing := make([]string, 0)
	for _, api := range RequiredAPIs {
		endpoint := fmt.Sprintf("https://serviceusage.googleapis.com/v1/projects/%s/services/%s?fields=state", url.PathEscape(c.cfg.Project), url.PathEscape(api))
		var response struct {
			State string `json:"state"`
		}
		status, err := c.getJSON(ctx, endpoint, &response)
		if err != nil || status != http.StatusOK || !strings.EqualFold(response.State, "ENABLED") {
			missing = append(missing, api)
		}
	}
	severity := SeverityOK
	detail := fmt.Sprintf("%d/%d enabled", len(RequiredAPIs)-len(missing), len(RequiredAPIs))
	var remediation string
	if len(missing) > 0 {
		severity = SeverityWarning
		detail = "missing: " + strings.Join(missing, ", ")
		remediation = fmt.Sprintf("gcloud services enable %s --project=%s", strings.Join(missing, " "), c.cfg.Project)
	}
	checks = append(checks, Check{Name: "apis", Severity: severity, Detail: detail, Remediation: remediation})
	return checks
}

func (c *checker) checkServiceAccount(ctx context.Context) Check {
	endpoint := fmt.Sprintf("https://iam.googleapis.com/v1/projects/%s/serviceAccounts/%s",
		url.PathEscape(c.cfg.Project), url.PathEscape(c.cfg.ServiceAccount))
	var response struct {
		Email    string `json:"email"`
		Disabled bool   `json:"disabled"`
	}
	status, err := c.getJSON(ctx, endpoint, &response)
	check := Check{Name: "service_account", Severity: SeverityOK, Detail: c.cfg.ServiceAccount}
	if err != nil {
		return Check{Name: "service_account", Severity: SeverityCritical, Detail: err.Error()}
	}
	if status == http.StatusNotFound {
		check.Severity = SeverityCritical
		check.Detail = "not found"
		check.Remediation = fmt.Sprintf("gcloud iam service-accounts create %s --project=%s",
			strings.SplitN(c.cfg.ServiceAccount, "@", 2)[0], c.cfg.Project)
		return check
	}
	if status != http.StatusOK {
		check.Severity = SeverityCritical
		check.Detail = fmt.Sprintf("HTTP %d", status)
	}
	if response.Disabled {
		check.Severity = SeverityCritical
		check.Detail = "service account is disabled"
	}
	return check
}

func (c *checker) checkBucket(ctx context.Context) Check {
	var response struct {
		Name string `json:"name"`
	}
	status, err := c.getJSON(ctx, "https://storage.googleapis.com/storage/v1/b/"+url.PathEscape(c.cfg.StateBucket), &response)
	check := Check{Name: "state_bucket", Severity: SeverityOK, Detail: "gs://" + c.cfg.StateBucket}
	if err != nil {
		return Check{Name: "state_bucket", Severity: SeverityCritical, Detail: err.Error()}
	}
	if status == http.StatusNotFound {
		check.Severity = SeverityCritical
		check.Detail = "bucket not found"
		check.Remediation = fmt.Sprintf("gcloud storage buckets create gs://%s --project=%s --location=%s --uniform-bucket-level-access",
			c.cfg.StateBucket, c.cfg.Project, c.cfg.Region)
		return check
	}
	if status != http.StatusOK {
		check.Severity = SeverityCritical
		check.Detail = fmt.Sprintf("HTTP %d", status)
	}
	return check
}

func (c *checker) checkKMS(ctx context.Context) Check {
	check := Check{Name: "kms_key", Severity: SeverityOK, Detail: c.cfg.KMSKey}
	if c.cfg.KMSKey == "" {
		check.Severity = SeverityWarning
		check.Detail = "no KMS key configured; OpenTofu state will not be encrypted at rest"
		check.Remediation = fmt.Sprintf("gcloud kms keyrings create guardian-tofu --location=%s --project=%s && "+
			"gcloud kms keys create state --keyring=guardian-tofu --location=%s --purpose=encryption --project=%s",
			c.cfg.Region, c.cfg.Project, c.cfg.Region, c.cfg.Project)
		return check
	}
	var response struct {
		Name string `json:"name"`
	}
	status, err := c.getJSON(ctx, "https://cloudkms.googleapis.com/v1/"+strings.TrimPrefix(c.cfg.KMSKey, "/"), &response)
	if err != nil {
		return Check{Name: "kms_key", Severity: SeverityCritical, Detail: err.Error()}
	}
	switch status {
	case http.StatusForbidden:
		check.Severity = SeverityWarning
		check.Detail = "key not readable by the deploy service account (HTTP 403); grant roles/cloudkms.viewer to verify it"
	case http.StatusNotFound:
		check.Severity = SeverityCritical
		check.Detail = "key not found"
	case http.StatusOK:
	default:
		check.Severity = SeverityCritical
		check.Detail = fmt.Sprintf("HTTP %d", status)
	}
	return check
}

// MissingAPIs returns the APIs that were reported missing.
func (r Report) MissingAPIs() []string {
	for _, check := range r.Checks {
		if check.Name == "apis" && strings.HasPrefix(check.Detail, "missing: ") {
			raw := strings.TrimPrefix(check.Detail, "missing: ")
			parts := strings.Split(raw, ",")
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				if api := strings.TrimSpace(part); api != "" {
					out = append(out, api)
				}
			}
			sort.Strings(out)
			return out
		}
	}
	return nil
}
