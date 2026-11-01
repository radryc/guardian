// Package repoimport imports a repository-authored partition bundle
// (guardian/partitions/<name>/ inside an ingested repository) into the
// canonical Guardian partition namespace (/partitions/<name>/...).
//
// It is the Guardian-side half of the repo → MonoFS → deploy flow: MonoFS
// ingests and refreshes the repository (its auto-refresh worker), and Guardian
// reads guardian/partitions/** through the /.repos/<display-path> logical
// mapping and imports it with the same guardrails as guardianctl repo sync.
//
// Import is level-triggered: files are compared against the current store
// content and only changed files are written, so an unchanged repository does
// not wake the partition watcher.
package repoimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/radryc/guardian/internal/compiler/manifest"
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// PartitionsSubdir is the repository-relative path holding partition bundles.
const PartitionsSubdir = "guardian/partitions"

// Source abstracts the repository content root (local checkout or a MonoFS
// logical path).
type Source interface {
	ListDir(ctx context.Context, dir string) ([]Entry, error)
	ReadFile(ctx context.Context, file string) ([]byte, error)
}

// Entry is one directory entry returned by a Source.
type Entry struct {
	Name  string
	IsDir bool
}

// LocalSource reads a repository from the local filesystem.
type LocalSource struct {
	Root string
}

// NewLocalSource returns a filesystem-backed repository source.
func NewLocalSource(root string) *LocalSource { return &LocalSource{Root: root} }

func (s *LocalSource) ListDir(_ context.Context, dir string) ([]Entry, error) {
	full := filepath.Join(s.Root, filepath.FromSlash(dir))
	items, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, item := range items {
		out = append(out, Entry{Name: item.Name(), IsDir: item.IsDir()})
	}
	return out, nil
}

func (s *LocalSource) ReadFile(_ context.Context, file string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(file)))
}

// StoreSource reads a repository through a MonoFS logical path (for example
// /.repos/github.com/example/strata).
type StoreSource struct {
	Store guardianapi.ReadStore
	Root  string
}

// NewStoreSource returns a store-backed repository source rooted at root.
func NewStoreSource(store guardianapi.ReadStore, root string) *StoreSource {
	return &StoreSource{Store: store, Root: strings.TrimRight(root, "/")}
}

func (s *StoreSource) ListDir(ctx context.Context, dir string) ([]Entry, error) {
	entries, err := s.Store.ListDir(ctx, path.Join(s.Root, dir))
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, Entry{Name: entry.Name, IsDir: entry.IsDir})
	}
	return out, nil
}

func (s *StoreSource) ReadFile(ctx context.Context, file string) ([]byte, error) {
	return s.Store.ReadFile(ctx, path.Join(s.Root, file))
}

// Options configures an import run.
type Options struct {
	// RepoName is recorded in mutation context and results.
	RepoName string
	// Partition limits the import to one partition. Empty imports every
	// partition under guardian/partitions/.
	Partition string
	// PartitionPath overrides the repo-relative bundle directory for Partition
	// (for example a custom RepoBinding.PartitionPath). Empty uses
	// guardian/partitions/<partition>.
	PartitionPath string
	// IgnoreMissing returns an empty result instead of an error when the
	// repository has no matching partition bundle.
	IgnoreMissing bool
	// Principal is the mutation principal id recorded in the store.
	Principal string
	// Source identifies the repository source (URL or logical path). Pruning
	// only removes files recorded by the same source.
	Source string
	// Prune removes previously imported files that are no longer present in
	// the repository. It is a no-op when no ownership manifest exists.
	Prune bool
}

// Result summarizes an import run.
type Result struct {
	RepoName string   `json:"repo"`
	Imported []string `json:"imported"`
	Skipped  []string `json:"skipped"`
	Warnings []string `json:"warnings,omitempty"`
	// FilesWritten counts changed files pushed into the store.
	FilesWritten int `json:"filesWritten"`
	// FilesDeleted counts stale files removed by pruning.
	FilesDeleted int `json:"filesDeleted,omitempty"`
	// Orphaned lists partitions owned by this source that disappeared from the
	// repository. They are reported for explicit approval, not deleted
	// automatically.
	Orphaned []string `json:"orphaned,omitempty"`
	// Changed reports whether any file content changed.
	Changed bool `json:"changed"`
}

type ownershipManifest struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Source     string   `json:"source,omitempty"`
	Repo       string   `json:"repo,omitempty"`
	Files      []string `json:"files"`
}

