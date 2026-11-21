// Package repoinit scaffolds the files Forge expects in a partition repository.
package repoinit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/repoconfig"
	"gopkg.in/yaml.v3"
)

// Options controls repository scaffolding.
type Options struct {
	Root      string
	Name      string
	Source    string
	Branch    string
	Partition string
	Force     bool
}

// Result lists the files written or updated.
type Result struct {
	Files []string `json:"files"`
}

// BindingOptions controls adding or updating one repository partition binding.
type BindingOptions struct {
	Root          string
	RepoName      string
	Source        string
	Branch        string
	Partition     string
	PartitionPath string
}

// EnsureBinding adds a partition binding to .guardian/config.yaml without
// discarding the repository's existing source, branch, or other bindings.
// It creates a minimal config when the repository does not have one yet.
func EnsureBinding(opts BindingOptions) (string, error) {
	root := opts.Root
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	partition := strings.TrimSpace(opts.Partition)
	if partition == "" {
		return "", fmt.Errorf("partition is required")
	}
	cfg, path, found, err := repoconfig.Load(root)
	if err != nil {
		return "", err
	}
	if !found || cfg == nil {
		cfg = &repoconfig.Config{APIVersion: "guardian/v1alpha1", Kind: "RepoConfig"}
		path = filepath.Join(root, repoconfig.FileName)
	}
	if strings.TrimSpace(cfg.Repo.Name) == "" {
		name := strings.TrimSpace(opts.RepoName)
		if name == "" {
			name = sanitizeName(filepath.Base(mustAbs(root)))
		}
		cfg.Repo.Name = name
	}
	if cfg.Repo.Source == "" {
		cfg.Repo.Source = strings.TrimSpace(opts.Source)
	}
	if cfg.Repo.Branch == "" {
		cfg.Repo.Branch = strings.TrimSpace(opts.Branch)
	}
	partitionPath := strings.TrimSpace(opts.PartitionPath)
	if partitionPath == "" {
		partitionPath = "partitions/" + partition
	}
	foundBinding := false
	for i := range cfg.Repo.Bindings {
		binding := &cfg.Repo.Bindings[i]
		if binding.Partition != partition {
			continue
		}
		foundBinding = true
		if strings.TrimSpace(binding.PartitionPath) == "" {
			binding.PartitionPath = partitionPath
		}
	}
	if !foundBinding {
		cfg.Repo.Bindings = append(cfg.Repo.Bindings, registry.RepoBinding{
			Partition:     partition,
			PartitionPath: partitionPath,
		})
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	name := strings.Trim(out.String(), "-")
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		name = "repo-" + name
	}
	return name
}

// Init writes the repository configuration, pipeline, and guardrail policy.
func Init(opts Options) (Result, error) {
	root := opts.Root
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	if strings.TrimSpace(opts.Name) == "" {
		return Result{}, fmt.Errorf("--name is required")
	}
	branch := opts.Branch
	if branch == "" {
		branch = "main"
	}

	files := map[string][]byte{}
	repo := repoconfig.RepoSpec{Name: opts.Name, Source: opts.Source, Branch: branch}
	if opts.Partition != "" {
		repo.Bindings = []registry.RepoBinding{{
			Partition:     opts.Partition,
			PartitionPath: "partitions/" + opts.Partition,
		}}
	}
	cfg := repoconfig.Config{APIVersion: "guardian/v1alpha1", Kind: "RepoConfig", Repo: repo}
	configData, err := yaml.Marshal(cfg)
	if err != nil {
		return Result{}, err
	}
	files[filepath.Join(".guardian", "config.yaml")] = configData

	files[filepath.Join(".monofs", "pipelines", "partition-pipeline.yml")] = []byte(pipelineConfig(branch))
	files[filepath.Join(".forge", "policy.yaml")] = []byte(guardrailPolicy())

	result := Result{}
	configPath := filepath.Join(root, repoconfig.FileName)
	if _, err := os.Stat(configPath); err == nil && !opts.Force {
		// Merge into the existing repository config instead of refusing to
		// run. A new partition must be able to register itself in a repo that
		// already has a .guardian/config.yaml.
		delete(files, filepath.Join(".guardian", "config.yaml"))
		if opts.Partition != "" {
			path, err := EnsureBinding(BindingOptions{
				Root: root, RepoName: opts.Name, Source: opts.Source, Branch: branch,
				Partition: opts.Partition,
			})
			if err != nil {
				return Result{}, err
			}
			result.Files = append(result.Files, path)
		}
	}
	for relative, data := range files {
		path := filepath.Join(root, relative)
		if _, err := os.Stat(path); err == nil && !opts.Force {
			// Keep the existing pipeline/policy file unless --force is set.
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return Result{}, err
		}
		result.Files = append(result.Files, path)
	}

	gitignore := filepath.Join(root, ".gitignore")
	updated, err := ensureGitignore(gitignore)
	if err != nil {
		return Result{}, err
	}
	if updated {
		result.Files = append(result.Files, gitignore)
	}
	return result, nil
}

func ensureGitignore(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(data), ".forge/") {
		return false, nil
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "\n# Forge local state\n.forge/\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func pipelineConfig(branch string) string {
	return fmt.Sprintf(`name: partition-pipeline

on:
  pull_request:
    branches: [%s]
    paths:
      - "partitions/**"
      - "access/**"
      - ".guardian/**"
  push:
    branches: [%s]

jobs:
  validate:
    runs-on: builder
    steps:
      - name: validate
        run: forge validate --all --strict
      - name: guardrails
        run: forge policy check --file .forge/policy.yaml
  plan:
    needs: [validate]
    runs-on: builder
    steps:
      - name: plan
        run: forge plan --format markdown
`, branch, branch)
}

func guardrailPolicy() string {
	return `version: 1
allowedPushers: [k8s-main, docker-local]
require:
  labels: [role, stack, component]
  secretRefs: true
  probesFor: [Compute]
  alertsFor: [Compute]
limits:
  replicas: 20
deny:
  images: ["*:latest"]
`
}
