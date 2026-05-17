package reconciler

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"github.com/rydzu/ainfra/guardian/internal/compiler/planner"
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

const reconcilerScope = "guardian/reconciler"

var (
	reconcileMetricsOnce sync.Once
	reconcileCounter     metricapi.Int64Counter
	reconcileFailCounter metricapi.Int64Counter
	reconcileDuration    metricapi.Float64Histogram
)

type Reconciler struct {
	store          guardianapi.Store
	dispatcher     *dispatcher.Dispatcher
	interval       time.Duration
	staleTaskAfter time.Duration // tasks older than this are treated as dead and re-queued
	parallelism    int
	partitionLocks sync.Map
}

func NewReconciler(store guardianapi.Store, dispatcher *dispatcher.Dispatcher, interval time.Duration) *Reconciler {
	return NewReconcilerWithOptions(store, dispatcher, interval, 0)
}

func NewReconcilerWithOptions(store guardianapi.Store, dispatcher *dispatcher.Dispatcher, interval, staleTaskAfter time.Duration) *Reconciler {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if staleTaskAfter <= 0 {
		staleTaskAfter = 5 * time.Minute
	}
	return &Reconciler{
		store:          store,
		dispatcher:     dispatcher,
		interval:       interval,
		staleTaskAfter: staleTaskAfter,
		parallelism:    defaultParallelism(),
	}
}

func (r *Reconciler) Run(ctx context.Context) error {
	log.Printf("reconciler: starting with interval=%s", r.interval)
	if err := r.reconcileAll(ctx); err != nil {
		log.Printf("reconciler: initial reconcile error (will retry): %v", err)
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.reconcileAll(ctx); err != nil {
				log.Printf("reconciler: reconcile cycle error (will retry in %s): %v", r.interval, err)
			}
		}
	}
}

// ReconcileAll performs a single full reconcile cycle across all partitions.
func (r *Reconciler) ReconcileAll(ctx context.Context) error {
	return r.reconcileAll(ctx)
}

func (r *Reconciler) ReconcilePartition(ctx context.Context, partitionName string, force bool) error {
	partitionLock := r.partitionLock(partitionName)
	partitionLock.Lock()
	defer partitionLock.Unlock()
	return r.reconcilePartition(ctx, partitionName, force)
}

