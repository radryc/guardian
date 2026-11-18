package driverutil

import (
	"strings"

	targetdomain "github.com/radryc/guardian/internal/domain/target"
	"github.com/radryc/guardian/internal/observability"
	"github.com/radryc/guardian/internal/pusher/registry"
	"github.com/radryc/guardian/internal/versioning/digest"
)

func ResourceName(prefix string, target targetdomain.Placement, partition, intent, asset string, extras ...string) string {
	parts := []string{prefix, target.Cluster, target.Namespace, target.Region, target.Account, partition, intent, asset}
	parts = append(parts, extras...)
	for i, part := range parts {
		parts[i] = sanitizePart(part)
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		filtered = []string{"guardian"}
	}
	base := strings.Join(filtered, "-")
	if len(base) <= 63 {
		return base
	}
	sum := digest.MustNormalizedHash(filtered)[:10]
	keep := 63 - 1 - len(sum)
	if keep < 1 {
		keep = 1
	}
	return strings.Trim(base[:keep], "-") + "-" + sum
}

func Labels(provider string, in registry.AssetInput, hash string) map[string]string {
	labels := map[string]string{
		"guardian.managed":       "true",
		"guardian.provider":      provider,
		"guardian.partition":     in.PartitionName,
		"guardian.intent":        in.IntentName,
		"guardian.asset":         in.Asset.Name,
		"guardian.asset.type":    in.Asset.Type,
		"guardian.identity.kind": "managed",
		"guardian.hash":          limitLabelValue(hash),
	}
	// Empty values are invalid AWS tag values; omit them instead of sending
	// `guardian.cluster=` (CloudFormation rejects empty tag values).
	if cluster := strings.TrimSpace(in.Target.Cluster); cluster != "" {
		labels["guardian.cluster"] = cluster
	}
	return labels
}

func limitLabelValue(value string) string {
	const maxLabelValueLen = 63
	if len(value) <= maxLabelValueLen {
		return value
	}
	return value[:maxLabelValueLen]
}

const MaxServiceNameLength = observability.MaxServiceNameLength

// ServiceIdentity is an alias for the shared canonical identity type.

type ServiceIdentity = observability.ServiceIdentity

// CanonicalServiceIdentity builds <partition>.<intent>.<asset> and applies a
// deterministic hash suffix when the name exceeds MaxServiceNameLength.

func CanonicalServiceIdentity(partition, intent, asset string) ServiceIdentity {
	return observability.CanonicalServiceIdentity(partition, intent, asset)
}

// AutoInstrumentDisabled reports whether a compute asset opts out of generated
// observability through the documented workload label. The label is also
// propagated to Kubernetes workloads so operators can see the opt-out.
