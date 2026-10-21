package paths

import (
	"path"
	"strings"
)

func normalize(parts ...string) string {
	joined := path.Join(parts...)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	return path.Clean(joined)
}

func PartitionsRoot() string { return "/partitions" }

func PartitionRoot(partition string) string { return normalize("/partitions", partition) }

// PartitionPath builds a logical path under a partition from a bundle-relative
// path such as "intents/app.yaml".
func PartitionPath(partition, relative string) string {
	return normalize(PartitionRoot(partition), relative)
}

func PartitionConfig(partition string) string {
	return normalize(PartitionRoot(partition), "config.yaml")
}

func PartitionIntentsDir(partition string) string {
	return normalize(PartitionRoot(partition), "intents")
}

func IntentManifest(partition, intent string) string {
	return normalize(PartitionIntentsDir(partition), intent+".yaml")
}

// GeneratedAWSPayloadDir holds the TypeScript CDK app that Guardian generates
// for an intent using spec.implementation=cdk. The AWS pusher stages this tree
// from the store and deploys it as one CloudFormation stack.
func GeneratedAWSPayloadDir(partition, intent string) string {
	return normalize(PartitionRoot(partition), "payloads", "aws-generated", intent)
}

func PartitionState(partition string) string {
	return normalize(PartitionRoot(partition), ".state", "partition.json")
}

// PartitionDeletion marks a partition for deferred deletion. While present,
// the reconciler destroys the partition's intents according to deletionPolicy
// and purges the subtree once teardown completes.
func PartitionDeletion(partition string) string {
	return normalize(PartitionRoot(partition), ".deletion.json")
}

func StateRoot(partition string) string { return normalize(PartitionRoot(partition), ".state") }

func StateIntentsDir(partition string) string { return normalize(StateRoot(partition), "intents") }

func IntentState(partition, intent string) string {
	return normalize(StateIntentsDir(partition), intent+".json")
}

func StateAssetsDir(partition string) string { return normalize(StateRoot(partition), "assets") }

func AssetState(partition, intent, asset string) string {
	return normalize(StateAssetsDir(partition), intent+"--"+asset+".json")
}

func StateEventsDir(partition string) string { return normalize(StateRoot(partition), "events") }

func EventState(partition, eventID string) string {
	return normalize(StateEventsDir(partition), eventID+".json")
}

// PartitionReleaseLog is the per-partition authoritative pointer to the
// latest release of each intent. Kept under .state/ alongside runtime.json.
func PartitionReleaseLog(partition string) string {
	return normalize(StateRoot(partition), "releases.json")
}

// ObservedRoot holds Guardian-captured observed state for AWS observed
// intents. It lives beside .state but is written by the observed collector,
// not the orchestrator dispatcher.
func ObservedRoot(partition string) string {
	return normalize(PartitionRoot(partition), ".observed")
}

func ObservedIntentDir(partition, intent string) string {
	return normalize(ObservedRoot(partition), intent)
}

func ObservedSnapshot(partition, intent, asset string) string {
	return normalize(ObservedIntentDir(partition, intent), asset+".json")
}

func ObservedEventsDir(partition, intent string) string {
	return normalize(ObservedIntentDir(partition, intent), "events")
}

func ObservedEvent(partition, intent, eventID string) string {
	return normalize(ObservedEventsDir(partition, intent), eventID+".json")
}

// RegistryRoot holds Guardian platform registrations (AWS pushers and their
// source repositories). It is store-backed so the UI, CLI and guardiand all
// read the same records.
func RegistryRoot() string { return "/.registry" }

func PushersRoot() string { return normalize(RegistryRoot(), "pushers") }

func PusherRecord(name string) string { return normalize(PushersRoot(), name+".json") }

func ReposRoot() string { return normalize(RegistryRoot(), "repos") }

func RepoRecord(name string) string { return normalize(ReposRoot(), name+".json") }

// OnboardingRoot holds the last onboarding descriptor pulled from each pusher.
// It is an audit record; the SSM parameter remains the source of truth.
func OnboardingRoot() string { return normalize(RegistryRoot(), "onboarding") }

func OnboardingDir(pusher string) string { return normalize(OnboardingRoot(), pusher) }

func OnboardingRecord(pusher, stack string) string {
	return normalize(OnboardingDir(pusher), stack+".json")
}

// DashboardsRoot holds generated Grafana dashboard JSON per partition.
func DashboardsRoot() string { return normalize(RegistryRoot(), "dashboards") }

