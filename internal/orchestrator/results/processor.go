package results

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/telemetry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricapi "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const processorScope = "guardian/results"

var (
	resultMetricsOnce sync.Once
	resultCounter     metricapi.Int64Counter
	resultFailCounter metricapi.Int64Counter
	resultDuration    metricapi.Float64Histogram
)

type Processor struct {
	store      guardianapi.Store
	dispatcher *dispatcher.Dispatcher
}

func NewProcessor(store guardianapi.Store, dispatcher *dispatcher.Dispatcher) *Processor {
	return &Processor{store: store, dispatcher: dispatcher}
}

func (p *Processor) ProcessResult(ctx context.Context, result *taskdomain.TaskResult) error {
	attrs := resultAttributes(result)
	count, failures, duration := resultInstruments()
	count.Add(ctx, 1, metricapi.WithAttributes(attrs...))
	ctx, span := otel.Tracer(processorScope).Start(ctx, "guardian.result.process", trace.WithAttributes(attrs...))
	startedAt := time.Now()
	telemetry.EmitInfo(ctx, processorScope, fmt.Sprintf("processing result %s for %s/%s", result.TaskID, result.Partition, result.Intent))
	log.Printf("results: processing task=%s op=%s partition=%s intent=%s status=%s pusher=%s", result.TaskID, result.Op, result.Partition, result.Intent, result.Status, result.Pusher)
	defer func() {
		duration.Record(ctx, time.Since(startedAt).Seconds(), metricapi.WithAttributes(attrs...))
		span.End()
	}()
	fail := func(err error) error {
		if err == nil {
			return nil
		}
		failures.Add(ctx, 1, metricapi.WithAttributes(attrs...))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		telemetry.EmitError(ctx, processorScope, fmt.Sprintf("process result %s failed: %v", result.TaskID, err))
		return err
	}

	state, err := common.LoadIntentState(ctx, p.store, result.Partition, result.Intent)
	if err != nil {
		return fail(err)
	}
	// Skip stale results: if the intent's last task ID no longer matches this
	// result (e.g. a newer task was already queued), this result is outdated.
	if state.LastTaskID != "" && state.LastTaskID != result.TaskID {
		log.Printf("results: skipping stale result task=%s (current task=%s) for %s/%s", result.TaskID, state.LastTaskID, result.Partition, result.Intent)
		p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
		span.SetStatus(codes.Ok, "stale")
		return nil
	}
	taskFile, err := p.loadTask(ctx, result.Pusher, result.TaskID)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}

	switch result.Op {
	case taskdomain.OpCheck:
		state.Timestamps.LastCheckAt = result.FinishedAt
		if result.Status == taskdomain.ResultFailed {
			state.Status = statedomain.StatusCheckFailed
			state.ApplyReadiness = &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessBlocked, Summary: derefErr(result.Error)}
			state.LastError = result.Error
			if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
				return fail(err)
			}
			errMsg := derefErr(result.Error)
			_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
				Partition: result.Partition,
				Intent:    result.Intent,
				Type:      "task.failed",
				Message:   "CHECK failed: " + errMsg,
				TaskID:    result.TaskID,
				Details:   map[string]string{"op": "CHECK", "error": errMsg},
			})
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			return nil
		}
		next, err := p.nextTask(ctx, taskFile, state, taskdomain.OpApply)
		if err != nil {
			return fail(err)
		}
		state.Status = common.QueuedStatus(state.Status, taskdomain.OpApply)
		state.LastTaskID = next.TaskID
		state.LastError = nil
		state.ApplyReadiness = &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessReady, Summary: "preflight checks passed"}
		state.Timestamps.LastQueuedAt = next.CreatedAt
		state.Timestamps.LastApplyAt = next.CreatedAt
		if err := p.dispatcher.QueueTask(ctx, next); err != nil {
			return fail(err)
		}
		log.Printf("results: queued apply task=%s from check task=%s partition=%s intent=%s pusher=%s", next.TaskID, result.TaskID, state.Partition, state.Intent, state.TargetPusher)
		if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
			return fail(err)
		}
		p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
		span.SetStatus(codes.Ok, "")
		telemetry.EmitInfo(ctx, processorScope, fmt.Sprintf("queued apply for %s/%s", state.Partition, state.Intent))
		return nil

	case taskdomain.OpDiff:
		state.Timestamps.LastDiffAt = result.FinishedAt
		if result.Status == taskdomain.ResultFailed {
			state.Status = statedomain.StatusDiffFailed
			state.Health = nil
			state.ApplyReadiness = nil
			state.AssetObservations = nil
			state.LastError = result.Error
			if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
				return fail(err)
			}
			errMsg := derefErr(result.Error)
			_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
				Partition: result.Partition,
				Intent:    result.Intent,
				Type:      "task.failed",
				Message:   "DIFF failed: " + errMsg,
				TaskID:    result.TaskID,
				Details:   map[string]string{"op": "DIFF", "error": errMsg},
			})
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			return nil
		}
		state.Drift = result.Drift
		state.Health = cloneHealthObservation(result.Health)
		state.ApplyReadiness = cloneApplyReadiness(result.ApplyReadiness)
		state.AssetObservations = cloneAssetObservationMap(result.AssetObservations)
		state.LastError = nil
		if result.Drift == nil || result.Drift.Status == "InSync" || len(result.Drift.ChangedAssets) == 0 {
			state.Status = statedomain.StatusHealthy
			if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
				return fail(err)
			}
			if err := p.queueDependents(ctx, state.Partition); err != nil {
				return fail(err)
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			span.SetStatus(codes.Ok, "")
			telemetry.EmitInfo(ctx, processorScope, fmt.Sprintf("intent %s/%s is healthy", state.Partition, state.Intent))
			return nil
		}
		if state.Locked || state.PartitionMode == "readonly" {
			state.Status = statedomain.StatusDriftedLocked
			if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
				return fail(err)
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			span.SetStatus(codes.Ok, "")
			if state.PartitionMode == "readonly" {
				telemetry.EmitWarn(ctx, processorScope, fmt.Sprintf("intent %s/%s drifted in readonly partition", state.Partition, state.Intent))
			} else {
				telemetry.EmitWarn(ctx, processorScope, fmt.Sprintf("intent %s/%s drifted while locked", state.Partition, state.Intent))
			}
			return nil
		}
		next, err := p.nextTask(ctx, taskFile, state, taskdomain.OpCheck)
		if err != nil {
			return fail(err)
		}
		state.Status = common.QueuedStatus(state.Status, taskdomain.OpCheck)
		state.LastTaskID = next.TaskID
		state.Timestamps.LastQueuedAt = next.CreatedAt
		state.Timestamps.LastCheckAt = next.CreatedAt
		if err := p.dispatcher.QueueTask(ctx, next); err != nil {
			return fail(err)
		}
		if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
			return fail(err)
		}
		p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
		span.SetStatus(codes.Ok, "")
		telemetry.EmitWarn(ctx, processorScope, fmt.Sprintf("queued check for drifted intent %s/%s", state.Partition, state.Intent))
		changedStr := strings.Join(result.Drift.ChangedAssets, ", ")
		_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
			Partition: result.Partition,
			Intent:    result.Intent,
			Type:      "drift.detected",
			Message:   valueOrDefault(result.Drift.Summary, "drift detected"),
			TaskID:    result.TaskID,
			Details: map[string]string{
				"summary":        valueOrDefault(result.Drift.Summary, ""),
				"changed_assets": changedStr,
			},
		})
		return nil

	case taskdomain.OpApply:
		state.Timestamps.LastApplyAt = result.FinishedAt
		if result.Status == taskdomain.ResultFailed {
			state.Status = statedomain.StatusApplyFailed
			state.LastError = result.Error
			if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
				return fail(err)
			}
			errMsg := derefErr(result.Error)
			_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
				Partition: result.Partition,
				Intent:    result.Intent,
				Type:      "task.failed",
				Message:   "APPLY failed: " + errMsg,
				TaskID:    result.TaskID,
				Details:   map[string]string{"op": "APPLY", "error": errMsg},
			})
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			return nil
		}
		preDrift := state.Drift
		selfHealing := state.LastAppliedSpecHash != "" && state.IntentSpecHash == state.LastAppliedSpecHash
		state.LastAppliedSpecHash = state.IntentSpecHash
		state.Status = statedomain.StatusHealthy
		state.LastError = nil
		state.Health = cloneHealthObservation(result.Health)
		state.ApplyReadiness = cloneApplyReadiness(result.ApplyReadiness)
		state.AssetObservations = cloneAssetObservationMap(result.AssetObservations)
		state.Outputs = copyStringMap(result.Outputs)
		state.Drift = &taskdomain.DriftReport{Status: "InSync", Summary: "apply completed"}
		state.DeploymentRevision = revisions.DeploymentRevisionID(state.PartitionRevision, state.IntentVersionID, result.FinishedAt)
		if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
			return fail(err)
		}
		rec := &historydomain.DeploymentRecord{
			APIVersion:         "guardian/v1alpha1",
			Kind:               "DeploymentRecord",
			DeploymentRevision: state.DeploymentRevision,
			Partition:          state.Partition,
			Intent:             state.Intent,
			Target:             state.Target,
			PartitionRevision:  state.PartitionRevision,
			IntentVersionID:    state.IntentVersionID,
			AssetVersionIDs:    copyStringMap(state.AssetVersionIDs),
			AssetVersions:      copyStringMap(state.AssetVersions),
			TaskIDs:            collectTaskIDs(taskFile, result.TaskID),
			ChangedAssets:      preDriftAssets(preDrift),
			SelfHealing:        selfHealing,
			Outputs:            copyStringMap(result.Outputs),
			CreatedAt:          result.FinishedAt,
		}
		manifestContent, err := p.loadIntentManifest(ctx, state)
		if err != nil {
			return fail(err)
		}
		if err := p.dispatcher.ArchiveDeployment(ctx, rec, manifestContent, result.Logs); err != nil {
			return fail(err)
		}
		if releaseTag := releaseTagFromAssetVersions(rec.AssetVersions); releaseTag != "" {
			log.Printf(
				"results: release task=%s op=%s partition=%s intent=%s status=%s release=%s deployment_revision=%s partition_revision=%s pusher=%s",
				result.TaskID,
				result.Op,
				result.Partition,
				result.Intent,
				result.Status,
				releaseTag,
				state.DeploymentRevision,
				state.PartitionRevision,
				result.Pusher,
			)
		}
		if err := p.queueDependents(ctx, state.Partition); err != nil {
			return fail(err)
		}
		p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
		span.SetStatus(codes.Ok, "")
		telemetry.EmitInfo(ctx, processorScope, fmt.Sprintf("applied intent %s/%s", state.Partition, state.Intent))
		changedAssets := preDriftAssets(preDrift)
		_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
			Partition:          result.Partition,
			Intent:             result.Intent,
			Type:               "deploy.completed",
			Message:            fmt.Sprintf("applied %d asset(s)", len(changedAssets)),
			TaskID:             result.TaskID,
			DeploymentRevision: state.DeploymentRevision,
			Details: map[string]string{
				"changed_assets":      strings.Join(changedAssets, ", "),
				"deployment_revision": state.DeploymentRevision,
			},
		})
		return nil

	case taskdomain.OpDestroy:
		if result.Status == taskdomain.ResultFailed {
			state.Status = statedomain.StatusApplyFailed
			state.LastError = result.Error
			if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
				return fail(err)
			}
			errMsg := derefErr(result.Error)
			_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
				Partition: result.Partition,
				Intent:    result.Intent,
				Type:      "task.failed",
				Message:   "DESTROY failed: " + errMsg,
				TaskID:    result.TaskID,
				Details:   map[string]string{"op": "DESTROY", "error": errMsg},
			})
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			return nil
		}
		state.Status = statedomain.StatusDestroyed
		state.LastError = nil
		state.Drift = nil
		state.Health = nil
		state.ApplyReadiness = nil
		state.AssetObservations = nil
		state.Outputs = map[string]string{}
		if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
			return fail(err)
		}
		p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
		span.SetStatus(codes.Ok, "")
		telemetry.EmitInfo(ctx, processorScope, fmt.Sprintf("destroyed intent %s/%s", state.Partition, state.Intent))
		return nil
	default:
		return fail(fmt.Errorf("unsupported result operation %q", result.Op))
	}
}

