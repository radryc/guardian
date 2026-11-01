package paths

import (
	"fmt"
	"regexp"
	"strings"
)

// Names that become path segments must be validated before they reach the
// store. path.Join/path.Clean collapse "..", so an unvalidated name such as
// "../../.registry" can escape its intended namespace.
var (
	namePattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	envPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// ValidateSegment rejects empty names, path separators, traversal, absolute
// paths, and names outside the Guardian naming grammar. kind is used in the
// error message only.
func ValidateSegment(kind, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", kind)
	}
	if value == "." || value == ".." || strings.ContainsAny(value, "/\\") {
		return fmt.Errorf("invalid %s %q", kind, value)
	}
	if !namePattern.MatchString(value) {
		return fmt.Errorf("invalid %s %q: must match %s", kind, value, namePattern.String())
	}
	return nil
}

// ValidatePartitionName validates a partition name used as a path segment.
func ValidatePartitionName(value string) error { return ValidateSegment("partition name", value) }

// ValidateIntentName validates an intent name used as a path segment.
func ValidateIntentName(value string) error { return ValidateSegment("intent name", value) }

// ValidatePusherName validates a pusher/queue name used as a path segment.
// Pusher names are less strict than partition names because cluster contexts
// can contain dots; the only hard requirements are non-empty, no separator,
// and no traversal.
func ValidatePusherName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("pusher name is required")
	}
	if value == "." || value == ".." || strings.ContainsAny(value, "/\\") || !taskIDPattern.MatchString(value) {
		return fmt.Errorf("invalid pusher name %q", value)
	}
	return nil
}

// ValidateEnvName validates an access-policy environment name.
func ValidateEnvName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("environment name is required")
	}
	if value == "." || value == ".." || strings.ContainsAny(value, "/\\") || !envPattern.MatchString(value) {
		return fmt.Errorf("invalid environment name %q", value)
	}
	return nil
}

// ValidateTaskID validates a task identifier used in queue/claim/result paths.
// Generated IDs use task_<unixnano>_<hex>; allow a conservative superset that
// cannot contain a separator or traversal.
func ValidateTaskID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("task id is required")
	}
	if value == "." || value == ".." || !taskIDPattern.MatchString(value) {
		return fmt.Errorf("invalid task id %q", value)
	}
	return nil
}

// ValidateQueueIdentity validates the untrusted identity fields of a task
// result before they are used to construct queue/claim/result paths.
func ValidateQueueIdentity(pusher, taskID string) error {
	if err := ValidatePusherName(pusher); err != nil {
		return err
	}
	return ValidateTaskID(taskID)
}
