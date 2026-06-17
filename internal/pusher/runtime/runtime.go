package runtime

import (
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

	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/telemetry"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricapi "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const runtimeScope = "guardian/pusher/runtime"

var (
	runtimeMetricsOnce sync.Once
	runtimeTaskCounter metricapi.Int64Counter
	runtimeFailCounter metricapi.Int64Counter
	runtimeDuration    metricapi.Float64Histogram
)

type Runtime struct {
	QueuePath               string
	WorkerID                string
	PrincipalID             string
	Store                   guardianapi.Store
	Registry                *registry.Registry
	PollInterval            time.Duration
	UnclaimedTaskRetryDelay time.Duration
	CanHandle               func(*taskdomain.Task) bool
	retryMu                 sync.Mutex
	nextClaimAttempt        map[string]time.Time
}

func (r *Runtime) Run(ctx context.Context) error {
	if r.Store == nil {
		return fmt.Errorf("runtime store is required")
	}
	if r.Registry == nil {
		return fmt.Errorf("runtime registry is required")
	}
	if r.PollInterval <= 0 {
		r.PollInterval = 5 * time.Second
	}

	if err := r.processPending(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		log.Printf("[Pusher] error on initial queue scan (will retry): %v", err)
	}

	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.processPending(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return err
				}
				log.Printf("[Pusher] error processing queue (will retry in %s): %v", r.PollInterval, err)
			}
		}
	}
}

func (r *Runtime) ProcessPending(ctx context.Context) error {
	return r.processPending(ctx)
}

func (r *Runtime) tryClaimTask(ctx context.Context, taskID string) (*taskdomain.Task, bool, error) {
	pusher := strings.TrimPrefix(strings.TrimPrefix(r.QueuePath, paths.QueueRoot()), "/")
	ready, err := r.taskReadyToClaim(ctx, pusher, taskID)
	if err != nil {
		return nil, false, err
	}
	if !ready {
		return nil, false, nil
	}
	taskPath := strings.TrimSuffix(r.QueuePath, "/") + "/" + taskID + ".json"
	raw, err := r.Store.ReadFile(ctx, taskPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var t taskdomain.Task
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, false, fmt.Errorf("decode task %s: %w", taskID, err)
	}
	if r.CanHandle != nil && !r.CanHandle(&t) {
		return nil, false, nil
	}

	claim := taskdomain.ClaimFile{
		TaskID:       taskID,
		WorkerID:     r.WorkerID,
		ClaimedAt:    time.Now().UTC(),
		LeaseSeconds: int((5 * time.Minute).Seconds()),
	}
	claimContent, err := json.MarshalIndent(claim, "", "  ")
	if err != nil {
		return nil, false, err
	}

	principalID := strings.TrimSpace(r.PrincipalID)
	if principalID == "" {
		principalID = r.WorkerID
	}
	_, err = r.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath:       paths.QueueClaim(pusher, taskID),
			Content:           claimContent,
			ExpectedVersionID: "absent",
		}},
		Context: guardianapi.MutationContext{PrincipalID: principalID, Reason: "claim task"},
	})
	if err != nil {
		if errors.Is(err, guardianapi.ErrConflict) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &t, true, nil
}

