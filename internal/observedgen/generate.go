// Package observedgen generates observed partition bundles from a synthesized
// CDK cloud assembly. The bundle can be pushed to MonoFS with
// `guardianctl partition push` or by the repo sync command.
package observedgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	"gopkg.in/yaml.v3"
)

var (
	slugSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronymSplit  = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
)

// Options configures bundle generation.
type Options struct {
	// AssemblyDir is the local CDK cloud assembly directory (cdk.out).
	AssemblyDir string
	// OutDir is the local partition bundle directory to create.
	OutDir string
	// Partition is the target Guardian partition name.
	Partition string
	// Account and Region identify the target AWS environment.
	Account string
	Region  string
	// Pusher is the target pusher name (default aws-<account>).
	Pusher string
	// PayloadDir is the logical MonoFS path for the generated payloads.
	// Defaults to /partitions/<partition>/payloads/aws.
	PayloadDir string
	// Interval is the partition reconciliation interval (default 1m).
	Interval string
}

type assemblyManifest struct {
	Artifacts map[string]struct {
		Type       string `json:"type"`
		Properties struct {
			TemplateFile string `json:"templateFile"`
			StackName    string `json:"stackName"`
		} `json:"properties"`
	} `json:"artifacts"`
}

type assemblyStack struct {
	Name         string
	TemplateFile string
}

// Generate writes an observed partition bundle and returns the intent names.
func Generate(opts Options) ([]string, error) {
	if strings.TrimSpace(opts.AssemblyDir) == "" || strings.TrimSpace(opts.OutDir) == "" ||
		strings.TrimSpace(opts.Partition) == "" {
		return nil, fmt.Errorf("assembly, out and partition are required")
	}
	pusher := strings.TrimSpace(opts.Pusher)
	if pusher == "" {
		pusher = "aws-" + strings.TrimSpace(opts.Account)
	}
	payloadDir := strings.TrimRight(strings.TrimSpace(opts.PayloadDir), "/")
	if payloadDir == "" {
		payloadDir = "/partitions/" + opts.Partition + "/payloads/aws"
	}
	interval := strings.TrimSpace(opts.Interval)
	if interval == "" {
		interval = "1m"
	}

	stacks, err := readAssemblyStacks(opts.AssemblyDir)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Join(opts.OutDir, "intents"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(opts.OutDir, "payloads", "aws"), 0o755); err != nil {
		return nil, err
	}
	partitionManifest := partitiondomain.Partition{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Partition",
		Metadata:   partitiondomain.Metadata{Name: opts.Partition},
		Spec: partitiondomain.Spec{
			DeletionPolicy: "orphan",
			Reconciliation: partitiondomain.ReconciliationSpec{Mode: "auto", Interval: interval},
		},
	}
	partitionYAML, err := yaml.Marshal(partitionManifest)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(opts.OutDir, "config.yaml"), partitionYAML, 0o644); err != nil {
		return nil, err
	}

	created := make([]string, 0, len(stacks))
	for _, stack := range stacks {
		intentName := Slug(stack.Name)
		payloadPath := payloadDir + "/" + intentName + ".yaml"
		payload := fmt.Sprintf(`sourceType: cdk-ts
prebuiltAssemblyDir: %s/cdk.out
stackName: %s
stackID: %s
packageManager: none
`, payloadDir, stack.Name, stack.Name)
		if err := os.WriteFile(filepath.Join(opts.OutDir, "payloads", "aws", intentName+".yaml"), []byte(payload), 0o644); err != nil {
			return nil, err
		}
		intent := fmt.Sprintf(`apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: %s
spec:
  intentType: observed
  targetPusher: %s
  target:
    account: "%s"
    region: %s
  assets:
    - type: CDKStack
      name: %s
      payload:
        aws: %s
`, intentName, pusher, opts.Account, opts.Region, intentName, payloadPath)
		if err := os.WriteFile(filepath.Join(opts.OutDir, "intents", intentName+".yaml"), []byte(intent), 0o644); err != nil {
			return nil, err
		}
		created = append(created, intentName)
	}
	if err := copyAssemblyTemplates(opts.AssemblyDir, filepath.Join(opts.OutDir, "payloads", "aws", "cdk.out")); err != nil {
		return nil, fmt.Errorf("copy assembly templates: %w", err)
	}
	return created, nil
}

func readAssemblyStacks(assemblyDir string) ([]assemblyStack, error) {
	content, err := os.ReadFile(filepath.Join(assemblyDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read assembly manifest: %w", err)
	}
	var manifest assemblyManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("parse assembly manifest: %w", err)
	}
	stacks := make([]assemblyStack, 0, len(manifest.Artifacts))
	for id, artifact := range manifest.Artifacts {
		if artifact.Type != "aws:cloudformation:stack" {
			continue
		}
		stackName := strings.TrimSpace(artifact.Properties.StackName)
		if stackName == "" {
			stackName = strings.TrimSpace(id)
		}
		if stackName == "" {
			continue
		}
		stacks = append(stacks, assemblyStack{Name: stackName, TemplateFile: artifact.Properties.TemplateFile})
	}
	if len(stacks) == 0 {
		return nil, fmt.Errorf("assembly %s contains no CloudFormation stacks", assemblyDir)
	}
	sort.Slice(stacks, func(i, j int) bool { return stacks[i].Name < stacks[j].Name })
	return stacks, nil
}

// copyAssemblyTemplates copies only the cloud assembly manifest and stack
// template files. Observed intents never deploy, so asset payloads (which can
// be many gigabytes of Docker context) are not needed.
func copyAssemblyTemplates(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name != "manifest.json" && !strings.HasSuffix(name, ".template.json") && !strings.HasSuffix(name, ".assets.json") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, name), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Slug converts a CloudFormation stack name into a valid Guardian intent name.
func Slug(value string) string {
	slug := strings.TrimSpace(value)
	slug = acronymSplit.ReplaceAllString(slug, "${1}-${2}")
	slug = camelBoundary.ReplaceAllString(slug, "${1}-${2}")
	slug = strings.ToLower(slug)
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = slugSanitizer.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "stack"
	}
	if slug[0] < 'a' || slug[0] > 'z' {
		slug = "stack-" + slug
	}
	return slug
}