func (r *Reconciler) reconcilePartition(ctx context.Context, partitionName string, force bool) error {
	attrs := []attribute.KeyValue{attribute.String("guardian.partition", partitionName)}
	count, failures, duration := reconcileInstruments()
	count.Add(ctx, 1, metricapi.WithAttributes(attrs...))
	ctx, span := otel.Tracer(reconcilerScope).Start(ctx, "guardian.reconcile.partition", trace.WithAttributes(attrs...))
	startedAt := time.Now()
	telemetry.EmitInfo(ctx, reconcilerScope, fmt.Sprintf("reconciling partition %s", partitionName))
	defer func() {
		duration.Record(ctx, time.Since(startedAt).Seconds(), metricapi.WithAttributes(attrs...))
		span.End()
	}()
	fail := func(err error) error {
		failures.Add(ctx, 1, metricapi.WithAttributes(attrs...))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		telemetry.EmitError(ctx, reconcilerScope, fmt.Sprintf("reconcile partition %s failed: %v", partitionName, err))
		return err
	}

	configPath := paths.PartitionConfig(partitionName)
	configContent, err := r.store.ReadFile(ctx, configPath)
	if err != nil {
		return fail(err)
	}
	configInfo, err := r.store.Stat(ctx, configPath)
	if err != nil {
		return fail(err)
	}

	intentEntries, err := r.store.ListDir(ctx, paths.PartitionIntentsDir(partitionName))
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	intentContents := map[string][]byte{}
	intentVersions := map[string]string{}
	intentModTimes := map[string]time.Time{}
	for _, entry := range intentEntries {
		if entry.IsDir || len(entry.Name) < 6 || entry.Name[len(entry.Name)-5:] != ".yaml" {
			continue
		}
		name := entry.Name[:len(entry.Name)-5]
		logicalPath := paths.IntentManifest(partitionName, name)
		content, err := r.store.ReadFile(ctx, logicalPath)
		if err != nil {
			return fail(err)
		}
		info, err := r.store.Stat(ctx, logicalPath)
		if err != nil {
			return fail(err)
		}
		intentContents[name] = content
		intentVersions[name] = info.VersionID
		intentModTimes[name] = info.ModTime
	}

	existingStates, err := common.LoadAllIntentStates(ctx, r.store, partitionName)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	if existingStates == nil {
		existingStates = map[string]*statedomain.IntentState{}
	}

	compiled, err := planner.Compile(ctx, planner.CompileInput{
		PartitionName:    partitionName,
		ConfigContent:    configContent,
		IntentContents:   intentContents,
		IntentVersionIDs: intentVersions,
		IntentModTimes:   intentModTimes,
		ConfigVersionID:  configInfo.VersionID,
		CurrentOutputs:   common.IntentOutputs(existingStates),
	})
	if err != nil {
		partitionState := &statedomain.PartitionState{
			APIVersion:        "guardian/v1alpha1",
			Kind:              "PartitionState",
			Partition:         partitionName,
			Status:            "Invalid",
			ConfigVersionID:   configInfo.VersionID,
			PartitionRevision: "",
			IntentVersions:    map[string]string{},
			LastCompiledAt:    time.Now().UTC(),
			LastReconciledAt:  time.Now().UTC(),
			Errors:            []string{err.Error()},
		}
		if writeErr := r.dispatcher.WritePartitionState(ctx, partitionState); writeErr != nil {
			return fail(fmt.Errorf("compile error: %v; state write error: %w", err, writeErr))
		}
		return fail(err)
	}

	partitionState := &statedomain.PartitionState{
		APIVersion:        "guardian/v1alpha1",
		Kind:              "PartitionState",
		Partition:         partitionName,
		Status:            "Compiled",
		ConfigVersionID:   compiled.ConfigVersionID,
		PartitionRevision: compiled.PartitionRevision,
		IntentVersions:    compiled.IntentVersions,
		LastCompiledAt:    time.Now().UTC(),
		LastReconciledAt:  time.Now().UTC(),
		Errors:            nil,
	}
	if err := r.dispatcher.WritePartitionState(ctx, partitionState); err != nil {
		return fail(err)
	}

	partitionSpec, err := manifest.ParsePartition(configContent)
	if err != nil {
		return fail(err)
	}

	// Manual-mode partitions are inventory-only: compile and write state
	// but do not queue any reconciliation tasks unless explicitly forced.
	if partitionSpec.Spec.Reconciliation.Mode == "manual" && !force {
		log.Printf("reconciler: partition %s is manual mode — compiled only, no tasks queued (use Reconcile Now to force)", partitionName)
		telemetry.EmitInfo(ctx, reconcilerScope, fmt.Sprintf("partition %s is manual mode, skipping reconciliation", partitionName))
		span.SetStatus(codes.Ok, "")
		return nil
	}

	if err := r.reconcileRemovedIntents(ctx, partitionName, partitionSpec.Spec.DeletionPolicy, existingStates, compiled.IntentVersions); err != nil {
		return fail(err)
	}

	// Snapshot intent statuses before the loop so that in-loop mutations
	// (e.g. queueing a refresh on a dependency) don't block dependents that
	// already had a healthy dependency at the start of this cycle.
	depsSnapshot := make(map[string]*statedomain.IntentState, len(existingStates))
	for k, v := range existingStates {
		snap := *v
		depsSnapshot[k] = &snap
	}

	outputs := common.IntentOutputs(existingStates)
	for _, name := range compiled.IntentOrder {
		compiledIntent := compiled.Intents[name]
		current := existingStates[name]
		activeTask, err := common.HasActiveTask(ctx, r.store, current)
		if err != nil {
			return fail(err)
		}
		// Treat in-flight task as dead if it has been queued longer than
		// staleTaskAfter — the pusher likely crashed without writing a result.
		if activeTask && current != nil && !current.Timestamps.LastQueuedAt.IsZero() {
			elapsed := time.Since(current.Timestamps.LastQueuedAt)
			if elapsed > r.staleTaskAfter {
				log.Printf("reconciler: partition=%s intent=%s task %s is stale (%v > %v), treating as dead",
					partitionName, name, current.LastTaskID, elapsed.Round(time.Second), r.staleTaskAfter)
				activeTask = false
			}
		}
		if current == nil {
			current = &statedomain.IntentState{
				APIVersion: "guardian/v1alpha1",
				Kind:       "IntentState",
				Partition:  partitionName,
				Intent:     name,
				Outputs:    map[string]string{},
			}
		}
		current.Locked = compiledIntent.Spec.Spec.Locked
		current.IntentVersionID = compiledIntent.IntentVersionID
		current.IntentSpecHash = compiledIntent.IntentSpecHash
		current.PartitionRevision = compiled.PartitionRevision
		current.TargetPusher = compiledIntent.Spec.Spec.TargetPusher
		current.Target = compiledIntent.Spec.Spec.Target
		current.Joins = append([]string(nil), compiledIntent.Spec.Spec.Joins...)
		current.AssetVersionIDs = copyStringMap(compiledIntent.AssetVersionIDs)
		current.AssetVersions = copyStringMap(compiledIntent.AssetVersions)
		if current.Outputs == nil {
			current.Outputs = map[string]string{}
		}

		if !common.DependenciesHealthy(current, depsSnapshot) {
			log.Printf("reconciler: partition=%s intent=%s blocked (dependencies not healthy)", partitionName, name)
			// Only update state for non-in-flight intents. If the intent is
			// currently tied to an active task, the result-processor
			// owns that state transition. Writing Blocked here with the old
			// LastTaskID would corrupt the result-processor's in-progress work,
			// causing subsequent results to be dropped as stale.
			if !activeTask {
				if current.Status != statedomain.StatusHealthy {
					current.Status = statedomain.StatusBlocked
				}
				if err := r.dispatcher.WriteIntentState(ctx, current); err != nil {
					return fail(err)
				}
			}
			existingStates[name] = current
			continue
		}
		if !activeTask {
			next, err := common.BuildTask(ctx, r.store, current, taskdomain.OpDiff, outputs)
			if err != nil {
				log.Printf("reconciler: partition=%s intent=%s build task failed: %v", partitionName, name, err)
				current.Status = statedomain.StatusBlocked
				msg := err.Error()
				current.LastError = &msg
			} else {
				log.Printf("reconciler: partition=%s intent=%s queued DIFF task=%s pusher=%s", partitionName, name, next.TaskID, next.TargetPusher)
				current.Status = common.QueuedStatus(current.Status, taskdomain.OpDiff)
				current.LastTaskID = next.TaskID
				current.LastError = nil
				current.Timestamps.LastQueuedAt = next.CreatedAt
				current.Timestamps.LastDiffAt = next.CreatedAt
				if err := r.dispatcher.QueueTask(ctx, next); err != nil {
					return fail(err)
				}
			}
			// Only write state for non-in-flight intents to avoid racing with
			// the result-processor which owns state transitions for in-flight tasks.
			if err := r.dispatcher.WriteIntentState(ctx, current); err != nil {
				return fail(err)
			}
		}
		existingStates[name] = current
		outputs[name] = copyStringMap(current.Outputs)
	}
	span.SetStatus(codes.Ok, "")
	log.Printf("reconciler: partition=%s reconcile complete", partitionName)
	telemetry.EmitInfo(ctx, reconcilerScope, fmt.Sprintf("reconciled partition %s", partitionName))
	return nil
}

