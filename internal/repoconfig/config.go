// Package repoconfig parses the declarative Guardian config that lives in an
// ingested repository (.guardian/config.yaml). It is the repo-side source of
// truth for the pusher registration and the observed partition binding, so a
// merge to the repository can drive registration + ingest without manual CLI
// steps.
package repoconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/registry"
	"gopkg.in/yaml.v3"
)

// FileName is the canonical repo config path relative to the repository root.
const FileName = ".guardian/config.yaml"

var (
	altFileNames = []string{FileName, "guardian.yaml", ".guardian.yaml"}
	namePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// Config is the repository-side Guardian configuration.
type Config struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Pusher     PusherSpec `yaml:"pusher,omitempty"`
	Repo       RepoSpec   `yaml:"repo,omitempty"`
}

// PusherSpec declares the AWS pusher registration.
type PusherSpec struct {
	Name           string            `yaml:"name,omitempty"`
	Account        string            `yaml:"account,omitempty"`
	Region         string            `yaml:"region,omitempty"`
	ObservedAPIURL string            `yaml:"observedApiUrl,omitempty"`
	Token          string            `yaml:"token,omitempty"`
	TokenEnv       string            `yaml:"tokenEnv,omitempty"`
	Description    string            `yaml:"description,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty"`
}

// RepoSpec declares the partition binding and repository source.
type RepoSpec struct {
	Name           string `yaml:"name,omitempty"`
	Partition      string `yaml:"partition,omitempty"`
	Source         string `yaml:"source,omitempty"`
	Branch         string `yaml:"branch,omitempty"`
	AssemblySubdir string `yaml:"assemblySubdir,omitempty"`
	Path           string `yaml:"path,omitempty"`
	Account        string `yaml:"account,omitempty"`
	Region         string `yaml:"region,omitempty"`
	Description    string `yaml:"description,omitempty"`
}

// Load reads the repo config from dir. found=false means no config file exists.
func Load(dir string) (*Config, string, bool, error) {
	for _, name := range altFileNames {
		path := filepath.Join(dir, filepath.FromSlash(name))
		content, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, "", false, err
		}
		config, err := Parse(content)
		if err != nil {
			return nil, path, true, err
		}
		return config, path, true, nil
	}
	return nil, "", false, nil
}

// Parse decodes and validates a repo config.
func Parse(content []byte) (*Config, error) {
	var config Config
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("parse repo config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

// Validate checks required fields.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("repo config is nil")
	}
	if c.APIVersion == "" {
		c.APIVersion = "guardian/v1alpha1"
	}
	if c.Kind == "" {
		c.Kind = "RepoConfig"
	}
	if c.Pusher.Name != "" && !namePattern.MatchString(c.Pusher.Name) {
		return fmt.Errorf("invalid pusher name %q", c.Pusher.Name)
	}
	if c.Repo.Name != "" && !namePattern.MatchString(c.Repo.Name) {
		return fmt.Errorf("invalid repo name %q", c.Repo.Name)
	}
	if c.Repo.Partition != "" && !namePattern.MatchString(c.Repo.Partition) {
		return fmt.Errorf("invalid partition name %q", c.Repo.Partition)
	}
	if c.Repo.Name == "" && c.Repo.Partition != "" {
		return fmt.Errorf("repo.name is required when repo.partition is set")
	}
	return nil
}

// PusherRegistration converts the config into a registry record.
func (c *Config) PusherRegistration() *registry.Pusher {
	if c == nil || c.Pusher.Name == "" {
		return nil
	}
	return &registry.Pusher{
		Name:           c.Pusher.Name,
		Account:        c.Pusher.Account,
		Region:         c.Pusher.Region,
		ObservedAPIURL: c.Pusher.ObservedAPIURL,
		Token:          c.Pusher.Token,
		TokenEnv:       c.Pusher.TokenEnv,
		Description:    c.Pusher.Description,
		Labels:         c.Pusher.Labels,
	}
}

// RepoRegistration converts the config into a registry record, using
// sourceOverride when the config does not declare a source.
func (c *Config) RepoRegistration(sourceOverride string) *registry.Repo {
	if c == nil || c.Repo.Name == "" {
		return nil
	}
	source := strings.TrimSpace(c.Repo.Source)
	if source == "" {
		source = strings.TrimSpace(sourceOverride)
	}
	return &registry.Repo{
		Name:           c.Repo.Name,
		Pusher:         c.Pusher.Name,
		Partition:      c.Repo.Partition,
		Source:         source,
		Branch:         c.Repo.Branch,
		AssemblySubdir: c.Repo.AssemblySubdir,
		Path:           c.Repo.Path,
		Account:        c.Repo.Account,
		Region:         c.Repo.Region,
		Description:    c.Repo.Description,
	}
}
