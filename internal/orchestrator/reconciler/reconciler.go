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

	"github.com/reugn/go-quartz/quartz"
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
	intervalStr    string
	staleTaskAfter time.Duration // tasks older than this are treated as dead and re-queued
	parallelism    int
	partitionLocks sync.Map

	// guarded by runMu; only non-nil while Run() is executing
	runMu                 sync.Mutex
	pool                  *priorityPool
	scheduler             quartz.Scheduler
	scheduledFingerprints map[string]string // partition → "intervalStr|jitterPct"
}

// NewReconciler creates a reconciler using a fixed interval. Suitable for
// tests, CLI, and UI handlers that call ReconcilePartition/ReconcileAll
// directly without running the quartz scheduler.
func NewReconciler(store guardianapi.Store, dispatcher *dispatcher.Dispatcher, interval time.Duration) *Reconciler {
	return NewReconcilerWithOptions(store, dispatcher, interval.String(), 0)
}

// NewReconcilerWithOptions creates a reconciler with a string interval that
// can be either a Go duration ("10m") or a quartz cron expression
// ("0 0/10 * * * ?"). Called from the daemon; intervalStr is validated lazily
// in Run() so that startup errors are reported clearly.
func NewReconcilerWithOptions(store guardianapi.Store, dispatcher *dispatcher.Dispatcher, intervalStr string, staleTaskAfter time.Duration) *Reconciler {
	if intervalStr == "" {
		intervalStr = "10m"
	}
	if staleTaskAfter <= 0 {
		staleTaskAfter = 5 * time.Minute
	}
	return &Reconciler{
		store:                 store,
		dispatcher:            dispatcher,
		intervalStr:           intervalStr,
		staleTaskAfter:        staleTaskAfter,
		parallelism:           defaultParallelism(),
		scheduledFingerprints: make(map[string]string),
	}
}

