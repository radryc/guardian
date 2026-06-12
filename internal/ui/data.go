package ui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	manifestpkg "github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"github.com/rydzu/ainfra/guardian/internal/compiler/planner"
	"github.com/rydzu/ainfra/guardian/internal/compiler/resolver"
	validatorpkg "github.com/rydzu/ainfra/guardian/internal/compiler/validator"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/historyquery"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type OverviewResponse struct {
	GeneratedAt time.Time          `json:"generatedAt"`
	Summary     DashboardSummary   `json:"summary"`
	Partitions  []PartitionSummary `json:"partitions"`
}

type DashboardSummary struct {
	Partitions        int `json:"partitions"`
	Intents           int `json:"intents"`
	Assets            int `json:"assets"`
	HealthyAssets     int `json:"healthyAssets"`
	AttentionAssets   int `json:"attentionAssets"`
	FailingAssets     int `json:"failingAssets"`
	HealthyIntents    int `json:"healthyIntents"`
	DriftedIntents    int `json:"driftedIntents"`
	FailedIntents     int `json:"failedIntents"`
	ServicesHealthy   int `json:"servicesHealthy"`
	ServicesAttention int `json:"servicesAttention"`
}

type PartitionSummary struct {
	Name          string            `json:"name"`
	Labels        map[string]string `json:"labels,omitempty"`
	Status        string            `json:"status"`
	DisplayStatus string            `json:"displayStatus"`
	Health        string            `json:"health"`
	// LastHealth and LastDisplayStatus reflect the resolved-asset state only
	// (ignoring pending assets). Used in the overview tile so transient
	// "Progressing" flicker is hidden; details still show the live status.
	LastHealth        string    `json:"lastHealth"`
	LastDisplayStatus string    `json:"lastDisplayStatus"`
	DeletionPolicy    string    `json:"deletionPolicy"`
	Reconciliation    string    `json:"reconciliation"`
	IntentCount       int       `json:"intentCount"`
	AssetCount        int       `json:"assetCount"`
	HealthyAssets     int       `json:"healthyAssets"`
	AttentionAssets   int       `json:"attentionAssets"`
	FailingAssets     int       `json:"failingAssets"`
	HealthyIntents    int       `json:"healthyIntents"`
	DriftedIntents    int       `json:"driftedIntents"`
	FailedIntents     int       `json:"failedIntents"`
	ServicesHealthy   int       `json:"servicesHealthy"`
	ServicesAttention int       `json:"servicesAttention"`
	LastReconciledAt  time.Time `json:"lastReconciledAt"`
	LastDeploymentAt  time.Time `json:"lastDeploymentAt"`
	Errors            []string  `json:"errors,omitempty"`
}

type PartitionDetailResponse struct {
	GeneratedAt   time.Time         `json:"generatedAt"`
	CompilerError string            `json:"compilerError,omitempty"`
	Partition     PartitionDocument `json:"partition"`
	Health        PartitionHealth   `json:"health"`
	Intents       []IntentDocument  `json:"intents"`
	Topology      TopologyData      `json:"topology"`
	RecentEvents  []TimelineEntry   `json:"recentEvents"`
}

type PartitionDocument struct {
	Manifest          partitiondomain.Partition   `json:"manifest"`
	ManifestVersionID string                      `json:"manifestVersionID"`
	State             *statedomain.PartitionState `json:"state,omitempty"`
}

type PartitionHealth struct {
	Status        string          `json:"status"`
	DisplayStatus string          `json:"displayStatus"`
	Summary       string          `json:"summary"`
	Healthy       int             `json:"healthy"`
	Attention     int             `json:"attention"`
	Failing       int             `json:"failing"`
	Pending       int             `json:"pending"`
	Services      []ServiceHealth `json:"services"`
}

type ServiceHealth struct {
	ID            string    `json:"id"`
	Intent        string    `json:"intent"`
	Asset         string    `json:"asset"`
	Type          string    `json:"type"`
	Status        string    `json:"status"`
	DisplayStatus string    `json:"displayStatus"`
	Summary       string    `json:"summary"`
	Target        string    `json:"target"`
	TaskActive    bool      `json:"taskActive"`
	TaskTimedOut  bool      `json:"taskTimedOut"`
	Ports         []string  `json:"ports,omitempty"`
	Replicas      int       `json:"replicas,omitempty"`
	LastUpdatedAt time.Time `json:"lastUpdatedAt,omitempty"`
}

type IntentDocument struct {
	Name              string                        `json:"name"`
	Manifest          intentdomain.Intent           `json:"manifest"`
	ManifestVersionID string                        `json:"manifestVersionID"`
	State             *statedomain.IntentState      `json:"state,omitempty"`
	ObservedHealth    *taskdomain.HealthObservation `json:"observedHealth,omitempty"`
	ApplyReadiness    *taskdomain.ApplyReadiness    `json:"applyReadiness,omitempty"`
	Status            string                        `json:"status"`
	DisplayStatus     string                        `json:"displayStatus"`
	Health            string                        `json:"health"`
	Summary           string                        `json:"summary"`
	TaskActive        bool                          `json:"taskActive"`
	TaskTimedOut      bool                          `json:"taskTimedOut"`
	TargetSummary     string                        `json:"targetSummary"`
	Joined            []string                      `json:"joined"`
	Locked            bool                          `json:"locked"`
	Outputs           map[string]string             `json:"outputs,omitempty"`
	OutputHints       []assetdefs.CatalogHint       `json:"outputHints,omitempty"`
	Assets            []AssetDocument               `json:"assets"`
	LastDeployment    *DeploymentSummary            `json:"lastDeployment,omitempty"`
	LastUpdatedAt     time.Time                     `json:"lastUpdatedAt,omitempty"`
}

type AssetDocument struct {
	ID             string                        `json:"id"`
	Name           string                        `json:"name"`
	Type           string                        `json:"type"`
	Version        string                        `json:"version,omitempty"`
	DependsOn      []string                      `json:"dependsOn"`
	ObservedHealth *taskdomain.HealthObservation `json:"observedHealth,omitempty"`
	ApplyReadiness *taskdomain.ApplyReadiness    `json:"applyReadiness,omitempty"`
	Status         string                        `json:"status"`
	DisplayStatus  string                        `json:"displayStatus"`
	Health         string                        `json:"health"`
	Summary        string                        `json:"summary"`
	TaskActive     bool                          `json:"taskActive"`
	TaskTimedOut   bool                          `json:"taskTimedOut"`
	Outputs        map[string]string             `json:"outputs,omitempty"`
	QuickFacts     []Fact                        `json:"quickFacts,omitempty"`
	References     []string                      `json:"references,omitempty"`
	Service        bool                          `json:"service"`
	Ports          []string                      `json:"ports,omitempty"`
	Replicas       int                           `json:"replicas,omitempty"`
	Properties     map[string]any                `json:"properties,omitempty"`
	Payload        map[string]string             `json:"payload,omitempty"`
	Hints          []assetdefs.CatalogHint       `json:"hints,omitempty"`
}

type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type DeploymentSummary struct {
	DeploymentRevision string            `json:"deploymentRevision"`
	CreatedAt          time.Time         `json:"createdAt"`
	Target             string            `json:"target"`
	TaskIDs            []string          `json:"taskIDs"`
	Outputs            map[string]string `json:"outputs,omitempty"`
}

type TopologyData struct {
	Partition string         `json:"partition"`
	Nodes     []TopologyNode `json:"nodes"`
	Edges     []TopologyEdge `json:"edges"`
}

type TopologyNode struct {
	ID            string            `json:"id"`
	Label         string            `json:"label"`
	Kind          string            `json:"kind"`
	ParentID      string            `json:"parentID,omitempty"`
	Intent        string            `json:"intent,omitempty"`
	Asset         string            `json:"asset,omitempty"`
	AssetType     string            `json:"assetType,omitempty"`
	Status        string            `json:"status"`
	DisplayStatus string            `json:"displayStatus"`
	Health        string            `json:"health"`
	Level         int               `json:"level"`
	Description   string            `json:"description,omitempty"`
	Meta          map[string]string `json:"meta,omitempty"`
}

type TopologyEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Kind  string `json:"kind"`
	Label string `json:"label,omitempty"`
}

type PartitionHistoryResponse struct {
	GeneratedAt time.Time        `json:"generatedAt"`
	Events      []TimelineEntry  `json:"events"`
	Deployments []DeploymentView `json:"deployments"`
}

type PartitionRolloutsResponse struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	Rollouts    []RolloutView `json:"rollouts"`
}

type RolloutView struct {
	DeploymentRevision string             `json:"deploymentRevision"`
	Intent             string             `json:"intent"`
	CreatedAt          time.Time          `json:"createdAt"`
	Target             string             `json:"target"`
	TaskIDs            []string           `json:"taskIDs,omitempty"`
	Current            bool               `json:"current,omitempty"`
	NewIntent          bool               `json:"newIntent,omitempty"`
	SelfHealing        bool               `json:"selfHealing,omitempty"`
	Summary            string             `json:"summary"`
	Assets             []RolloutAssetView `json:"assets"`
}

