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

func PartitionConfig(partition string) string {
	return normalize(PartitionRoot(partition), "config.yaml")
}

func PartitionIntentsDir(partition string) string {
	return normalize(PartitionRoot(partition), "intents")
}

func IntentManifest(partition, intent string) string {
	return normalize(PartitionIntentsDir(partition), intent+".yaml")
}

func PartitionState(partition string) string {
	return normalize(PartitionRoot(partition), ".state", "partition.json")
}

func StateRoot(partition string) string { return normalize(PartitionRoot(partition), ".state") }

func PartitionRuntime(partition string) string {
	return normalize(StateRoot(partition), "runtime.json")
}

func StateIntentsDir(partition string) string { return normalize(StateRoot(partition), "intents") }

func IntentState(partition, intent string) string {
	return normalize(StateIntentsDir(partition), intent+".json")
}

func StateAssetsDir(partition string) string { return normalize(StateRoot(partition), "assets") }

func AssetState(partition, intent, asset string) string {
	return normalize(StateAssetsDir(partition), intent+"--"+asset+".json")
}

func StateTasksDir(partition string) string { return normalize(StateRoot(partition), "tasks") }

func TaskState(partition, taskID string) string {
	return normalize(StateTasksDir(partition), taskID+".json")
}

func StateEventsDir(partition string) string { return normalize(StateRoot(partition), "events") }

func EventState(partition, eventID string) string {
	return normalize(StateEventsDir(partition), eventID+".json")
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
