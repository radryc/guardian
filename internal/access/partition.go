package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
	coreauthz "github.com/radryc/monofs/pkg/authz"
	"github.com/radryc/monofs/pkg/authz/policy"
)

// PartitionPolicyFileName is the repository-relative file a partition bundle
// may use to add grants scoped to that partition.
const PartitionPolicyFileName = "access.yaml"

// PartitionPolicyMeta records the repository a partition ACL came from. It is
// written by the repo importer next to the imported ACL and used to scope
// monofs grants to the owning repository.
type PartitionPolicyMeta struct {
	Partition   string `json:"partition"`
	Repo        string `json:"repo,omitempty"`
	Source      string `json:"source,omitempty"`
	DisplayPath string `json:"displayPath,omitempty"`
}

// PartitionPolicy is one repo-owned ACL imported for a partition.
type PartitionPolicy struct {
	Partition   string
	DisplayPath string
	File        *policy.File
	Digest      string
}

// ValidatePartitionPolicy parses and validates a repo-owned access.yaml. The
// file may only grant the named partition; imports, defaults, selectors,
// quotas, aws_bindings, clients, deny rules, wildcards, and cross-partition
// scopes are rejected. monofs patterns must resolve to the owning repository.
func ValidatePartitionPolicy(data []byte, partition, displayPath string) (*policy.File, error) {
	partition = strings.TrimSpace(partition)
	if err := paths.ValidatePartitionName(partition); err != nil {
		return nil, err
	}
	file, err := policy.ParseFile(data)
	if err != nil {
		return nil, err
	}
	if err := validatePartitionPolicyFile(file, partition, displayPath); err != nil {
		return nil, err
	}
	return file, nil
}

func validatePartitionPolicyFile(file *policy.File, partition, displayPath string) error {
	if file == nil {
		return fmt.Errorf("partition access: policy is nil")
	}
	partition = strings.TrimSpace(partition)
	if err := paths.ValidatePartitionName(partition); err != nil {
		return err
	}
	if len(file.Imports) > 0 {
		return fmt.Errorf("partition access: imports are not allowed")
	}
	if len(file.Defaults) > 0 {
		return fmt.Errorf("partition access: defaults are platform-owned and not allowed")
	}
	if len(file.Selectors) > 0 {
		return fmt.Errorf("partition access: selectors are platform-owned and not allowed")
	}
	if len(file.AWSBindings) > 0 {
		return fmt.Errorf("partition access: aws_bindings are platform-owned and not allowed")
	}
	if len(file.QuotaProfiles) > 0 || file.QuotaDefaults != nil || file.Quotas != nil {
		return fmt.Errorf("partition access: quota sections are platform-owned and not allowed")
	}
	if len(file.Clients) > 0 {
		return fmt.Errorf("partition access: clients are platform-owned and not allowed")
	}
	if len(file.Users)+len(file.Groups) == 0 {
		return fmt.Errorf("partition access: at least one user or group grant is required")
	}
	for name, subject := range file.Groups {
		if err := validatePartitionSubject("group", name, partition, displayPath, subject); err != nil {
			return err
		}
	}
	for name, subject := range file.Users {
		if err := validatePartitionSubject("user", name, partition, displayPath, subject); err != nil {
			return err
		}
	}
	return nil
}

func validatePartitionSubject(kind, name, partition, displayPath string, subject policy.Subject) error {
	label := fmt.Sprintf("partition access: %s %q", kind, strings.TrimSpace(name))
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("partition access: %s name is required", kind)
	}
	if len(subject.Deny) > 0 {
		return fmt.Errorf("%s: deny rules are not allowed", label)
	}
	if subject.Role != "" && !coreauthz.Role(subject.Role).Valid() {
		return fmt.Errorf("%s: invalid role %q", label, subject.Role)
	}
	for _, scope := range []struct {
		name  string
		items []string
	}{
		{name: "partitions", items: subject.Partitions},
		{name: "logs", items: subject.Logs},
		{name: "traces", items: subject.Traces},
	} {
		for _, item := range scope.items {
			if strings.TrimSpace(item) != partition {
				return fmt.Errorf("%s: %s %q is outside partition %q", label, scope.name, item, partition)
			}
		}
	}
	for _, pattern := range subject.MonoFS {
		if err := validatePartitionMonoFS(pattern, displayPath); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	if subject.Role != "" && !scopeContains(subject.Partitions, partition) {
		return fmt.Errorf("%s: role %q requires partitions: [%s]", label, subject.Role, partition)
	}
	if len(subject.Partitions)+len(subject.Logs)+len(subject.Traces)+len(subject.MonoFS) == 0 {
		return fmt.Errorf("%s: at least one scope is required", label)
	}
	return nil
}

// validatePartitionMonoFS only accepts a repo-root pattern for the owning
// repository. Wildcards are allowed for host/owner segments (for example
// github.com/*/memebank) but never for the repository name or a subtree.
func validatePartitionMonoFS(pattern, displayPath string) error {
	raw := strings.TrimSpace(pattern)
	if raw == "" {
		return fmt.Errorf("monofs scope is empty")
	}
	if raw == "*" || strings.Contains(raw, "**") {
		return fmt.Errorf("monofs scope %q is not partition-scoped", raw)
	}
	displayPath = strings.Trim(strings.TrimSpace(displayPath), "/")
	if displayPath == "" {
		return fmt.Errorf("monofs scope %q requires a repository-backed partition", raw)
	}
	pattern = strings.Trim(raw, "/")
	patternParts := strings.Split(pattern, "/")
	displayParts := strings.Split(displayPath, "/")
	if len(patternParts) != len(displayParts) {
		return fmt.Errorf("monofs scope %q does not target the owning repository %q", raw, displayPath)
	}
	last := patternParts[len(patternParts)-1]
	if strings.ContainsAny(last, "*?[]") || last != displayParts[len(displayParts)-1] {
		return fmt.Errorf("monofs scope %q must name the owning repository %q", raw, displayParts[len(displayParts)-1])
	}
	if ok, err := path.Match(pattern, displayPath); err != nil || !ok {
		return fmt.Errorf("monofs scope %q does not match the owning repository %q", raw, displayPath)
	}
	return nil
}