type RolloutAssetView struct {
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Version string `json:"version,omitempty"`
	Change  string `json:"change"`
}

type PartitionHistoryOptions struct {
	LimitPerIntent int
	Since          *time.Time
	Until          *time.Time
}

type TimelineEntry struct {
	Kind               string            `json:"kind"`
	Timestamp          time.Time         `json:"timestamp"`
	Intent             string            `json:"intent,omitempty"`
	Asset              string            `json:"asset,omitempty"`
	Status             string            `json:"status"`
	DisplayStatus      string            `json:"displayStatus"`
	Title              string            `json:"title"`
	Message            string            `json:"message"`
	DeploymentRevision string            `json:"deploymentRevision,omitempty"`
	TaskID             string            `json:"taskID,omitempty"`
	Details            map[string]string `json:"details,omitempty"`
}

type DeploymentView struct {
	DeploymentRevision string            `json:"deploymentRevision"`
	Intent             string            `json:"intent"`
	CreatedAt          time.Time         `json:"createdAt"`
	Target             string            `json:"target"`
	TaskIDs            []string          `json:"taskIDs"`
	ChangedAssets      []string          `json:"changedAssets,omitempty"`
	Outputs            map[string]string `json:"outputs,omitempty"`
	Assets             []AssetDeployment `json:"assets"`
}

type AssetDeployment struct {
	Asset         string                `json:"asset"`
	Version       string                `json:"version,omitempty"`
	Status        string                `json:"status"`
	DisplayStatus string                `json:"displayStatus"`
	Summary       string                `json:"summary"`
	Logs          []taskdomain.LogEntry `json:"logs"`
	Outputs       map[string]string     `json:"outputs,omitempty"`
}

type IntentActivityResponse struct {
	GeneratedAt    time.Time                     `json:"generatedAt"`
	Partition      string                        `json:"partition"`
	Intent         string                        `json:"intent"`
	Status         string                        `json:"status"`
	DisplayStatus  string                        `json:"displayStatus"`
	Summary        string                        `json:"summary"`
	TaskActive     bool                          `json:"taskActive"`
	TaskTimedOut   bool                          `json:"taskTimedOut"`
	ObservedHealth *taskdomain.HealthObservation `json:"observedHealth,omitempty"`
	ApplyReadiness *taskdomain.ApplyReadiness    `json:"applyReadiness,omitempty"`
	LastError      string                        `json:"lastError,omitempty"`
	LastTaskID     string                        `json:"lastTaskID,omitempty"`
	LastOp         string                        `json:"lastOp,omitempty"`
	Timestamps     *statedomain.StateTimestamps  `json:"timestamps,omitempty"`
	Drift          *taskdomain.DriftReport       `json:"drift,omitempty"`
	Logs           []taskdomain.LogEntry         `json:"logs"`
}

type intentTaskRuntime struct {
	Active   bool
	TimedOut bool
}

const defaultStaleTaskAfter = 5 * time.Minute

type CatalogResponse struct {
	Pushers    []string                    `json:"pushers"`
	AssetTypes []assetdefs.CatalogTemplate `json:"assetTypes"`
}

type AssetTemplate = assetdefs.CatalogTemplate

type BuilderField = assetdefs.CatalogField

type SaveBundleRequest struct {
	Partition                  partitiondomain.Partition `json:"partition"`
	PartitionExpectedVersionID string                    `json:"partitionExpectedVersionID,omitempty"`
	Intents                    []SaveIntentRequest       `json:"intents"`
	RemoveMissingIntents       bool                      `json:"removeMissingIntents,omitempty"`
}

type SaveIntentRequest struct {
	Manifest          intentdomain.Intent `json:"manifest"`
	ExpectedVersionID string              `json:"expectedVersionID,omitempty"`
}

type SaveBundleResponse struct {
	Success            bool              `json:"success"`
	BatchRevisionID    string            `json:"batchRevisionID"`
	PartitionVersionID string            `json:"partitionVersionID,omitempty"`
	IntentVersionIDs   map[string]string `json:"intentVersionIDs,omitempty"`
	RemovedIntents     []string          `json:"removedIntents,omitempty"`
}

func (s *Server) buildOverview(ctx context.Context) (*OverviewResponse, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	names, err := s.partitionNames(ctx)
	if err != nil {
		return nil, err
	}
	response := &OverviewResponse{
		GeneratedAt: time.Now().UTC(),
		Partitions:  make([]PartitionSummary, 0, len(names)),
	}
	type overviewResult struct {
		summary PartitionSummary
		loaded  bool
	}
	results := make([]overviewResult, len(names))
	parallelism := len(names)
	if parallelism > 8 {
		parallelism = 8
	}
	if parallelism < 1 {
		parallelism = 1
	}
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for idx, name := range names {
		wg.Add(1)
		go func(index int, partitionName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			detail, err := s.loadPartitionOverviewDetail(ctx, partitionName)
			if err != nil {
				results[index] = overviewResult{summary: PartitionSummary{
					Name:          partitionName,
					Status:        "Error",
					DisplayStatus: "Unavailable",
					Health:        "failing",
					Errors:        []string{err.Error()},
				}}
				return
			}
			results[index] = overviewResult{summary: summarizePartition(detail), loaded: true}
		}(idx, name)
	}
	wg.Wait()
	for _, result := range results {
		response.Partitions = append(response.Partitions, result.summary)
		if !result.loaded {
			continue
		}
		summary := result.summary
		response.Summary.Partitions++
		response.Summary.Intents += summary.IntentCount
		response.Summary.Assets += summary.AssetCount
		response.Summary.HealthyAssets += summary.HealthyAssets
		response.Summary.AttentionAssets += summary.AttentionAssets
		response.Summary.FailingAssets += summary.FailingAssets
		response.Summary.HealthyIntents += summary.HealthyIntents
		response.Summary.DriftedIntents += summary.DriftedIntents
		response.Summary.FailedIntents += summary.FailedIntents
		response.Summary.ServicesHealthy += summary.ServicesHealthy
		response.Summary.ServicesAttention += summary.ServicesAttention
	}
	sort.Slice(response.Partitions, func(i, j int) bool {
		return response.Partitions[i].Name < response.Partitions[j].Name
	})
	return response, nil
}

func summarizePartition(detail *PartitionDetailResponse) PartitionSummary {
	summary := PartitionSummary{
		Name:             detail.Partition.Manifest.Metadata.Name,
		Labels:           mergeStringMaps(detail.Partition.Manifest.Metadata.Labels, detail.Partition.Manifest.Spec.Labels),
		Status:           detail.Health.Status,
		DisplayStatus:    detail.Health.DisplayStatus,
		Health:           detail.Health.Status,
		DeletionPolicy:   detail.Partition.Manifest.Spec.DeletionPolicy,
		Reconciliation:   detail.Partition.Manifest.Spec.Reconciliation.Mode,
		IntentCount:      len(detail.Intents),
		LastReconciledAt: time.Time{},
	}
	if detail.Partition.State != nil {
		summary.LastReconciledAt = detail.Partition.State.LastReconciledAt
		if detail.Partition.State.Status != "" {
			summary.Status = detail.Partition.State.Status
		}
		summary.Errors = append(summary.Errors, detail.Partition.State.Errors...)
	}
	if detail.CompilerError != "" {
		summary.Errors = append(summary.Errors, detail.CompilerError)
	}
	for _, intent := range detail.Intents {
		summary.AssetCount += len(intent.Assets)
		switch intent.Health {
		case "healthy":
			summary.HealthyIntents++
		case "attention":
			summary.DriftedIntents++
		case "failing":
			summary.FailedIntents++
		}
		if intent.LastDeployment != nil && intent.LastDeployment.CreatedAt.After(summary.LastDeploymentAt) {
			summary.LastDeploymentAt = intent.LastDeployment.CreatedAt
		}
	}
	summary.HealthyAssets = detail.Health.Healthy
	summary.AttentionAssets = detail.Health.Attention + detail.Health.Pending
	summary.FailingAssets = detail.Health.Failing

	// Compute last stable state based on resolved assets only (ignore pending).
	// This is shown in the overview tile so a brief reconcile pass doesn't
	// replace the known good/bad state with "Progressing".
	switch {
	case detail.Health.Failing > 0:
		summary.LastHealth = "failing"
		summary.LastDisplayStatus = "Needs action"
	case detail.Health.Attention > 0:
		summary.LastHealth = "attention"
		summary.LastDisplayStatus = "Attention"
	case detail.Health.Healthy > 0:
		summary.LastHealth = "healthy"
		summary.LastDisplayStatus = "Stable"
	default:
		// No resolved assets at all — keep the live status (first deploy).
		summary.LastHealth = summary.Health
		summary.LastDisplayStatus = summary.DisplayStatus
	}

	for _, service := range detail.Health.Services {
		if service.Status == "healthy" {
			summary.ServicesHealthy++
			continue
		}
		summary.ServicesAttention++
	}
	return summary
}

