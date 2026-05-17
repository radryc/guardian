package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	statedomain "github.com/rydzu/ainfra/guardian/internal/domain/state"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type compliancePublisher interface {
	Publish(logicalPath string, content []byte, contentType string)
}

type Dispatcher struct {
	store       guardianapi.Store
	principalID string
	compliance  compliancePublisher

	// lastHashMu guards lastWriteHash, which is used to skip redundant
	// WriteIntentState / WritePartitionState calls when the serialised content
	// is identical to what was last written.
	lastHashMu    sync.RWMutex
	lastWriteHash map[string]uint64 // logical path → FNV-64 of last written bytes

	runtimeMu    sync.RWMutex
	runtimeCache map[string]*statedomain.PartitionRuntime

	partitionWriteMu    sync.Mutex
	partitionWriteLocks map[string]*sync.Mutex
}

func NewDispatcher(store guardianapi.Store, principalID string) *Dispatcher {
	return &Dispatcher{
		store:               store,
		principalID:         principalID,
		lastWriteHash:       make(map[string]uint64),
		runtimeCache:        make(map[string]*statedomain.PartitionRuntime),
		partitionWriteLocks: make(map[string]*sync.Mutex),
	}
}

func (d *Dispatcher) SetCompliancePublisher(publisher compliancePublisher) {
	d.compliance = publisher
}

// SeedRuntimeMetrics warms the in-memory runtime cache from the durable store
// so global status gauges are accurate immediately after process startup.
func (d *Dispatcher) SeedRuntimeMetrics(ctx context.Context) error {
	partitions, err := d.partitionNames(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(partitions))
	for _, partition := range partitions {
		runtime, err := common.LoadPartitionRuntime(ctx, d.store, partition)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		seen[partition] = struct{}{}
		d.storePartitionRuntime(runtime)
	}
	for _, partition := range d.staleRuntimePartitions(seen) {
		d.evictPartitionRuntime(partition)
	}
	return nil
}

// contentHash returns a fast FNV-64a hash of content for change detection.
func contentHash(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

// contentUnchanged returns true when content is byte-for-byte identical to
// the last value written to logicalPath.
func (d *Dispatcher) contentUnchanged(logicalPath string, content []byte) bool {
	h := contentHash(content)
	d.lastHashMu.RLock()
	prev, ok := d.lastWriteHash[logicalPath]
	d.lastHashMu.RUnlock()
	return ok && prev == h
}

// recordHash records the hash of content that was just written to logicalPath.
func (d *Dispatcher) recordHash(logicalPath string, content []byte) {
	h := contentHash(content)
	d.lastHashMu.Lock()
	d.lastWriteHash[logicalPath] = h
	d.lastHashMu.Unlock()
}

func (d *Dispatcher) QueueTask(ctx context.Context, t *taskdomain.Task) error {
	content, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	correlationID := t.CorrelationID
	if correlationID == "" {
		correlationID = revisions.NewCorrelationID()
	}
	// Write only the queue file — TaskState under .state/tasks/ is an audit
	// duplicate of the same content.  Removing it halves the write cost per
	// task queue operation.
	_, err = d.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{
			{LogicalPath: paths.QueueTask(t.TargetPusher, t.TaskID), Content: content},
		},
		Context: guardianapi.MutationContext{PrincipalID: d.principalID, Reason: "queue task", CorrelationID: correlationID},
	})
	if err != nil {
		return err
	}
	guardianWritesTotal.WithLabelValues("queue_task").Inc()
	guardianWriteBytesTotal.WithLabelValues("queue_task").Add(float64(len(content)))
	return nil
}

func (d *Dispatcher) WriteIntentState(ctx context.Context, s *statedomain.IntentState) error {
	unlock := d.lockPartitionWrites(s.Partition)
	defer unlock()

	content, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	runtime, err := d.partitionRuntime(ctx, s.Partition)
	if err != nil {
		return err
	}
	runtime.Intents[s.Intent] = statedomain.CloneIntentState(s)
	runtime.UpdatedAt = nowUTC()
	return d.writeStateAndRuntime(ctx, paths.IntentState(s.Partition, s.Intent), content, runtime, "write intent state", "write_intent_state")
}

func (d *Dispatcher) WritePartitionState(ctx context.Context, s *statedomain.PartitionState) error {
	unlock := d.lockPartitionWrites(s.Partition)
	defer unlock()

	content, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	runtime, err := d.partitionRuntime(ctx, s.Partition)
	if err != nil {
		return err
	}
	runtime.PartitionState = statedomain.ClonePartitionState(s)
	runtime.UpdatedAt = nowUTC()
	return d.writeStateAndRuntime(ctx, paths.PartitionState(s.Partition), content, runtime, "write partition state", "write_partition_state")
}

