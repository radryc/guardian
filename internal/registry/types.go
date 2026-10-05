// Package registry stores platform registrations: AWS pushers (with their
// observed read API URL) and the source repositories that feed observed
// partitions. Records live in the MonoFS-backed store under /.registry so the
// CLI, UI and guardiand all read the same state.
package registry

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Pusher is a registered AWS pusher.
type Pusher struct {
	APIVersion     string            `json:"apiVersion"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Account        string            `json:"account,omitempty"`
	Region         string            `json:"region,omitempty"`
	ObservedAPIURL string            `json:"observedApiUrl"`
	Token          string            `json:"token,omitempty"`
	TokenEnv       string            `json:"tokenEnv,omitempty"`
	Description    string            `json:"description,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// TokenValue resolves the observed API bearer token.
func (p Pusher) TokenValue() string {
	if token := strings.TrimSpace(p.Token); token != "" {
		return token
	}
	if env := strings.TrimSpace(p.TokenEnv); env != "" {
		return strings.TrimSpace(os.Getenv(env))
	}
	return ""
}

// Repo is a registered source repository that feeds an observed partition.
type Repo struct {
	APIVersion     string    `json:"apiVersion"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name"`
	Pusher         string    `json:"pusher"`
	Partition      string    `json:"partition"`
	Path           string    `json:"path,omitempty"`
	Source         string    `json:"source,omitempty"`
	Branch         string    `json:"branch,omitempty"`
	AssemblySubdir string    `json:"assemblySubdir,omitempty"`
	Account        string    `json:"account,omitempty"`
	Region         string    `json:"region,omitempty"`
	Description    string    `json:"description,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	LastSyncedAt   time.Time `json:"lastSyncedAt,omitempty"`
	LastSyncCommit string    `json:"lastSyncCommit,omitempty"`
	LastSyncError  string    `json:"lastSyncError,omitempty"`
	// LastNotifiedAt/Commit record the most recent CI merge notification, even
	// when the assembly was not yet available to ingest.
	LastNotifiedAt     time.Time `json:"lastNotifiedAt,omitempty"`
	LastNotifiedCommit string    `json:"lastNotifiedCommit,omitempty"`
}

// Normalize fills defaults and trims fields.
func (p *Pusher) Normalize() {
	if p == nil {
		return
	}
	p.APIVersion = "guardian/v1alpha1"
	p.Kind = "PusherRegistration"
	p.Name = strings.TrimSpace(p.Name)
	p.Account = strings.TrimSpace(p.Account)
	p.Region = strings.TrimSpace(p.Region)
	p.ObservedAPIURL = strings.TrimRight(strings.TrimSpace(p.ObservedAPIURL), "/")
	p.Token = strings.TrimSpace(p.Token)
	p.TokenEnv = strings.TrimSpace(p.TokenEnv)
	p.Description = strings.TrimSpace(p.Description)
}

// Validate checks a pusher registration.
func (p *Pusher) Validate() error {
	if p == nil {
		return fmt.Errorf("pusher is nil")
	}
	p.Normalize()
	if !namePattern.MatchString(p.Name) {
		return fmt.Errorf("invalid pusher name %q", p.Name)
	}
	if p.ObservedAPIURL == "" {
		return fmt.Errorf("observedApiUrl is required")
	}
	if !strings.HasPrefix(p.ObservedAPIURL, "http://") && !strings.HasPrefix(p.ObservedAPIURL, "https://") {
		return fmt.Errorf("observedApiUrl must be an http(s) URL")
	}
	if p.Token != "" && p.TokenEnv != "" {
		return fmt.Errorf("provide at most one of token or tokenEnv")
	}
	return nil
}

// Normalize fills defaults and trims fields.
func (r *Repo) Normalize() {
	if r == nil {
		return
	}
	r.APIVersion = "guardian/v1alpha1"
	r.Kind = "RepoRegistration"
	r.Name = strings.TrimSpace(r.Name)
	r.Pusher = strings.TrimSpace(r.Pusher)
	r.Partition = strings.TrimSpace(r.Partition)
	r.Path = strings.TrimSpace(r.Path)
	r.Source = strings.TrimSpace(r.Source)
	r.Branch = strings.TrimSpace(r.Branch)
	r.AssemblySubdir = strings.TrimSpace(r.AssemblySubdir)
	if r.AssemblySubdir == "" {
		r.AssemblySubdir = "cdk.out"
	}
	r.Account = strings.TrimSpace(r.Account)
	r.Region = strings.TrimSpace(r.Region)
	r.Description = strings.TrimSpace(r.Description)
}

// Validate checks a repo registration.
func (r *Repo) Validate() error {
	if r == nil {
		return fmt.Errorf("repo is nil")
	}
	r.Normalize()
	if !namePattern.MatchString(r.Name) {
		return fmt.Errorf("invalid repo name %q", r.Name)
	}
	if !namePattern.MatchString(r.Partition) {
		return fmt.Errorf("invalid partition name %q", r.Partition)
	}
	if r.Pusher == "" {
		return fmt.Errorf("pusher is required")
	}
	if r.Path != "" && !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("path must be an absolute logical path")
	}
	return nil
}

// ObservedPartitionPath returns the default MonoFS payload path for a repo's
// generated observed bundle.
func (r Repo) ObservedPartitionPath() string {
	if strings.TrimSpace(r.Path) != "" {
		return strings.TrimRight(strings.TrimSpace(r.Path), "/")
	}
	return "/partitions/" + r.Partition + "/payloads/aws"
}
