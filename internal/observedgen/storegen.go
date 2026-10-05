package observedgen

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

// StoreOptions configures a store-native observed bundle ingest.
type StoreOptions struct {
	// AssemblyDir is the logical MonoFS path of the CDK cloud assembly.
	AssemblyDir string
	// Partition is the target Guardian partition name.
	Partition string
	// Account and Region identify the target AWS environment.
	Account string
	Region  string
	// Pusher is the target pusher name (default aws-<account>).
	Pusher string
	// PayloadDir is the logical payload root (default /partitions/<partition>/payloads/aws).
	PayloadDir string
	// Interval is the partition reconciliation interval (default 1m).
	Interval string
}

// GenerateToStore reads a cloud assembly from the store and writes the observed
// partition bundle (config, intents, payload manifests and templates) back into
// the store. It is the server-side ingest path used by the UI and CLI.
func GenerateToStore(ctx context.Context, store guardianapi.Store, opts StoreOptions) ([]string, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	assemblyDir := strings.TrimRight(strings.TrimSpace(opts.AssemblyDir), "/")
	if assemblyDir == "" || !strings.HasPrefix(assemblyDir, "/") {
		return nil, fmt.Errorf("assemblyDir must be an absolute logical path")
	}
	if strings.TrimSpace(opts.Partition) == "" {
		return nil, fmt.Errorf("partition is required")
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

	manifestContent, err := store.ReadFile(ctx, path.Join(assemblyDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read assembly manifest: %w", err)
	}
	stacks, err := parseAssemblyStacks(manifestContent)
	if err != nil {
		return nil, err
	}

	writes := make([]guardianapi.PathWrite, 0, len(stacks)*4+4)
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
	writes = append(writes, guardianapi.PathWrite{LogicalPath: paths.PartitionConfig(opts.Partition), Content: partitionYAML})

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
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath: payloadPath,
			Content:     []byte(payload),
		})
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
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath: paths.IntentManifest(opts.Partition, intentName),
			Content:     []byte(intent),
		})
		if stack.TemplateFile != "" {
			templateContent, readErr := store.ReadFile(ctx, path.Join(assemblyDir, stack.TemplateFile))
			if readErr != nil {
				return nil, fmt.Errorf("read template %s: %w", stack.TemplateFile, readErr)
			}
			writes = append(writes, guardianapi.PathWrite{
				LogicalPath: path.Join(payloadDir, "cdk.out", stack.TemplateFile),
				Content:     templateContent,
			})
		}
		created = append(created, intentName)
	}
	writes = append(writes, guardianapi.PathWrite{
		LogicalPath: path.Join(payloadDir, "cdk.out", "manifest.json"),
		Content:     manifestContent,
	})
	if assetsFiles, err := copyStoreAssemblyExtras(ctx, store, assemblyDir, payloadDir); err == nil {
		writes = append(writes, assetsFiles...)
	}

	if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: writes,
		Context: guardianapi.MutationContext{
			PrincipalID:   "guardiand",
			Reason:        "ingest observed partition bundle",
			CorrelationID: fmt.Sprintf("observed-ingest-%d", time.Now().UnixNano()),
		},
	}); err != nil {
		return nil, err
	}
	return created, nil
}

func parseAssemblyStacks(content []byte) ([]assemblyStack, error) {
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
		return nil, fmt.Errorf("assembly contains no CloudFormation stacks")
	}
	sort.Slice(stacks, func(i, j int) bool { return stacks[i].Name < stacks[j].Name })
	return stacks, nil
}

func copyStoreAssemblyExtras(ctx context.Context, store guardianapi.Store, assemblyDir, payloadDir string) ([]guardianapi.PathWrite, error) {
	entries, err := store.ListDir(ctx, assemblyDir)
	if err != nil {
		return nil, err
	}
	writes := make([]guardianapi.PathWrite, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".assets.json") {
			continue
		}
		content, err := store.ReadFile(ctx, path.Join(assemblyDir, entry.Name))
		if err != nil {
			continue
		}
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath: path.Join(payloadDir, "cdk.out", entry.Name),
			Content:     content,
		})
	}
	return writes, nil
}
