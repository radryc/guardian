// Package onboard materializes Guardian observed partitions from the
// onboarding descriptors published by customer CDK stacks. It runs in the
// Guardian control plane, pulls descriptors from the account pusher, and never
// mutates the customer AWS account.
package onboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/dashboardgen"
	"github.com/radryc/guardian/internal/observed"
	"github.com/radryc/guardian/internal/observedgen"
	"github.com/radryc/guardian/internal/onboarding"
	"github.com/radryc/guardian/internal/orchestrator/dispatcher"
	"github.com/radryc/guardian/internal/orchestrator/reconciler"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/pusher/probes"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/repoconfig"
	"github.com/radryc/guardian/internal/workspacepub"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// Service materializes observed partitions from pusher onboarding descriptors.
type Service struct {
	Store     guardianapi.Store
	Registry  *registry.Store
	Resolver  observed.EndpointResolver
	Publisher Publisher
	Now       func() time.Time
}

// Publisher commits a generated partition bundle to a Guardian-owned repo.
type Publisher interface {
	Publish(ctx context.Context, req workspacepub.PublishRequest) (*workspacepub.PublishResult, error)
}

// Options filters the onboarding run.
type Options struct {
	// Pusher limits the run to one registered pusher.
	Pusher string
	// Stack limits the run to one CloudFormation stack.
	Stack string
	// Partition limits the run to one Guardian partition.
	Partition string
	// Reconcile triggers the observed reconciliation loop after writing.
	Reconcile bool
}