func (p *Processor) loadTask(ctx context.Context, pusher, taskID string) (*taskdomain.Task, error) {
	data, err := p.store.ReadFile(ctx, paths.QueueTask(pusher, taskID))
	if err != nil {
		return nil, err
	}
	var task taskdomain.Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (p *Processor) cleanupQueueArtifacts(ctx context.Context, pusher, taskID string) {
	if pusher == "" || taskID == "" {
		return
	}
	_, err := p.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{
			{LogicalPath: paths.QueueTask(pusher, taskID)},
			{LogicalPath: paths.QueueClaim(pusher, taskID)},
		},
		Context: guardianapi.MutationContext{
			PrincipalID:   "guardiand",
			Reason:        "cleanup processed task",
			CorrelationID: taskID,
		},
	})
	if err != nil {
		telemetry.EmitWarn(ctx, processorScope, fmt.Sprintf("cleanup queue artifacts for %s failed: %v", taskID, err))
	}
}

func releaseTagFromAssetVersions(assetVersions map[string]string) string {
	tag := ""
	for _, version := range assetVersions {
		version = strings.TrimSpace(version)
		if version == "" {
			continue
		}
		if tag == "" {
			tag = version
			continue
		}
		if tag != version {
			return ""
		}
	}
	return tag
}

func (p *Processor) nextTask(ctx context.Context, currentTask *taskdomain.Task, state *statedomain.IntentState, op taskdomain.Operation) (*taskdomain.Task, error) {
	if next := common.BuildTaskFromExisting(currentTask, op); next != nil {
		return next, nil
	}
	states, err := common.LoadAllIntentStates(ctx, p.store, state.Partition)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if states == nil {
		states = map[string]*statedomain.IntentState{}
	}
	states[state.Intent] = state
	return common.BuildTask(ctx, p.store, state, op, common.IntentOutputs(states))
}