func (r *Runtime) taskReadyToClaim(ctx context.Context, pusher, taskID string) (bool, error) {
	if pusher == "" || taskID == "" {
		return false, nil
	}
	if done, err := pathExists(ctx, r.Store, paths.QueueResult(pusher, taskID)); err != nil {
		return false, err
	} else if done {
		return false, nil
	}

	claimPath := paths.QueueClaim(pusher, taskID)
	info, err := r.Store.Stat(ctx, claimPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	raw, err := r.Store.ReadFile(ctx, claimPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	var claim taskdomain.ClaimFile
	if err := json.Unmarshal(raw, &claim); err != nil {
		return false, fmt.Errorf("decode claim %s: %w", taskID, err)
	}
	if !claimExpired(claim, time.Now().UTC()) {
		return false, nil
	}

	principalID := strings.TrimSpace(r.PrincipalID)
	if principalID == "" {
		principalID = r.WorkerID
	}
	_, err = r.Store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{
			LogicalPath:       claimPath,
			ExpectedVersionID: info.VersionID,
		}},
		Context: guardianapi.MutationContext{
			PrincipalID:   principalID,
			Reason:        "release expired claim",
			CorrelationID: taskID,
		},
	})
	if err != nil {
		if errors.Is(err, guardianapi.ErrConflict) || errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func claimExpired(claim taskdomain.ClaimFile, now time.Time) bool {
	if claim.ClaimedAt.IsZero() || claim.LeaseSeconds <= 0 {
		return false
	}
	return now.After(claim.ClaimedAt.Add(time.Duration(claim.LeaseSeconds) * time.Second))
}

func pathExists(ctx context.Context, store guardianapi.ReadStore, logicalPath string) (bool, error) {
	if _, err := store.Stat(ctx, logicalPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *Runtime) writeResult(ctx context.Context, result *taskdomain.TaskResult) error {
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	pusher := result.Pusher
	principalID := strings.TrimSpace(r.PrincipalID)
	if principalID == "" {
		principalID = r.WorkerID
	}
	_, err = r.Store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{
			LogicalPath: paths.QueueResult(pusher, result.TaskID),
			Content:     content,
		}},
		Context: guardianapi.MutationContext{PrincipalID: principalID, Reason: "write result", CorrelationID: result.TaskID},
	})
	return err
}

func (r *Runtime) processPending(ctx context.Context) error {
	entries, err := r.Store.ListDir(ctx, r.QueuePath)
	if err != nil {
		return err
	}
	pusher := strings.TrimPrefix(strings.TrimPrefix(r.QueuePath, paths.QueueRoot()), "/")
	activeTaskIDs := make(map[string]struct{}, len(entries))
	scanAt := time.Now()
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") || strings.HasPrefix(entry.Name, ".") {
			continue
		}
		taskID := strings.TrimSuffix(entry.Name, ".json")
		activeTaskIDs[taskID] = struct{}{}
		if !r.shouldAttemptClaim(taskID, scanAt) {
			continue
		}
		t, claimed, err := r.tryClaimTask(ctx, taskID)
		if err != nil {
			log.Printf("[Pusher] Error trying to claim task %s: %v", taskID, err)
			continue
		}
		if !claimed {
			// Task already has a result file and is awaiting control-plane cleanup.
			// Do not emit a noisy warning in this expected completion window.
			if completed, err := pathExists(ctx, r.Store, paths.QueueResult(pusher, taskID)); err == nil && completed {
				r.forgetClaimRetry(taskID)
				continue
			}
			r.scheduleClaimRetry(taskID, scanAt)
			log.Printf("[Pusher] Task %s was not claimed (maybe locked or not ready).", taskID)
			continue
		}
		r.forgetClaimRetry(taskID)
		log.Printf("[Pusher] Successfully claimed task %s! op=%s partition=%s intent=%s assets=%d Executing...",
			taskID, t.Op, t.Partition, t.Intent, len(t.Assets))
		result := r.executeTask(ctx, t)
		changed := []string{}
		if result.Drift != nil {
			changed = result.Drift.ChangedAssets
		}
		log.Printf("[Pusher] Task %s execution finished. op=%s partition=%s intent=%s status=%v changed=%v",
			taskID, t.Op, t.Partition, t.Intent, result.Status, changed)
		if err := r.writeResult(ctx, result); err != nil {
			log.Printf("[Pusher] Error writing result for task %s: %v", taskID, err)
			continue
		}
	}
	r.pruneClaimRetry(activeTaskIDs)
	return nil
}

func (r *Runtime) shouldAttemptClaim(taskID string, now time.Time) bool {
	if r.UnclaimedTaskRetryDelay <= 0 || taskID == "" {
		return true
	}
	r.retryMu.Lock()
	defer r.retryMu.Unlock()
	if len(r.nextClaimAttempt) == 0 {
		return true
	}
	nextAttempt, ok := r.nextClaimAttempt[taskID]
	if !ok {
		return true
	}
	if now.Before(nextAttempt) {
		return false
	}
	delete(r.nextClaimAttempt, taskID)
	return true
}