// Result reports one materialized stack.
type Result struct {
	Pusher    string `json:"pusher"`
	Stack     string `json:"stack"`
	Partition string `json:"partition"`
	Intent    string `json:"intent"`
	Probes    int    `json:"probes"`
	Error     string `json:"error,omitempty"`
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Run discovers descriptors, writes observed partition bundles and probe
// snapshots, and optionally reconciles the affected partitions.
func (s *Service) Run(ctx context.Context, opts Options) ([]Result, error) {
	if s == nil || s.Store == nil {
		return nil, fmt.Errorf("onboard: store is required")
	}
	if s.Registry == nil {
		return nil, fmt.Errorf("onboard: registry is required")
	}
	if s.Resolver == nil {
		return nil, fmt.Errorf("onboard: pusher resolver is required")
	}
	pushers, err := s.Registry.ListPushers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pushers: %w", err)
	}
	selected := make([]registry.Pusher, 0, len(pushers))
	for _, pusher := range pushers {
		if filter := strings.TrimSpace(opts.Pusher); filter != "" && pusher.Name != filter {
			continue
		}
		if strings.TrimSpace(pusher.ObservedAPIURL) == "" {
			continue
		}
		selected = append(selected, pusher)
	}
	if len(selected) == 0 {
		if filter := strings.TrimSpace(opts.Pusher); filter != "" {
			return nil, fmt.Errorf("pusher %s is not registered or has no observed API URL", filter)
		}
		return nil, fmt.Errorf("no registered pushers with an observed API URL")
	}

	writes := make([]guardianapi.PathWrite, 0)
	partitions := map[string]struct{}{}
	dashboards := map[string]*dashboardgen.Info{}
	configSeen := map[string]bool{}
	results := make([]Result, 0)
	errs := make([]error, 0)

	for _, pusher := range selected {
		endpoint, ok := s.Resolver.Resolve(pusher.Name)
		if !ok || endpoint.Client == nil {
			errs = append(errs, fmt.Errorf("pusher %s has no observed API client", pusher.Name))
			continue
		}
		descriptors, err := endpoint.Client.Onboarding(ctx, pusher.Account, pusher.Region)
		if err != nil {
			errs = append(errs, fmt.Errorf("pusher %s: list onboarding descriptors: %w", pusher.Name, err))
			continue
		}
		partitionsForPusher := map[string]struct{}{}
		for _, descriptor := range descriptors {
			if filter := strings.TrimSpace(opts.Stack); filter != "" && descriptor.Stack != filter {
				continue
			}
			if filter := strings.TrimSpace(opts.Partition); filter != "" && descriptor.Partition != filter {
				continue
			}
			result, descriptorWrites, err := s.materialize(ctx, pusher, endpoint, descriptor)
			if err != nil {
				result = Result{
					Pusher:    pusher.Name,
					Stack:     descriptor.Stack,
					Partition: descriptor.Partition,
					Intent:    descriptor.Intent,
					Error:     err.Error(),
				}
				errs = append(errs, fmt.Errorf("%s/%s: %w", pusher.Name, descriptor.Stack, err))
			} else {
				for _, write := range descriptorWrites {
					if strings.HasSuffix(write.LogicalPath, "/config.yaml") {
						if configSeen[descriptor.Partition] {
							continue
						}
						configSeen[descriptor.Partition] = true
					}
					writes = append(writes, write)
				}
				if descriptor.Publish != nil && strings.EqualFold(strings.TrimSpace(descriptor.Publish.Mode), "repo") {
					if err := s.publishRepo(ctx, descriptor, descriptorWrites); err != nil {
						result.Error = err.Error()
						errs = append(errs, fmt.Errorf("%s/%s: publish repo: %w", pusher.Name, descriptor.Stack, err))
					}
				}
				partitions[descriptor.Partition] = struct{}{}
				partitionsForPusher[descriptor.Partition] = struct{}{}
				info := dashboards[descriptor.Partition]
				if info == nil {
					info = &dashboardgen.Info{Partition: descriptor.Partition, Account: resultAccount(descriptor, pusher), Region: resultRegion(descriptor, pusher)}
					dashboards[descriptor.Partition] = info
				}
				info.Stacks = appendUnique(info.Stacks, descriptor.Stack)
				if descriptor.Telemetry != nil {
					if strings.TrimSpace(descriptor.Telemetry.LogGroup) != "" {
						info.LogGroup = descriptor.Telemetry.LogGroup
					}
					if strings.TrimSpace(descriptor.Telemetry.Source) != "" {
						info.Source = descriptor.Telemetry.Source
					}
				}
			}
			results = append(results, result)
		}
		for partition := range partitionsForPusher {
			probeWrites, probeCount, err := s.probeSnapshot(ctx, pusher, endpoint, partition)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s/%s: probes: %w", pusher.Name, partition, err))
				continue
			}
			if probeCount > 0 {
				writes = append(writes, probeWrites...)
			}
			for idx := range results {
				if results[idx].Pusher == pusher.Name && results[idx].Partition == partition {
					results[idx].Probes = probeCount
				}
			}
		}
	}

	for partition, info := range dashboards {
		raw, err := dashboardgen.Render(*info)
		if err != nil {
			errs = append(errs, fmt.Errorf("dashboard %s: %w", partition, err))
			continue
		}
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath: paths.DashboardRecord(partition),
			Content:     append(raw, '\n'),
		})
	}

	if len(writes) > 0 {
		if _, err := s.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: writes,
			Context: guardianapi.MutationContext{
				PrincipalID:   "guardianctl",
				Reason:        "materialize observed customer onboarding",
				CorrelationID: fmt.Sprintf("onboard-%d", s.now().UnixNano()),
			},
		}); err != nil {
			return results, fmt.Errorf("write onboarding bundles: %w", err)
		}
	}

	if opts.Reconcile {
		names := make([]string, 0, len(partitions))
		for partition := range partitions {
			names = append(names, partition)
		}
		sort.Strings(names)
		disp := dispatcher.NewDispatcher(s.Store, "guardianctl")
		recon := reconciler.NewReconciler(s.Store, disp, time.Minute)
		for _, partition := range names {
			if err := recon.ReconcilePartition(ctx, partition, true); err != nil {
				errs = append(errs, fmt.Errorf("reconcile %s: %w", partition, err))
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Pusher != results[j].Pusher {
			return results[i].Pusher < results[j].Pusher
		}
		return results[i].Stack < results[j].Stack
	})
	return results, errors.Join(errs...)
}

