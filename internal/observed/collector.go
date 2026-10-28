package observed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/awsobserve"
	"github.com/radryc/guardian/internal/cdktemplate"
	"github.com/radryc/guardian/internal/compiler/manifest"
	historydomain "github.com/radryc/guardian/internal/domain/history"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	statedomain "github.com/radryc/guardian/internal/domain/state"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/orchestrator/common"
	"github.com/radryc/guardian/internal/orchestrator/dispatcher"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

const (
	defaultInterval     = 5 * time.Minute
	defaultTimeout      = 30 * time.Second
	defaultMaxEvents    = 200
	defaultEventHorizon = 24 * time.Hour
)

// Endpoint describes one pusher's observed read API.
type Endpoint struct {
	Name   string
	Client *Client
}

// Collector polls pusher observed APIs for every observed intent in the store,
// diffs the observed CloudFormation state against the desired CDK app in
// MonoFS and persists snapshots plus intent state. It never queues tasks and
// never applies anything.
type Collector struct {
	Store       guardianapi.Store
	Dispatcher  *dispatcher.Dispatcher
	Endpoints   map[string]Endpoint
	Resolver    EndpointResolver
	Interval    time.Duration
	Timeout     time.Duration
	MaxEvents   int
	DetectDrift bool
}

// Run polls until the context is cancelled.
func (c *Collector) Run(ctx context.Context) error {
	if c.Store == nil || c.Dispatcher == nil {
		return fmt.Errorf("observed collector requires a store and dispatcher")
	}
	interval := c.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	if err := c.CollectOnce(ctx); err != nil && ctx.Err() == nil {
		log.Printf("[observed] initial collection failed: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.CollectOnce(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[observed] collection failed (will retry in %s): %v", interval, err)
			}
		}
	}
}

// CollectOnce runs a single collection pass over every partition.
func (c *Collector) CollectOnce(ctx context.Context) error {
	entries, err := c.Store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		return fmt.Errorf("list partitions: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if err := c.collectPartition(ctx, entry.Name); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("[observed] partition %s: %v", entry.Name, err)
		}
	}
	return nil
}

func (c *Collector) collectPartition(ctx context.Context, partition string) error {
	entries, err := c.Store.ListDir(ctx, paths.PartitionIntentsDir(partition))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".yaml") {
			continue
		}
		intentName := strings.TrimSuffix(entry.Name, ".yaml")
		content, err := c.Store.ReadFile(ctx, paths.IntentManifest(partition, intentName))
		if err != nil {
			continue
		}
		parsed, err := manifest.ParseIntent(content)
		if err != nil {
			continue
		}
		if parsed.Spec.IntentType != intentdomain.TypeObserved {
			continue
		}
		if err := c.collectIntent(ctx, partition, parsed); err != nil {
			log.Printf("[observed] %s/%s: %v", partition, intentName, err)
		}
	}
	return nil
}

type assetResult struct {
	asset       string
	changed     bool
	summary     string
	err         error
	health      *taskdomain.HealthObservation
	readiness   *taskdomain.ApplyReadiness
	observation *taskdomain.AssetObservation
	snapshot    Snapshot
}

