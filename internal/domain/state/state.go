package state

import (
	"reflect"
	"time"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
)

type IntentStatus string

const (
	StatusInvalid         IntentStatus = "Invalid"
	StatusBlocked         IntentStatus = "Blocked"
	StatusReady           IntentStatus = "Ready"
	StatusChecking        IntentStatus = "Checking"
	StatusCheckFailed     IntentStatus = "CheckFailed"
	StatusDiffing         IntentStatus = "Diffing"
	StatusDiffFailed      IntentStatus = "DiffFailed"
	StatusDrifted         IntentStatus = "Drifted"
	StatusDriftedLocked   IntentStatus = "DriftedLocked"
	StatusObserved        IntentStatus = "Observed"
	StatusObservedDrifted IntentStatus = "ObservedDrifted"
	StatusApplying        IntentStatus = "Applying"
	StatusHealthy         IntentStatus = "Healthy"
	StatusApplyFailed     IntentStatus = "ApplyFailed"
	StatusDestroying      IntentStatus = "Destroying"
	StatusDestroyed       IntentStatus = "Destroyed"
	StatusOrphaned        IntentStatus = "Orphaned"
)

type PartitionState struct {
	APIVersion        string                 `json:"apiVersion"`
	Kind              string                 `json:"kind"`
	Partition         string                 `json:"partition"`
	Status            string                 `json:"status"`
	DisplayStatus     string                 `json:"displayStatus,omitempty"`
	Summary           string                 `json:"summary,omitempty"`
	ConfigVersionID   string                 `json:"configVersionID"`
	PartitionRevision string                 `json:"partitionRevision"`
	IntentVersions    map[string]string      `json:"intentVersions"`
	LastCompiledAt    time.Time              `json:"lastCompiledAt"`
	LastReconciledAt  time.Time              `json:"lastReconciledAt"`
	Errors            []string               `json:"errors"`
	Metrics           PartitionStatusMetrics `json:"metrics"`
}

type PartitionRuntime struct {
	APIVersion     string                  `json:"apiVersion"`
	Kind           string                  `json:"kind"`
	Partition      string                  `json:"partition"`
	UpdatedAt      time.Time               `json:"updatedAt"`
	PartitionState *PartitionState         `json:"partitionState,omitempty"`
	Intents        map[string]*IntentState `json:"intents"`
}

type IntentState struct {
	APIVersion          string                                  `json:"apiVersion"`
	Kind                string                                  `json:"kind"`
	Partition           string                                  `json:"partition"`
	Intent              string                                  `json:"intent"`
	Status              IntentStatus                            `json:"status"`
	IntentType          string                                  `json:"intentType,omitempty"`
	Locked              bool                                    `json:"locked"`
	IntentVersionID     string                                  `json:"intentVersionID"`
	IntentSpecHash      string                                  `json:"intentSpecHash"`
	LastAppliedSpecHash string                                  `json:"lastAppliedSpecHash,omitempty"`
	PartitionRevision   string                                  `json:"partitionRevision"`
	DeploymentRevision  string                                  `json:"deploymentRevision"`
	TargetPusher        string                                  `json:"targetPusher"`
	Target              targetdomain.Placement                  `json:"target,omitempty"`
	Joins               []string                                `json:"joins"`
	AssetVersionIDs     map[string]string                       `json:"assetVersionIDs"`
	AssetVersions       map[string]string                       `json:"assetVersions,omitempty"`
	Outputs             map[string]string                       `json:"outputs"`
	Drift               *taskdomain.DriftReport                 `json:"drift,omitempty"`
	Health              *taskdomain.HealthObservation           `json:"health,omitempty"`
	ApplyReadiness      *taskdomain.ApplyReadiness              `json:"applyReadiness,omitempty"`
	AssetObservations   map[string]*taskdomain.AssetObservation `json:"assetObservations,omitempty"`
	// Resources is the per-resource inventory of an observed CloudFormation
	// stack, computed at collection time. It is not part of the intent
	// manifest: observed intents remain report-only and the stack template is
	// the source of truth.
	Resources      []ResourceObservation `json:"resources,omitempty"`
	LastTaskID     string                `json:"lastTaskID"`
	LastError      *string               `json:"lastError,omitempty"`
	PartitionMode  string                `json:"partitionMode,omitempty"`
	RollbackTo     string                `json:"rollbackTo,omitempty"`
	RollbackReason string                `json:"rollbackReason,omitempty"`
	Timestamps     StateTimestamps       `json:"timestamps"`
}