func (r *Reconciler) reconcileRemovedIntents(ctx context.Context, partitionName, deletionPolicy string, existingStates map[string]*statedomain.IntentState, currentIntents map[string]string) error {
	if existingStates == nil {
		return nil
	}
	names := make([]string, 0, len(existingStates))
	for name := range existingStates {
		names = append(names, name)
	}
	sort.Strings(names)
	policy := normalizedDeletionPolicy(deletionPolicy)
	for _, name := range names {
		if _, ok := currentIntents[name]; ok {
			continue
		}
		state := existingStates[name]
		if state == nil {
			delete(existingStates, name)
			continue
		}
		switch policy {
		case "destroy":
			if state.Status == statedomain.StatusDestroying || state.Status == statedomain.StatusDestroyed {
				existingStates[name] = state
				continue
			}
			manifestContent, err := r.loadDeletedIntentManifest(ctx, partitionName, name, state)
			if err != nil {
				if !os.IsNotExist(err) {
					return err
				}
				// Manifest is gone and was never archived — we can't generate a
				// DESTROY task.  Fall back to orphaning the state so the reconciler
				// is not permanently broken by an unresolvable intent.
				correlationID := revisions.NewCorrelationID()
				if delErr := r.dispatcher.DeleteIntentState(ctx, partitionName, name, correlationID, "destroy policy: manifest unresolvable, orphaning state"); delErr != nil && !os.IsNotExist(delErr) {
					return delErr
				}
				delete(existingStates, name)
				if evtErr := r.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
					Partition:     partitionName,
					Intent:        name,
					Type:          "intent.orphaned",
					Message:       "intent removed under destroy policy but manifest is unresolvable; state orphaned",
					TaskID:        state.LastTaskID,
					CorrelationID: correlationID,
				}); evtErr != nil {
					return evtErr
				}
				continue
			}
			next, err := common.BuildTaskFromManifest(state, manifestContent, taskdomain.OpDestroy, common.IntentOutputs(existingStates))
			if err != nil {
				return err
			}
			state.Status = statedomain.StatusDestroying
			state.LastTaskID = next.TaskID
			state.LastError = nil
			state.Timestamps.LastQueuedAt = next.CreatedAt
			state.Timestamps.LastApplyAt = next.CreatedAt
			if err := r.dispatcher.QueueTask(ctx, next); err != nil {
				return err
			}
			if err := r.dispatcher.WriteIntentState(ctx, state); err != nil {
				return err
			}
			existingStates[name] = state
		default:
			if state.Status == statedomain.StatusDestroying {
				existingStates[name] = state
				continue
			}
			correlationID := revisions.NewCorrelationID()
			if err := r.dispatcher.DeleteIntentState(ctx, partitionName, name, correlationID, "orphan deleted intent state"); err != nil && !os.IsNotExist(err) {
				return err
			}
			delete(existingStates, name)
			if err := r.dispatcher.WriteEvent(ctx, &historydomain.EventRecord{
				Partition:     partitionName,
				Intent:        name,
				Type:          "intent.orphaned",
				Message:       "intent removed under orphan deletion policy",
				TaskID:        state.LastTaskID,
				CorrelationID: correlationID,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Reconciler) loadDeletedIntentManifest(ctx context.Context, partitionName, intentName string, state *statedomain.IntentState) ([]byte, error) {
	logicalPath := paths.IntentManifest(partitionName, intentName)
	if state != nil && state.IntentVersionID != "" {
		version, err := r.store.GetVersion(ctx, logicalPath, state.IntentVersionID)
		if err == nil {
			return version.Content, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if state != nil && state.DeploymentRevision != "" {
		return r.store.ReadFile(ctx, paths.ArchiveManifest(partitionName, intentName, state.DeploymentRevision))
	}
	return nil, os.ErrNotExist
}

func (r *Reconciler) reconcileAll(ctx context.Context) error {
	log.Printf("reconciler: starting full reconcile cycle")
	names, err := r.partitionNames(ctx)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	workerCount := r.parallelism
	if workerCount <= 0 {
		workerCount = 1
	}
	if workerCount > len(names) {
		workerCount = len(names)
	}
	partitions := make(chan string)
	errCh := make(chan error, len(names))
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range partitions {
				if ctx.Err() != nil {
					return
				}
				if err := r.ReconcilePartition(ctx, name, false); err != nil {
					log.Printf("reconciler: partition=%s error (continuing): %v", name, err)
					errCh <- err
				}
			}
		}()
	}
	for _, name := range names {
		select {
		case <-ctx.Done():
			close(partitions)
			wg.Wait()
			return ctx.Err()
		case partitions <- name:
		}
	}
	close(partitions)
	wg.Wait()
	close(errCh)
	var firstErr error
	for err := range errCh {
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (r *Reconciler) partitionNames(ctx context.Context) ([]string, error) {
	entries, err := r.store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if _, err := r.store.Stat(ctx, paths.PartitionConfig(entry.Name)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names, nil
}

func (r *Reconciler) partitionLock(partitionName string) *sync.Mutex {
	lock, _ := r.partitionLocks.LoadOrStore(partitionName, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func defaultParallelism() int {
	parallelism := runtime.GOMAXPROCS(0) * 4
	if parallelism < 4 {
		parallelism = 4
	}
	if parallelism > 128 {
		parallelism = 128
	}
	return parallelism
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

func normalizedDeletionPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "destroy":
		return "destroy"
	default:
		return "orphan"
	}
}

func reconcileInstruments() (metricapi.Int64Counter, metricapi.Int64Counter, metricapi.Float64Histogram) {
	reconcileMetricsOnce.Do(func() {
		meter := otel.Meter(reconcilerScope)
		reconcileCounter, _ = meter.Int64Counter("guardian.reconcile.partition.executions")
		reconcileFailCounter, _ = meter.Int64Counter("guardian.reconcile.partition.failures")
		reconcileDuration, _ = meter.Float64Histogram("guardian.reconcile.partition.duration")
	})
	return reconcileCounter, reconcileFailCounter, reconcileDuration
}