func (c *Collector) collectIntent(ctx context.Context, partition string, intent *intentdomain.Intent) error {
	intentName := intent.Metadata.Name
	endpoint, ok := c.endpointFor(intent.Spec.TargetPusher)
	if !ok || endpoint.Client == nil {
		return c.recordIntentError(ctx, partition, intent, fmt.Errorf("no observed API configured for pusher %q", intent.Spec.TargetPusher))
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	stackAssets := 0
	for _, asset := range intent.Spec.Assets {
		if asset.Type == "CDKStack" {
			stackAssets++
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout*time.Duration(max(1, stackAssets)))
	defer cancel()

	results := make([]assetResult, 0, len(intent.Spec.Assets))
	for _, asset := range intent.Spec.Assets {
		if asset.Type != "CDKStack" {
			continue
		}
		results = append(results, c.observeAsset(runCtx, partition, intent, endpoint, asset))
	}
	if len(results) == 0 {
		return nil
	}

	changedAssets := make([]string, 0, len(results))
	changedSet := make(map[string]struct{}, len(results))
	addChanged := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, exists := changedSet[name]; exists {
			return
		}
		changedSet[name] = struct{}{}
		changedAssets = append(changedAssets, name)
	}
	assetObservations := map[string]*taskdomain.AssetObservation{}
	var aggregatedHealth *taskdomain.HealthObservation
	var aggregatedReadiness *taskdomain.ApplyReadiness
	errorMessages := make([]string, 0)
	summaries := make([]string, 0)
	for _, result := range results {
		if result.err != nil {
			errorMessages = append(errorMessages, fmt.Sprintf("%s: %v", result.asset, result.err))
			continue
		}
		if result.observation != nil {
			assetObservations[result.asset] = result.observation
		}
		aggregatedHealth = mergeHealth(aggregatedHealth, result.health, result.asset)
		aggregatedReadiness = mergeReadiness(aggregatedReadiness, result.readiness, result.asset)
		if result.changed {
			addChanged(result.asset)
		}
		if result.summary != "" {
			summaries = append(summaries, result.summary)
		}
	}
	mergedResources := mergeResourceObservations(results)
	if len(errorMessages) == 0 {
		for _, resource := range mergedResources {
			if resource.Status != statedomain.ResourceInSync {
				addChanged(resource.AssetName)
			}
		}
	}

	state, err := common.LoadIntentState(ctx, c.Store, partition, intentName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if state == nil {
		state = &statedomain.IntentState{
			APIVersion: "guardian/v1alpha1",
			Kind:       "IntentState",
			Partition:  partition,
			Intent:     intentName,
			Outputs:    map[string]string{},
		}
	}
	state.IntentType = intentdomain.TypeObserved
	state.TargetPusher = intent.Spec.TargetPusher
	state.Target = intent.Spec.Target
	state.Joins = append([]string(nil), intent.Spec.Joins...)
	if state.Outputs == nil {
		state.Outputs = map[string]string{}
	}
	state.AssetObservations = assetObservations
	state.Health = aggregatedHealth
	state.ApplyReadiness = aggregatedReadiness
	if len(errorMessages) == 0 {
		state.Resources = statedomain.CloneResourceObservations(mergedResources)
	}
	state.Timestamps.LastDiffAt = time.Now().UTC()

	if len(errorMessages) > 0 {
		message := strings.Join(errorMessages, "; ")
		state.Status = statedomain.StatusDiffFailed
		state.LastError = &message
		state.Drift = &taskdomain.DriftReport{Status: "Changed", Summary: message}
	} else if len(changedAssets) > 0 {
		summary := strings.Join(summaries, "; ")
		if summary == "" {
			summary = "observed state differs from the desired CDK app"
		}
		state.Status = statedomain.StatusObservedDrifted
		state.LastError = nil
		state.Drift = &taskdomain.DriftReport{Status: "Changed", Summary: summary, ChangedAssets: changedAssets}
	} else {
		state.Status = statedomain.StatusObserved
		state.LastError = nil
		state.Drift = &taskdomain.DriftReport{Status: "InSync", Summary: "observed state matches the desired CDK app"}
	}
	if err := c.Dispatcher.WriteIntentState(ctx, state); err != nil {
		return err
	}
	if len(changedAssets) > 0 {
		_ = c.Dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
			Partition: partition,
			Intent:    intentName,
			Type:      "drift.detected",
			Message:   state.Drift.Summary,
			Details: map[string]string{
				"changed_assets": strings.Join(changedAssets, ", "),
				"intentType":     intentdomain.TypeObserved,
			},
		})
	}
	log.Printf("[observed] %s/%s status=%s changed=%d errors=%d", partition, intentName, state.Status, len(changedAssets), len(errorMessages))
	return nil
}

func (c *Collector) endpointFor(pusher string) (Endpoint, bool) {
	if c.Resolver != nil {
		if endpoint, ok := c.Resolver.Resolve(pusher); ok {
			return endpoint, true
		}
	}
	endpoint, ok := c.Endpoints[pusher]
	return endpoint, ok
}