// ResourceObservation is one logical resource derived from an observed
// provider inventory (CloudFormation, Cloud Asset Inventory, or Kubernetes).
// AssetName is the synthetic Guardian asset name shown in the UI; it is stable
// for a given logical ID. CfnType holds the provider resource type.
type ResourceObservation struct {
	LogicalID string   `json:"logicalId"`
	CfnType   string   `json:"cfnType"`
	AssetName string   `json:"assetName"`
	Name      string   `json:"name,omitempty"`
	Status    string   `json:"status"`
	DependsOn []string `json:"dependsOn,omitempty"`
	Condition string   `json:"condition,omitempty"`
	// Provider identifies the inventory source: aws, gcp, or kubernetes.
	Provider string `json:"provider,omitempty"`
	// Location is the provider region, zone, or namespace.
	Location string `json:"location,omitempty"`
	// Ephemeral marks live artifacts that are not topology components
	// (Cloud Run revisions, Kubernetes pods). They remain in the inventory for
	// deployment history but are hidden from the default topology.
	Ephemeral bool `json:"ephemeral,omitempty"`

	// Live CloudFormation identity and status from ListStackResources.
	PhysicalID       string    `json:"physicalId,omitempty"`
	LiveStatus       string    `json:"liveStatus,omitempty"`
	LiveStatusReason string    `json:"liveStatusReason,omitempty"`
	DriftStatus      string    `json:"driftStatus,omitempty"`
	LiveUpdatedAt    time.Time `json:"liveUpdatedAt,omitempty"`

	// Parent is the logical ID of the containing resource for display nesting.
	Parent string `json:"parent,omitempty"`
	// Relations are typed semantic edges derived from the template.
	Relations []ResourceRelation `json:"relations,omitempty"`
}

// ResourceRelation is a non-containment edge between two observed CloudFormation
// resources. Target is a logical resource ID.
type ResourceRelation struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

const (
	ResourceInSync  = "InSync"
	ResourceChanged = "Changed"
	ResourceAdded   = "Added"
	ResourceRemoved = "Removed"
)

type StateTimestamps struct {
	LastQueuedAt   time.Time `json:"lastQueuedAt"`
	LastCheckAt    time.Time `json:"lastCheckAt"`
	LastDiffAt     time.Time `json:"lastDiffAt"`
	LastApplyAt    time.Time `json:"lastApplyAt"`
	LastObservedAt time.Time `json:"lastObservedAt,omitempty"`
}

func NewPartitionRuntime(partition string) *PartitionRuntime {
	return &PartitionRuntime{
		APIVersion: "guardian/v1alpha1",
		Kind:       "PartitionRuntime",
		Partition:  partition,
		Intents:    map[string]*IntentState{},
	}
}

func ClonePartitionState(in *PartitionState) *PartitionState {
	if in == nil {
		return nil
	}
	out := *in
	out.IntentVersions = CloneStringMap(in.IntentVersions)
	out.Errors = append([]string(nil), in.Errors...)
	out.Metrics = ClonePartitionStatusMetrics(in.Metrics)
	return &out
}

func CloneIntentState(in *IntentState) *IntentState {
	if in == nil {
		return nil
	}
	out := *in
	out.Joins = append([]string(nil), in.Joins...)
	out.AssetVersionIDs = CloneStringMap(in.AssetVersionIDs)
	out.AssetVersions = CloneStringMap(in.AssetVersions)
	out.Outputs = CloneStringMap(in.Outputs)
	out.Drift = CloneDriftReport(in.Drift)
	out.Health = CloneHealthObservation(in.Health)
	out.ApplyReadiness = CloneApplyReadiness(in.ApplyReadiness)
	out.AssetObservations = CloneAssetObservationMap(in.AssetObservations)
	out.Resources = CloneResourceObservations(in.Resources)
	out.LastError = cloneStringPtr(in.LastError)
	return &out
}