func (s *Server) loadPartitionDetail(ctx context.Context, partitionName string) (*PartitionDetailResponse, error) {
	return s.loadPartitionDataCached(ctx, partitionName, false, false)
}

func (s *Server) loadPartitionOverviewDetail(ctx context.Context, partitionName string) (*PartitionDetailResponse, error) {
	return s.loadPartitionDataCached(ctx, partitionName, false, false)
}

func (s *Server) loadPartitionDataCached(ctx context.Context, partitionName string, includeLatestDeployments, includeRecentEvents bool) (*PartitionDetailResponse, error) {
	cacheKey := fmt.Sprintf("%s|latest=%t|events=%t", partitionName, includeLatestDeployments, includeRecentEvents)
	if cached, ok := s.partitionData.get(cacheKey, time.Now().UTC()); ok {
		return cached, nil
	}
	data, err := s.loadPartitionData(ctx, partitionName, includeLatestDeployments, includeRecentEvents)
	if err != nil {
		return nil, err
	}
	s.partitionData.set(cacheKey, time.Now().UTC(), data)
	return data, nil
}

func (s *Server) loadPartitionData(ctx context.Context, partitionName string, includeLatestDeployments, includeRecentEvents bool) (*PartitionDetailResponse, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	configPath := paths.PartitionConfig(partitionName)
	configContent, err := s.store.ReadFile(ctx, configPath)
	if err != nil {
		return nil, err
	}
	partitionManifest, err := parsePartition(configContent)
	if err != nil {
		return nil, err
	}
	configInfo, err := s.store.Stat(ctx, configPath)
	if err != nil {
		return nil, err
	}

	intentSources, err := s.intentManifestSources(ctx, partitionName)
	if err != nil {
		return nil, err
	}
	intentNames := make([]string, 0, len(intentSources))
	intentContents := make(map[string][]byte, len(intentSources))
	intentVersions := make(map[string]string, len(intentSources))
	intentModTimes := make(map[string]time.Time, len(intentSources))
	intentManifests := make(map[string]*intentdomain.Intent, len(intentSources))
	for _, source := range intentSources {
		intentNames = append(intentNames, source.Name)
		intentContents[source.Name] = source.Content
		intentVersions[source.Name] = source.VersionID
		intentModTimes[source.Name] = source.ModTime
		intentManifests[source.Name] = source.Manifest
	}

	partitionState, err := loadPartitionState(ctx, s.store, partitionName)
	if err != nil {
		return nil, err
	}
	intentStates, err := loadIntentStates(ctx, s.store, partitionName)
	if err != nil {
		return nil, err
	}
	intentRuntimes, err := s.intentTaskRuntimes(ctx, intentStates, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	compiled, compileErr := planner.Compile(ctx, planner.CompileInput{
		PartitionName:    partitionName,
		ConfigContent:    configContent,
		IntentContents:   intentContents,
		IntentVersionIDs: intentVersions,
		IntentModTimes:   intentModTimes,
		ConfigVersionID:  configInfo.VersionID,
		CurrentOutputs:   common.IntentOutputs(intentStates),
	})

	latestDeployments := map[string]*DeploymentView{}
	if includeLatestDeployments {
		latestDeployments, err = s.latestDeploymentsByIntent(ctx, partitionName, intentNames)
		if err != nil {
			return nil, err
		}
	}

	var recentEvents []TimelineEntry
	if includeRecentEvents {
		recentEvents, err = s.recentEvents(ctx, partitionName, 8)
		if err != nil {
			return nil, err
		}
	}

	order := append([]string(nil), intentNames...)
	if compiled != nil && len(compiled.IntentOrder) > 0 {
		order = append([]string(nil), compiled.IntentOrder...)
	}
	intents := make([]IntentDocument, 0, len(order))
	for _, name := range order {
		manifest := intentManifests[name]
		if manifest == nil {
			continue
		}
		assetOrder := manifestAssetOrder(manifest.Spec.Assets)
		if compiled != nil {
			if compiledIntent := compiled.Intents[name]; compiledIntent != nil && len(compiledIntent.AssetOrder) > 0 {
				assetOrder = mergeAssetOrder(compiledIntent.AssetOrder, manifest.Spec.Assets)
			}
		}
		intents = append(intents, buildIntentDocument(*manifest, intentVersions[name], intentStates[name], intentRuntimes[name], assetOrder, latestDeployments[name]))
	}

	health := buildPartitionHealth(intents, partitionState)
	return &PartitionDetailResponse{
		GeneratedAt:   time.Now().UTC(),
		CompilerError: errorString(compileErr),
		Partition: PartitionDocument{
			Manifest:          *partitionManifest,
			ManifestVersionID: configInfo.VersionID,
			State:             partitionState,
		},
		Health:       health,
		Intents:      intents,
		Topology:     buildTopology(*partitionManifest, intents),
		RecentEvents: recentEvents,
	}, nil
}

func (s *Server) loadPartitionHistory(ctx context.Context, partitionName string, opts PartitionHistoryOptions) (*PartitionHistoryResponse, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	if _, err := s.store.Stat(ctx, paths.PartitionConfig(partitionName)); err != nil {
		return nil, err
	}
	intentNames, err := s.intentNames(ctx, partitionName)
	if err != nil {
		return nil, err
	}
	deployments := make([]DeploymentView, 0)
	for _, intent := range intentNames {
		records, err := s.loadIntentDeployments(ctx, partitionName, intent, opts)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, records...)
	}
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].CreatedAt.After(deployments[j].CreatedAt)
	})
	events, err := s.events(ctx, partitionName, opts)
	if err != nil {
		return nil, err
	}
	return &PartitionHistoryResponse{
		GeneratedAt: time.Now().UTC(),
		Events:      events,
		Deployments: deployments,
	}, nil
}

func (s *Server) loadPartitionRollouts(ctx context.Context, partitionName string, opts PartitionHistoryOptions) (*PartitionRolloutsResponse, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	if _, err := s.store.Stat(ctx, paths.PartitionConfig(partitionName)); err != nil {
		return nil, err
	}
	rollouts, err := historyquery.LoadPartitionRollouts(ctx, s.store, partitionName, historyquery.DeploymentFilter{
		Limit: opts.LimitPerIntent,
		Since: opts.Since,
		Until: opts.Until,
	})
	if err != nil {
		return nil, err
	}
	views := make([]RolloutView, 0, len(rollouts))
	for _, rollout := range rollouts {
		views = append(views, buildRolloutView(rollout))
	}
	return &PartitionRolloutsResponse{
		GeneratedAt: time.Now().UTC(),
		Rollouts:    views,
	}, nil
}

func (s *Server) loadIntentActivity(ctx context.Context, partitionName, intentName string) (*IntentActivityResponse, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	var istate *statedomain.IntentState
	var rawState statedomain.IntentState
	rawData, err := s.store.ReadFile(ctx, paths.IntentState(partitionName, intentName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if jsonErr := json.Unmarshal(rawData, &rawState); jsonErr != nil {
			return nil, jsonErr
		}
		istate = &rawState
	}

	runtime, err := s.intentTaskRuntime(ctx, istate, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	status, displayStatus, _, summary := deriveIntentPresentation(istate, runtime)
	resp := &IntentActivityResponse{
		GeneratedAt:   time.Now().UTC(),
		Partition:     partitionName,
		Intent:        intentName,
		Status:        status,
		DisplayStatus: displayStatus,
		Summary:       summary,
		TaskActive:    runtime.Active,
		TaskTimedOut:  runtime.TimedOut,
		Logs:          []taskdomain.LogEntry{},
	}
	if istate != nil {
		resp.LastTaskID = istate.LastTaskID
		resp.Timestamps = &istate.Timestamps
		resp.Drift = istate.Drift
		resp.ObservedHealth = cloneHealthObservation(istate.Health)
		resp.ApplyReadiness = cloneApplyReadiness(istate.ApplyReadiness)
		if istate.LastError != nil {
			resp.LastError = *istate.LastError
		}
		if istate.LastTaskID != "" && istate.TargetPusher != "" {
			var result taskdomain.TaskResult
			resultData, readErr := s.store.ReadFile(ctx, paths.QueueResult(istate.TargetPusher, istate.LastTaskID))
			if readErr == nil {
				if jsonErr := json.Unmarshal(resultData, &result); jsonErr == nil {
					resp.Logs = result.Logs
					resp.LastOp = string(result.Op)
				}
			}
		}
	}
	return resp, nil
}

func (s *Server) partitionNames(ctx context.Context) ([]string, error) {
	entries, err := s.store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if _, err := s.store.Stat(ctx, paths.PartitionConfig(entry.Name)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *Server) intentNames(ctx context.Context, partitionName string) ([]string, error) {
	sources, err := s.intentManifestSources(ctx, partitionName)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.Name)
	}
	return names, nil
}

func loadPartitionState(ctx context.Context, store guardianapi.ReadStore, partitionName string) (*statedomain.PartitionState, error) {
	state, err := common.LoadPartitionState(ctx, store, partitionName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return state, nil
}

func loadIntentStates(ctx context.Context, store guardianapi.ReadStore, partitionName string) (map[string]*statedomain.IntentState, error) {
	states, err := common.LoadAllIntentStates(ctx, store, partitionName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]*statedomain.IntentState{}, nil
		}
		return nil, err
	}
	if states == nil {
		return map[string]*statedomain.IntentState{}, nil
	}
	return states, nil
}