func (c *Collector) observeAsset(ctx context.Context, partition string, intent *intentdomain.Intent, endpoint Endpoint, asset intentdomain.AssetSpec) assetResult {
	result := assetResult{asset: asset.Name}
	payload, err := c.loadStackPayload(ctx, asset)
	if err != nil {
		result.err = err
		return result
	}
	account := strings.TrimSpace(intent.Spec.Target.Account)
	region := strings.TrimSpace(intent.Spec.Target.Region)
	stackName := strings.TrimSpace(payload.StackName)
	if stackName == "" {
		stackName = asset.Name
	}
	now := time.Now().UTC()
	snapshot := Snapshot{
		APIVersion: "guardian/v1alpha1",
		Kind:       "ObservedStackSnapshot",
		Partition:  partition,
		Intent:     intent.Metadata.Name,
		Asset:      asset.Name,
		Account:    account,
		Region:     region,
		StackName:  stackName,
		CapturedAt: now,
	}

	// Refresh live state first so a stack with no EventBridge history yet is
	// still observable. Refresh failures are recorded but not fatal: the
	// event-fed cache may still have state.
	if _, err := endpoint.Client.Refresh(ctx, account, region, []string{stackName}, c.DetectDrift); err != nil {
		result.summary = "refresh failed: " + err.Error()
	}
	state, found, err := endpoint.Client.Stack(ctx, account, region, stackName)
	if err != nil {
		snapshot.Error = err.Error()
		result.err = err
		result.snapshot = snapshot
		return result
	}
	if !found {
		snapshot.Error = "stack not observed"
		result.changed = true
		result.summary = "stack " + stackName + " is not observed"
		result.snapshot = snapshot
		return result
	}
	snapshot.StackID = state.StackID
	snapshot.StackStatus = state.StackStatus
	snapshot.StatusReason = state.StatusReason
	snapshot.DriftStatus = state.DriftStatus
	snapshot.DriftedResourceCount = state.DriftedResourceCount
	snapshot.LastEventType = state.LastEventType
	snapshot.LastEventID = state.LastEventID
	snapshot.LastEventTime = state.LastEventTime

	// Live resource identity is not part of the template. Fetch it so the UI
	// can show physical ARNs/names and so later service-level enrichment can
	// use the real ECS/ELB/Lambda identifiers. Failure is non-fatal: the
	// template inventory is still useful on its own.
	liveStackResources, resourcesErr := endpoint.Client.StackResources(ctx, account, region, stackName)
	if resourcesErr != nil {
		log.Printf("[observed] %s/%s: live stack resources: %v", partition, intent.Metadata.Name, resourcesErr)
	}

	var (
		changed          []string
		compareSummary   string
		desiredInventory []cdktemplate.Resource
		liveInventory    []cdktemplate.Resource
	)
	switch {
	case strings.TrimSpace(payload.PrebuiltAssemblyDir) != "":
		observedTemplate, err := endpoint.Client.Template(ctx, account, region, stackName)
		if err != nil {
			snapshot.Error = err.Error()
			result.err = err
			result.snapshot = snapshot
			return result
		}
		snapshot.TemplateHash = TemplateHash(observedTemplate)
		desiredTemplate, err := c.loadDesiredTemplate(ctx, payload, stackName)
		if err != nil {
			snapshot.Error = err.Error()
			result.err = err
			result.snapshot = snapshot
			return result
		}
		changed, err = CompareTemplates(desiredTemplate, observedTemplate)
		if err != nil {
			snapshot.Error = err.Error()
			result.err = err
			result.snapshot = snapshot
			return result
		}
		desiredInventory, err = cdktemplate.Inventory(desiredTemplate)
		if err != nil {
			snapshot.Error = err.Error()
			result.err = err
			result.snapshot = snapshot
			return result
		}
		liveInventory, err = cdktemplate.Inventory(observedTemplate)
		if err != nil {
			snapshot.Error = err.Error()
			result.err = err
			result.snapshot = snapshot
			return result
		}
	case strings.TrimSpace(payload.SourceDir) != "":
		// No prebuilt assembly: upload the source tree to the pusher and let it
		// synthesize. Guardian still initiates the connection.
		files, err := c.readSourceFiles(ctx, payload.SourceDir)
		if err != nil {
			snapshot.Error = err.Error()
			result.err = err
			result.snapshot = snapshot
			return result
		}
		diff, err := endpoint.Client.DiffSource(ctx, awsobserve.SourceDiffRequest{
			Account:        account,
			Region:         region,
			StackName:      stackName,
			StackID:        payload.StackID,
			SourceFiles:    files,
			AppCommand:     payload.AppCommand,
			PackageManager: payload.PackageManager,
			Context:        payload.Context,
		})
		if err != nil {
			snapshot.Error = err.Error()
			result.err = err
			result.snapshot = snapshot
			return result
		}
		changed = diff.ChangedResources
		compareSummary = diff.Summary
		desiredInventory = diff.Resources
	default:
		err := fmt.Errorf("observed asset %s requires prebuiltAssemblyDir or sourceDir", asset.Name)
		snapshot.Error = err.Error()
		result.err = err
		result.snapshot = snapshot
		return result
	}
	snapshot.ChangedResources = changed
	snapshot.Resources = buildResourceObservations(desiredInventory, liveInventory, changed, liveStackResources)

	resourceSummary := summarizeResourceDrift(snapshot.Resources, state.DriftedResources)
	reasons := make([]string, 0, 3)
	if len(changed) > 0 {
		if compareSummary != "" {
			reasons = append(reasons, compareSummary)
		} else {
			reasons = append(reasons, fmt.Sprintf("%d resource(s) differ from desired", len(changed)))
		}
	}
	if strings.EqualFold(state.DriftStatus, "DRIFTED") {
		reasons = append(reasons, fmt.Sprintf("CloudFormation drift on %d resource(s)", state.DriftedResourceCount))
	}
	if resourceSummary != "" {
		reasons = append(reasons, "changed resources: "+resourceSummary)
	}
	if strings.HasSuffix(strings.ToUpper(state.StackStatus), "_FAILED") {
		reasons = append(reasons, "stack status "+state.StackStatus)
	}
	if len(reasons) > 0 {
		result.changed = true
		result.summary = fmt.Sprintf("%s: %s", stackName, strings.Join(reasons, ", "))
		if state.LastEventType != "" && !state.LastEventTime.IsZero() {
			result.summary += fmt.Sprintf(" (last event %s at %s)", state.LastEventType, state.LastEventTime.UTC().Format(time.RFC3339))
		}
	}

	health, readiness := HealthFor(state.StackStatus, state.DriftStatus, state.DriftedResourceCount)
	if health != nil && resourceSummary != "" {
		health.Summary = strings.TrimSpace(health.Summary)
		if health.Summary != "" {
			health.Summary += "; "
		}
		health.Summary += "changed resources: " + resourceSummary
	}
	result.health = health
	result.readiness = readiness
	result.observation = &taskdomain.AssetObservation{Health: health, ApplyReadiness: readiness}
	result.snapshot = snapshot

	c.persistSnapshot(ctx, partition, intent.Metadata.Name, asset.Name, snapshot)
	c.persistEvents(ctx, partition, intent.Metadata.Name, endpoint, account, region, stackName)
	return result
}