// Import reads guardian/partitions/** from source and writes changed files into
// the canonical partition namespace. Inline Secret values are rejected and
// secrets/** is never imported.
func Import(ctx context.Context, source Source, store guardianapi.Store, opts Options) (*Result, error) {
	if source == nil || store == nil {
		return nil, fmt.Errorf("source and store are required")
	}
	result := &Result{RepoName: opts.RepoName}
	if strings.TrimSpace(opts.PartitionPath) != "" && strings.TrimSpace(opts.Partition) == "" {
		return nil, fmt.Errorf("partitionPath requires a single partition filter")
	}

	partitions, err := listPartitions(ctx, source, opts.Partition, opts.PartitionPath)
	if err != nil {
		return nil, err
	}
	if len(partitions) == 0 {
		if opts.IgnoreMissing {
			return result, nil
		}
		return nil, fmt.Errorf("no %s/<partition> found in the repository", PartitionsSubdir)
	}

	sourceID := strings.TrimSpace(opts.Source)
	if sourceID == "" {
		sourceID = strings.TrimSpace(opts.RepoName)
	}
	if opts.Prune && strings.TrimSpace(opts.Partition) == "" && sourceID != "" {
		current := make(map[string]struct{}, len(partitions))
		for _, partition := range partitions {
			current[partition] = struct{}{}
		}
		stored, listErr := store.ListDir(ctx, paths.PartitionsRoot())
		if listErr != nil && !isNotExist(listErr) {
			return nil, fmt.Errorf("list stored partitions: %w", listErr)
		}
		for _, entry := range stored {
			if !entry.IsDir {
				continue
			}
			if _, ok := current[entry.Name]; ok {
				continue
			}
			previous, _, loadErr := loadOwnershipManifest(ctx, store, entry.Name)
			if loadErr == nil && previous.Source == sourceID {
				result.Orphaned = append(result.Orphaned, entry.Name)
			}
		}
		sort.Strings(result.Orphaned)
		if len(result.Orphaned) > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("partitions no longer present in %s require explicit deletion: %s", opts.RepoName, strings.Join(result.Orphaned, ", ")))
		}
	}
	writes := make([]guardianapi.PathWrite, 0, 16)
	deletes := make([]guardianapi.PathDelete, 0)
	manifests := make([]guardianapi.PathWrite, 0, len(partitions))
	for _, partition := range partitions {
		partitionWrites, warnings, err := collectPartition(ctx, source, partition, partitionBundlePath(partition, opts.PartitionPath))
		if err != nil {
			return nil, fmt.Errorf("partition %s: %w", partition, err)
		}
		result.Warnings = append(result.Warnings, warnings...)

		desired := make(map[string]bool, len(partitionWrites))
		for _, write := range partitionWrites {
			desired[write.LogicalPath] = true
		}
		changed := false
		for _, write := range partitionWrites {
			current, readErr := store.ReadFile(ctx, write.LogicalPath)
			if readErr == nil && bytes.Equal(current, write.Content) {
				continue
			}
			if readErr != nil && !isNotExist(readErr) {
				return nil, fmt.Errorf("read current %s: %w", write.LogicalPath, readErr)
			}
			writes = append(writes, write)
			changed = true
		}
		partitionDeletes := 0
		if opts.Prune && sourceID != "" {
			previous, previousRaw, loadErr := loadOwnershipManifest(ctx, store, partition)
			if loadErr == nil && previous.Source == sourceID {
				prefix := paths.PartitionRoot(partition) + "/"
				for _, owned := range previous.Files {
					if desired[owned] || !strings.HasPrefix(owned, prefix) {
						continue
					}
					deletes = append(deletes, guardianapi.PathDelete{LogicalPath: owned})
					partitionDeletes++
				}
			} else if loadErr == nil && previous.Source != "" && previous.Source != sourceID {
				result.Warnings = append(result.Warnings, fmt.Sprintf("partition %s: refusing to prune files owned by %s", partition, previous.Source))
			}
			manifestContent, marshalErr := buildOwnershipManifest(sourceID, opts.RepoName, desired)
			if marshalErr != nil {
				return nil, fmt.Errorf("partition %s: %w", partition, marshalErr)
			}
			if !bytes.Equal(previousRaw, manifestContent) {
				manifests = append(manifests, guardianapi.PathWrite{
					LogicalPath: paths.RepoImportManifest(partition),
					Content:     manifestContent,
				})
			}
		}
		if changed || partitionDeletes > 0 {
			result.Imported = append(result.Imported, partition)
			result.Changed = true
		} else {
			result.Skipped = append(result.Skipped, partition)
		}
	}

	principal := strings.TrimSpace(opts.Principal)
	if principal == "" {
		principal = "guardian-repo-import"
	}
	mutationCtx := guardianapi.MutationContext{PrincipalID: principal, Reason: "repo import " + opts.RepoName}
	if len(writes) > 0 {
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: writes, Context: mutationCtx}); err != nil {
			return nil, fmt.Errorf("write partition bundle: %w", err)
		}
		result.FilesWritten = len(writes)
	}
	if len(deletes) > 0 {
		if _, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{Deletes: deletes, Context: mutationCtx}); err != nil {
			return nil, fmt.Errorf("prune repository files: %w", err)
		}
		result.FilesDeleted = len(deletes)
	}
	if len(manifests) > 0 {
		if _, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: manifests, Context: mutationCtx}); err != nil {
			return nil, fmt.Errorf("write ownership manifest: %w", err)
		}
	}
	sort.Strings(result.Imported)
	sort.Strings(result.Skipped)
	return result, nil
}

