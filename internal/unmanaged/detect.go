// Package unmanaged detects CloudFormation stacks that are visible through a
// pusher but not covered by a repository-backed Guardian partition, and builds
// the repo-association bundle that turns them into observed partitions.
//
// Detection is level-triggered from store content plus the pusher read API:
//
//   - stacks covered by an observed/generated intent AND a repo binding are
//     fully managed and not listed;
//   - stacks covered by an intent but not by any repo binding are listed as
//     "missing-repo" (associate a repo to the existing partition);
//   - stacks with no intent at all are listed as "unmanaged" (associate a repo
//     to create the partition snapshot).
package unmanaged

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/awsobserve"
	"github.com/rydzu/ainfra/guardian/internal/cdkgen"
	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

// Stack states reported by Detect.
const (
	StateUnmanaged   = "unmanaged"
	StateMissingRepo = "missing-repo"
)

// StackLister lists live stacks for one pusher (implemented by
// *observed.Client).
type StackLister interface {
	ListStacks(ctx context.Context, account, region string) ([]awsobserve.StackState, error)
}

// StackRefresher forces a live DescribeStacks on the pusher. Discovery must
// refresh first: the pusher's cache only holds stacks it has already observed,
// so a stack created outside Guardian would otherwise never show up.
type StackRefresher interface {
	Refresh(ctx context.Context, account, region string, stacks []string, detectDrift bool) ([]awsobserve.StackState, error)
}

// Pusher pairs a pusher registration with its read API client.
type Pusher struct {
	Name    string
	Account string
	Region  string
	Client  StackLister
}

// Stack is one detected stack that needs a repository.
type Stack struct {
	State       string `json:"state"`
	Pusher      string `json:"pusher"`
	Account     string `json:"account,omitempty"`
	Region      string `json:"region,omitempty"`
	StackName   string `json:"stackName"`
	StackStatus string `json:"stackStatus,omitempty"`
	DriftStatus string `json:"driftStatus,omitempty"`
	// Partition is set for missing-repo stacks: the existing partition that
	// manages the stack but has no repository binding.
	Partition string `json:"partition,omitempty"`
}

// Result is the detection outcome.
type Result struct {
	Stacks   []Stack  `json:"stacks"`
	Warnings []string `json:"warnings,omitempty"`
}

// Detect lists stacks per pusher and classifies those that need a repository.
// isInfrastructureStack reports whether a stack belongs to the CDK bootstrap
// or the Guardian control plane rather than to a user workload, so discovery
// does not list it as "needing a repository".
func isInfrastructureStack(name string) bool {
	switch strings.TrimSpace(name) {
	case "CDKToolkit", "StrataStack", "AwsPusherStack", "AwsObservedPusherStack":
		return true
	}
	return strings.HasPrefix(name, "cdk-hnb659fds")
}

func Detect(ctx context.Context, store guardianapi.ReadStore, pushers []Pusher) (*Result, error) {
	if store == nil {
		return nil, fmt.Errorf("store is required")
	}
	managed, partitionRepos, warnings := scanManaged(ctx, store)
	result := &Result{Warnings: warnings}

	for _, pusher := range pushers {
		if pusher.Client == nil {
			continue
		}
		if refresher, ok := pusher.Client.(StackRefresher); ok {
			// Empty stack list = refresh every stack in the account/region.
			if _, err := refresher.Refresh(ctx, pusher.Account, pusher.Region, nil, false); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("pusher %s refresh: %v", pusher.Name, err))
			}
		}
		stacks, err := pusher.Client.ListStacks(ctx, pusher.Account, pusher.Region)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("pusher %s: %v", pusher.Name, err))
			continue
		}
		for _, state := range stacks {
			if isInfrastructureStack(state.StackName) {
				continue
			}
			region := strings.TrimSpace(state.Region)
			if region == "" {
				region = pusher.Region
			}
			key := stackKey(region, state.StackName)
			entry := Stack{
				Pusher:      pusher.Name,
				Account:     firstNonEmpty(state.Account, pusher.Account),
				Region:      region,
				StackName:   state.StackName,
				StackStatus: state.StackStatus,
				DriftStatus: state.DriftStatus,
			}
			partition, isManaged := managed[key]
			if !isManaged {
				entry.State = StateUnmanaged
				result.Stacks = append(result.Stacks, entry)
				continue
			}
			if _, hasRepo := partitionRepos[partition]; hasRepo {
				continue
			}
			entry.State = StateMissingRepo
			entry.Partition = partition
			result.Stacks = append(result.Stacks, entry)
		}
	}
	sort.Slice(result.Stacks, func(i, j int) bool {
		if result.Stacks[i].State != result.Stacks[j].State {
			return result.Stacks[i].State < result.Stacks[j].State
		}
		if result.Stacks[i].StackName != result.Stacks[j].StackName {
			return result.Stacks[i].StackName < result.Stacks[j].StackName
		}
		return result.Stacks[i].Pusher < result.Stacks[j].Pusher
	})
	return result, nil
}