func (d *Dispatcher) DeleteIntentState(ctx context.Context, partition, intent, correlationID, reason string) error {
	unlock := d.lockPartitionWrites(partition)
	defer unlock()

	runtime, loadErr := d.partitionRuntime(ctx, partition)
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	if runtime != nil {
		delete(runtime.Intents, intent)
		runtime.UpdatedAt = nowUTC()
		statedomain.NormalizePartitionRuntime(runtime)
	}
	if correlationID == "" {
		correlationID = revisions.NewCorrelationID()
	}
	partitionStatePath := paths.PartitionState(partition)
	runtimePath := paths.PartitionRuntime(partition)
	_, err := d.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.IntentState(partition, intent)}, {LogicalPath: partitionStatePath}, {LogicalPath: runtimePath}},
		Context: guardianapi.MutationContext{PrincipalID: d.principalID, Reason: reason, CorrelationID: correlationID},
	})
	if err == nil {
		guardianDeletesTotal.WithLabelValues("delete_intent_state").Inc()
		guardianDeletesTotal.WithLabelValues("delete_partition_state").Inc()
		guardianDeletesTotal.WithLabelValues("delete_partition_runtime").Inc()
		// Evict cached hash so a future write to this path is not skipped.
		logicalPath := paths.IntentState(partition, intent)
		d.lastHashMu.Lock()
		delete(d.lastWriteHash, logicalPath)
		delete(d.lastWriteHash, partitionStatePath)
		delete(d.lastWriteHash, runtimePath)
		d.lastHashMu.Unlock()
		if runtime != nil {
			d.storePartitionRuntime(runtime)
		} else {
			d.evictPartitionRuntime(partition)
		}
	}
	return err
}

func (d *Dispatcher) WriteEvent(ctx context.Context, event *historydomain.EventRecord) error {
	if event == nil || event.Partition == "" {
		return nil
	}
	if event.APIVersion == "" {
		event.APIVersion = "guardian/v1alpha1"
	}
	if event.Kind == "" {
		event.Kind = "EventRecord"
	}
	if event.EventID == "" {
		event.EventID = revisions.NewEventID()
	}
	if event.CorrelationID == "" {
		event.CorrelationID = revisions.NewCorrelationID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = nowUTC()
	}
	content, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	logicalPath := paths.EventState(event.Partition, event.EventID)
	if _, err := d.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}},
		Context: guardianapi.MutationContext{PrincipalID: d.principalID, Reason: "write event", CorrelationID: event.CorrelationID},
	}); err != nil {
		return err
	}
	guardianWritesTotal.WithLabelValues("write_event").Inc()
	guardianWriteBytesTotal.WithLabelValues("write_event").Add(float64(len(content)))
	d.publish(logicalPath, content, contentTypeJSON)
	return nil
}

func (d *Dispatcher) ArchiveDeployment(ctx context.Context, rec *historydomain.DeploymentRecord, manifestContent []byte, logs []taskdomain.LogEntry) error {
	jsonContent, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	logsContent, err := marshalLogEntries(logs)
	if err != nil {
		return err
	}
	correlationID := revisions.NewCorrelationID()
	archiveWrites := []guardianapi.PathWrite{
		{LogicalPath: paths.ArchiveState(rec.Partition, rec.Intent, rec.DeploymentRevision), Content: jsonContent},
		{LogicalPath: paths.ArchiveManifest(rec.Partition, rec.Intent, rec.DeploymentRevision), Content: append([]byte(nil), manifestContent...)},
		{LogicalPath: paths.ArchiveLogs(rec.Partition, rec.Intent, rec.DeploymentRevision), Content: logsContent},
	}
	_, err = d.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  archiveWrites,
		Context: guardianapi.MutationContext{PrincipalID: d.principalID, Reason: "archive deployment", CorrelationID: correlationID},
	})
	if err != nil {
		return err
	}
	var archiveBytes int
	for _, w := range archiveWrites {
		archiveBytes += len(w.Content)
	}
	guardianWritesTotal.WithLabelValues("archive_deployment").Add(float64(len(archiveWrites)))
	guardianWriteBytesTotal.WithLabelValues("archive_deployment").Add(float64(archiveBytes))

	d.publish(paths.ArchiveState(rec.Partition, rec.Intent, rec.DeploymentRevision), jsonContent, contentTypeJSON)
	d.publish(paths.ArchiveManifest(rec.Partition, rec.Intent, rec.DeploymentRevision), manifestContent, contentTypeYAML)
	d.publish(paths.ArchiveLogs(rec.Partition, rec.Intent, rec.DeploymentRevision), logsContent, contentTypeNDJSON)
	return d.WriteEvent(ctx, &historydomain.EventRecord{
		Partition:          rec.Partition,
		Intent:             rec.Intent,
		Type:               "deployment.archived",
		Message:            "deployment archived",
		DeploymentRevision: rec.DeploymentRevision,
		CorrelationID:      correlationID,
		Details: map[string]string{
			"taskCount": strconv.Itoa(len(rec.TaskIDs)),
		},
	})
}

