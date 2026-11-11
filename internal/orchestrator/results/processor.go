package results

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	historydomain "github.com/radryc/guardian/internal/domain/history"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	releasedomain "github.com/radryc/guardian/internal/domain/release"
	statedomain "github.com/radryc/guardian/internal/domain/state"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/orchestrator/common"
	"github.com/radryc/guardian/internal/orchestrator/dispatcher"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/telemetry"
	"github.com/radryc/guardian/internal/versioning/revisions"
	"github.com/radryc/guardian/pkg/guardianapi"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricapi "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const processorScope = "guardian.results"

var (
	resultMetricsOnce    sync.Once
	resultCounter        metricapi.Int64Counter
	resultFailCounter    metricapi.Int64Counter
	resultDiscardCounter metricapi.Int64Counter
	resultDuration       metricapi.Float64Histogram
)

type Processor struct {
	store      guardianapi.Store
	dispatcher *dispatcher.Dispatcher

	failureMu sync.Mutex
	failures  map[string]resultFailure
}

// A result normally finishes in seconds. When the same result keeps failing,
// its state write can never be committed (for example a permanently unreadable
// intent-state path), and every scan retries it forever. After this window the
// result is discarded and the reconciler re-queues the intent from scratch.
const resultFailureMaxAttempts = 5

// resultFailureWindow is a variable so tests can shorten it.
var resultFailureWindow = 2 * time.Minute

type resultFailure struct {
	attempts int
	first    time.Time
}

func NewProcessor(store guardianapi.Store, dispatcher *dispatcher.Dispatcher) *Processor {
	return &Processor{store: store, dispatcher: dispatcher}
}

// recordResultFailure tracks consecutive failures for one result and reports
// whether the result should be abandoned.
func (p *Processor) recordResultFailure(taskID string) bool {
	now := time.Now()
	p.failureMu.Lock()
	defer p.failureMu.Unlock()
	if p.failures == nil {
		p.failures = make(map[string]resultFailure)
	}
	failure := p.failures[taskID]
	if failure.attempts == 0 {
		failure.first = now
	}
	failure.attempts++
	p.failures[taskID] = failure
	return failure.attempts >= resultFailureMaxAttempts && now.Sub(failure.first) >= resultFailureWindow
}

func (p *Processor) clearResultFailure(taskID string) {
	p.failureMu.Lock()
	delete(p.failures, taskID)
	p.failureMu.Unlock()
}