// scanManaged returns stack key -> partition for every intent-covered stack,
// plus partition -> repo name for every repo binding.
func scanManaged(ctx context.Context, store guardianapi.ReadStore) (map[string]string, map[string]string, []string) {
	managed := map[string]string{}
	partitionRepos := map[string]string{}
	warnings := []string{}

	repos, err := store.ListDir(ctx, "/.registry/repos")
	if err != nil && !isNotExist(err) {
		warnings = append(warnings, fmt.Sprintf("list repos: %v", err))
	}
	for _, entry := range repos {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		raw, err := store.ReadFile(ctx, "/.registry/repos/"+entry.Name)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("read repo %s: %v", entry.Name, err))
			continue
		}
		var repo registry.Repo
		if err := json.Unmarshal(raw, &repo); err != nil {
			warnings = append(warnings, fmt.Sprintf("parse repo %s: %v", entry.Name, err))
			continue
		}
		for _, binding := range repo.BindingList() {
			if _, ok := partitionRepos[binding.Partition]; !ok {
				partitionRepos[binding.Partition] = repo.Name
			}
		}
	}

	partitions, err := store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		if !isNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("list partitions: %v", err))
		}
		return managed, partitionRepos, warnings
	}
	for _, partition := range partitions {
		if !partition.IsDir {
			continue
		}
		intentEntries, err := store.ListDir(ctx, paths.PartitionIntentsDir(partition.Name))
		if err != nil {
			if !isNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("list intents for %s: %v", partition.Name, err))
			}
			continue
		}
		for _, intentEntry := range intentEntries {
			if intentEntry.IsDir || (!strings.HasSuffix(intentEntry.Name, ".yaml") && !strings.HasSuffix(intentEntry.Name, ".yml")) {
				continue
			}
			raw, err := store.ReadFile(ctx, paths.PartitionIntentsDir(partition.Name)+"/"+intentEntry.Name)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("read intent %s/%s: %v", partition.Name, intentEntry.Name, err))
				continue
			}
			intent, err := manifest.ParseIntent(raw)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("parse intent %s/%s: %v", partition.Name, intentEntry.Name, err))
				continue
			}
			region := strings.TrimSpace(intent.Spec.Target.Region)
			switch {
			case intent.Spec.IntentType == intentdomain.TypeObserved:
				for _, asset := range intent.Spec.Assets {
					if asset.Type != assetdomain.TypeCDKStack {
						continue
					}
					stackName := observedStackName(ctx, store, asset)
					if stackName == "" {
						continue
					}
					managed[stackKey(region, stackName)] = partition.Name
				}
			case intent.Spec.Implementation == intentdomain.ImplementationCDK:
				stackName := cdkgen.StackName(partition.Name, intent.Metadata.Name)
				managed[stackKey(region, stackName)] = partition.Name
			}
		}
	}
	return managed, partitionRepos, warnings
}

// observedStackName reads the payload manifest of a CDKStack asset and returns
// its stackName.
func observedStackName(ctx context.Context, store guardianapi.ReadStore, asset assetdomain.Spec) string {
	payloadPath := strings.TrimSpace(asset.Payload["aws"])
	if payloadPath == "" {
		return ""
	}
	raw, err := store.ReadFile(ctx, payloadPath)
	if err != nil {
		return ""
	}
	var payload struct {
		StackName string `yaml:"stackName"`
	}
	if err := yaml.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.StackName)
}

func stackKey(region, stackName string) string {
	return strings.ToLower(strings.TrimSpace(region)) + "|" + strings.ToLower(strings.TrimSpace(stackName))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") || strings.Contains(message, "no such file") || strings.Contains(message, "does not exist")
}