// summarizeResourceDrift merges the template-diff inventory and CloudFormation
// resource-drift details into one bounded, human-readable list.
func summarizeResourceDrift(resources []statedomain.ResourceObservation, drifted []awsobserve.DriftedResource) string {
	const maxNames = 12
	seen := map[string]struct{}{}
	names := make([]string, 0, len(resources)+len(drifted))
	add := func(logicalID, cfnType, status string) {
		logicalID = strings.TrimSpace(logicalID)
		if logicalID == "" {
			return
		}
		if _, ok := seen[logicalID]; ok {
			return
		}
		seen[logicalID] = struct{}{}
		label := logicalID
		if cfnType = strings.TrimSpace(cfnType); cfnType != "" {
			label += " (" + cfnType
			if status = strings.TrimSpace(status); status != "" {
				label += ", " + status
			}
			label += ")"
		} else if status = strings.TrimSpace(status); status != "" {
			label += " (" + status + ")"
		}
		names = append(names, label)
	}
	for _, resource := range resources {
		if resource.Status == "" || resource.Status == statedomain.ResourceInSync {
			continue
		}
		add(resource.LogicalID, resource.CfnType, resource.Status)
	}
	for _, resource := range drifted {
		add(resource.LogicalID, resource.ResourceType, resource.Status)
	}
	sort.Strings(names)
	if len(names) > maxNames {
		extra := len(names) - maxNames
		names = append(names[:maxNames], fmt.Sprintf("+%d more", extra))
	}
	return strings.Join(names, ", ")
}