func (r *Reconciler) Run(ctx context.Context) error {
	// Validate the default trigger before allocating any resources.
	if _, err := parseTrigger(r.intervalStr); err != nil {
		return fmt.Errorf("reconciler: invalid interval: %w", err)
	}

	sched, err := quartz.NewStdScheduler()
	if err != nil {
		return fmt.Errorf("reconciler: create scheduler: %w", err)
	}
	pool := newPriorityPool(r.parallelism)
	pool.start(ctx)

	r.runMu.Lock()
	r.pool = pool
	r.scheduler = sched
	r.scheduledFingerprints = make(map[string]string)
	r.runMu.Unlock()

	defer func() {
		sched.Stop()
		pool.Shutdown()
		r.runMu.Lock()
		r.pool = nil
		r.scheduler = nil
		r.scheduledFingerprints = make(map[string]string)
		r.runMu.Unlock()
	}()

	sched.Start(ctx)
	log.Printf("reconciler: starting with default interval=%s parallelism=%d", r.intervalStr, r.parallelism)

	// Initial full reconcile (synchronous) so the daemon is current on startup.
	if err := r.reconcileAll(ctx); err != nil {
		log.Printf("reconciler: initial reconcile error (will retry): %v", err)
	}

	// Set up per-partition quartz jobs based on current partition configs.
	if err := r.syncPartitionSchedules(ctx); err != nil {
		log.Printf("reconciler: initial partition schedule sync error: %v", err)
	}

	// Discovery job: keep partition jobs in sync as partitions are added/removed.
	discoveryDetail := quartz.NewJobDetail(
		&discoveryJob{r: r},
		quartz.NewJobKey("guardian.discovery"),
	)
	if err := sched.ScheduleJob(discoveryDetail, quartz.NewSimpleTrigger(30*time.Second)); err != nil {
		return fmt.Errorf("reconciler: schedule discovery job: %w", err)
	}

	<-ctx.Done()
	return ctx.Err()
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
		current.PartitionMode = partitionSpec.Spec.Reconciliation.Mode
		oldSpecHash := current.IntentSpecHash
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
			specHashChanged := oldSpecHash != "" && oldSpecHash != compiledIntent.IntentSpecHash
			nextOp := taskdomain.OpDiff
			// Recovery path: if we already know the intent is drifted and no task
			// is active, resume at CHECK instead of repeatedly running DIFF.
			// Apply/Check failures need the same recovery path once the operator
			// has corrected the underlying issue and asks Guardian to reconcile again.
			if current.Status == statedomain.StatusDrifted ||
				current.Status == statedomain.StatusApplyFailed ||
				current.Status == statedomain.StatusCheckFailed {
				nextOp = taskdomain.OpCheck
			}
			next, err := common.BuildTask(ctx, r.store, current, nextOp, outputs)
			if err != nil {
				log.Printf("reconciler: partition=%s intent=%s build task failed: %v", partitionName, name, err)
				current.Status = statedomain.StatusBlocked
				msg := err.Error()
				current.LastError = &msg
			} else {
				log.Printf("reconciler: partition=%s intent=%s queued %s task=%s pusher=%s%s", partitionName, name, nextOp, next.TaskID, next.TargetPusher, rolloutLogSuffix(specHashChanged))
				current.Status = common.QueuedStatus(current.Status, nextOp)
				current.LastTaskID = next.TaskID
				current.LastError = nil
				current.Timestamps.LastQueuedAt = next.CreatedAt
				if nextOp == taskdomain.OpCheck {
					current.Timestamps.LastCheckAt = next.CreatedAt
				} else {
					current.Timestamps.LastDiffAt = next.CreatedAt
				}
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
	r.updateScheduleFromSpec(partitionName, partitionSpec.Spec.Reconciliation.Interval, partitionSpec.Spec.Reconciliation.JitterPercent)
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

// ─── Scheduler management ─────────────────────────────────────────────────────

// syncPartitionSchedules reconciles the quartz job registry against the live
// partition list: adds jobs for new partitions, updates jobs whose schedule
// config changed, and removes jobs for deleted partitions.
func (r *Reconciler) syncPartitionSchedules(ctx context.Context) error {
	names, err := r.partitionNames(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
		configContent, readErr := r.store.ReadFile(ctx, paths.PartitionConfig(name))
		if readErr != nil {
			log.Printf("reconciler: discovery: partition=%s config read error: %v", name, readErr)
			r.schedulePartitionJob(name, r.intervalStr, 0)
			continue
		}
		partition, parseErr := manifest.ParsePartition(configContent)
		if parseErr != nil {
			log.Printf("reconciler: discovery: partition=%s config parse error: %v", name, parseErr)
			r.schedulePartitionJob(name, r.intervalStr, 0)
			continue
		}
		if partition.Spec.Reconciliation.Mode == "manual" {
			r.removePartitionJob(name)
			continue
		}
		intervalStr := r.intervalStr
		if partition.Spec.Reconciliation.Interval != "" {
			intervalStr = partition.Spec.Reconciliation.Interval
		}
		r.schedulePartitionJob(name, intervalStr, partition.Spec.Reconciliation.JitterPercent)
	}

	// Remove jobs for partitions that no longer exist.
	r.runMu.Lock()
	var stale []string
	for name := range r.scheduledFingerprints {
		if !known[name] {
			stale = append(stale, name)
		}
	}
	r.runMu.Unlock()
	for _, name := range stale {
		r.removePartitionJob(name)
		log.Printf("reconciler: discovery: partition=%s removed from schedule (partition deleted)", name)
	}
	return nil
}

// schedulePartitionJob creates or updates the quartz job for a single
// partition. It is a no-op when the schedule fingerprint has not changed and
// the job is already registered.
func (r *Reconciler) schedulePartitionJob(name, effectiveInterval string, jitterPct int) {
	fingerprint := fmt.Sprintf("%s|%d", effectiveInterval, jitterPct)

	r.runMu.Lock()
	sched := r.scheduler
	oldFP := r.scheduledFingerprints[name]
	r.runMu.Unlock()

	if sched == nil {
		return
	}

	jobKey := quartz.NewJobKeyWithGroup(name, "partitions")

	// No-op when fingerprint is unchanged and job is live.
	if fingerprint == oldFP {
		if _, err := sched.GetScheduledJob(jobKey); err == nil {
			return
		}
	}

	_ = sched.DeleteJob(jobKey)

	innerTrigger, err := parseTrigger(effectiveInterval)
	if err != nil {
		log.Printf("reconciler: partition=%s invalid interval %q, falling back to default %s: %v",
			name, effectiveInterval, r.intervalStr, err)
		innerTrigger, _ = parseTrigger(r.intervalStr)
	}

	// Add an initial offset only for simple (interval-based) triggers. Cron
	// triggers are wall-clock-anchored so spreading makes no sense for them.
	var trigger quartz.Trigger = innerTrigger
	if _, isCron := innerTrigger.(*quartz.CronTrigger); !isCron {
		if d := intervalDuration(effectiveInterval); d > 0 {
			if offset := partitionOffset(name, jitterPct, d); offset > 0 {
				trigger = &offsetTrigger{inner: innerTrigger, offset: offset}
			}
		}
	}

	jobDetail := quartz.NewJobDetail(&partitionJob{name: name, r: r}, jobKey)
	if err := sched.ScheduleJob(jobDetail, trigger); err != nil {
		log.Printf("reconciler: partition=%s schedule job error: %v", name, err)
		return
	}

	r.runMu.Lock()
	r.scheduledFingerprints[name] = fingerprint
	r.runMu.Unlock()

	log.Printf("reconciler: partition=%s scheduled with interval=%s jitter=%d%%", name, effectiveInterval, jitterPct)
}

// updateScheduleFromSpec is called at the end of each reconcilePartition run
// to pick up any interval/jitter changes in the partition config.
func (r *Reconciler) updateScheduleFromSpec(name, specInterval string, jitterPct int) {
	r.runMu.Lock()
	pool := r.pool
	r.runMu.Unlock()
	if pool == nil {
		return // not running (CLI / test path)
	}
	intervalStr := r.intervalStr
	if specInterval != "" {
		intervalStr = specInterval
	}
	r.schedulePartitionJob(name, intervalStr, jitterPct)
}

// removePartitionJob deletes a partition's quartz job and clears its fingerprint.
func (r *Reconciler) removePartitionJob(name string) {
	r.runMu.Lock()
	sched := r.scheduler
	delete(r.scheduledFingerprints, name)
	r.runMu.Unlock()
	if sched != nil {
		_ = sched.DeleteJob(quartz.NewJobKeyWithGroup(name, "partitions"))
	}
}

// partitionJobPriority inspects current intent statuses to assign a dispatch
// priority: Critical for active rollouts, High for unhealthy intents, Normal
// otherwise.
func (r *Reconciler) partitionJobPriority(ctx context.Context, partitionName string) priority {
	states, err := common.LoadAllIntentStates(ctx, r.store, partitionName)
	if err != nil {
		return priorityNormal
	}
	result := priorityNormal
	for _, s := range states {
		switch s.Status {
		case statedomain.StatusApplying, statedomain.StatusDestroying:
			return priorityCritical
		case statedomain.StatusDrifted, statedomain.StatusDriftedLocked,
			statedomain.StatusApplyFailed, statedomain.StatusCheckFailed, statedomain.StatusDiffFailed:
			result = priorityHigh
		}
	}
	return result
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

func rolloutLogSuffix(specHashChanged bool) string {
	if specHashChanged {
		return " rollout"
	}
	return ""
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