func (s *Server) intentTaskRuntimes(ctx context.Context, states map[string]*statedomain.IntentState, now time.Time) (map[string]intentTaskRuntime, error) {
	out := make(map[string]intentTaskRuntime, len(states))
	for name, state := range states {
		runtime, err := s.intentTaskRuntime(ctx, state, now)
		if err != nil {
			return nil, err
		}
		out[name] = runtime
	}
	return out, nil
}

func (s *Server) intentTaskRuntime(ctx context.Context, state *statedomain.IntentState, now time.Time) (intentTaskRuntime, error) {
	active, err := common.HasActiveTask(ctx, s.store, state)
	if err != nil {
		return intentTaskRuntime{}, err
	}
	runtime := intentTaskRuntime{Active: active}
	if state != nil && active && !state.Timestamps.LastQueuedAt.IsZero() {
		timeQueueElapsed := now.Sub(state.Timestamps.LastQueuedAt)
		if timeQueueElapsed > s.staleTaskAfter {
			runtime.TimedOut = true
			log.Printf("[UI] Task for intent '%s' (taskID: %s) evaluates as timed out! Queued at: %v (%v ago), Stale timeout threshold: %v",
				state.Intent, state.LastTaskID, state.Timestamps.LastQueuedAt, timeQueueElapsed, s.staleTaskAfter)
		} else {
			log.Printf("[UI] Task for intent '%s' (taskID: %s) is currently active. Queued at: %v (%v ago), Stale timeout threshold: %v",
				state.Intent, state.LastTaskID, state.Timestamps.LastQueuedAt, timeQueueElapsed, s.staleTaskAfter)
		}
	}
	return runtime, nil
}

func buildIntentDocument(manifest intentdomain.Intent, versionID string, state *statedomain.IntentState, runtime intentTaskRuntime, assetOrder []string, latest *DeploymentView) IntentDocument {
	status, displayStatus, health, summary := deriveIntentPresentation(state, runtime)
	lastUpdatedAt := latestStateTimestamp(state)
	outputs := map[string]string{}
	if state != nil {
		outputs = copyStringMap(state.Outputs)
	}
	doc := IntentDocument{
		Name:              manifest.Metadata.Name,
		Manifest:          manifest,
		ManifestVersionID: versionID,
		State:             state,
		ObservedHealth:    cloneHealthObservation(stateHealth(state)),
		ApplyReadiness:    cloneApplyReadiness(stateApplyReadiness(state)),
		Status:            status,
		DisplayStatus:     displayStatus,
		Health:            health,
		Summary:           summary,
		TaskActive:        runtime.Active,
		TaskTimedOut:      runtime.TimedOut,
		TargetSummary:     formatTarget(resolveTarget(state, manifest.Spec.Target)),
		Joined:            append([]string(nil), manifest.Spec.Joins...),
		Locked:            manifest.Spec.Locked,
		Outputs:           outputs,
		OutputHints:       assetdefs.ResolveIntentOutputHints(manifest.Spec.Hints),
		LastUpdatedAt:     lastUpdatedAt,
	}
	if latest != nil {
		doc.LastDeployment = &DeploymentSummary{
			DeploymentRevision: latest.DeploymentRevision,
			CreatedAt:          latest.CreatedAt,
			Target:             latest.Target,
			TaskIDs:            append([]string(nil), latest.TaskIDs...),
			Outputs:            copyStringMap(latest.Outputs),
		}
		if latest.CreatedAt.After(doc.LastUpdatedAt) {
			doc.LastUpdatedAt = latest.CreatedAt
		}
	}

	specByName := map[string]assetdomain.Spec{}
	for _, asset := range manifest.Spec.Assets {
		specByName[asset.Name] = asset
	}
	for _, name := range assetOrder {
		spec, ok := specByName[name]
		if !ok {
			continue
		}
		doc.Assets = append(doc.Assets, buildAssetDocument(manifest.Metadata.Name, spec, manifest.Spec.Hints, state, runtime, latest))
	}
	return doc
}

func buildAssetDocument(intentName string, spec assetdomain.Spec, intentHints []assetdomain.Hint, state *statedomain.IntentState, runtime intentTaskRuntime, latest *DeploymentView) AssetDocument {
	status, displayStatus, health, summary := deriveAssetPresentation(state, spec.Name, runtime)
	assetSummary, facts, service, ports, replicas := assetFacts(spec)
	if summary == "" {
		summary = assetSummary
	}
	if summary == "" {
		summary = strings.ToLower(spec.Type) + " asset"
	}
	version := latestAssetVersion(latest, spec.Name)
	if version == "" {
		version = strings.TrimSpace(spec.Version)
	}
	if version != "" {
		facts = append([]Fact{{Label: "Release", Value: version}}, facts...)
	}
	refs := uniqueRefs(spec.Properties)
	return AssetDocument{
		ID:             intentName + "/" + spec.Name,
		Name:           spec.Name,
		Type:           spec.Type,
		Version:        version,
		DependsOn:      append([]string(nil), spec.DependsOn...),
		ObservedHealth: cloneHealthObservation(stateAssetHealth(state, spec.Name)),
		ApplyReadiness: cloneApplyReadiness(stateAssetApplyReadiness(state, spec.Name)),
		Status:         status,
		DisplayStatus:  displayStatus,
		Health:         health,
		Summary:        summary,
		TaskActive:     runtime.Active,
		TaskTimedOut:   runtime.TimedOut,
		Outputs:        assetOutputs(state, spec.Name),
		QuickFacts:     facts,
		References:     refs,
		Service:        service,
		Ports:          ports,
		Replicas:       replicas,
		Properties:     cloneMap(spec.Properties),
		Payload:        copyStringMap(spec.Payload),
		Hints:          assetdefs.ResolveAssetHints(spec.Type, spec.Name, spec.Hints, intentHints),
	}
}

func buildPartitionHealth(intents []IntentDocument, partitionState *statedomain.PartitionState) PartitionHealth {
	services := make([]ServiceHealth, 0)
	health := PartitionHealth{}
	assetCount := 0
	intentAttention := 0
	intentFailing := 0
	for _, intent := range intents {
		switch intent.Health {
		case "attention":
			intentAttention++
		case "failing":
			intentFailing++
		}
		for _, asset := range intent.Assets {
			assetCount++
			if asset.Service {
				service := ServiceHealth{
					ID:            intent.Name + "/" + asset.Name,
					Intent:        intent.Name,
					Asset:         asset.Name,
					Type:          asset.Type,
					Status:        asset.Health,
					DisplayStatus: asset.DisplayStatus,
					Summary:       asset.Summary,
					Target:        intent.TargetSummary,
					TaskActive:    asset.TaskActive,
					TaskTimedOut:  asset.TaskTimedOut,
					Ports:         append([]string(nil), asset.Ports...),
					Replicas:      asset.Replicas,
					LastUpdatedAt: intent.LastUpdatedAt,
				}
				services = append(services, service)
			}
			switch asset.Health {
			case "healthy":
				health.Healthy++
			case "attention":
				health.Attention++
			case "failing":
				health.Failing++
			default:
				health.Pending++
			}
		}
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].Intent == services[j].Intent {
			return services[i].Asset < services[j].Asset
		}
		return services[i].Intent < services[j].Intent
	})
	health.Services = services

	switch {
	case health.Failing > 0 || intentFailing > 0:
		health.Status = "failing"
		health.DisplayStatus = "Needs action"
	case health.Attention > 0 || intentAttention > 0:
		health.Status = "attention"
		health.DisplayStatus = "Attention"
	case health.Pending > 0:
		health.Status = "pending"
		health.DisplayStatus = "Progressing"
	default:
		health.Status = "healthy"
		health.DisplayStatus = "Stable"
	}
	if partitionState != nil && len(partitionState.Errors) > 0 {
		health.Status = "failing"
		health.DisplayStatus = "Invalid"
	}
	switch {
	case partitionState != nil && len(partitionState.Errors) > 0:
		health.Summary = formatCountSummary(len(partitionState.Errors), "partition error", "requires", "require", "attention")
	case assetCount == 0:
		health.Summary = "No assets modeled yet."
	case health.Status == "failing" && health.Failing == 0 && intentFailing > 0:
		health.Summary = formatCountSummary(intentFailing, "intent", "reports", "report", "an unhealthy live state")
	case health.Status == "attention" && health.Attention == 0 && intentAttention > 0:
		health.Summary = formatCountSummary(intentAttention, "intent", "needs", "need", "attention")
	case health.Status == "healthy":
		health.Summary = formatCountSummary(assetCount, "asset", "matches", "match", "desired state")
	case health.Status == "pending":
		health.Summary = formatCountSummary(health.Pending, "asset", "is", "are", "still progressing")
	case health.Status == "attention":
		health.Summary = formatCountSummary(health.Attention, "asset", "needs", "need", "attention")
	default:
		health.Summary = formatCountSummary(health.Failing, "asset", "is", "are", "failing")
	}
	return health
}

