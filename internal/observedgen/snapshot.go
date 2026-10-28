package observedgen

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/radryc/guardian/internal/cdktemplate"
	partitiondomain "github.com/radryc/guardian/internal/domain/partition"
	"gopkg.in/yaml.v3"
)

// SnapshotOptions configures a single-stack observed snapshot.
type SnapshotOptions struct {
	Partition string
	// Intent defaults to a slug of StackName.
	Intent    string
	StackName string
	Account   string
	Region    string
	Pusher    string
	// Template is the live CloudFormation template (JSON or YAML).
	Template string
	Interval string
	// ObserveOnly omits the adopted-origin labels so the UI reports the
	// partition as observe-only instead of adopted from AWS.
	ObserveOnly bool
}

// Snapshot builds an observed partition bundle for one live CloudFormation
// stack. The returned paths are relative to the partition directory so the
// caller can place them under guardian/partitions/<name>/ in a repository.
//
// The desired assembly is the live template itself, so the stack reconciles as
// Observed immediately after the repository is imported; users then replace
// the snapshot with their authored CDK app.
func Snapshot(opts SnapshotOptions) (map[string][]byte, error) {
	if strings.TrimSpace(opts.Partition) == "" {
		return nil, fmt.Errorf("partition is required")
	}
	if strings.TrimSpace(opts.StackName) == "" {
		return nil, fmt.Errorf("stack name is required")
	}
	if strings.TrimSpace(opts.Pusher) == "" {
		return nil, fmt.Errorf("pusher is required")
	}
	template, err := normalizeTemplate(opts.Template)
	if err != nil {
		return nil, err
	}
	interval := strings.TrimSpace(opts.Interval)
	if interval == "" {
		interval = "1m"
	}

	slug := Slug(opts.StackName)
	intentName := strings.TrimSpace(opts.Intent)
	if intentName == "" {
		intentName = slug
	}
	payloadDir := "/partitions/" + opts.Partition + "/payloads/aws"
	files := map[string][]byte{}

	labels := map[string]string(nil)
	if !opts.ObserveOnly {
		// Marks the partition as an adopted existing stack so the UI can
		// distinguish it from observe-only snapshots.
		labels = map[string]string{
			"guardian.io/origin":        "adopted",
			"guardian.io/adopted-stack": opts.StackName,
		}
	}
	partitionManifest := partitiondomain.Partition{
		APIVersion: "guardian/v1alpha1",
		Kind:       "Partition",
		Metadata: partitiondomain.Metadata{
			Name:   opts.Partition,
			Labels: labels,
		},
		Spec: partitiondomain.Spec{
			DeletionPolicy: "orphan",
			Reconciliation: partitiondomain.ReconciliationSpec{Mode: "auto", Interval: interval},
		},
	}
	partitionYAML, err := yaml.Marshal(partitionManifest)
	if err != nil {
		return nil, err
	}
	files["config.yaml"] = partitionYAML

	payload := fmt.Sprintf(`sourceType: cdk-ts
prebuiltAssemblyDir: %s/cdk.out
stackName: %s
stackID: %s
packageManager: none
snapshot: true
`, payloadDir, opts.StackName, opts.StackName)
	files["payloads/aws/"+slug+".yaml"] = []byte(payload)

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
        aws: %s/%s.yaml
`, intentName, opts.Pusher, opts.Account, opts.Region, intentName, payloadDir, slug)
	files["intents/"+intentName+".yaml"] = []byte(intent)

	assembly := map[string]any{
		"version": "36.0.0",
		"artifacts": map[string]any{
			opts.StackName: map[string]any{
				"type":        "aws:cloudformation:stack",
				"environment": fmt.Sprintf("aws://%s/%s", opts.Account, opts.Region),
				"properties": map[string]any{
					"templateFile": opts.StackName + ".template.json",
					"stackName":    opts.StackName,
				},
			},
		},
	}
	manifestJSON, err := json.MarshalIndent(assembly, "", "  ")
	if err != nil {
		return nil, err
	}
	files["payloads/aws/cdk.out/manifest.json"] = append(manifestJSON, '\n')
	files["payloads/aws/cdk.out/"+opts.StackName+".template.json"] = template

	return files, nil
}

// normalizeTemplate accepts a CloudFormation template as JSON or YAML and
// returns indented JSON, which is what the observed differ compares.
func normalizeTemplate(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("template is empty")
	}
	normalized, err := cdktemplate.NormalizeTemplate(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	return normalized, nil
}

// SnapshotPaths returns the sorted file paths of a snapshot (test helper).
func SnapshotPaths(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for path := range files {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