func loadOwnershipManifest(ctx context.Context, store guardianapi.ReadStore, partition string) (*ownershipManifest, []byte, error) {
	raw, err := store.ReadFile(ctx, paths.RepoImportManifest(partition))
	if err != nil {
		return nil, nil, err
	}
	var manifest ownershipManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, raw, err
	}
	return &manifest, raw, nil
}

func buildOwnershipManifest(source, repo string, desired map[string]bool) ([]byte, error) {
	files := make([]string, 0, len(desired))
	for file := range desired {
		files = append(files, file)
	}
	sort.Strings(files)
	return json.MarshalIndent(ownershipManifest{
		APIVersion: "guardian/v1alpha1",
		Kind:       "RepoImportOwnership",
		Source:     source,
		Repo:       repo,
		Files:      files,
	}, "", "  ")
}

func partitionBundlePath(partition, override string) string {
	override = strings.TrimSpace(override)
	if override == "" {
		return PartitionsSubdir + "/" + partition
	}
	return path.Clean(strings.TrimPrefix(override, "/"))
}

func validateRepoPath(root string) error {
	root = strings.TrimSpace(root)
	if root == "" || root == "." || root == ".." || strings.HasPrefix(root, "../") || strings.Contains(root, "/../") || strings.HasPrefix(root, "/") {
		return fmt.Errorf("invalid repository path %q", root)
	}
	return nil
}

func listPartitions(ctx context.Context, source Source, filter, partitionPath string) ([]string, error) {
	if strings.TrimSpace(filter) != "" {
		if err := paths.ValidatePartitionName(filter); err != nil {
			return nil, err
		}
		root := partitionBundlePath(filter, partitionPath)
		if err := validateRepoPath(root); err != nil {
			return nil, err
		}
		entries, err := source.ListDir(ctx, root)
		if err != nil {
			if isNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		if !hasPartitionManifest(ctx, source, filter, entries) {
			return nil, nil
		}
		return []string{filter}, nil
	}
	entries, err := source.ListDir(ctx, PartitionsSubdir)
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	partitions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if err := paths.ValidatePartitionName(entry.Name); err != nil {
			return nil, fmt.Errorf("invalid partition directory: %w", err)
		}
		partitions = append(partitions, entry.Name)
	}
	sort.Strings(partitions)
	return partitions, nil
}

func hasPartitionManifest(ctx context.Context, source Source, partition string, entries []Entry) bool {
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		if entry.Name == "partition.yaml" || entry.Name == "config.yaml" {
			return true
		}
	}
	_ = partition
	return false
}