// MergeIntentState applies the changes made between base and incoming to dst.
//
// It is a three-way merge, not a plain assignment: a field that did not change
// between base and incoming keeps the value already present in dst. This lets
// concurrent writers own independent fields without clobbering each other. The
// main case is the observed collector refreshing Resources/LastObservedAt while
// the reconciler or result processor persists status/health/task fields from a
// snapshot loaded earlier in its pass.
//
// Timestamps are additionally monotonic: a merge never moves a timestamp
// backwards. A nil base means "no previous state": fields the incoming state
// sets are applied, while fields it leaves zero keep whatever another writer
// created concurrently.
func MergeIntentState(dst, base, incoming *IntentState) {
	if dst == nil || incoming == nil {
		return
	}
	if base == nil {
		// Treat a missing snapshot as the zero state: apply every field the
		// incoming state actually set, but do not clear fields another writer
		// may have created between our load and the atomic update.
		base = &IntentState{}
	}

	if incoming.APIVersion != base.APIVersion {
		dst.APIVersion = incoming.APIVersion
	}
	if incoming.Kind != base.Kind {
		dst.Kind = incoming.Kind
	}
	if incoming.Partition != base.Partition {
		dst.Partition = incoming.Partition
	}
	if incoming.Intent != base.Intent {
		dst.Intent = incoming.Intent
	}
	if incoming.Status != base.Status {
		dst.Status = incoming.Status
	}
	if incoming.IntentType != base.IntentType {
		dst.IntentType = incoming.IntentType
	}
	if incoming.Locked != base.Locked {
		dst.Locked = incoming.Locked
	}
	if incoming.IntentVersionID != base.IntentVersionID {
		dst.IntentVersionID = incoming.IntentVersionID
	}
	if incoming.IntentSpecHash != base.IntentSpecHash {
		dst.IntentSpecHash = incoming.IntentSpecHash
	}
	if incoming.LastAppliedSpecHash != base.LastAppliedSpecHash {
		dst.LastAppliedSpecHash = incoming.LastAppliedSpecHash
	}
	if incoming.PartitionRevision != base.PartitionRevision {
		dst.PartitionRevision = incoming.PartitionRevision
	}
	if incoming.DeploymentRevision != base.DeploymentRevision {
		dst.DeploymentRevision = incoming.DeploymentRevision
	}
	if incoming.TargetPusher != base.TargetPusher {
		dst.TargetPusher = incoming.TargetPusher
	}
	if incoming.Target != base.Target {
		dst.Target = incoming.Target
	}
	if !stringsEqual(incoming.Joins, base.Joins) {
		dst.Joins = append([]string(nil), incoming.Joins...)
	}
	if !mapsEqual(incoming.AssetVersionIDs, base.AssetVersionIDs) {
		dst.AssetVersionIDs = mergeStringMap(dst.AssetVersionIDs, base.AssetVersionIDs, incoming.AssetVersionIDs)
	}
	if !mapsEqual(incoming.AssetVersions, base.AssetVersions) {
		dst.AssetVersions = mergeStringMap(dst.AssetVersions, base.AssetVersions, incoming.AssetVersions)
	}
	if !mapsEqual(incoming.Outputs, base.Outputs) {
		dst.Outputs = mergeStringMap(dst.Outputs, base.Outputs, incoming.Outputs)
	}
	if !reflect.DeepEqual(incoming.Drift, base.Drift) {
		dst.Drift = CloneDriftReport(incoming.Drift)
	}
	if !reflect.DeepEqual(incoming.Health, base.Health) {
		dst.Health = CloneHealthObservation(incoming.Health)
	}
	if !reflect.DeepEqual(incoming.ApplyReadiness, base.ApplyReadiness) {
		dst.ApplyReadiness = CloneApplyReadiness(incoming.ApplyReadiness)
	}
	if !reflect.DeepEqual(incoming.AssetObservations, base.AssetObservations) {
		dst.AssetObservations = mergeAssetObservations(dst.AssetObservations, base.AssetObservations, incoming.AssetObservations)
	}
	if !reflect.DeepEqual(incoming.Resources, base.Resources) {
		dst.Resources = CloneResourceObservations(incoming.Resources)
	}
	if incoming.LastTaskID != base.LastTaskID {
		dst.LastTaskID = incoming.LastTaskID
	}
	if !reflect.DeepEqual(incoming.LastError, base.LastError) {
		dst.LastError = cloneStringPtr(incoming.LastError)
	}
	if incoming.PartitionMode != base.PartitionMode {
		dst.PartitionMode = incoming.PartitionMode
	}
	if incoming.RollbackTo != base.RollbackTo {
		dst.RollbackTo = incoming.RollbackTo
	}
	if incoming.RollbackReason != base.RollbackReason {
		dst.RollbackReason = incoming.RollbackReason
	}

	dst.Timestamps.LastQueuedAt = mergeTime(dst.Timestamps.LastQueuedAt, base.Timestamps.LastQueuedAt, incoming.Timestamps.LastQueuedAt)
	dst.Timestamps.LastCheckAt = mergeTime(dst.Timestamps.LastCheckAt, base.Timestamps.LastCheckAt, incoming.Timestamps.LastCheckAt)
	dst.Timestamps.LastDiffAt = mergeTime(dst.Timestamps.LastDiffAt, base.Timestamps.LastDiffAt, incoming.Timestamps.LastDiffAt)
	dst.Timestamps.LastApplyAt = mergeTime(dst.Timestamps.LastApplyAt, base.Timestamps.LastApplyAt, incoming.Timestamps.LastApplyAt)
	dst.Timestamps.LastObservedAt = mergeTime(dst.Timestamps.LastObservedAt, base.Timestamps.LastObservedAt, incoming.Timestamps.LastObservedAt)
}