func formatCountSummary(count int, noun, singularVerb, pluralVerb, phrase string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s %s %s.", noun, singularVerb, phrase)
	}
	return fmt.Sprintf("%d %ss %s %s.", count, noun, pluralVerb, phrase)
}

func buildTopology(partition partitiondomain.Partition, intents []IntentDocument) TopologyData {
	partitionNodeID := "partition:" + partition.Metadata.Name
	intentLevels := computeIntentLevels(intents)
	nodes := []TopologyNode{{
		ID:            partitionNodeID,
		Label:         partition.Metadata.Name,
		Kind:          "partition",
		Status:        "healthy",
		DisplayStatus: "Partition",
		Health:        "healthy",
		Level:         0,
		Description:   "Guardian partition",
		Meta: map[string]string{
			"deletionPolicy": partition.Spec.DeletionPolicy,
			"reconciliation": partition.Spec.Reconciliation.Mode,
		},
	}}
	edges := make([]TopologyEdge, 0)
	for _, intent := range intents {
		intentNodeID := "intent:" + intent.Name
		nodes = append(nodes, TopologyNode{
			ID:            intentNodeID,
			Label:         intent.Name,
			Kind:          "intent",
			ParentID:      partitionNodeID,
			Intent:        intent.Name,
			Status:        intent.Status,
			DisplayStatus: intent.DisplayStatus,
			Health:        intent.Health,
			Level:         intentLevels[intent.Name] + 1,
			Description:   intent.Summary,
			Meta: map[string]string{
				"target": intent.TargetSummary,
				"locked": fmt.Sprintf("%t", intent.Locked),
			},
		})
		edges = append(edges, TopologyEdge{From: partitionNodeID, To: intentNodeID, Kind: "contains"})
		for _, join := range intent.Joined {
			edges = append(edges, TopologyEdge{
				From: "intent:" + join,
				To:   intentNodeID,
				Kind: "join",
			})
		}
		assetLevels := computeAssetLevels(intent.Assets)
		for _, asset := range intent.Assets {
			assetNodeID := "asset:" + intent.Name + ":" + asset.Name
			nodes = append(nodes, TopologyNode{
				ID:            assetNodeID,
				Label:         asset.Name,
				Kind:          "asset",
				ParentID:      intentNodeID,
				Intent:        intent.Name,
				Asset:         asset.Name,
				AssetType:     asset.Type,
				Status:        asset.Status,
				DisplayStatus: asset.DisplayStatus,
				Health:        asset.Health,
				Level:         intentLevels[intent.Name] + assetLevels[asset.Name] + 2,
				Description:   asset.Summary,
				Meta: map[string]string{
					"type": asset.Type,
				},
			})
			edges = append(edges, TopologyEdge{From: intentNodeID, To: assetNodeID, Kind: "contains"})
			for _, dep := range asset.DependsOn {
				edges = append(edges, TopologyEdge{
					From: "asset:" + intent.Name + ":" + dep,
					To:   assetNodeID,
					Kind: "dependsOn",
				})
			}
			for _, ref := range uniqueRefs(asset.Properties) {
				sourceIntent := strings.SplitN(ref, ".", 2)[0]
				edges = append(edges, TopologyEdge{
					From:  "intent:" + sourceIntent,
					To:    assetNodeID,
					Kind:  "outputRef",
					Label: ref,
				})
			}
		}
	}
	return TopologyData{
		Partition: partition.Metadata.Name,
		Nodes:     nodes,
		Edges:     edges,
	}
}

func computeIntentLevels(intents []IntentDocument) map[string]int {
	docs := map[string]IntentDocument{}
	for _, intent := range intents {
		docs[intent.Name] = intent
	}
	levels := map[string]int{}
	var visit func(name string, stack map[string]struct{}) int
	visit = func(name string, stack map[string]struct{}) int {
		if level, ok := levels[name]; ok {
			return level
		}
		if _, ok := stack[name]; ok {
			return 0
		}
		stack[name] = struct{}{}
		level := 0
		if intent, ok := docs[name]; ok {
			for _, dep := range intent.Joined {
				if candidate := visit(dep, stack) + 1; candidate > level {
					level = candidate
				}
			}
		}
		delete(stack, name)
		levels[name] = level
		return level
	}
	for name := range docs {
		visit(name, map[string]struct{}{})
	}
	return levels
}

func computeAssetLevels(assets []AssetDocument) map[string]int {
	docs := map[string]AssetDocument{}
	for _, asset := range assets {
		docs[asset.Name] = asset
	}
	levels := map[string]int{}
	var visit func(name string, stack map[string]struct{}) int
	visit = func(name string, stack map[string]struct{}) int {
		if level, ok := levels[name]; ok {
			return level
		}
		if _, ok := stack[name]; ok {
			return 0
		}
		stack[name] = struct{}{}
		level := 0
		if asset, ok := docs[name]; ok {
			for _, dep := range asset.DependsOn {
				if candidate := visit(dep, stack) + 1; candidate > level {
					level = candidate
				}
			}
		}
		delete(stack, name)
		levels[name] = level
		return level
	}
	for name := range docs {
		visit(name, map[string]struct{}{})
	}
	return levels
}

func (s *Server) latestDeploymentsByIntent(ctx context.Context, partitionName string, intentNames []string) (map[string]*DeploymentView, error) {
	out := make(map[string]*DeploymentView, len(intentNames))
	for _, intentName := range intentNames {
		deployments, err := s.loadIntentDeployments(ctx, partitionName, intentName, PartitionHistoryOptions{LimitPerIntent: 1})
		if err != nil {
			return nil, err
		}
		if len(deployments) > 0 {
			deployment := deployments[0]
			out[intentName] = &deployment
		}
	}
	return out, nil
}

func (s *Server) loadIntentDeployments(ctx context.Context, partitionName, intentName string, opts PartitionHistoryOptions) ([]DeploymentView, error) {
	records, err := historyquery.LoadDeploymentRecords(ctx, s.store, partitionName, intentName, historyquery.DeploymentFilter{
		Limit: opts.LimitPerIntent,
		Since: opts.Since,
		Until: opts.Until,
	})
	if err != nil {
		return nil, err
	}
	deployments := make([]DeploymentView, 0, len(records))
	for _, record := range records {
		logs, err := readLogEntries(ctx, s.store, paths.ArchiveLogs(partitionName, intentName, record.DeploymentRevision))
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, buildDeploymentView(record, logs))
	}
	return deployments, nil
}