func (r *Runtime) scheduleClaimRetry(taskID string, now time.Time) {
	if r.UnclaimedTaskRetryDelay <= 0 || taskID == "" {
		return
	}
	r.retryMu.Lock()
	defer r.retryMu.Unlock()
	if r.nextClaimAttempt == nil {
		r.nextClaimAttempt = make(map[string]time.Time)
	}
	r.nextClaimAttempt[taskID] = now.Add(r.UnclaimedTaskRetryDelay)
}

func (r *Runtime) forgetClaimRetry(taskID string) {
	if r.UnclaimedTaskRetryDelay <= 0 || taskID == "" {
		return
	}
	r.retryMu.Lock()
	defer r.retryMu.Unlock()
	delete(r.nextClaimAttempt, taskID)
}

func (r *Runtime) pruneClaimRetry(activeTaskIDs map[string]struct{}) {
	if r.UnclaimedTaskRetryDelay <= 0 || len(activeTaskIDs) == 0 {
		return
	}
	r.retryMu.Lock()
	defer r.retryMu.Unlock()
	for taskID := range r.nextClaimAttempt {
		if _, ok := activeTaskIDs[taskID]; ok {
			continue
		}
		delete(r.nextClaimAttempt, taskID)
	}
}

func (r *Runtime) executeTask(ctx context.Context, t *taskdomain.Task) *taskdomain.TaskResult {
	attrs := runtimeTaskAttributes(t, r.WorkerID)
	taskCounter, failCounter, duration := runtimeInstruments()
	taskCounter.Add(ctx, 1, metricapi.WithAttributes(attrs...))
	ctx, span := otel.Tracer(runtimeScope).Start(ctx, "guardian.task.execute", trace.WithAttributes(attrs...))
	span.AddEvent("guardian.task.input", trace.WithAttributes(
		attribute.String("guardian.task.input_json", runtimeTaskTraceJSON(t, r.WorkerID)),
	))
	startedAt := time.Now()
	telemetry.EmitInfo(ctx, runtimeScope, fmt.Sprintf("executing %s task %s for %s/%s", strings.ToLower(string(t.Op)), t.TaskID, t.Partition, t.Intent))
	defer func() {
		duration.Record(ctx, time.Since(startedAt).Seconds(), metricapi.WithAttributes(attrs...))
		span.End()
	}()

	result := &taskdomain.TaskResult{
		APIVersion: "guardian/v1alpha1",
		Kind:       "TaskResult",
		TaskID:     t.TaskID,
		Op:         t.Op,
		Partition:  t.Partition,
		Intent:     t.Intent,
		Pusher:     t.TargetPusher,
		FinishedAt: time.Now().UTC(),
	}
	result.Logs = append(result.Logs, logEntry("info", "", "starting "+string(t.Op)+" task"))

	fail := func(err error) *taskdomain.TaskResult {
		failCounter.Add(ctx, 1, metricapi.WithAttributes(attrs...))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		telemetry.EmitError(ctx, runtimeScope, fmt.Sprintf("task %s failed: %v", t.TaskID, err))
		msg := err.Error()
		result.Status = taskdomain.ResultFailed
		result.Error = &msg
		result.Logs = append(result.Logs, logEntry("error", "", msg))
		result.FinishedAt = time.Now().UTC()
		annotateRuntimeTaskResultSpan(span, result)
		return result
	}

	assets := t.Assets
	if t.Op == taskdomain.OpDestroy {
		reversed := make([]taskdomain.AbstractAsset, len(assets))
		for i := range assets {
			reversed[i] = assets[len(assets)-1-i]
		}
		assets = reversed
	}

	changedAssets := make([]string, 0)
	outputs := map[string]string{}
	var aggregatedHealth *taskdomain.HealthObservation
	var aggregatedApplyReadiness *taskdomain.ApplyReadiness
	assetObservations := map[string]*taskdomain.AssetObservation{}
	assetIndex := make(map[string]taskdomain.AbstractAsset, len(t.Assets))
	for _, taskAsset := range t.Assets {
		assetIndex[taskAsset.Name] = taskAsset
	}
	for _, asset := range assets {
		driver, ok := r.Registry.Get(asset.Type)
		if !ok {
			return fail(fmt.Errorf("no driver registered for asset type %q", asset.Type))
		}
		assetAttrs := append([]attribute.KeyValue{}, attrs...)
		assetAttrs = append(assetAttrs,
			attribute.String("guardian.asset.name", asset.Name),
			attribute.String("guardian.asset.type", asset.Type),
		)
		assetCtx, assetSpan := otel.Tracer(runtimeScope).Start(ctx, "guardian.asset."+strings.ToLower(string(t.Op)), trace.WithAttributes(assetAttrs...))
		if err := driver.Validate(asset.Properties); err != nil {
			assetSpan.RecordError(err)
			assetSpan.SetStatus(codes.Error, err.Error())
			assetSpan.End()
			return fail(err)
		}
		input := registry.AssetInput{
			PartitionName: t.Partition,
			IntentName:    t.Intent,
			Asset:         asset,
			Assets:        assetIndex,
			AssetVersions: t.AssetVersions,
			Target:        t.Target,
			Store:         r.Store,
			WorkerID:      r.WorkerID,
		}
		result.Logs = append(result.Logs, logEntry("info", asset.Name, strings.ToLower(string(t.Op))+" "+asset.Type+" asset"))
		telemetry.EmitInfo(assetCtx, runtimeScope, fmt.Sprintf("%s %s asset %s", strings.ToLower(string(t.Op)), asset.Type, asset.Name))
		var assetErr error
		switch t.Op {
		case taskdomain.OpCheck:
			if err := driver.Check(assetCtx, input); err != nil {
				assetErr = err
				break
			}
			result.Logs = append(result.Logs, logEntry("info", asset.Name, "check succeeded"))
			telemetry.EmitInfo(assetCtx, runtimeScope, fmt.Sprintf("check succeeded for asset %s", asset.Name))
		case taskdomain.OpDiff:
			drift, err := driver.Diff(assetCtx, input)
			if err != nil {
				assetErr = err
				break
			}
			if observed, ok := driver.(registry.ObservedStateDriver); ok {
				health, readiness, err := observed.ObserveState(assetCtx, input)
				if err != nil {
					assetErr = err
					break
				}
				if observation := buildAssetObservation(health, readiness); observation != nil {
					assetObservations[asset.Name] = observation
				}
				aggregatedHealth = mergeHealthObservation(aggregatedHealth, health, asset.Name)
				aggregatedApplyReadiness = mergeApplyReadiness(aggregatedApplyReadiness, readiness, asset.Name)
			}
			if drift.Status == "Changed" || len(drift.ChangedAssets) > 0 {
				changedAssets = append(changedAssets, drift.ChangedAssets...)
				msg := fmt.Sprintf("drift detected: %s", drift.Summary)
				result.Logs = append(result.Logs, logEntry("warn", asset.Name, msg))
				log.Printf("[Pusher] DRIFT %s/%s asset=%s reason=%q", t.Partition, t.Intent, asset.Name, drift.Summary)
				telemetry.EmitWarn(assetCtx, runtimeScope, fmt.Sprintf("drift detected for asset %s: %s", asset.Name, drift.Summary))
			} else {
				result.Logs = append(result.Logs, logEntry("info", asset.Name, "no drift detected"))
				telemetry.EmitInfo(assetCtx, runtimeScope, fmt.Sprintf("no drift detected for asset %s", asset.Name))
			}
		case taskdomain.OpApply:
			applied, err := driver.Apply(assetCtx, input)
			if err != nil {
				assetErr = err
				break
			}
			if observed, ok := driver.(registry.ObservedStateDriver); ok {
				health, readiness, err := observed.ObserveState(assetCtx, input)
				if err != nil {
					assetErr = err
					break
				}
				if observation := buildAssetObservation(health, readiness); observation != nil {
					assetObservations[asset.Name] = observation
				}
				aggregatedHealth = mergeHealthObservation(aggregatedHealth, health, asset.Name)
				aggregatedApplyReadiness = mergeApplyReadiness(aggregatedApplyReadiness, readiness, asset.Name)
			}
			result.Logs = append(result.Logs, logEntry("info", asset.Name, "apply succeeded"))
			telemetry.EmitInfo(assetCtx, runtimeScope, fmt.Sprintf("apply succeeded for asset %s", asset.Name))
			result.Logs = append(result.Logs, applied.Logs...)
			for key, value := range applied.Outputs {
				if _, exists := outputs[key]; !exists {
					outputs[key] = value
				}
				outputs[asset.Name+"."+key] = value
			}
		case taskdomain.OpDestroy:
			if err := driver.Destroy(assetCtx, input); err != nil {
				assetErr = err
				break
			}
			result.Logs = append(result.Logs, logEntry("info", asset.Name, "destroy succeeded"))
			telemetry.EmitInfo(assetCtx, runtimeScope, fmt.Sprintf("destroy succeeded for asset %s", asset.Name))
		default:
			assetErr = fmt.Errorf("unsupported operation %q", t.Op)
		}
		if assetErr != nil {
			assetSpan.RecordError(assetErr)
			assetSpan.SetStatus(codes.Error, assetErr.Error())
			assetSpan.End()
			return fail(assetErr)
		}
		assetSpan.SetStatus(codes.Ok, "")
		assetSpan.End()
	}

	result.Status = taskdomain.ResultSucceeded
	if t.Op == taskdomain.OpDiff || t.Op == taskdomain.OpApply {
		driftStatus := "InSync"
		summary := "no drift detected"
		if len(changedAssets) > 0 {
			driftStatus = "Changed"
			summary = fmt.Sprintf("%d asset(s) changed", len(changedAssets))
		}
		result.Drift = &taskdomain.DriftReport{Status: driftStatus, Summary: summary, ChangedAssets: uniqueStrings(changedAssets)}
		result.Health = aggregatedHealth
		result.ApplyReadiness = aggregatedApplyReadiness
		if len(assetObservations) > 0 {
			result.AssetObservations = assetObservations
		}
	}
	if len(outputs) > 0 {
		result.Outputs = outputs
	}
	result.Logs = append(result.Logs, logEntry("info", "", "task completed"))
	result.FinishedAt = time.Now().UTC()
	annotateRuntimeTaskResultSpan(span, result)
	span.SetStatus(codes.Ok, "")
	telemetry.EmitInfo(ctx, runtimeScope, fmt.Sprintf("completed %s task %s for %s/%s", strings.ToLower(string(t.Op)), t.TaskID, t.Partition, t.Intent))
	return result
}