func (d *Dispatcher) publish(logicalPath string, content []byte, contentType string) {
	if d.compliance == nil {
		return
	}
	d.compliance.Publish(logicalPath, content, contentType)
}

func marshalLogEntries(entries []taskdomain.LogEntry) ([]byte, error) {
	if len(entries) == 0 {
		return []byte{}, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func (d *Dispatcher) lockPartitionWrites(partition string) func() {
	if partition == "" {
		return func() {}
	}
	d.partitionWriteMu.Lock()
	lock, ok := d.partitionWriteLocks[partition]
	if !ok {
		lock = &sync.Mutex{}
		d.partitionWriteLocks[partition] = lock
	}
	d.partitionWriteMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (d *Dispatcher) writeStateAndRuntime(ctx context.Context, logicalPath string, content []byte, runtime *statedomain.PartitionRuntime, reason, operation string) error {
	runtime = statedomain.NormalizePartitionRuntime(runtime)
	partitionStatePath := paths.PartitionState(runtime.Partition)
	var partitionStateContent []byte
	if runtime.PartitionState != nil {
		var err error
		partitionStateContent, err = json.MarshalIndent(runtime.PartitionState, "", "  ")
		if err != nil {
			return err
		}
	}
	if logicalPath == partitionStatePath && len(partitionStateContent) > 0 {
		content = partitionStateContent
	}
	runtimeContent, err := json.MarshalIndent(runtime, "", "  ")
	if err != nil {
		return err
	}
	runtimePath := paths.PartitionRuntime(runtime.Partition)
	writes := make([]guardianapi.PathWrite, 0, 3)
	if d.contentUnchanged(logicalPath, content) {
		guardianSkippedWritesTotal.WithLabelValues(operation).Inc()
	} else {
		writes = append(writes, guardianapi.PathWrite{LogicalPath: logicalPath, Content: content})
	}
	if len(partitionStateContent) > 0 && logicalPath != partitionStatePath {
		if d.contentUnchanged(partitionStatePath, partitionStateContent) {
			guardianSkippedWritesTotal.WithLabelValues("write_partition_state").Inc()
		} else {
			writes = append(writes, guardianapi.PathWrite{LogicalPath: partitionStatePath, Content: partitionStateContent})
		}
	}
	if d.contentUnchanged(runtimePath, runtimeContent) {
		guardianSkippedWritesTotal.WithLabelValues("write_partition_runtime").Inc()
	} else {
		writes = append(writes, guardianapi.PathWrite{LogicalPath: runtimePath, Content: runtimeContent})
	}
	if len(writes) == 0 {
		d.storePartitionRuntime(runtime)
		return nil
	}
	if _, err := d.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  writes,
		Context: guardianapi.MutationContext{PrincipalID: d.principalID, Reason: reason},
	}); err != nil {
		return err
	}
	for _, write := range writes {
		d.recordHash(write.LogicalPath, write.Content)
		switch write.LogicalPath {
		case logicalPath:
			guardianWritesTotal.WithLabelValues(operation).Inc()
			guardianWriteBytesTotal.WithLabelValues(operation).Add(float64(len(write.Content)))
			d.publish(logicalPath, write.Content, contentTypeJSON)
		case partitionStatePath:
			guardianWritesTotal.WithLabelValues("write_partition_state").Inc()
			guardianWriteBytesTotal.WithLabelValues("write_partition_state").Add(float64(len(write.Content)))
			d.publish(partitionStatePath, write.Content, contentTypeJSON)
		case runtimePath:
			guardianWritesTotal.WithLabelValues("write_partition_runtime").Inc()
			guardianWriteBytesTotal.WithLabelValues("write_partition_runtime").Add(float64(len(write.Content)))
		}
	}
	d.storePartitionRuntime(runtime)
	return nil
}

func (d *Dispatcher) partitionRuntime(ctx context.Context, partition string) (*statedomain.PartitionRuntime, error) {
	d.runtimeMu.RLock()
	cached, ok := d.runtimeCache[partition]
	d.runtimeMu.RUnlock()
	if ok {
		return statedomain.ClonePartitionRuntime(cached), nil
	}
	runtime, err := common.LoadPartitionRuntime(ctx, d.store, partition)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		runtime = statedomain.NewPartitionRuntime(partition)
	}
	d.storePartitionRuntime(runtime)
	return statedomain.ClonePartitionRuntime(runtime), nil
}