func buildDeploymentView(record historydomain.DeploymentRecord, logs []taskdomain.LogEntry) DeploymentView {
	assets := map[string]*AssetDeployment{}
	for _, assetName := range sortedMapKeys(record.AssetVersionIDs) {
		assets[assetName] = &AssetDeployment{
			Asset:         assetName,
			Version:       strings.TrimSpace(record.AssetVersions[assetName]),
			Status:        "healthy",
			DisplayStatus: "Pushed",
			Summary:       "Apply completed",
			Logs:          []taskdomain.LogEntry{},
			Outputs:       map[string]string{},
		}
	}
	for _, entry := range logs {
		if strings.TrimSpace(entry.Asset) == "" {
			continue
		}
		item, ok := assets[entry.Asset]
		if !ok {
			item = &AssetDeployment{
				Asset:         entry.Asset,
				Version:       strings.TrimSpace(record.AssetVersions[entry.Asset]),
				Status:        "healthy",
				DisplayStatus: "Pushed",
				Summary:       entry.Message,
				Logs:          []taskdomain.LogEntry{},
				Outputs:       map[string]string{},
			}
			assets[entry.Asset] = item
		}
		item.Logs = append(item.Logs, entry)
		item.Summary = entry.Message
		if strings.EqualFold(entry.Level, "error") {
			item.Status = "failing"
			item.DisplayStatus = "Error"
		}
	}
	for key, value := range record.Outputs {
		assetName, outputKey, ok := splitAssetOutputKey(key)
		if !ok {
			continue
		}
		item, ok := assets[assetName]
		if !ok {
			item = &AssetDeployment{
				Asset:         assetName,
				Version:       strings.TrimSpace(record.AssetVersions[assetName]),
				Status:        "healthy",
				DisplayStatus: "Pushed",
				Summary:       "Apply completed",
				Logs:          []taskdomain.LogEntry{},
				Outputs:       map[string]string{},
			}
			assets[assetName] = item
		}
		item.Outputs[outputKey] = value
	}
	items := make([]AssetDeployment, 0, len(assets))
	for _, name := range sortedAssetDeploymentKeys(assets) {
		item := assets[name]
		if item == nil {
			continue
		}
		items = append(items, AssetDeployment{
			Asset:         item.Asset,
			Version:       item.Version,
			Status:        item.Status,
			DisplayStatus: item.DisplayStatus,
			Summary:       item.Summary,
			Logs:          append([]taskdomain.LogEntry(nil), item.Logs...),
			Outputs:       copyStringMap(item.Outputs),
		})
	}
	return DeploymentView{
		DeploymentRevision: record.DeploymentRevision,
		Intent:             record.Intent,
		CreatedAt:          record.CreatedAt,
		Target:             formatTarget(record.Target),
		TaskIDs:            append([]string(nil), record.TaskIDs...),
		ChangedAssets:      append([]string(nil), record.ChangedAssets...),
		Outputs:            copyStringMap(record.Outputs),
		Assets:             items,
	}
}

func buildRolloutView(record historyquery.RolloutRecord) RolloutView {
	assets := make([]RolloutAssetView, 0, len(record.Assets))
	for _, asset := range record.Assets {
		assets = append(assets, RolloutAssetView{
			Name:    asset.Name,
			Type:    asset.Type,
			Version: asset.Version,
			Change:  asset.Change,
		})
	}
	return RolloutView{
		DeploymentRevision: record.DeploymentRevision,
		Intent:             record.Intent,
		CreatedAt:          record.CreatedAt,
		Target:             formatTarget(record.Target),
		TaskIDs:            append([]string(nil), record.TaskIDs...),
		Current:            record.Current,
		NewIntent:          record.NewIntent,
		SelfHealing:        record.SelfHealing,
		Summary:            record.Summary,
		Assets:             assets,
	}
}

func latestAssetVersion(latest *DeploymentView, assetName string) string {
	if latest == nil {
		return ""
	}
	for _, asset := range latest.Assets {
		if asset.Asset == assetName {
			return strings.TrimSpace(asset.Version)
		}
	}
	return ""
}

func (s *Server) recentEvents(ctx context.Context, partitionName string, limit int) ([]TimelineEntry, error) {
	entries, err := s.store.ListDir(ctx, paths.StateEventsDir(partitionName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		files = append(files, entry.Name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	timeline := make([]TimelineEntry, 0, len(files))
	for _, name := range files {
		var event historydomain.EventRecord
		if err := readJSON(ctx, s.store, paths.EventState(partitionName, strings.TrimSuffix(name, ".json")), &event); err != nil {
			return nil, err
		}
		timeline = append(timeline, buildEventTimeline(event))
	}
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Timestamp.After(timeline[j].Timestamp)
	})
	return timeline, nil
}

func (s *Server) events(ctx context.Context, partitionName string, opts PartitionHistoryOptions) ([]TimelineEntry, error) {
	filter := historyquery.DeploymentFilter{Since: opts.Since, Until: opts.Until}
	entries, err := s.store.ListDir(ctx, paths.StateEventsDir(partitionName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	timeline := make([]TimelineEntry, 0)
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		var event historydomain.EventRecord
		if err := readJSON(ctx, s.store, paths.EventState(partitionName, strings.TrimSuffix(entry.Name, ".json")), &event); err != nil {
			return nil, err
		}
		if !filter.Match(event.CreatedAt) {
			continue
		}
		timeline = append(timeline, buildEventTimeline(event))
	}
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Timestamp.After(timeline[j].Timestamp)
	})
	return timeline, nil
}

func buildEventTimeline(event historydomain.EventRecord) TimelineEntry {
	status, display := classifyEvent(event)
	return TimelineEntry{
		Kind:               "event",
		Timestamp:          event.CreatedAt,
		Intent:             event.Intent,
		Status:             status,
		DisplayStatus:      display,
		Title:              humanizeWords(event.Type),
		Message:            event.Message,
		DeploymentRevision: event.DeploymentRevision,
		TaskID:             event.TaskID,
		Details:            copyStringMap(event.Details),
	}
}

func classifyEvent(event historydomain.EventRecord) (string, string) {
	switch event.Type {
	case "task.failed":
		return "failing", "Failed"
	case "drift.detected":
		return "attention", "Drift"
	case "deploy.completed":
		return "healthy", "Pushed"
	}
	status := strings.ToLower(strings.TrimSpace(event.Details["status"]))
	switch {
	case strings.Contains(status, "failed") || strings.Contains(strings.ToLower(event.Type), "error"):
		return "failing", "Error"
	case strings.Contains(status, "drifted") || strings.Contains(strings.ToLower(event.Type), "orphaned"):
		return "attention", "Attention"
	case strings.Contains(strings.ToLower(event.Type), "queued"):
		return "pending", "Queued"
	case strings.Contains(status, "healthy") || strings.Contains(strings.ToLower(event.Type), "archived"):
		return "healthy", "Stable"
	default:
		return "neutral", "Event"
	}
}

func deriveIntentPresentation(state *statedomain.IntentState, runtime intentTaskRuntime) (string, string, string, string) {
	if state == nil {
		return "Unknown", "Waiting", "pending", "Waiting for first reconcile"
	}
	if runtime.TimedOut {
		return "TimedOut", "Attention", "attention", "The latest reconcile task exceeded the worker lease window"
	}
	switch state.Status {
	case statedomain.StatusHealthy:
		if displayStatus, health, summary, ok := observedIntentHealthPresentation(state); ok {
			return string(state.Status), displayStatus, health, summary
		}
		return string(state.Status), "No diff", "healthy", "Desired and live state match"
	case statedomain.StatusChecking:
		return string(state.Status), "Checking", "pending", "Validating that drift can be applied safely"
	case statedomain.StatusDiffing:
		return string(state.Status), "Comparing", "pending", "Comparing desired and live state"
	case statedomain.StatusApplying:
		return string(state.Status), "Pushing", "pending", "Applying the latest blueprint"
	case statedomain.StatusDiffFailed, statedomain.StatusCheckFailed, statedomain.StatusApplyFailed:
		return string(state.Status), "Error", "failing", valueOrDefault(observedFailureSummary(state), valueOrDefault(pointerString(state.LastError), "The last task failed"))
	case statedomain.StatusDrifted:
		if _, _, observedSummary, ok := observedIntentHealthPresentation(state); ok {
			return string(state.Status), "Diff found", "attention", combineDriftSummary("Drift detected and waiting for push", observedSummary)
		}
		return string(state.Status), "Diff found", "attention", "Drift detected and waiting for push"
	case statedomain.StatusDriftedLocked:
		if _, _, observedSummary, ok := observedIntentHealthPresentation(state); ok {
			return string(state.Status), "Locked drift", "attention", combineDriftSummary("Drift detected but the intent is locked", observedSummary)
		}
		return string(state.Status), "Locked drift", "attention", "Drift detected but the intent is locked"
	case statedomain.StatusBlocked:
		return string(state.Status), "Blocked", "attention", "Waiting for joined intents to become healthy"
	case statedomain.StatusDestroying:
		return string(state.Status), "Removing", "pending", "Destroying provisioned assets"
	case statedomain.StatusDestroyed:
		return string(state.Status), "Removed", "pending", "Intent was destroyed"
	default:
		return string(state.Status), humanizeWords(string(state.Status)), "pending", humanizeWords(string(state.Status))
	}
}

func observedIntentHealthPresentation(state *statedomain.IntentState) (string, string, string, bool) {
	if state == nil || state.Health == nil {
		return "", "", "", false
	}
	summary := valueOrDefault(strings.TrimSpace(state.Health.Summary), "Live target health is unknown")
	switch state.Health.Status {
	case taskdomain.HealthUnhealthy:
		return "Unhealthy", "failing", summary, true
	case taskdomain.HealthDegraded:
		return "Attention", "attention", summary, true
	default:
		return "", "", "", false
	}
}

