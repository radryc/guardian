// Package observability owns the canonical telemetry identity shared by every
// signal producer. Keeping the identity in one package prevents drivers and
// generated stacks from drifting apart.
package observability

import (
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/versioning/digest"
)

// MaxServiceNameLength is the backend-safe cap for the generated service.name.
// 200 matches the AWS X-Ray segment-name limit and is accepted by the other
// Doctor/Grafana backends.
const MaxServiceNameLength = 200

// ServiceIdentity is the canonical telemetry identity of one asset.
type ServiceIdentity struct {
	Name      string
	FullName  string
	Truncated bool
}

// CanonicalServiceIdentity builds <partition>.<intent>.<asset> and applies a
// deterministic hash suffix when the name exceeds MaxServiceNameLength.
func CanonicalServiceIdentity(partition, intent, asset string) ServiceIdentity {
	parts := make([]string, 0, 3)
	for _, part := range []string{partition, intent, asset} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	full := strings.Join(parts, ".")
	if full == "" {
		return ServiceIdentity{}
	}
	if len(full) <= MaxServiceNameLength {
		return ServiceIdentity{Name: full, FullName: full}
	}
	sum := digest.MustNormalizedHash([]string{full})[:10]
	keep := MaxServiceNameLength - 1 - len(sum)
	if keep < 1 {
		keep = 1
	}
	prefix := strings.TrimRight(full[:keep], "._-")
	if prefix == "" {
		prefix = "service"
	}
	return ServiceIdentity{Name: prefix + "." + sum, FullName: full, Truncated: true}
}