// mergeTime applies an incoming timestamp change without moving dst backwards.
// An unchanged base/incoming pair keeps the (possibly newer) dst value.
func mergeTime(dst, base, incoming time.Time) time.Time {
	if incoming.Equal(base) {
		return dst
	}
	if incoming.After(dst) {
		return incoming
	}
	return dst
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeStringMap applies the base→incoming delta to dst instead of replacing
// the whole map. Two writers that add different keys concurrently both survive.
func mergeStringMap(dst, base, incoming map[string]string) map[string]string {
	out := CloneStringMap(dst)
	if out == nil {
		out = map[string]string{}
	}
	for key := range base {
		if _, ok := incoming[key]; !ok {
			delete(out, key)
		}
	}
	for key, value := range incoming {
		previous, existed := base[key]
		if !existed || previous != value {
			out[key] = value
		}
	}
	return out
}

func mergeAssetObservations(dst, base, incoming map[string]*taskdomain.AssetObservation) map[string]*taskdomain.AssetObservation {
	out := CloneAssetObservationMap(dst)
	if out == nil {
		out = map[string]*taskdomain.AssetObservation{}
	}
	for key := range base {
		if _, ok := incoming[key]; !ok {
			delete(out, key)
		}
	}
	for key, value := range incoming {
		previous, existed := base[key]
		if !existed || !reflect.DeepEqual(previous, value) {
			out[key] = CloneAssetObservation(value)
		}
	}
	return out
}

// CloneResourceObservations deep-copies the observed resource inventory.
func CloneResourceObservations(in []ResourceObservation) []ResourceObservation {
	if in == nil {
		return nil
	}
	out := make([]ResourceObservation, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].DependsOn = append([]string(nil), in[i].DependsOn...)
		out[i].Relations = append([]ResourceRelation(nil), in[i].Relations...)
	}
	return out
}

func ClonePartitionRuntime(in *PartitionRuntime) *PartitionRuntime {
	if in == nil {
		return nil
	}
	out := *in
	out.PartitionState = ClonePartitionState(in.PartitionState)
	out.Intents = make(map[string]*IntentState, len(in.Intents))
	for name, state := range in.Intents {
		out.Intents[name] = CloneIntentState(state)
	}
	return &out
}

func CloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringPtr(in *string) *string {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func CloneDriftReport(in *taskdomain.DriftReport) *taskdomain.DriftReport {
	if in == nil {
		return nil
	}
	out := *in
	out.ChangedAssets = append([]string(nil), in.ChangedAssets...)
	return &out
}

func CloneHealthObservation(in *taskdomain.HealthObservation) *taskdomain.HealthObservation {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func CloneApplyReadiness(in *taskdomain.ApplyReadiness) *taskdomain.ApplyReadiness {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func CloneAssetObservationMap(in map[string]*taskdomain.AssetObservation) map[string]*taskdomain.AssetObservation {
	if in == nil {
		return nil
	}
	out := make(map[string]*taskdomain.AssetObservation, len(in))
	for key, value := range in {
		out[key] = CloneAssetObservation(value)
	}
	return out
}

func CloneAssetObservation(in *taskdomain.AssetObservation) *taskdomain.AssetObservation {
	if in == nil {
		return nil
	}
	out := *in
	out.Health = CloneHealthObservation(in.Health)
	out.ApplyReadiness = CloneApplyReadiness(in.ApplyReadiness)
	return &out
}