func runtimeInstruments() (metricapi.Int64Counter, metricapi.Int64Counter, metricapi.Float64Histogram) {
	runtimeMetricsOnce.Do(func() {
		meter := otel.Meter(runtimeScope)
		runtimeTaskCounter, _ = meter.Int64Counter("guardian.task.executions")
		runtimeFailCounter, _ = meter.Int64Counter("guardian.task.failures")
		runtimeDuration, _ = meter.Float64Histogram("guardian.task.duration")
	})
	return runtimeTaskCounter, runtimeFailCounter, runtimeDuration
}

func runtimeTaskAttributes(t *taskdomain.Task, workerID string) []attribute.KeyValue {
	assetNames := make([]string, 0, len(t.Assets))
	assetTypes := make([]string, 0, len(t.Assets))
	for _, asset := range t.Assets {
		if asset.Name != "" {
			assetNames = append(assetNames, asset.Name)
		}
		if asset.Type != "" {
			assetTypes = append(assetTypes, asset.Type)
		}
	}

	attrs := []attribute.KeyValue{
		attribute.String("guardian.partition", t.Partition),
		attribute.String("guardian.intent", t.Intent),
		attribute.String("guardian.operation", string(t.Op)),
		attribute.String("guardian.task.id", t.TaskID),
		attribute.Int("guardian.asset.count", len(t.Assets)),
	}
	if strings.TrimSpace(workerID) != "" {
		attrs = append(attrs, attribute.String("guardian.worker.id", workerID))
	}
	if t.CorrelationID != "" {
		attrs = append(attrs, attribute.String("guardian.task.correlation_id", t.CorrelationID))
	}
	if t.PartitionRevision != "" {
		attrs = append(attrs, attribute.String("guardian.partition.revision", t.PartitionRevision))
	}
	if t.IntentVersionID != "" {
		attrs = append(attrs, attribute.String("guardian.intent.version_id", t.IntentVersionID))
	}
	if t.IntentSpecHash != "" {
		attrs = append(attrs, attribute.String("guardian.intent.spec_hash", t.IntentSpecHash))
	}
	if t.TargetPusher != "" {
		attrs = append(attrs, attribute.String("guardian.pusher", t.TargetPusher))
	}
	if t.Target.Cluster != "" {
		attrs = append(attrs, attribute.String("guardian.cluster", t.Target.Cluster))
	}
	if t.Target.Namespace != "" {
		attrs = append(attrs, attribute.String("guardian.target.namespace", t.Target.Namespace))
	}
	if t.Target.Region != "" {
		attrs = append(attrs, attribute.String("guardian.target.region", t.Target.Region))
	}
	if t.Target.Account != "" {
		attrs = append(attrs, attribute.String("guardian.target.account", t.Target.Account))
	}
	if len(assetNames) > 0 {
		attrs = append(attrs, attribute.StringSlice("guardian.asset.names", uniqueSortedStrings(assetNames)))
	}
	if len(assetTypes) > 0 {
		attrs = append(attrs, attribute.StringSlice("guardian.asset.types", uniqueSortedStrings(assetTypes)))
	}
	return attrs
}

