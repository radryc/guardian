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
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
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
	// Principal is the mutation principal id recorded in the store.
	Principal string
}

// Result summarizes an import run.
type Result struct {
	RepoName string   `json:"repo"`
	Imported []string `json:"imported"`
	Skipped  []string `json:"skipped"`
	Warnings []string `json:"warnings,omitempty"`
	// FilesWritten counts changed files pushed into the store.
	FilesWritten int `json:"filesWritten"`
	// Changed reports whether any file content changed.
	Changed bool `json:"changed"`
}

// Import reads guardian/partitions/** from source and writes changed files into
// the canonical partition namespace. Inline Secret values are rejected and
// secrets/** is never imported.
func Import(ctx context.Context, source Source, store guardianapi.Store, opts Options) (*Result, error) {
	if source == nil || store == nil {
		return nil, fmt.Errorf("source and store are required")
	}
	result := &Result{RepoName: opts.RepoName}

	partitions, err := listPartitions(ctx, source, opts.Partition)
	if err != nil {
		return nil, err
	}
	if len(partitions) == 0 {
		return nil, fmt.Errorf("no %s/<partition> found in the repository", PartitionsSubdir)
	}

	writes := make([]guardianapi.PathWrite, 0, 16)
	for _, partition := range partitions {
		partitionWrites, warnings, err := collectPartition(ctx, source, partition)
		if err != nil {
			return nil, fmt.Errorf("partition %s: %w", partition, err)
		}
		result.Warnings = append(result.Warnings, warnings...)

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
		if changed {
			result.Imported = append(result.Imported, partition)
			result.Changed = true
		} else {
			result.Skipped = append(result.Skipped, partition)
		}
	}

	if len(writes) > 0 {
		principal := strings.TrimSpace(opts.Principal)
		if principal == "" {
			principal = "guardian-repo-import"
		}
		batch := guardianapi.MutationBatch{
			Writes: writes,
			Context: guardianapi.MutationContext{
				PrincipalID: principal,
				Reason:      "repo import " + opts.RepoName,
			},
		}
		if _, err := store.UpsertFiles(ctx, batch); err != nil {
			return nil, fmt.Errorf("write partition bundle: %w", err)
		}
		result.FilesWritten = len(writes)
	}
	sort.Strings(result.Imported)
	sort.Strings(result.Skipped)
	return result, nil
}

func listPartitions(ctx context.Context, source Source, filter string) ([]string, error) {
	if strings.TrimSpace(filter) != "" {
		entries, err := source.ListDir(ctx, PartitionsSubdir+"/"+filter)
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
func collectPartition(ctx context.Context, source Source, partition string) ([]guardianapi.PathWrite, []string, error) {
	root := PartitionsSubdir + "/" + partition
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
				intentWrites, intentWarnings, err := collectIntents(ctx, source, partition)
				if err != nil {
					return nil, nil, err
				}
				writes = append(writes, intentWrites...)
				warnings = append(warnings, intentWarnings...)
			case "payloads":
				payloadWrites, err := collectTree(ctx, source, partition, "payloads", "payloads")
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

func collectIntents(ctx context.Context, source Source, partition string) ([]guardianapi.PathWrite, []string, error) {
	root := PartitionsSubdir + "/" + partition + "/intents"
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
func collectTree(ctx context.Context, source Source, partition, dir, logicalSubdir string) ([]guardianapi.PathWrite, error) {
	root := PartitionsSubdir + "/" + partition + "/" + dir
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
			nested, err := collectTree(ctx, source, partition, dir+"/"+entry.Name, rel)
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