func DashboardRecord(partition string) string {
	return normalize(DashboardsRoot(), partition+".json")
}

// ProbesRoot holds the latest probe snapshots written by pushers. History
// lives in telemetry, not in MonoFS.
func ProbesRoot() string { return normalize(RegistryRoot(), "probes") }

func ProbesDir(pusher string) string { return normalize(ProbesRoot(), pusher) }

func ProbeSnapshot(pusher, partition string) string {
	return normalize(ProbesDir(pusher), partition+".json")
}

// SetsRoot holds the virtual navigation sets. Sets are metadata only; they do
// not affect partitions, deployments, or MonoFS paths.
func SetsRoot() string { return normalize(RegistryRoot(), "sets") }

func SetRecord(id string) string { return normalize(SetsRoot(), id+".json") }

// SetPrimaryRoot records the optional primary set for a partition.
func SetPrimaryRoot() string { return normalize(RegistryRoot(), "set-primary") }

func SetPrimaryRecord(partition string) string {
	return normalize(SetPrimaryRoot(), partition+".json")
}

func QueueRoot() string { return "/.queues" }

func QueueDir(pusher string) string { return normalize(QueueRoot(), pusher) }

func QueueTask(pusher, taskID string) string { return normalize(QueueDir(pusher), taskID+".json") }

func QueueClaimsDir(pusher string) string { return normalize(QueueDir(pusher), ".claims") }

func QueueClaim(pusher, taskID string) string {
	return normalize(QueueClaimsDir(pusher), taskID+".json")
}

func QueueResultsDir(pusher string) string { return normalize(QueueDir(pusher), ".results") }

func QueueResult(pusher, taskID string) string {
	return normalize(QueueResultsDir(pusher), taskID+".json")
}

func ScansRoot() string { return "/.scans" }

func ScanDir(pusher string) string { return normalize(ScansRoot(), pusher) }

func ScanRequestsDir(pusher string) string { return normalize(ScanDir(pusher), "requests") }

func ScanRequest(pusher, scanID string) string {
	return normalize(ScanRequestsDir(pusher), scanID+".json")
}

func ScanClaimsDir(pusher string) string { return normalize(ScanDir(pusher), ".claims") }

func ScanClaim(pusher, scanID string) string {
	return normalize(ScanClaimsDir(pusher), scanID+".json")
}

func ScanResultsDir(pusher string) string { return normalize(ScanDir(pusher), ".results") }

func ScanResult(pusher, scanID string) string {
	return normalize(ScanResultsDir(pusher), scanID+".json")
}

// AccessRoot holds the repo-managed access policy: the raw ACL published per
// environment plus the compiled bundle that MonoFS and Doctor consume.
func AccessRoot() string { return "/.access" }

// AccessPolicyPath is the raw ACL for one environment (access.yaml published
// by CI into the store).
func AccessPolicyPath(env string) string {
	return normalize(AccessRoot(), "envs", env+".yaml")
}

// AccessCompiledPath is the compiled policy bundle served by guardiand.
func AccessCompiledPath() string {
	return normalize(AccessRoot(), "compiled", "policy.json")
}

func ArchiveRoot() string { return "/.archive" }

func ArchiveIntentRoot(partition, intent string) string {
	return normalize(ArchiveRoot(), partition, intent)
}

func ArchiveIndex(partition, intent string) string {
	return normalize(ArchiveIntentRoot(partition, intent), "index.json")
}

func ArchiveManifest(partition, intent, deployment string) string {
	return normalize(ArchiveIntentRoot(partition, intent), deployment, "manifest.yaml")
}

func ArchiveState(partition, intent, deployment string) string {
	return normalize(ArchiveIntentRoot(partition, intent), deployment, "state.json")
}

func ArchiveLogs(partition, intent, deployment string) string {
	return normalize(ArchiveIntentRoot(partition, intent), deployment, "logs.ndjson")
}

func PartitionFromLogicalPath(logicalPath string) (string, bool) {
	clean := path.Clean(logicalPath)
	if !strings.HasPrefix(clean, "/partitions/") {
		return "", false
	}
	rest := strings.TrimPrefix(clean, "/partitions/")
	if rest == "" {
		return "", false
	}
	segs := strings.Split(rest, "/")
	if len(segs) == 0 || segs[0] == "" {
		return "", false
	}
	return segs[0], true
}