func (d *Dispatcher) storePartitionRuntime(runtime *statedomain.PartitionRuntime) {
	if runtime == nil {
		return
	}
	current := statedomain.ClonePartitionRuntime(statedomain.NormalizePartitionRuntime(runtime))
	d.runtimeMu.Lock()
	previous := statedomain.ClonePartitionRuntime(d.runtimeCache[current.Partition])
	d.runtimeCache[current.Partition] = current
	d.runtimeMu.Unlock()
	applyRuntimeMetricDelta(previous, current)
}

func (d *Dispatcher) evictPartitionRuntime(partition string) {
	d.runtimeMu.Lock()
	previous := statedomain.ClonePartitionRuntime(d.runtimeCache[partition])
	delete(d.runtimeCache, partition)
	d.runtimeMu.Unlock()
	applyRuntimeMetricDelta(previous, nil)
}

type runtimeMetricSnapshot struct {
	partitionStatus   string
	intentStatusCount map[string]int
}

func applyRuntimeMetricDelta(previous, current *statedomain.PartitionRuntime) {
	prev := runtimeMetricSnapshotFor(previous)
	next := runtimeMetricSnapshotFor(current)
	if prev.partitionStatus != next.partitionStatus {
		if prev.partitionStatus != "" {
			guardianPartitionStatusCurrent.WithLabelValues(prev.partitionStatus).Dec()
		}
		if next.partitionStatus != "" {
			guardianPartitionStatusCurrent.WithLabelValues(next.partitionStatus).Inc()
		}
	}
	statusSet := make(map[string]struct{}, len(prev.intentStatusCount)+len(next.intentStatusCount))
	for _, status := range statedomain.KnownIntentStatuses() {
		statusSet[status] = struct{}{}
	}
	for status := range prev.intentStatusCount {
		statusSet[status] = struct{}{}
	}
	for status := range next.intentStatusCount {
		statusSet[status] = struct{}{}
	}
	for status := range statusSet {
		delta := next.intentStatusCount[status] - prev.intentStatusCount[status]
		if delta != 0 {
			guardianIntentStatusCurrent.WithLabelValues(status).Add(float64(delta))
		}
	}
}

func runtimeMetricSnapshotFor(runtime *statedomain.PartitionRuntime) runtimeMetricSnapshot {
	if runtime == nil {
		return runtimeMetricSnapshot{intentStatusCount: map[string]int{}}
	}
	runtime = statedomain.NormalizePartitionRuntime(statedomain.ClonePartitionRuntime(runtime))
	snapshot := runtimeMetricSnapshot{intentStatusCount: map[string]int{}}
	if runtime.PartitionState != nil {
		snapshot.partitionStatus = runtime.PartitionState.Status
		for status, count := range runtime.PartitionState.Metrics.IntentStatusCounts {
			snapshot.intentStatusCount[status] = count
		}
	}
	return snapshot
}

func (d *Dispatcher) partitionNames(ctx context.Context) ([]string, error) {
	entries, err := d.store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if _, err := d.store.Stat(ctx, paths.PartitionConfig(entry.Name)); err != nil {
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

func (d *Dispatcher) staleRuntimePartitions(seen map[string]struct{}) []string {
	d.runtimeMu.RLock()
	stale := make([]string, 0, len(d.runtimeCache))
	for partition := range d.runtimeCache {
		if _, ok := seen[partition]; ok {
			continue
		}
		stale = append(stale, partition)
	}
	d.runtimeMu.RUnlock()
	sort.Strings(stale)
	return stale
}

const (
	contentTypeJSON   = "application/json"
	contentTypeYAML   = "application/x-yaml"
	contentTypeNDJSON = "application/x-ndjson"
)