// LoadPartitionPolicies reads repo-owned ACLs from the store. Invalid files are
// returned as errors and skipped; they never affect the compiled policy.
func LoadPartitionPolicies(ctx context.Context, store guardianapi.ReadStore, catalog *policy.Catalog) ([]PartitionPolicy, []error) {
	if store == nil {
		return nil, []error{fmt.Errorf("partition access: no store configured")}
	}
	entries, err := store.ListDir(ctx, paths.AccessPartitionsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("partition access: list %s: %w", paths.AccessPartitionsDir(), err)}
	}
	known := map[string]bool{}
	if catalog != nil {
		for _, partition := range catalog.Partitions {
			known[partition.Name] = true
		}
	}
	out := make([]PartitionPolicy, 0, len(entries))
	var problems []error
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".yaml") {
			continue
		}
		partition := strings.TrimSuffix(entry.Name, ".yaml")
		if err := paths.ValidatePartitionName(partition); err != nil {
			problems = append(problems, fmt.Errorf("partition access: %s: %w", entry.Name, err))
			continue
		}
		if len(known) > 0 && !known[partition] {
			problems = append(problems, fmt.Errorf("partition access: %q is not in the catalog", partition))
			continue
		}
		data, err := store.ReadFile(ctx, paths.AccessPartitionPolicy(partition))
		if err != nil {
			problems = append(problems, fmt.Errorf("partition access: read %s: %w", partition, err))
			continue
		}
		displayPath, err := readPartitionMeta(ctx, store, partition)
		if err != nil {
			problems = append(problems, fmt.Errorf("partition access: %s metadata: %w", partition, err))
			continue
		}
		file, err := ValidatePartitionPolicy(data, partition, displayPath)
		if err != nil {
			problems = append(problems, fmt.Errorf("partition access %s: %w", partition, err))
			continue
		}
		out = append(out, PartitionPolicy{
			Partition:   partition,
			DisplayPath: displayPath,
			File:        file,
			Digest:      digestBytes(data),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Partition < out[j].Partition })
	return out, problems
}

func readPartitionMeta(ctx context.Context, store guardianapi.ReadStore, partition string) (string, error) {
	raw, err := store.ReadFile(ctx, paths.AccessPartitionMeta(partition))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var meta PartitionPolicyMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return "", err
	}
	if strings.TrimSpace(meta.Partition) != "" && strings.TrimSpace(meta.Partition) != partition {
		return "", fmt.Errorf("metadata partition %q does not match %q", meta.Partition, partition)
	}
	return strings.Trim(strings.TrimSpace(meta.DisplayPath), "/"), nil
}

// PartitionPoliciesDigest returns a stable digest of the imported partition
// ACLs so the service can skip recompiles when nothing changed.
func PartitionPoliciesDigest(policies []PartitionPolicy) string {
	if len(policies) == 0 {
		return ""
	}
	sorted := append([]PartitionPolicy(nil), policies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Partition < sorted[j].Partition })
	h := sha256.New()
	for _, p := range sorted {
		fmt.Fprintf(h, "%s\x00%s\x00%s\n", p.Partition, p.DisplayPath, p.Digest)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// CompileWithPartitions compiles the platform policy and appends the grants
// from imported partition ACLs. The partition files are validated again here
// so a stored file written outside the repo importer cannot widen its scope.
func CompileWithPartitions(base *policy.File, catalog *policy.Catalog, partitions []PartitionPolicy, now time.Time) (*policy.Policy, []policy.Issue, error) {
	return CompileWithRouting(base, catalog, partitions, nil, now)
}

// CompileWithRouting compiles the platform policy together with the
// repo-owned partition ACLs and the platform routing definitions (shards and
// placements). The routing definitions are embedded in the compiled bundle and
// consumed by the federated SQL router.
func CompileWithRouting(base *policy.File, catalog *policy.Catalog, partitions []PartitionPolicy, routing []policy.PartitionPolicy, now time.Time) (*policy.Policy, []policy.Issue, error) {
	baseBundle, issues, err := policy.CompileWithPartitions(base, catalog, routing, now)
	if err != nil {
		return nil, issues, err
	}
	merged := *baseBundle
	// The base bundle already carries a version; clear it so MarshalBundle
	// recomputes the version over the merged grants.
	merged.Version = ""
	for _, partition := range partitions {
		if err := validatePartitionPolicyFile(partition.File, partition.Partition, partition.DisplayPath); err != nil {
			return nil, issues, fmt.Errorf("partition %s access: %w", partition.Partition, err)
		}
		bundle, partitionIssues, err := policy.Compile(partition.File, catalog, now)
		issues = append(issues, partitionIssues...)
		if err != nil {
			return nil, issues, fmt.Errorf("partition %s access: %w", partition.Partition, err)
		}
		merged.Grants = append(merged.Grants, bundle.Grants...)
	}
	data, err := policy.MarshalBundle(merged)
	if err != nil {
		return nil, issues, err
	}
	compiled, err := policy.ParseBundle(data)
	if err != nil {
		return nil, issues, err
	}
	return compiled, issues, nil
}

func scopeContains(list []string, value string) bool {
	for _, item := range list {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}
