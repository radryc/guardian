package probes

import (
	"sort"
	"strings"
)

// SortResults orders probe results by the target service identity, not by the
// producing pusher. The pusher remains the final tiebreaker so two pushers that
// observe the same service are still deterministic.
func SortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		return resultLess(results[i], results[j])
	})
}

func resultLess(a, b Result) bool {
	if a.Service != b.Service {
		return a.Service < b.Service
	}
	if a.Partition != b.Partition {
		return a.Partition < b.Partition
	}
	if a.Intent != b.Intent {
		return a.Intent < b.Intent
	}
	if a.Asset != b.Asset {
		return a.Asset < b.Asset
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	if a.Target != b.Target {
		return a.Target < b.Target
	}
	if a.Pusher != b.Pusher {
		return a.Pusher < b.Pusher
	}
	return a.ProbeID < b.ProbeID
}

// sortTargets orders probe targets by priority and then by the canonical
// service identity. Sorting by ProbeID would order by a hash that embeds the
// pusher name, which is exactly what service-oriented ordering must avoid.
func sortTargets(targets []Target) {
	sort.Slice(targets, func(i, j int) bool {
		left, right := targets[i], targets[j]
		if lp, rp := targetPriority(left), targetPriority(right); lp != rp {
			return lp < rp
		}
		if left.Service != right.Service {
			return left.Service < right.Service
		}
		if left.Partition != right.Partition {
			return left.Partition < right.Partition
		}
		if left.Intent != right.Intent {
			return left.Intent < right.Intent
		}
		if left.Asset != right.Asset {
			return left.Asset < right.Asset
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Type != right.Type {
			return left.Type < right.Type
		}
		if leftValue, rightValue := targetValue(left), targetValue(right); leftValue != rightValue {
			return leftValue < rightValue
		}
		return left.ID < right.ID
	})
}

// targetPriority follows the design order: explicit probes first, then HTTP,
// then other network probes, then raw TCP. Provider-health probes are not a
// separate target type yet; when they are added they belong at priority 2.
func targetPriority(target Target) int {
	if strings.EqualFold(strings.TrimSpace(target.Mode), "explicit") {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(target.Type)) {
	case "http", "https":
		return 1
	case "grpc", "dns", "tls":
		return 2
	default:
		return 3
	}
}