func observedAssetHealthPresentation(state *statedomain.IntentState, assetName string) (string, string, string, bool) {
	observation := stateAssetObservation(state, assetName)
	if observation == nil {
		return "", "", "", false
	}
	if observation.Health != nil {
		summary := valueOrDefault(strings.TrimSpace(observation.Health.Summary), "Live target health is unknown")
		switch observation.Health.Status {
		case taskdomain.HealthUnhealthy:
			return "Unhealthy", "failing", summary, true
		case taskdomain.HealthDegraded:
			return "Attention", "attention", summary, true
		}
	}
	if observation.ApplyReadiness != nil && observation.ApplyReadiness.Status == taskdomain.ApplyReadinessBlocked {
		return "Blocked", "attention", valueOrDefault(strings.TrimSpace(observation.ApplyReadiness.Summary), "Asset dependencies are not ready"), true
	}
	return "", "", "", false
}

func observedFailureSummary(state *statedomain.IntentState) string {
	if state == nil || state.ApplyReadiness == nil {
		return ""
	}
	if state.ApplyReadiness.Status != taskdomain.ApplyReadinessBlocked {
		return ""
	}
	return strings.TrimSpace(state.ApplyReadiness.Summary)
}

func stateHealth(state *statedomain.IntentState) *taskdomain.HealthObservation {
	if state == nil {
		return nil
	}
	return state.Health
}

func stateAssetObservation(state *statedomain.IntentState, assetName string) *taskdomain.AssetObservation {
	if state == nil || len(state.AssetObservations) == 0 {
		return nil
	}
	return state.AssetObservations[assetName]
}

func stateAssetHealth(state *statedomain.IntentState, assetName string) *taskdomain.HealthObservation {
	observation := stateAssetObservation(state, assetName)
	if observation == nil {
		return nil
	}
	return observation.Health
}

func stateAssetApplyReadiness(state *statedomain.IntentState, assetName string) *taskdomain.ApplyReadiness {
	observation := stateAssetObservation(state, assetName)
	if observation == nil {
		return nil
	}
	return observation.ApplyReadiness
}

func stateApplyReadiness(state *statedomain.IntentState) *taskdomain.ApplyReadiness {
	if state == nil {
		return nil
	}
	return state.ApplyReadiness
}

