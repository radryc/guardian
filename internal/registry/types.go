// Package registry stores platform registrations: AWS pushers (with their
// observed read API URL) and the source repositories that feed observed
// partitions. Records live in the MonoFS-backed store under /.registry so the
// CLI, UI and guardiand all read the same state.
package registry

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// parseSelector splits a comma-separated stack selector and validates each
// glob so malformed patterns fail at registration time.
func parseSelector(selector string) ([]string, error) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return nil, nil
	}
	out := make([]string, 0, strings.Count(trimmed, ",")+1)
	for _, pattern := range strings.Split(trimmed, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if _, err := path.Match(pattern, "probe"); err != nil {
			return nil, fmt.Errorf("invalid selector glob %q", pattern)
		}
		out = append(out, pattern)
	}
	return out, nil
}

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

// Repo is a registered source repository that feeds one or more observed
// partitions. Credentials (pusher token, source URL) stay in this record; the
// partition side only ever references the repo by name.
// NativeRepoName is the built-in source used for partitions that do not have
// (and do not need) a repository. It is a logical repo-like assignment so the
// normal partition binding and assignment views can represent it.
const NativeRepoName = "native"

type Repo struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Pusher     string `json:"pusher"`
	// Partition is the legacy single-partition binding. It is kept in sync with
	// the first entry of Bindings for older readers.
	Partition string `json:"partition,omitempty"`
	// Bindings maps this repo to the partitions it feeds. A repo can feed many
	// partitions and a partition can be fed by many repos.
	Bindings []RepoBinding `json:"bindings,omitempty"`
	Path     string        `json:"path,omitempty"`
	Source   string        `json:"source,omitempty"`
	// SourceType is "git" (Git URL/local checkout), "monofs" (logical MonoFS
	// path), or "native" (no repository required). It is inferred from Source
	// when omitted.
	SourceType     string    `json:"sourceType,omitempty"`
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

// RepoBinding binds a repo to one partition with an optional payload path,
// CDK stack selector and account/region override. No credentials are stored
// here, so bindings are safe to mirror into partition manifests or shared
// files.
type RepoBinding struct {
	Partition string `json:"partition" yaml:"partition"`
	// Path is the logical MonoFS payload dir for this partition. Defaults to
	// /partitions/<partition>/payloads/aws.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// Selector is a comma-separated list of globs matched against CloudFormation
	// stack names. Empty means every stack in the assembly.
	Selector string `json:"selector,omitempty" yaml:"selector,omitempty"`
	// Account and Region override the repo/pusher values for this partition.
	Account string `json:"account,omitempty" yaml:"account,omitempty"`
	Region  string `json:"region,omitempty" yaml:"region,omitempty"`
}

// PayloadDir returns the MonoFS payload directory for this binding.
func (b RepoBinding) PayloadDir() string {
	if path := strings.TrimRight(strings.TrimSpace(b.Path), "/"); path != "" {
		return path
	}
	return "/partitions/" + strings.TrimSpace(b.Partition) + "/payloads/aws"
}

// BindingList returns the effective bindings, deriving one from the legacy
// single-partition field when no explicit bindings exist.
func (r Repo) BindingList() []RepoBinding {
	if len(r.Bindings) > 0 {
		return r.Bindings
	}
	if strings.TrimSpace(r.Partition) == "" {
		return nil
	}
	return []RepoBinding{{
		Partition: strings.TrimSpace(r.Partition),
		Path:      strings.TrimSpace(r.Path),
		Account:   strings.TrimSpace(r.Account),
		Region:    strings.TrimSpace(r.Region),
	}}
}

// PartitionNames returns the distinct partitions this repo feeds.
func (r Repo) PartitionNames() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(r.Bindings)+1)
	for _, binding := range r.BindingList() {
		name := strings.TrimSpace(binding.Partition)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
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
	r.SourceType = strings.ToLower(strings.TrimSpace(r.SourceType))
	if r.SourceType == "native" {
		r.Source = ""
		r.Branch = ""
	}
	if r.SourceType == "" && r.Source != "" {
		if strings.HasPrefix(r.Source, "/") || strings.HasPrefix(r.Source, "monofs://") {
			r.SourceType = "monofs"
		} else {
			r.SourceType = "git"
		}
	}
	r.Branch = strings.TrimSpace(r.Branch)
	r.AssemblySubdir = strings.TrimSpace(r.AssemblySubdir)
	if r.AssemblySubdir == "" {
		r.AssemblySubdir = "cdk.out"
	}
	r.Account = strings.TrimSpace(r.Account)
	r.Region = strings.TrimSpace(r.Region)
	r.Description = strings.TrimSpace(r.Description)

	bindings := make([]RepoBinding, 0, len(r.Bindings))
	seen := map[string]bool{}
	for _, binding := range r.Bindings {
		binding.Partition = strings.TrimSpace(binding.Partition)
		binding.Path = strings.TrimRight(strings.TrimSpace(binding.Path), "/")
		binding.Selector = strings.TrimSpace(binding.Selector)
		binding.Account = strings.TrimSpace(binding.Account)
		binding.Region = strings.TrimSpace(binding.Region)
		if binding.Partition == "" || seen[binding.Partition] {
			continue
		}
		seen[binding.Partition] = true
		bindings = append(bindings, binding)
	}
	if len(bindings) == 0 && r.Partition != "" {
		bindings = append(bindings, RepoBinding{
			Partition: r.Partition,
			Path:      strings.TrimRight(r.Path, "/"),
			Account:   r.Account,
			Region:    r.Region,
		})
	}
	r.Bindings = bindings
	if r.Partition == "" && len(bindings) > 0 {
		r.Partition = bindings[0].Partition
	}
	if r.Path == "" && len(bindings) > 0 {
		r.Path = bindings[0].Path
	}
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
	switch r.SourceType {
	case "", "git", "monofs", "native":
	default:
		return fmt.Errorf("sourceType must be git, monofs, or native")
	}
	if r.SourceType == "monofs" && r.Source != "" && !strings.HasPrefix(r.Source, "/") && !strings.HasPrefix(r.Source, "monofs://") {
		return fmt.Errorf("MonoFS source must be an absolute logical path or monofs:// URL")
	}
	for _, binding := range r.Bindings {
		if !namePattern.MatchString(binding.Partition) {
			return fmt.Errorf("invalid partition name %q", binding.Partition)
		}
		if binding.Path != "" && !strings.HasPrefix(binding.Path, "/") {
			return fmt.Errorf("binding %s: path must be an absolute logical path", binding.Partition)
		}
		if _, err := parseSelector(binding.Selector); err != nil {
			return fmt.Errorf("binding %s: %w", binding.Partition, err)
		}
	}
	return nil
}

// ObservedPartitionPath returns the default MonoFS payload path for a repo's
// generated observed bundle. Prefer RepoBinding.PayloadDir for per-partition
// paths.
func (r Repo) ObservedPartitionPath() string {
	if strings.TrimSpace(r.Path) != "" {
		return strings.TrimRight(strings.TrimSpace(r.Path), "/")
	}
	if partition := strings.TrimSpace(r.Partition); partition != "" {
		return "/partitions/" + partition + "/payloads/aws"
	}
	return ""
}