func (p *Processor) ProcessResult(ctx context.Context, result *taskdomain.TaskResult) error {
	attrs := resultAttributes(result)
	count, failures, discarded, duration := resultInstruments()
	if count != nil {
		count.Add(ctx, 1, metricapi.WithAttributes(attrs...))
	}
	ctx, span := otel.Tracer(processorScope).Start(ctx, "guardian.results.process", trace.WithAttributes(attrs...))
	startedAt := time.Now()
	telemetry.EmitInfo(ctx, processorScope, fmt.Sprintf("processing result %s for %s/%s", result.TaskID, result.Partition, result.Intent))
	log.Printf("results: processing task=%s op=%s partition=%s intent=%s status=%s pusher=%s", result.TaskID, result.Op, result.Partition, result.Intent, result.Status, result.Pusher)
	defer func() {
		if duration != nil {
			duration.Record(ctx, time.Since(startedAt).Seconds(), metricapi.WithAttributes(attrs...))
		}
		span.End()
	}()
	fail := func(err error) error {
		if err == nil {
			return nil
		}
		if failures != nil {
			failures.Add(ctx, 1, metricapi.WithAttributes(attrs...))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		telemetry.EmitError(ctx, processorScope, fmt.Sprintf("process result %s failed: %v", result.TaskID, err))
		if p.recordResultFailure(result.TaskID) {
			p.abandonResult(ctx, result, err, discarded)
			return nil
		}
		return err
	}

	if err := validateResultIdentity(result); err != nil {
		return fail(err)
	}
	state, err := common.LoadIntentState(ctx, p.store, result.Partition, result.Intent)
	if err != nil {
		return fail(err)
	}
	// Capture the snapshot this result started from. Writes below merge their
	// changes onto the latest state so concurrent observation updates survive.
	base := statedomain.CloneIntentState(state)
	// Skip stale results: if the intent's last task ID no longer matches this
	// result (e.g. a newer task was already queued), this result is outdated.
	// An empty LastTaskID is still verified against the queued task below.
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
	if taskFile == nil {
		return fail(fmt.Errorf("result %s has no queued task; refusing to apply an unverifiable result", result.TaskID))
	}
	if err := validateTaskResult(taskFile, result); err != nil {
		return fail(err)
	}
	if state.TargetPusher != "" && result.Pusher != state.TargetPusher {
		return fail(fmt.Errorf("result pusher %q does not match intent target pusher %q", result.Pusher, state.TargetPusher))
	}
	redactResultSecrets(taskFile, result)

	switch result.Op {
	case taskdomain.OpCheck:
		state.Timestamps.LastCheckAt = result.FinishedAt
		if result.Status == taskdomain.ResultFailed {
			state.Status = statedomain.StatusCheckFailed
			state.ApplyReadiness = &taskdomain.ApplyReadiness{Status: taskdomain.ApplyReadinessBlocked, Summary: derefErr(result.Error)}
			state.LastError = result.Error
			if err := p.persistIntentState(ctx, base, state); err != nil {
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
			if state.LastAppliedSpecHash != "" && state.IntentSpecHash != state.LastAppliedSpecHash {
				if err := p.recordRollback(ctx, state, result); err != nil {
					return fail(err)
				}
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			return nil
		}
		if state.IntentType == intentdomain.TypeObserved {
			// Report-only: never queue APPLY after a successful CHECK.
			state.Status = statedomain.StatusObserved
			state.LastError = nil
			if err := p.persistIntentState(ctx, base, state); err != nil {
				return fail(err)
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			span.SetStatus(codes.Ok, "observed")
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
		if err := p.persistIntentState(ctx, base, state); err != nil {
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
			if err := p.persistIntentState(ctx, base, state); err != nil {
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
			if state.LastAppliedSpecHash != "" && state.IntentSpecHash != state.LastAppliedSpecHash {
				if err := p.recordRollback(ctx, state, result); err != nil {
					return fail(err)
				}
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			return nil
		}
		state.Drift = result.Drift
		state.Health = statedomain.CloneHealthObservation(result.Health)
		state.ApplyReadiness = statedomain.CloneApplyReadiness(result.ApplyReadiness)
		state.AssetObservations = statedomain.CloneAssetObservationMap(result.AssetObservations)
		recordAssetObservations(result, taskFile)
		state.LastError = nil
		driftSettled := result.Drift == nil || result.Drift.Status == "InSync" || len(result.Drift.ChangedAssets) == 0
		if state.IntentType == intentdomain.TypeObserved {
			// Report-only: surface the observed state and stop. Never queue
			// CHECK or APPLY.
			if driftSettled {
				state.Status = statedomain.StatusObserved
			} else {
				state.Status = statedomain.StatusObservedDrifted
			}
			state.LastError = nil
			if err := p.persistIntentState(ctx, base, state); err != nil {
				return fail(err)
			}
			if driftSettled {
				if err := p.queueDependents(ctx, state.Partition); err != nil {
					return fail(err)
				}
			} else if result.Drift != nil {
				changedStr := strings.Join(result.Drift.ChangedAssets, ", ")
				_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
					Partition: result.Partition,
					Intent:    result.Intent,
					Type:      "drift.detected",
					Message:   valueOrDefault(result.Drift.Summary, "observed drift detected"),
					TaskID:    result.TaskID,
					Details: map[string]string{
						"summary":        valueOrDefault(result.Drift.Summary, ""),
						"changed_assets": changedStr,
						"intentType":     intentdomain.TypeObserved,
					},
				})
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			span.SetStatus(codes.Ok, "observed")
			return nil
		}
		if driftSettled && applyResultSettled(result) {
			state.Status = statedomain.StatusHealthy
			if err := p.persistIntentState(ctx, base, state); err != nil {
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
		if driftSettled {
			state.LastError = nil
			state.Status = common.QueuedStatus(state.Status, taskdomain.OpDiff)
			state.Drift = statedomain.CloneDriftReport(result.Drift)
			if state.Drift == nil || state.Drift.Status == "InSync" || len(state.Drift.ChangedAssets) == 0 {
				state.Drift = &taskdomain.DriftReport{Status: "Changed", Summary: applyNotReadySummary(result)}
			}
			next, err := p.nextTask(ctx, taskFile, state, taskdomain.OpDiff)
			if err != nil {
				return fail(err)
			}
			state.LastTaskID = next.TaskID
			state.Timestamps.LastQueuedAt = next.CreatedAt
			state.Timestamps.LastDiffAt = next.CreatedAt
			if err := p.dispatcher.QueueTask(ctx, next); err != nil {
				return fail(err)
			}
			if err := p.persistIntentState(ctx, base, state); err != nil {
				return fail(err)
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			span.SetStatus(codes.Ok, "awaiting observation readiness")
			telemetry.EmitWarn(ctx, processorScope, fmt.Sprintf("diff for %s/%s is waiting on observation readiness", state.Partition, state.Intent))
			return nil
		}
		if state.Locked || state.PartitionMode == "readonly" {
			state.Status = statedomain.StatusDriftedLocked
			if err := p.persistIntentState(ctx, base, state); err != nil {
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
		if err := p.persistIntentState(ctx, base, state); err != nil {
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
		if state.IntentType == intentdomain.TypeObserved {
			// Defense in depth: observed intents must never be applied.
			return fail(fmt.Errorf("observed intent %s/%s must not receive APPLY tasks", result.Partition, result.Intent))
		}
		if result.Status == taskdomain.ResultFailed {
			state.Status = statedomain.StatusApplyFailed
			state.LastError = result.Error
			if err := p.persistIntentState(ctx, base, state); err != nil {
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
			skipRollback := taskFile != nil && taskFile.ForceApply
			if state.LastAppliedSpecHash != "" && state.IntentSpecHash != state.LastAppliedSpecHash && !skipRollback {
				if err := p.recordRollback(ctx, state, result); err != nil {
					return fail(err)
				}
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			return nil
		}
		state.Health = statedomain.CloneHealthObservation(result.Health)
		state.ApplyReadiness = statedomain.CloneApplyReadiness(result.ApplyReadiness)
		state.AssetObservations = statedomain.CloneAssetObservationMap(result.AssetObservations)
		recordAssetObservations(result, taskFile)
		state.Outputs = copyStringMap(result.Outputs)
		if !applyResultSettled(result) {
			state.LastError = nil
			state.Status = common.QueuedStatus(state.Status, taskdomain.OpDiff)
			state.Drift = statedomain.CloneDriftReport(result.Drift)
			if state.Drift == nil {
				state.Drift = &taskdomain.DriftReport{Status: "Changed", Summary: applyNotReadySummary(result)}
			}
			next, err := p.nextTask(ctx, taskFile, state, taskdomain.OpDiff)
			if err != nil {
				return fail(err)
			}
			state.LastTaskID = next.TaskID
			state.Timestamps.LastQueuedAt = next.CreatedAt
			state.Timestamps.LastDiffAt = next.CreatedAt
			if err := p.dispatcher.QueueTask(ctx, next); err != nil {
				return fail(err)
			}
			if err := p.persistIntentState(ctx, base, state); err != nil {
				return fail(err)
			}
			p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
			span.SetStatus(codes.Ok, "awaiting dependency readiness")
			telemetry.EmitWarn(ctx, processorScope, fmt.Sprintf("apply for %s/%s is waiting on dependency readiness", state.Partition, state.Intent))
			return nil
		}
		preDrift := state.Drift
		rollbackTo := state.RollbackTo
		selfHealing := state.LastAppliedSpecHash != "" && state.IntentSpecHash == state.LastAppliedSpecHash
		state.LastAppliedSpecHash = state.IntentSpecHash
		state.Status = statedomain.StatusHealthy
		state.LastError = nil
		state.RollbackTo = ""
		state.Drift = &taskdomain.DriftReport{Status: "InSync", Summary: "apply completed"}
		state.DeploymentRevision = revisions.DeploymentRevisionID(state.PartitionRevision, state.IntentVersionID, result.FinishedAt)
		if err := p.persistIntentState(ctx, base, state); err != nil {
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
			Rollback:           rollbackTo != "",
			RollbackTo:         rollbackTo,
			RollbackReason:     state.RollbackReason,
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
		// Advance the canonical partition release log so the UI's latest-release
		// view reads from one source instead of walking the archive directory.
		relRec := &releasedomain.ReleaseRecord{
			APIVersion:         "guardian/v1alpha1",
			Kind:               "ReleaseRecord",
			DeploymentRevision: rec.DeploymentRevision,
			Partition:          rec.Partition,
			Intent:             rec.Intent,
			PartitionRevision:  rec.PartitionRevision,
			IntentVersionID:    rec.IntentVersionID,
			AssetVersionIDs:    copyStringMap(rec.AssetVersionIDs),
			AssetVersions:      copyStringMap(rec.AssetVersions),
			TaskIDs:            append([]string(nil), rec.TaskIDs...),
			SelfHealing:        rec.SelfHealing,
			Rollback:           rec.Rollback,
			RollbackTo:         rec.RollbackTo,
			RollbackReason:     rec.RollbackReason,
			Outputs:            copyStringMap(rec.Outputs),
			CreatedAt:          rec.CreatedAt,
		}
		if err := p.dispatcher.WriteRelease(ctx, relRec); err != nil {
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
			if err := p.persistIntentState(ctx, base, state); err != nil {
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
		if err := p.persistIntentState(ctx, base, state); err != nil {
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

func validateResultIdentity(result *taskdomain.TaskResult) error {
	if result == nil {
		return fmt.Errorf("result is nil")
	}
	if err := paths.ValidatePartitionName(result.Partition); err != nil {
		return fmt.Errorf("result partition: %w", err)
	}
	if err := paths.ValidateIntentName(result.Intent); err != nil {
		return fmt.Errorf("result intent: %w", err)
	}
	if err := paths.ValidateQueueIdentity(result.Pusher, result.TaskID); err != nil {
		return fmt.Errorf("result queue identity: %w", err)
	}
	return nil
}

func validateTaskResult(task *taskdomain.Task, result *taskdomain.TaskResult) error {
	if task == nil || result == nil {
		return fmt.Errorf("task and result are required")
	}
	if task.TaskID != result.TaskID {
		return fmt.Errorf("result task id %q does not match queued task %q", result.TaskID, task.TaskID)
	}
	if task.Op != result.Op {
		return fmt.Errorf("result op %q does not match queued task op %q", result.Op, task.Op)
	}
	if task.Partition != result.Partition {
		return fmt.Errorf("result partition %q does not match queued task partition %q", result.Partition, task.Partition)
	}
	if task.Intent != result.Intent {
		return fmt.Errorf("result intent %q does not match queued task intent %q", result.Intent, task.Intent)
	}
	if task.TargetPusher != "" && task.TargetPusher != result.Pusher {
		return fmt.Errorf("result pusher %q does not match queued task pusher %q", result.Pusher, task.TargetPusher)
	}
	return nil
}

func redactResultSecrets(task *taskdomain.Task, result *taskdomain.TaskResult) {
	if task == nil || result == nil {
		return
	}
	names := task.SecretAssetNames()
	if len(names) == 0 {
		return
	}
	secrets := make([]string, 0, len(names)+1)
	for _, name := range names {
		if value := result.Outputs[name+".value"]; strings.TrimSpace(value) != "" && value != taskdomain.RedactedValue {
			secrets = append(secrets, value)
		}
	}
	if value := result.Outputs["value"]; strings.TrimSpace(value) != "" && value != taskdomain.RedactedValue {
		secrets = append(secrets, value)
	}
	result.Outputs = taskdomain.RedactSecretOutputs(task, result.Outputs)
	if len(secrets) == 0 {
		return
	}
	for i := range result.Logs {
		result.Logs[i].Message = taskdomain.RedactSecretStrings([]string{result.Logs[i].Message}, secrets)[0]
	}
	if result.Error != nil {
		redacted := taskdomain.RedactSecretStrings([]string{*result.Error}, secrets)[0]
		result.Error = &redacted
	}
}

func (p *Processor) loadTask(ctx context.Context, pusher, taskID string) (*taskdomain.Task, error) {
	if err := paths.ValidateQueueIdentity(pusher, taskID); err != nil {
		return nil, err
	}
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

// persistIntentState merges the changes between base and state into the latest
// persisted intent state. State is loaded once per result, so writing it back
// verbatim would clobber observations (Resources, LastObservedAt) persisted by
// the observed collector while the task was being processed.
func (p *Processor) persistIntentState(ctx context.Context, base, state *statedomain.IntentState) error {
	if state == nil {
		return nil
	}
	_, err := p.dispatcher.UpdateIntentState(ctx, state.Partition, state.Intent, func(latest *statedomain.IntentState) error {
		statedomain.MergeIntentState(latest, base, state)
		return nil
	})
	return err
}

// DiscardStaleResult removes the queue task/claim/result for a result whose
// intent no longer tracks it. The live-result scan ignores such results, so
// without this they accumulate until the queue is full.
func (p *Processor) DiscardStaleResult(ctx context.Context, pusher, taskID string) {
	p.cleanupQueueArtifacts(ctx, pusher, taskID)
}

// abandonResult drops a result that has failed for longer than
// resultFailureWindow so one poison result cannot be retried on every scan
// forever. The reconciler recompiles the intent from its manifest and queues a
// fresh task, which also clears any state the result could not commit.
func (p *Processor) abandonResult(ctx context.Context, result *taskdomain.TaskResult, cause error, discarded metricapi.Int64Counter) {
	message := fmt.Sprintf("result %s discarded after repeated failures for %s/%s: %v", result.TaskID, result.Partition, result.Intent, cause)
	log.Printf("results: %s", message)
	if discarded != nil {
		discarded.Add(ctx, 1, metricapi.WithAttributes(resultAttributes(result)...))
	}
	_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
		Partition: result.Partition,
		Intent:    result.Intent,
		Type:      "task.failed",
		Message:   message,
		TaskID:    result.TaskID,
		Details: map[string]string{
			"op":    string(result.Op),
			"error": cause.Error(),
		},
	})
	p.cleanupQueueArtifacts(ctx, result.Pusher, result.TaskID)
}

func (p *Processor) cleanupQueueArtifacts(ctx context.Context, pusher, taskID string) {
	if err := paths.ValidateQueueIdentity(pusher, taskID); err != nil {
		telemetry.EmitWarn(ctx, processorScope, fmt.Sprintf("refusing to clean queue artifacts: %v", err))
		return
	}
	p.clearResultFailure(taskID)
	_, err := p.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{
			{LogicalPath: paths.QueueTask(pusher, taskID)},
			{LogicalPath: paths.QueueClaim(pusher, taskID)},
			{LogicalPath: paths.QueueResult(pusher, taskID)},
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
	// Reuse the in-flight task's payload only when it still matches the desired
	// spec. If a newer manifest was pushed while the task was running, rebuild
	// from the manifest so the rollout picks up the new spec instead of being
	// pinned to the previous task's assets.
	if currentTask != nil && state.IntentSpecHash == currentTask.IntentSpecHash {
		if next := common.BuildTaskFromExisting(currentTask, op); next != nil {
			return next, nil
		}
	}
	states, err := common.LoadAllIntentStates(ctx, p.store, state.Partition)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if states == nil {
		states = map[string]*statedomain.IntentState{}
	}
	states[state.Intent] = state
	return common.BuildTask(ctx, p.store, state, op, common.IntentOutputs(states), false)
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
		// Observed intents are report-only and owned by the observed collector;
		// never queue reconciliation tasks for them.
		if state.IntentType == intentdomain.TypeObserved {
			continue
		}
		if !common.DependenciesHealthy(state, states) {
			continue
		}
		prevStatus := state.Status
		base := statedomain.CloneIntentState(state)
		next, err := common.BuildTask(ctx, p.store, state, taskdomain.OpDiff, outputs, false)
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
		if err := p.persistIntentState(ctx, base, state); err != nil {
			return err
		}
		if prevStatus == statedomain.StatusBlocked {
			_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
				Partition: partition,
				Intent:    name,
				Type:      "intent.unblocked",
				Message:   "Dependencies satisfied; queued reconciliation",
				Details: map[string]string{
					"taskID": next.TaskID,
				},
			})
		}
		states[name] = state
		outputs[name] = copyStringMap(state.Outputs)
	}
	return nil
}

// recordRollback records that a rollout failed and the intent should be viewed
// as reverted to state.DeploymentRevision. It deliberately does NOT rewrite the
// live intent manifest: the operator's freshly pushed spec is the source of
// truth and must survive so the next reconcile retries it automatically. Only
// the last known-good deployment revision is recorded for history/UI.
func (p *Processor) recordRollback(ctx context.Context, state *statedomain.IntentState, result *taskdomain.TaskResult) error {
	if state.DeploymentRevision == "" {
		return fmt.Errorf("cannot rollback %s/%s: no previous deployment revision", state.Partition, state.Intent)
	}
	base := statedomain.CloneIntentState(state)
	rollbackReason := derefErr(result.Error)
	state.RollbackTo = state.DeploymentRevision
	state.RollbackReason = rollbackReason
	if state.LastError == nil {
		state.LastError = &rollbackReason
	}
	state.Drift = nil
	state.Health = nil
	state.ApplyReadiness = nil
	state.AssetObservations = nil
	if err := p.persistIntentState(ctx, base, state); err != nil {
		return err
	}
	correlationID := revisions.NewCorrelationID()
	_ = p.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
		Partition:          state.Partition,
		Intent:             state.Intent,
		Type:               "rollback.triggered",
		Message:            fmt.Sprintf("rollout failed; reverting to deployment %s after %s failure: %s (desired manifest preserved)", state.DeploymentRevision, result.Op, rollbackReason),
		TaskID:             result.TaskID,
		DeploymentRevision: state.DeploymentRevision,
		CorrelationID:      correlationID,
		Details: map[string]string{
			"failed_task_id": result.TaskID,
			"failed_op":      string(result.Op),
			"error":          rollbackReason,
			"manifest":       "preserved",
		},
	})
	log.Printf("results: rollout failed for %s/%s; desired manifest preserved, last good deployment %s after %s failure: %s", state.Partition, state.Intent, state.DeploymentRevision, result.Op, rollbackReason)
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
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func applyResultSettled(result *taskdomain.TaskResult) bool {
	if result == nil {
		return true
	}
	if result.Health != nil && result.Health.Status != taskdomain.HealthHealthy {
		return false
	}
	if result.ApplyReadiness != nil && result.ApplyReadiness.Status != taskdomain.ApplyReadinessReady {
		return false
	}
	return true
}

func applyNotReadySummary(result *taskdomain.TaskResult) string {
	if result == nil {
		return "apply completed but asset is not ready"
	}
	if result.ApplyReadiness != nil && strings.TrimSpace(result.ApplyReadiness.Summary) != "" {
		return result.ApplyReadiness.Summary
	}
	if result.Health != nil && strings.TrimSpace(result.Health.Summary) != "" {
		return result.Health.Summary
	}
	return "apply completed but asset is not ready"
}

func collectTaskIDs(taskFile *taskdomain.Task, current string) []string {
	ids := []string{current}
	if taskFile != nil && taskFile.TaskID != "" && taskFile.TaskID != current {
		ids = append(ids, taskFile.TaskID)
	}
	return ids
}

func sortStrings(values []string) {
	sort.Strings(values)
}

func resultInstruments() (metricapi.Int64Counter, metricapi.Int64Counter, metricapi.Int64Counter, metricapi.Float64Histogram) {
	resultMetricsOnce.Do(func() {
		meter := otel.Meter(processorScope)
		var err error
		resultCounter, err = meter.Int64Counter("guardian.results.executions")
		if err != nil {
			telemetry.EmitError(context.Background(), processorScope, fmt.Sprintf("create result counter: %v", err))
		}
		resultFailCounter, err = meter.Int64Counter("guardian.results.failures")
		if err != nil {
			telemetry.EmitError(context.Background(), processorScope, fmt.Sprintf("create result fail counter: %v", err))
		}
		resultDiscardCounter, err = meter.Int64Counter("guardian.results.discarded")
		if err != nil {
			telemetry.EmitError(context.Background(), processorScope, fmt.Sprintf("create result discard counter: %v", err))
		}
		resultDuration, err = meter.Float64Histogram("guardian.results.duration.seconds", metricapi.WithUnit("s"))
		if err != nil {
			telemetry.EmitError(context.Background(), processorScope, fmt.Sprintf("create result duration histogram: %v", err))
		}
	})
	return resultCounter, resultFailCounter, resultDiscardCounter, resultDuration
}

func resultAttributes(result *taskdomain.TaskResult) []attribute.KeyValue {
	if result == nil {
		return nil
	}
	attrs := []attribute.KeyValue{
		attribute.String("guardian.target.partition", result.Partition),
		attribute.String("guardian.target.intent", result.Intent),
		attribute.String("guardian.operation", string(result.Op)),
		attribute.String("guardian.task.id", result.TaskID),
		attribute.String("guardian.status", string(result.Status)),
	}
	if result.Pusher != "" {
		attrs = append(attrs, attribute.String("guardian.target.pusher", result.Pusher))
	}
	return attrs
}
