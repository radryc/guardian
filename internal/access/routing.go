package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/radryc/guardian/pkg/guardianapi"
	"github.com/radryc/monofs/pkg/authz/policy"
)

// RoutingFileName is the per-partition shard/placement definition written by
// `guardianctl access publish` next to the environment access policy.
const RoutingFileName = "partition.yaml"

// RoutingDefaultsFileName is the global override file for partition routing.
const RoutingDefaultsFileName = "defaults.yaml"

// RoutingDir returns the routing directory that belongs to one environment
// access policy path (/.access/envs/<env>.yaml -> /.access/envs/<env>/routing).
func RoutingDir(accessPolicyPath string) string {
	return path.Join(path.Dir(accessPolicyPath), strings.TrimSuffix(path.Base(accessPolicyPath), ".yaml"), "routing")
}

// RoutingPartitionPath is the store path for one partition routing definition.
func RoutingPartitionPath(dir, partition string) string {
	return path.Join(dir, partition, RoutingFileName)
}

// RoutingDefaultsPath is the store path for the global routing overrides.
func RoutingDefaultsPath(dir string) string { return path.Join(dir, RoutingDefaultsFileName) }

// LoadRoutingPolicies reads the per-partition routing definitions for an
// environment. Missing data is not an error: older packs keep using the
// default single-backend path.
func LoadRoutingPolicies(ctx context.Context, store guardianapi.ReadStore, dir string) ([]policy.PartitionPolicy, []error) {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		return nil, nil
	}
	var problems []error
	var defaults *policy.PartitionDefaults
	if raw, err := store.ReadFile(ctx, RoutingDefaultsPath(dir)); err == nil {
		parsed, parseErr := policy.ParsePartitionDefaults(raw)
		if parseErr != nil {
			problems = append(problems, fmt.Errorf("routing: defaults: %w", parseErr))
		} else {
			defaults = parsed
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		problems = append(problems, fmt.Errorf("routing: read defaults: %w", err))
	}

	entries, err := store.ListDir(ctx, dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, problems
		}
		return nil, append(problems, fmt.Errorf("routing: list %s: %w", dir, err))
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir {
			names = append(names, entry.Name)
		}
	}
	sort.Strings(names)
	out := make([]policy.PartitionPolicy, 0, len(names))
	for _, name := range names {
		if err := validateRoutingPartitionName(name); err != nil {
			problems = append(problems, err)
			continue
		}
		raw, err := store.ReadFile(ctx, RoutingPartitionPath(dir, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				problems = append(problems, fmt.Errorf("routing: partition %q has no %s", name, RoutingFileName))
				continue
			}
			problems = append(problems, fmt.Errorf("routing: read %s: %w", name, err))
			continue
		}
		parsed, err := policy.ParsePartitionPolicy(raw)
		if err != nil {
			problems = append(problems, fmt.Errorf("routing %s: %w", name, err))
			continue
		}
		if strings.TrimSpace(parsed.Name) == "" {
			parsed.Name = name
		}
		if parsed.Name != name {
			problems = append(problems, fmt.Errorf("routing: %s names partition %q", name, parsed.Name))
			continue
		}
		applied, err := defaults.Apply(parsed)
		if err != nil {
			problems = append(problems, fmt.Errorf("routing %s: %w", name, err))
			continue
		}
		out = append(out, *applied)
	}
	for _, issue := range policy.ValidatePartitions(out) {
		if issue.Severity == "error" {
			problems = append(problems, fmt.Errorf("routing: %s", issue.Message))
		}
	}
	return out, problems
}

// RoutingPoliciesDigest returns a stable digest so refresh can skip unchanged
// definitions.
func RoutingPoliciesDigest(policies []policy.PartitionPolicy) string {
	if len(policies) == 0 {
		return ""
	}
	sorted := append([]policy.PartitionPolicy(nil), policies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	h := sha256.New()
	for _, p := range sorted {
		fmt.Fprintf(h, "%s\x00%v\n", p.Name, p)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func validateRoutingPartitionName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("routing: invalid partition directory %q", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("routing: invalid partition directory %q", name)
		}
	}
	return nil
}

// LocalRouting holds routing definitions read from a local directory before
// they are published into the store.
type LocalRouting struct {
	Defaults   []byte
	Partitions map[string][]byte
	Policies   []policy.PartitionPolicy
	Problems   []error
}

// ReadRoutingDir loads a local routing directory. The expected layout is
//
//	<dir>/defaults.yaml
//	<dir>/<partition>/partition.yaml
//
// A missing directory returns an empty result. Partitions whose name does not
// match the directory are reported and skipped.
func ReadRoutingDir(dir string) *LocalRouting {
	out := &LocalRouting{Partitions: map[string][]byte{}}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			out.Problems = append(out.Problems, fmt.Errorf("routing: read %s: %w", dir, err))
		}
		return out
	}
	var defaults *policy.PartitionDefaults
	if raw, err := os.ReadFile(path.Join(dir, RoutingDefaultsFileName)); err == nil {
		out.Defaults = raw
		parsed, parseErr := policy.ParsePartitionDefaults(raw)
		if parseErr != nil {
			out.Problems = append(out.Problems, fmt.Errorf("routing: defaults: %w", parseErr))
		} else {
			defaults = parsed
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		out.Problems = append(out.Problems, fmt.Errorf("routing: read defaults: %w", err))
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := validateRoutingPartitionName(name); err != nil {
			out.Problems = append(out.Problems, err)
			continue
		}
		raw, err := os.ReadFile(path.Join(dir, name, RoutingFileName))
		if err != nil {
			out.Problems = append(out.Problems, fmt.Errorf("routing: partition %q has no %s", name, RoutingFileName))
			continue
		}
		parsed, err := policy.ParsePartitionPolicy(raw)
		if err != nil {
			out.Problems = append(out.Problems, fmt.Errorf("routing %s: %w", name, err))
			continue
		}
		if strings.TrimSpace(parsed.Name) == "" {
			parsed.Name = name
		} else if parsed.Name != name {
			out.Problems = append(out.Problems, fmt.Errorf("routing: %s names partition %q", name, parsed.Name))
			continue
		}
		applied, err := defaults.Apply(parsed)
		if err != nil {
			out.Problems = append(out.Problems, fmt.Errorf("routing %s: %w", name, err))
			continue
		}
		out.Partitions[name] = raw
		out.Policies = append(out.Policies, *applied)
	}
	for _, issue := range policy.ValidatePartitions(out.Policies) {
		if issue.Severity == "error" {
			out.Problems = append(out.Problems, fmt.Errorf("routing: %s", issue.Message))
		}
	}
	return out
}