func (c *Collector) loadStackPayload(ctx context.Context, asset intentdomain.AssetSpec) (StackPayload, error) {
	logicalPath := strings.TrimSpace(asset.Payload["aws"])
	if logicalPath == "" {
		return StackPayload{}, fmt.Errorf("asset %s is missing payload.aws", asset.Name)
	}
	content, err := c.Store.ReadFile(ctx, logicalPath)
	if err != nil {
		return StackPayload{}, fmt.Errorf("read stack payload %s: %w", logicalPath, err)
	}
	var payload StackPayload
	if err := yaml.Unmarshal(content, &payload); err != nil {
		return StackPayload{}, fmt.Errorf("parse stack payload %s: %w", logicalPath, err)
	}
	return payload, nil
}

const (
	maxSourceFiles = 2000
	maxSourceBytes = 64 << 20
)

// readSourceFiles walks a logical source tree in the store and returns a
// relative-path keyed file map for the pusher source-diff fallback. Build and
// dependency directories are skipped.
func (c *Collector) readSourceFiles(ctx context.Context, logicalDir string) (map[string]string, error) {
	files := map[string]string{}
	total := 0
	base := strings.TrimRight(logicalDir, "/")
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := c.Store.ListDir(ctx, dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if len(files) >= maxSourceFiles {
				return fmt.Errorf("source tree exceeds %d files", maxSourceFiles)
			}
			if strings.HasPrefix(entry.Name, ".") {
				continue
			}
			fullPath := path.Join(dir, entry.Name)
			if entry.IsDir {
				switch entry.Name {
				case "node_modules", "cdk.out", "dist", "build", "out":
					continue
				}
				if err := walk(fullPath); err != nil {
					return err
				}
				continue
			}
			content, err := c.Store.ReadFile(ctx, fullPath)
			if err != nil {
				return err
			}
			total += len(content)
			if total > maxSourceBytes {
				return fmt.Errorf("source tree exceeds %d bytes", maxSourceBytes)
			}
			rel := strings.TrimPrefix(fullPath, base+"/")
			files[rel] = string(content)
		}
		return nil
	}
	if err := walk(logicalDir); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("sourceDir %q contains no files", logicalDir)
	}
	return files, nil
}

func (c *Collector) loadDesiredTemplate(ctx context.Context, payload StackPayload, stackName string) (string, error) {
	assemblyDir := strings.TrimSpace(payload.PrebuiltAssemblyDir)
	if assemblyDir == "" {
		return "", fmt.Errorf("observed intent asset %s requires prebuiltAssemblyDir (sourceDir synth fallback is not configured)", stackName)
	}
	manifestPath := path.Join(assemblyDir, "manifest.json")
	if content, err := c.Store.ReadFile(ctx, manifestPath); err == nil {
		if templateFile := templateFileFromManifest(content, stackName); templateFile != "" {
			templatePath := path.Join(assemblyDir, templateFile)
			template, readErr := c.Store.ReadFile(ctx, templatePath)
			if readErr != nil {
				return "", fmt.Errorf("read desired template %s: %w", templatePath, readErr)
			}
			return string(template), nil
		}
	}
	fallback := path.Join(assemblyDir, stackName+".template.json")
	template, err := c.Store.ReadFile(ctx, fallback)
	if err != nil {
		return "", fmt.Errorf("desired template for stack %s not found in %s: %w", stackName, assemblyDir, err)
	}
	return string(template), nil
}

type cloudAssemblyManifest struct {
	Artifacts map[string]struct {
		Type       string `json:"type"`
		Properties struct {
			TemplateFile string `json:"templateFile"`
			StackName    string `json:"stackName"`
		} `json:"properties"`
	} `json:"artifacts"`
}

func templateFileFromManifest(content []byte, stackName string) string {
	var assembly cloudAssemblyManifest
	if err := json.Unmarshal(content, &assembly); err != nil {
		return ""
	}
	for id, artifact := range assembly.Artifacts {
		if artifact.Type != "aws:cloudformation:stack" {
			continue
		}
		if artifact.Properties.StackName == stackName || (artifact.Properties.StackName == "" && id == stackName) {
			return artifact.Properties.TemplateFile
		}
	}
	return ""
}

func (c *Collector) persistSnapshot(ctx context.Context, partition, intent, asset string, snapshot Snapshot) {
	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	logicalPath := paths.ObservedSnapshot(partition, intent, asset)
	_, err = c.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: payload}},
		Context: guardianapi.MutationContext{
			PrincipalID:   "guardiand",
			Reason:        "observed stack snapshot",
			CorrelationID: partition + "/" + intent + "/" + asset,
		},
	})
	if err != nil {
		log.Printf("[observed] persist snapshot %s: %v", logicalPath, err)
	}
}