func (p *Processor) loadIntentManifest(ctx context.Context, state *statedomain.IntentState) ([]byte, error) {
	logicalPath := paths.IntentManifest(state.Partition, state.Intent)
	if state.IntentVersionID != "" {
		version, err := p.store.GetVersion(ctx, logicalPath, state.IntentVersionID)
		if err == nil {
			return version.Content, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return p.store.ReadFile(ctx, logicalPath)
}

func (p *Processor) queueDependents(ctx context.Context, partition string) error {
	states, err := common.LoadAllIntentStates(ctx, p.store, partition)
	if err != nil {
		return err
	}
	outputs := common.IntentOutputs(states)
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		state := states[name]
		activeTask, err := common.HasActiveTask(ctx, p.store, state)
		if err != nil {
			return err
		}
		if state == nil || activeTask || state.Status == statedomain.StatusHealthy || state.Status == statedomain.StatusDestroyed || state.Status == statedomain.StatusDestroying {
			continue
		}
		if !common.DependenciesHealthy(state, states) {
			continue
		}
		next, err := common.BuildTask(ctx, p.store, state, taskdomain.OpDiff, outputs)
		if err != nil {
			return err
		}
		state.Status = common.QueuedStatus(state.Status, taskdomain.OpDiff)
		state.LastTaskID = next.TaskID
		state.LastError = nil
		state.Timestamps.LastQueuedAt = next.CreatedAt
		state.Timestamps.LastDiffAt = next.CreatedAt
		if err := p.dispatcher.QueueTask(ctx, next); err != nil {
			return err
		}
		if err := p.dispatcher.WriteIntentState(ctx, state); err != nil {
			return err
		}
		states[name] = state
		outputs[name] = copyStringMap(state.Outputs)
	}
	return nil
}

func derefErr(p *string) string {
	if p == nil {
		return "unknown error"
	}
	return *p
}

func preDriftAssets(drift *taskdomain.DriftReport) []string {
	if drift == nil || len(drift.ChangedAssets) == 0 {
		return nil
	}
	out := make([]string, len(drift.ChangedAssets))
	copy(out, drift.ChangedAssets)
	return out
}

func valueOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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

func collectTaskIDs(taskFile *taskdomain.Task, current string) []string {
	ids := []string{current}
	if taskFile != nil && taskFile.TaskID != "" && taskFile.TaskID != current {
		ids = append(ids, taskFile.TaskID)
	}
	return ids
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func resultInstruments() (metricapi.Int64Counter, metricapi.Int64Counter, metricapi.Float64Histogram) {
	resultMetricsOnce.Do(func() {
		meter := otel.Meter(processorScope)
		resultCounter, _ = meter.Int64Counter("guardian.result.executions")
		resultFailCounter, _ = meter.Int64Counter("guardian.result.failures")
		resultDuration, _ = meter.Float64Histogram("guardian.result.duration")
	})
	return resultCounter, resultFailCounter, resultDuration
}

func resultAttributes(result *taskdomain.TaskResult) []attribute.KeyValue {
	if result == nil {
		return nil
	}
	attrs := []attribute.KeyValue{
		attribute.String("guardian.partition", result.Partition),
		attribute.String("guardian.intent", result.Intent),
		attribute.String("guardian.operation", string(result.Op)),
		attribute.String("guardian.task.id", result.TaskID),
		attribute.String("guardian.status", string(result.Status)),
	}
	if result.Pusher != "" {
		attrs = append(attrs, attribute.String("guardian.pusher", result.Pusher))
	}
	return attrs
}