// collectPartition reads one partition bundle and returns the canonical
// store writes plus non-fatal warnings.
func collectPartition(ctx context.Context, source Source, partition, root string) ([]guardianapi.PathWrite, []string, error) {
	if err := validateRepoPath(root); err != nil {
		return nil, nil, err
	}
	entries, err := source.ListDir(ctx, root)
	if err != nil {
		return nil, nil, err
	}

	writes := make([]guardianapi.PathWrite, 0, 8)
	warnings := make([]string, 0)

	for _, entry := range entries {
		if entry.IsDir {
			switch entry.Name {
			case "intents":
				intentWrites, intentWarnings, err := collectIntents(ctx, source, root, partition)
				if err != nil {
					return nil, nil, err
				}
				writes = append(writes, intentWrites...)
				warnings = append(warnings, intentWarnings...)
			case "payloads":
				payloadWrites, err := collectTree(ctx, source, root, partition, "payloads", "payloads")
				if err != nil {
					return nil, nil, err
				}
				writes = append(writes, payloadWrites...)
			case "secrets":
				// Secret material never round-trips from a repository.
				warnings = append(warnings, fmt.Sprintf("partition %s: secrets/** is not imported from repositories", partition))
			default:
				warnings = append(warnings, fmt.Sprintf("partition %s: ignoring unsupported directory %s", partition, entry.Name))
			}
			continue
		}
		switch entry.Name {
		case "partition.yaml", "config.yaml":
			raw, err := source.ReadFile(ctx, root+"/"+entry.Name)
			if err != nil {
				return nil, nil, err
			}
			parsed, err := manifest.ParsePartition(raw)
			if err != nil {
				return nil, nil, fmt.Errorf("parse %s: %w", entry.Name, err)
			}
			if parsed.Metadata.Name != partition {
				return nil, nil, fmt.Errorf("metadata.name %q does not match directory %q", parsed.Metadata.Name, partition)
			}
			writes = append(writes, guardianapi.PathWrite{
				LogicalPath: paths.PartitionConfig(partition),
				Content:     raw,
			})
		default:
			warnings = append(warnings, fmt.Sprintf("partition %s: ignoring unsupported file %s", partition, entry.Name))
		}
	}
	return writes, warnings, nil
}

func collectIntents(ctx context.Context, source Source, partitionRoot, partition string) ([]guardianapi.PathWrite, []string, error) {
	root := partitionRoot + "/intents"
	entries, err := source.ListDir(ctx, root)
	if err != nil {
		if isNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	writes := make([]guardianapi.PathWrite, 0, len(entries))
	warnings := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		if !strings.HasSuffix(entry.Name, ".yaml") && !strings.HasSuffix(entry.Name, ".yml") {
			warnings = append(warnings, fmt.Sprintf("partition %s: ignoring non-manifest intent file %s", partition, entry.Name))
			continue
		}
		raw, err := source.ReadFile(ctx, root+"/"+entry.Name)
		if err != nil {
			return nil, nil, err
		}
		parsed, err := manifest.ParseIntent(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("parse intent %s: %w", entry.Name, err)
		}
		if err := paths.ValidateIntentName(parsed.Metadata.Name); err != nil {
			return nil, nil, fmt.Errorf("intent %s: %w", entry.Name, err)
		}
		if err := rejectInlineSecretValues(parsed); err != nil {
			return nil, nil, fmt.Errorf("intent %s: %w", entry.Name, err)
		}
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath: paths.PartitionIntentsDir(partition) + "/" + entry.Name,
			Content:     raw,
		})
	}
	return writes, warnings, nil
}

// collectTree walks dir recursively and maps files under the partition payload
// namespace.
func collectTree(ctx context.Context, source Source, partitionRoot, partition, dir, logicalSubdir string) ([]guardianapi.PathWrite, error) {
	root := partitionRoot + "/" + dir
	entries, err := source.ListDir(ctx, root)
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	writes := make([]guardianapi.PathWrite, 0, len(entries))
	for _, entry := range entries {
		rel := logicalSubdir + "/" + entry.Name
		if entry.IsDir {
			nested, err := collectTree(ctx, source, partitionRoot, partition, dir+"/"+entry.Name, rel)
			if err != nil {
				return nil, err
			}
			writes = append(writes, nested...)
			continue
		}
		raw, err := source.ReadFile(ctx, root+"/"+entry.Name)
		if err != nil {
			return nil, err
		}
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath: path.Join(paths.PartitionRoot(partition), rel),
			Content:     raw,
		})
	}
	return writes, nil
}

func rejectInlineSecretValues(intent *intentdomain.Intent) error {
	for _, asset := range intent.Spec.Assets {
		if asset.Type != assetdomain.TypeSecret {
			continue
		}
		if value, ok := asset.Properties["value"].(string); ok && strings.TrimSpace(value) != "" {
			return fmt.Errorf("inline Secret value is not allowed in repo-sourced partitions; use secretRef")
		}
	}
	return nil
}

func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") || strings.Contains(message, "no such file")
}