func (c *Collector) persistEvents(ctx context.Context, partition, intent string, endpoint Endpoint, account, region, stackName string) {
	events, err := endpoint.Client.Events(ctx, account, region, stackName, time.Now().UTC().Add(-defaultEventHorizon), 50)
	if err != nil || len(events) == 0 {
		return
	}
	writes := make([]guardianapi.PathWrite, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.ID) == "" {
			continue
		}
		payload, err := json.MarshalIndent(event, "", "  ")
		if err != nil {
			continue
		}
		eventID := event.Time.UTC().Format("20060102T150405Z") + "-" + sanitizeEventID(event.ID)
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath: paths.ObservedEvent(partition, intent, eventID),
			Content:     payload,
		})
	}
	if len(writes) == 0 {
		return
	}
	if _, err := c.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: writes,
		Context: guardianapi.MutationContext{
			PrincipalID: "guardiand",
			Reason:      "observed cloudformation events",
		},
	}); err != nil {
		log.Printf("[observed] persist events %s/%s: %v", partition, intent, err)
		return
	}
	c.pruneEvents(ctx, partition, intent)
}

func (c *Collector) pruneEvents(ctx context.Context, partition, intent string) {
	maxEvents := c.MaxEvents
	if maxEvents <= 0 {
		maxEvents = defaultMaxEvents
	}
	entries, err := c.Store.ListDir(ctx, paths.ObservedEventsDir(partition, intent))
	if err != nil || len(entries) <= maxEvents {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			names = append(names, entry.Name)
		}
	}
	sort.Strings(names)
	excess := names[:len(names)-maxEvents]
	deletes := make([]guardianapi.PathDelete, 0, len(excess))
	for _, name := range excess {
		deletes = append(deletes, guardianapi.PathDelete{LogicalPath: path.Join(paths.ObservedEventsDir(partition, intent), name)})
	}
	if _, err := c.Store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: deletes,
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "prune observed events"},
	}); err != nil {
		log.Printf("[observed] prune events %s/%s: %v", partition, intent, err)
	}
}

func (c *Collector) recordIntentError(ctx context.Context, partition string, intent *intentdomain.Intent, err error) error {
	state, loadErr := common.LoadIntentState(ctx, c.Store, partition, intent.Metadata.Name)
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	if state == nil {
		state = &statedomain.IntentState{
			APIVersion: "guardian/v1alpha1",
			Kind:       "IntentState",
			Partition:  partition,
			Intent:     intent.Metadata.Name,
			Outputs:    map[string]string{},
		}
	}
	state.IntentType = intentdomain.TypeObserved
	state.TargetPusher = intent.Spec.TargetPusher
	state.Target = intent.Spec.Target
	state.Status = statedomain.StatusDiffFailed
	message := err.Error()
	state.LastError = &message
	state.Drift = &taskdomain.DriftReport{Status: "Changed", Summary: message}
	return c.Dispatcher.WriteIntentState(ctx, state)
}

func sanitizeEventID(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	cleaned := replacer.Replace(strings.TrimSpace(value))
	if len(cleaned) > 80 {
		cleaned = cleaned[:80]
	}
	return cleaned
}

func mergeHealth(current *taskdomain.HealthObservation, next *taskdomain.HealthObservation, asset string) *taskdomain.HealthObservation {
	if next == nil {
		return current
	}
	candidate := *next
	if current == nil {
		return &candidate
	}
	rank := func(status taskdomain.HealthStatus) int {
		switch status {
		case taskdomain.HealthUnhealthy:
			return 3
		case taskdomain.HealthDegraded:
			return 2
		case taskdomain.HealthHealthy:
			return 1
		default:
			return 0
		}
	}
	if rank(candidate.Status) > rank(current.Status) {
		if candidate.Summary == "" {
			candidate.Summary = asset
		}
		return &candidate
	}
	return current
}

func mergeReadiness(current *taskdomain.ApplyReadiness, next *taskdomain.ApplyReadiness, asset string) *taskdomain.ApplyReadiness {
	if next == nil {
		return current
	}
	candidate := *next
	if current == nil {
		return &candidate
	}
	if candidate.Status == taskdomain.ApplyReadinessBlocked && current.Status != taskdomain.ApplyReadinessBlocked {
		return &candidate
	}
	return current
}