func annotateRuntimeTaskResultSpan(span trace.Span, result *taskdomain.TaskResult) {
	attrs := []attribute.KeyValue{
		attribute.String("guardian.result.status", string(result.Status)),
		attribute.Int("guardian.result.log_count", len(result.Logs)),
	}
	if result.Error != nil && *result.Error != "" {
		attrs = append(attrs, attribute.String("guardian.result.error", *result.Error))
	}
	if result.Drift != nil {
		attrs = append(attrs,
			attribute.String("guardian.result.drift.status", result.Drift.Status),
			attribute.String("guardian.result.drift.summary", result.Drift.Summary),
		)
		if len(result.Drift.ChangedAssets) > 0 {
			attrs = append(attrs, attribute.StringSlice("guardian.result.changed_assets", uniqueSortedStrings(result.Drift.ChangedAssets)))
		}
	}
	if outputKeys := sortedKeys(result.Outputs); len(outputKeys) > 0 {
		attrs = append(attrs, attribute.StringSlice("guardian.result.output_keys", outputKeys))
	}
	span.SetAttributes(attrs...)
	span.AddEvent("guardian.task.result", trace.WithAttributes(
		attribute.String("guardian.task.result_json", runtimeTaskResultTraceJSON(result)),
	))
}

type runtimeTaskTraceSummary struct {
	TaskID            string                         `json:"taskID"`
	CorrelationID     string                         `json:"correlationID,omitempty"`
	WorkerID          string                         `json:"workerID,omitempty"`
	Partition         string                         `json:"partition"`
	Intent            string                         `json:"intent"`
	Operation         taskdomain.Operation           `json:"op"`
	TargetPusher      string                         `json:"targetPusher,omitempty"`
	Target            runtimeTaskTraceTarget         `json:"target,omitempty"`
	PartitionRevision string                         `json:"partitionRevision,omitempty"`
	IntentVersionID   string                         `json:"intentVersionID,omitempty"`
	IntentSpecHash    string                         `json:"intentSpecHash,omitempty"`
	AssetVersionIDs   map[string]string              `json:"assetVersionIDs,omitempty"`
	AssetVersions     map[string]string              `json:"assetVersions,omitempty"`
	Assets            []runtimeTaskTraceAssetSummary `json:"assets,omitempty"`
	CreatedAt         time.Time                      `json:"createdAt,omitempty"`
}