func (s *Service) materialize(
	ctx context.Context,
	pusher registry.Pusher,
	endpoint observed.Endpoint,
	descriptor onboarding.Descriptor,
) (Result, []guardianapi.PathWrite, error) {
	result := Result{
		Pusher:    pusher.Name,
		Stack:     descriptor.Stack,
		Partition: descriptor.Partition,
		Intent:    descriptor.Intent,
	}
	account := firstNonEmpty(descriptor.Account, pusher.Account)
	region := firstNonEmpty(descriptor.Region, pusher.Region)
	if account == "" || region == "" {
		return result, nil, fmt.Errorf("account and region are required (descriptor or pusher registration)")
	}
	template, err := endpoint.Client.Template(ctx, account, region, descriptor.Stack)
	if err != nil {
		return result, nil, fmt.Errorf("fetch live template: %w", err)
	}
	files, err := observedgen.Snapshot(observedgen.SnapshotOptions{
		Partition:   descriptor.Partition,
		Intent:      descriptor.Intent,
		StackName:   descriptor.Stack,
		Account:     account,
		Region:      region,
		Pusher:      pusher.Name,
		Template:    template,
		ObserveOnly: true,
	})
	if err != nil {
		return result, nil, fmt.Errorf("build observed snapshot: %w", err)
	}
	if result.Intent == "" {
		result.Intent = observedgen.Slug(descriptor.Stack)
	}

	writes := make([]guardianapi.PathWrite, 0, len(files)+1)
	configPath := paths.PartitionConfig(descriptor.Partition)
	if _, err := s.Store.ReadFile(ctx, configPath); err == nil {
		delete(files, "config.yaml")
	}
	for rel, content := range files {
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath: paths.PartitionPath(descriptor.Partition, rel),
			Content:     content,
		})
	}
	raw, err := json.MarshalIndent(descriptor, "", "  ")
	if err != nil {
		return result, nil, err
	}
	writes = append(writes, guardianapi.PathWrite{
		LogicalPath: paths.OnboardingRecord(pusher.Name, descriptor.Stack),
		Content:     append(raw, '\n'),
	})
	return result, writes, nil
}

func (s *Service) probeSnapshot(
	ctx context.Context,
	pusher registry.Pusher,
	endpoint observed.Endpoint,
	partition string,
) ([]guardianapi.PathWrite, int, error) {
	results, err := endpoint.Client.Probes(ctx, partition)
	if err != nil {
		return nil, 0, err
	}
	if len(results) == 0 {
		return nil, 0, nil
	}
	snapshot := probes.Snapshot{
		APIVersion: "guardian/v1alpha1",
		Kind:       "ProbeSnapshot",
		Pusher:     pusher.Name,
		Partition:  partition,
		UpdatedAt:  s.now(),
		Probes:     results,
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, 0, err
	}
	return []guardianapi.PathWrite{{
		LogicalPath: paths.ProbeSnapshot(pusher.Name, partition),
		Content:     append(raw, '\n'),
	}}, len(results), nil
}

func (s *Service) publishRepo(
	ctx context.Context,
	descriptor onboarding.Descriptor,
	writes []guardianapi.PathWrite,
) error {
	if s.Publisher == nil {
		return fmt.Errorf("repo publish requested but no workspace publisher is configured")
	}
	repoName := strings.TrimSpace(descriptor.Publish.Repo)
	if repoName == "" {
		return fmt.Errorf("publish.repo is required for mode=repo")
	}
	repo, err := s.Registry.GetRepo(ctx, repoName)
	if err != nil {
		return fmt.Errorf("read repo %s: %w", repoName, err)
	}
	displayPath := repoconfig.DisplayPathFromSource(repo.Source)
	if displayPath == "" {
		return fmt.Errorf("repo %s has no usable source URL", repoName)
	}
	prefix := paths.PartitionRoot(descriptor.Partition) + "/"
	operations := make([]workspacepub.Operation, 0, len(writes))
	for _, write := range writes {
		if !strings.HasPrefix(write.LogicalPath, prefix) {
			continue
		}
		relative := strings.TrimPrefix(write.LogicalPath, prefix)
		operations = append(operations, workspacepub.Operation{
			Kind:    workspacepub.OperationUpsert,
			Path:    "guardian/partitions/" + descriptor.Partition + "/" + relative,
			Content: write.Content,
		})
	}
	if len(operations) == 0 {
		return nil
	}
	branch := strings.TrimSpace(descriptor.Publish.Branch)
	if branch == "" {
		branch = repo.Branch
	}
	_, err = s.Publisher.Publish(ctx, workspacepub.PublishRequest{
		WorkspaceID:    "guardian-onboard-" + descriptor.Partition,
		BranchStrategy: workspacepub.StrategyPerRepoBranch,
		CommitMessage:  fmt.Sprintf("Guardian onboarding: %s (%s)", descriptor.Stack, descriptor.Partition),
		AuthorName:     "Guardian",
		AuthorEmail:    "guardian@local",
		Repository: workspacepub.Repository{
			StorageID:   repo.Name,
			DisplayPath: displayPath,
			RepoURL:     repo.Source,
			Branch:      branch,
			Operations:  operations,
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func resultAccount(descriptor onboarding.Descriptor, pusher registry.Pusher) string {
	return firstNonEmpty(descriptor.Account, pusher.Account)
}

func resultRegion(descriptor onboarding.Descriptor, pusher registry.Pusher) string {
	return firstNonEmpty(descriptor.Region, pusher.Region)
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