func cloneHealthObservation(in *taskdomain.HealthObservation) *taskdomain.HealthObservation {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneApplyReadiness(in *taskdomain.ApplyReadiness) *taskdomain.ApplyReadiness {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func deriveAssetPresentation(state *statedomain.IntentState, assetName string, runtime intentTaskRuntime) (string, string, string, string) {
	if state == nil {
		return "Unknown", "Planned", "pending", "No reconcile state yet"
	}
	if runtime.TimedOut {
		return "TimedOut", "Attention", "attention", "The latest reconcile task timed out before this asset was revalidated"
	}
	changed := state.Drift != nil && containsString(state.Drift.ChangedAssets, assetName)
	switch state.Status {
	case statedomain.StatusHealthy:
		if displayStatus, health, summary, ok := observedAssetHealthPresentation(state, assetName); ok {
			return string(state.Status), displayStatus, health, summary
		}
		return string(state.Status), "No diff", "healthy", "Matches desired state"
	case statedomain.StatusChecking:
		return string(state.Status), "Checking", "pending", "Validating that drift can be applied safely"
	case statedomain.StatusDiffing:
		return string(state.Status), "Comparing", "pending", "Inspecting live drift"
	case statedomain.StatusApplying:
		return string(state.Status), "Pushing", "pending", "Applying the asset"
	case statedomain.StatusDiffFailed, statedomain.StatusCheckFailed, statedomain.StatusApplyFailed:
		if displayStatus, health, summary, ok := observedAssetHealthPresentation(state, assetName); ok {
			return string(state.Status), displayStatus, health, summary
		}
		return string(state.Status), "Error", "failing", valueOrDefault(pointerString(state.LastError), "Last task failed")
	case statedomain.StatusDrifted:
		if changed {
			if _, _, observedSummary, ok := observedAssetHealthPresentation(state, assetName); ok {
				return string(state.Status), "Diff found", "attention", combineDriftSummary("Live state differs from blueprint", observedSummary)
			}
			return string(state.Status), "Diff found", "attention", "Live state differs from blueprint"
		}
		if displayStatus, health, summary, ok := observedAssetHealthPresentation(state, assetName); ok {
			return string(state.Status), displayStatus, health, summary
		}
		return string(state.Status), "No diff", "healthy", "No current drift on this asset"
	case statedomain.StatusDriftedLocked:
		if changed {
			if _, _, observedSummary, ok := observedAssetHealthPresentation(state, assetName); ok {
				return string(state.Status), "Locked drift", "attention", combineDriftSummary("Drift exists but push is locked", observedSummary)
			}
			return string(state.Status), "Locked drift", "attention", "Drift exists but push is locked"
		}
		if displayStatus, health, summary, ok := observedAssetHealthPresentation(state, assetName); ok {
			return string(state.Status), displayStatus, health, summary
		}
		return string(state.Status), "No diff", "healthy", "No current drift on this asset"
	case statedomain.StatusDestroying:
		return string(state.Status), "Removing", "pending", "Destroy in progress"
	case statedomain.StatusDestroyed:
		return string(state.Status), "Removed", "pending", "Asset was destroyed"
	case statedomain.StatusBlocked:
		if displayStatus, health, summary, ok := observedAssetHealthPresentation(state, assetName); ok {
			return string(state.Status), displayStatus, health, summary
		}
		return string(state.Status), "Blocked", "attention", "Waiting for intent dependencies"
	default:
		return string(state.Status), humanizeWords(string(state.Status)), "pending", humanizeWords(string(state.Status))
	}
}

func combineDriftSummary(base, observed string) string {
	base = strings.TrimSpace(base)
	observed = strings.TrimSpace(observed)
	if observed == "" {
		return base
	}
	if base == "" {
		return observed
	}
	return base + ": " + observed
}

func assetFacts(spec assetdomain.Spec) (string, []Fact, bool, []string, int) {
	typed, _, err := assetdefs.Decode(spec)
	if err != nil {
		return strings.ToLower(spec.Type) + " asset", nil, false, nil, 0
	}
	switch value := typed.(type) {
	case *assetdefs.ComputeSpec:
		ports := computePortLabels(value.Ports)
		replicas := 1
		if value.Replicas != nil {
			replicas = *value.Replicas
		}
		facts := []Fact{
			{Label: "Image", Value: value.Image},
			{Label: "Scale", Value: fmt.Sprintf("%d pods", replicas)},
		}
		if r := value.Resources; r != nil {
			if r.Requests.CPU != "" || r.Limits.CPU != "" {
				cpuVal := r.Requests.CPU
				if r.Limits.CPU != "" {
					if cpuVal != "" {
						cpuVal += " / " + r.Limits.CPU
					} else {
						cpuVal = r.Limits.CPU
					}
				}
				facts = append(facts, Fact{Label: "CPU", Value: cpuVal})
			}
			if r.Requests.Memory != "" || r.Limits.Memory != "" {
				memVal := r.Requests.Memory
				if r.Limits.Memory != "" {
					if memVal != "" {
						memVal += " / " + r.Limits.Memory
					} else {
						memVal = r.Limits.Memory
					}
				}
				facts = append(facts, Fact{Label: "Memory", Value: memVal})
			}
		}
		if len(value.Env) > 0 {
			facts = append(facts, Fact{Label: "Env", Value: fmt.Sprintf("%d vars", len(value.Env))})
		}
		if len(value.ConfigMounts) > 0 {
			facts = append(facts, Fact{Label: "Config", Value: fmt.Sprintf("%d mounts", len(value.ConfigMounts))})
		}
		if len(value.VolumeMounts) > 0 {
			facts = append(facts, Fact{Label: "Storage", Value: fmt.Sprintf("%d mounts", len(value.VolumeMounts))})
		}
		if len(ports) > 0 {
			facts = append(facts, Fact{Label: "Ports", Value: strings.Join(ports, ", ")})
		}
		if value.HealthCheck != nil {
			facts = append(facts, Fact{Label: "Health", Value: "Configured"})
		}
		facts = appendOutputsFact(facts, computeOutputKeys(value))
		return "Compute service", facts, true, ports, replicas
	case *assetdefs.VolumeSpec:
		facts := []Fact{}
		if value.Size != "" {
			facts = append(facts, Fact{Label: "Size", Value: value.Size})
		}
		if value.AccessMode != "" {
			facts = append(facts, Fact{Label: "Access", Value: value.AccessMode})
		}
		if value.Ephemeral != nil && *value.Ephemeral {
			facts = append(facts, Fact{Label: "Mode", Value: "Ephemeral"})
		}
		facts = appendOutputsFact(facts, []string{"name"})
		return "Storage volume", facts, false, nil, 0
	case *assetdefs.ConfigSpec:
		facts := []Fact{}
		if value.Format != "" {
			facts = append(facts, Fact{Label: "Format", Value: value.Format})
		}
		if value.Content != "" {
			facts = append(facts, Fact{Label: "Inline", Value: "Yes"})
		}
		if len(value.Data) > 0 {
			facts = append(facts, Fact{Label: "Files", Value: describeConfigFiles(value.Data)})
		}
		facts = appendOutputsFact(facts, []string{"name"})
		return "Config files", facts, false, nil, 0
	case *assetdefs.ObjectStoreSpec:
		facts := []Fact{{Label: "Engine", Value: value.Engine}}
		if value.Endpoint != "" {
			facts = append(facts,
				Fact{Label: "Mode", Value: "External"},
				Fact{Label: "Endpoint", Value: value.Endpoint},
			)
		} else {
			facts = append(facts, Fact{Label: "Mode", Value: "Managed"})
		}
		if len(value.Buckets) > 0 {
			facts = append(facts, Fact{Label: "Buckets", Value: strings.Join(value.Buckets, ", ")})
		}
		facts = appendOutputsFact(facts, []string{"id", "endpoint"})
		if value.Endpoint != "" {
			return "External object store", facts, true, nil, 1
		}
		return "Object storage service", facts, true, nil, 1
	case *assetdefs.SQLDatabaseSpec:
		facts := []Fact{{Label: "Engine", Value: value.Engine}}
		if value.Version != "" {
			facts = append(facts, Fact{Label: "Version", Value: value.Version})
		}
		if value.Database != "" {
			facts = append(facts, Fact{Label: "Database", Value: value.Database})
		}
		if value.Port != nil {
			facts = append(facts, Fact{Label: "Port", Value: fmt.Sprintf("%d", *value.Port)})
		}
		ports := []string{}
		if value.Port != nil {
			ports = append(ports, fmt.Sprintf("%d", *value.Port))
		}
		facts = appendOutputsFact(facts, []string{"id", "engine", "url"})
		return "SQL database", facts, true, ports, 1
	case *assetdefs.LoadBalancerSpec:
		facts := []Fact{
			{Label: "Targets", Value: fmt.Sprintf("%d", len(value.Targets))},
			{Label: "Listeners", Value: fmt.Sprintf("%d", len(value.Listeners))},
		}
		if value.Config != "" {
			facts = append(facts, Fact{Label: "Config", Value: value.Config})
		}
		ports := make([]string, 0, len(value.Listeners))
		for _, listener := range value.Listeners {
			if listener.Port == nil {
				continue
			}
			label := fmt.Sprintf("%d", *listener.Port)
			if listener.Name != "" {
				label = listener.Name + ":" + label
			}
			ports = append(ports, label)
		}
		facts = appendOutputsFact(facts, []string{"id", "address"})
		return "Network edge", facts, true, ports, 1
	case *assetdefs.ObservabilitySpec:
		facts := []Fact{}
		if value.Provider != "" {
			facts = append(facts, Fact{Label: "Provider", Value: value.Provider})
		}
		if value.Endpoint != "" {
			facts = append(facts, Fact{Label: "Endpoint", Value: value.Endpoint})
		}
		if len(value.Receivers) > 0 {
			facts = append(facts, Fact{Label: "Receivers", Value: strings.Join(value.Receivers, ", ")})
		}
		if len(value.Exporters) > 0 {
			facts = append(facts, Fact{Label: "Exporters", Value: strings.Join(value.Exporters, ", ")})
		}
		facts = appendOutputsFact(facts, []string{"id", "endpoint"})
		return "Observability service", facts, true, nil, 1
	default:
		return strings.ToLower(spec.Type) + " asset", nil, false, nil, 0
	}
}

func computePortLabels(ports []assetdefs.PortSpec) []string {
	labels := make([]string, 0, len(ports))
	for _, port := range ports {
		value := preferredPort(port.ServicePort, port.HostPort, port.Port, port.ContainerPort)
		if value == nil {
			continue
		}
		label := fmt.Sprintf("%d", *value)
		if port.Name != "" {
			label = port.Name + ":" + label
		}
		labels = append(labels, label)
	}
	return labels
}

func preferredPort(values ...*int) *int {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func computeOutputKeys(spec *assetdefs.ComputeSpec) []string {
	keys := []string{"id", "image", "running"}
	if len(computePortLabels(spec.Ports)) > 0 {
		keys = append(keys, "address")
	}
	return keys
}

func describeConfigFiles(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) <= 3 {
		return strings.Join(keys, ", ")
	}
	return strings.Join(keys[:3], ", ") + fmt.Sprintf(" +%d", len(keys)-3)
}

func appendOutputsFact(facts []Fact, keys []string) []Fact {
	if len(keys) == 0 {
		return facts
	}
	return append(facts, Fact{Label: "Outputs", Value: strings.Join(keys, ", ")})
}

func uniqueRefs(properties map[string]any) []string {
	refs := resolver.FindRefs(properties)
	values := make([]string, 0, len(refs))
	for _, ref := range refs {
		values = append(values, ref.IntentName+"."+ref.OutputKey)
	}
	sort.Strings(values)
	return uniqueStrings(values)
}

func assetOutputs(state *statedomain.IntentState, assetName string) map[string]string {
	if state == nil || len(state.Outputs) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range state.Outputs {
		prefix := assetName + "."
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		out[strings.TrimPrefix(key, prefix)] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func latestStateTimestamp(state *statedomain.IntentState) time.Time {
	if state == nil {
		return time.Time{}
	}
	out := state.Timestamps.LastApplyAt
	out = maxTime(out, state.Timestamps.LastDiffAt)
	out = maxTime(out, state.Timestamps.LastCheckAt)
	out = maxTime(out, state.Timestamps.LastQueuedAt)
	return out
}

func resolveTarget(state *statedomain.IntentState, fallback targetdomain.Placement) targetdomain.Placement {
	if state != nil {
		if !isZeroPlacement(state.Target) {
			return state.Target
		}
	}
	return fallback
}

func isZeroPlacement(value targetdomain.Placement) bool {
	return strings.TrimSpace(value.Cluster) == "" &&
		strings.TrimSpace(value.Namespace) == "" &&
		strings.TrimSpace(value.Region) == "" &&
		strings.TrimSpace(value.Account) == ""
}

func formatTarget(target targetdomain.Placement) string {
	parts := make([]string, 0, 4)
	if target.Cluster != "" {
		parts = append(parts, target.Cluster)
	}
	if target.Namespace != "" {
		parts = append(parts, target.Namespace)
	}
	if target.Region != "" {
		parts = append(parts, target.Region)
	}
	if target.Account != "" {
		parts = append(parts, target.Account)
	}
	if len(parts) == 0 {
		return "Unassigned"
	}
	return strings.Join(parts, " / ")
}

func manifestAssetOrder(assets []assetdomain.Spec) []string {
	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.Name)
	}
	return names
}

func mergeAssetOrder(preferred []string, assets []assetdomain.Spec) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(assets))
	for _, name := range preferred {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, asset := range assets {
		if _, ok := seen[asset.Name]; ok {
			continue
		}
		seen[asset.Name] = struct{}{}
		out = append(out, asset.Name)
	}
	return out
}

func readJSON(ctx context.Context, store guardianapi.ReadStore, logicalPath string, out any) error {
	data, err := store.ReadFile(ctx, logicalPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func readLogEntries(ctx context.Context, store guardianapi.ReadStore, logicalPath string) ([]taskdomain.LogEntry, error) {
	content, err := store.ReadFile(ctx, logicalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	entries := make([]taskdomain.LogEntry, 0)
	for scanner.Scan() {
		var entry taskdomain.LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

func parsePartition(content []byte) (*partitiondomain.Partition, error) {
	return manifestpkg.ParsePartition(content)
}

func validatePartition(partition *partitiondomain.Partition) error {
	return validatorpkg.ValidatePartition(partition)
}

func parseIntent(content []byte) (*intentdomain.Intent, error) {
	return manifestpkg.ParseIntent(content)
}

func validateIntent(intent *intentdomain.Intent, knownIntents []string, knownPushers []string) error {
	return validatorpkg.ValidateIntent(intent, knownIntents, knownPushers)
}

func splitAssetOutputKey(key string) (string, string, bool) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func humanizeWords(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	value = strings.ReplaceAll(value, ".", " ")
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	parts := strings.Fields(value)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mergeStringMaps(values ...map[string]string) map[string]string {
	count := 0
	for _, value := range values {
		count += len(value)
	}
	if count == 0 {
		return nil
	}
	out := make(map[string]string, count)
	for _, value := range values {
		for key, item := range value {
			out[key] = item
		}
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedAssetDeploymentKeys(values map[string]*AssetDeployment) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Server) buildCatalog() CatalogResponse {
	pushers := append([]string(nil), s.pushers...)
	if len(pushers) == 0 {
		pushers = []string{"local"}
	}
	return CatalogResponse{
		Pushers:    pushers,
		AssetTypes: assetdefs.Catalog(),
	}
}