type runtimeTaskTraceTarget struct {
	Cluster   string `json:"cluster,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Region    string `json:"region,omitempty"`
	Account   string `json:"account,omitempty"`
}

type runtimeTaskTraceAssetSummary struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Version      string   `json:"version,omitempty"`
	DependsOn    []string `json:"dependsOn,omitempty"`
	PayloadKeys  []string `json:"payloadKeys,omitempty"`
	PropertyKeys []string `json:"propertyKeys,omitempty"`
}

type runtimeTaskResultTraceSummary struct {
	TaskID            string                                  `json:"taskID"`
	Status            taskdomain.ResultStatus                 `json:"status"`
	Error             string                                  `json:"error,omitempty"`
	Drift             *taskdomain.DriftReport                 `json:"drift,omitempty"`
	Health            *taskdomain.HealthObservation           `json:"health,omitempty"`
	ApplyReadiness    *taskdomain.ApplyReadiness              `json:"applyReadiness,omitempty"`
	AssetObservations map[string]*taskdomain.AssetObservation `json:"assetObservations,omitempty"`
	OutputKeys        []string                                `json:"outputKeys,omitempty"`
	Logs              []taskdomain.LogEntry                   `json:"logs,omitempty"`
	FinishedAt        time.Time                               `json:"finishedAt"`
}

func runtimeTaskTraceJSON(t *taskdomain.Task, workerID string) string {
	summary := runtimeTaskTraceSummary{
		TaskID:            t.TaskID,
		CorrelationID:     t.CorrelationID,
		WorkerID:          strings.TrimSpace(workerID),
		Partition:         t.Partition,
		Intent:            t.Intent,
		Operation:         t.Op,
		TargetPusher:      t.TargetPusher,
		Target:            runtimeTaskTraceTarget{Cluster: t.Target.Cluster, Namespace: t.Target.Namespace, Region: t.Target.Region, Account: t.Target.Account},
		PartitionRevision: t.PartitionRevision,
		IntentVersionID:   t.IntentVersionID,
		IntentSpecHash:    t.IntentSpecHash,
		AssetVersionIDs:   cloneStringMap(t.AssetVersionIDs),
		AssetVersions:     cloneStringMap(t.AssetVersions),
		Assets:            make([]runtimeTaskTraceAssetSummary, 0, len(t.Assets)),
		CreatedAt:         t.CreatedAt,
	}
	for _, asset := range t.Assets {
		summary.Assets = append(summary.Assets, runtimeTaskTraceAssetSummary{
			Name:         asset.Name,
			Type:         asset.Type,
			Version:      strings.TrimSpace(t.AssetVersions[asset.Name]),
			DependsOn:    append([]string(nil), asset.DependsOn...),
			PayloadKeys:  sortedKeys(asset.Payload),
			PropertyKeys: sortedKeys(asset.Properties),
		})
	}
	return marshalRuntimeTraceJSON(summary)
}

func runtimeTaskResultTraceJSON(result *taskdomain.TaskResult) string {
	summary := runtimeTaskResultTraceSummary{
		TaskID:     result.TaskID,
		Status:     result.Status,
		OutputKeys: sortedKeys(result.Outputs),
		Logs:       append([]taskdomain.LogEntry(nil), result.Logs...),
		FinishedAt: result.FinishedAt,
	}
	if result.Error != nil {
		summary.Error = *result.Error
	}
	if result.Drift != nil {
		driftCopy := *result.Drift
		summary.Drift = &driftCopy
	}
	if result.Health != nil {
		healthCopy := *result.Health
		summary.Health = &healthCopy
	}
	if result.ApplyReadiness != nil {
		readinessCopy := *result.ApplyReadiness
		summary.ApplyReadiness = &readinessCopy
	}
	if len(result.AssetObservations) > 0 {
		summary.AssetObservations = cloneAssetObservationMap(result.AssetObservations)
	}
	return marshalRuntimeTraceJSON(summary)
}

func buildAssetObservation(health *taskdomain.HealthObservation, readiness *taskdomain.ApplyReadiness) *taskdomain.AssetObservation {
	if isEmptyHealthObservation(health) && isEmptyApplyReadiness(readiness) {
		return nil
	}
	return &taskdomain.AssetObservation{
		Health:         cloneHealthObservation(health),
		ApplyReadiness: cloneApplyReadiness(readiness),
	}
}

func isEmptyHealthObservation(observation *taskdomain.HealthObservation) bool {
	return observation == nil || observation.Status == "" || observation.Status == taskdomain.HealthUnknown
}

func isEmptyApplyReadiness(observation *taskdomain.ApplyReadiness) bool {
	return observation == nil || observation.Status == "" || observation.Status == taskdomain.ApplyReadinessUnknown
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

func cloneAssetObservationMap(in map[string]*taskdomain.AssetObservation) map[string]*taskdomain.AssetObservation {
	if in == nil {
		return nil
	}
	out := make(map[string]*taskdomain.AssetObservation, len(in))
	for key, value := range in {
		out[key] = cloneAssetObservation(value)
	}
	return out
}

func cloneAssetObservation(in *taskdomain.AssetObservation) *taskdomain.AssetObservation {
	if in == nil {
		return nil
	}
	out := *in
	out.Health = cloneHealthObservation(in.Health)
	out.ApplyReadiness = cloneApplyReadiness(in.ApplyReadiness)
	return &out
}

func mergeHealthObservation(current, next *taskdomain.HealthObservation, assetName string) *taskdomain.HealthObservation {
	if next == nil || next.Status == "" || next.Status == taskdomain.HealthUnknown {
		return current
	}
	if current == nil || healthSeverity(next.Status) > healthSeverity(current.Status) {
		return qualifyHealthObservation(next, assetName)
	}
	if current.Summary == "" && next.Summary != "" {
		return qualifyHealthObservation(next, assetName)
	}
	return current
}

func qualifyHealthObservation(observation *taskdomain.HealthObservation, assetName string) *taskdomain.HealthObservation {
	if observation == nil {
		return nil
	}
	copy := *observation
	if assetName != "" && copy.Summary != "" {
		copy.Summary = assetName + ": " + copy.Summary
	}
	return &copy
}

func mergeApplyReadiness(current, next *taskdomain.ApplyReadiness, assetName string) *taskdomain.ApplyReadiness {
	if next == nil || next.Status == "" || next.Status == taskdomain.ApplyReadinessUnknown {
		return current
	}
	if current == nil || applyReadinessSeverity(next.Status) > applyReadinessSeverity(current.Status) {
		return qualifyApplyReadiness(next, assetName)
	}
	if current.Summary == "" && next.Summary != "" {
		return qualifyApplyReadiness(next, assetName)
	}
	return current
}

func qualifyApplyReadiness(observation *taskdomain.ApplyReadiness, assetName string) *taskdomain.ApplyReadiness {
	if observation == nil {
		return nil
	}
	copy := *observation
	if assetName != "" && copy.Summary != "" {
		copy.Summary = assetName + ": " + copy.Summary
	}
	return &copy
}

func healthSeverity(status taskdomain.HealthStatus) int {
	switch status {
	case taskdomain.HealthHealthy:
		return 1
	case taskdomain.HealthDegraded:
		return 2
	case taskdomain.HealthUnhealthy:
		return 3
	default:
		return 0
	}
}

func applyReadinessSeverity(status taskdomain.ApplyReadinessStatus) int {
	switch status {
	case taskdomain.ApplyReadinessReady:
		return 1
	case taskdomain.ApplyReadinessBlocked:
		return 2
	default:
		return 0
	}
}

func marshalRuntimeTraceJSON(value any) string {
	payload, err := json.Marshal(value)
	if err == nil {
		return string(payload)
	}
	fallback, fallbackErr := json.Marshal(map[string]string{"marshalError": err.Error()})
	if fallbackErr != nil {
		return `{"marshalError":"unknown"}`
	}
	return string(fallback)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func sortedKeys[T any](values map[string]T) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueSortedStrings(values []string) []string {
	out := uniqueStrings(values)
	sort.Strings(out)
	return out
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

func logEntry(level, asset, message string) taskdomain.LogEntry {
	return taskdomain.LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Asset:     asset,
		Message:   message,
	}
}
