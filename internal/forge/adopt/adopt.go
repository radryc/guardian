// Package adopt imports a live partition bundle into a repository working tree.
package adopt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// Options controls adoption.
type Options struct {
	Root      string
	Partition string
	OutDir    string
	Force     bool
	Store     guardianapi.ReadStore
}

// Result lists the written files.
type Result struct {
	Partition string   `json:"partition"`
	OutDir    string   `json:"outDir"`
	Files     []string `json:"files"`
}

// Run copies a live partition bundle to disk. Read-only against MonoFS.
func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Store == nil {
		return Result{}, fmt.Errorf("a MonoFS store is required")
	}
	if strings.TrimSpace(opts.Partition) == "" {
		return Result{}, fmt.Errorf("--partition is required")
	}
	outDir := opts.OutDir
	if strings.TrimSpace(outDir) == "" {
		outDir = filepath.Join(opts.Root, "partitions", opts.Partition)
	}
	result := Result{Partition: opts.Partition, OutDir: outDir}

	root := paths.PartitionRoot(opts.Partition)
	files, err := walk(ctx, opts.Store, root)
	if err != nil {
		return result, err
	}
	if len(files) == 0 {
		return result, fmt.Errorf("partition %q has no files in MonoFS", opts.Partition)
	}

	// Check targets before writing anything.
	for _, logical := range files {
		relative, ok := relativePath(root, logical)
		if !ok {
			continue
		}
		target := filepath.Join(outDir, filepath.FromSlash(relative))
		if _, err := os.Stat(target); err == nil && !opts.Force {
			return result, fmt.Errorf("%s already exists (use --force)", target)
		}
	}

	for _, logical := range files {
		relative, ok := relativePath(root, logical)
		if !ok || skip(relative) {
			continue
		}
		content, err := opts.Store.ReadFile(ctx, logical)
		if err != nil {
			return result, fmt.Errorf("read %s: %w", logical, err)
		}
		target := filepath.Join(outDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return result, err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return result, err
		}
		result.Files = append(result.Files, target)
	}
	return result, nil
}

func walk(ctx context.Context, store guardianapi.ReadStore, dir string) ([]string, error) {
	entries, err := store.ListDir(ctx, dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		path := dir + "/" + entry.Name
		if entry.IsDir {
			children, err := walk(ctx, store, path)
			if err != nil {
				return nil, err
			}
			out = append(out, children...)
			continue
		}
		out = append(out, path)
	}
	return out, nil
}

func relativePath(root, logical string) (string, bool) {
	prefix := root + "/"
	if !strings.HasPrefix(logical, prefix) {
		return "", false
	}
	relative := strings.TrimPrefix(logical, prefix)
	if relative == "" || strings.HasPrefix(relative, "../") {
		return "", false
	}
	return relative, true
}

func skip(relative string) bool {
	return strings.HasPrefix(relative, ".state/") ||
		strings.HasPrefix(relative, "secrets/") ||
		strings.HasPrefix(relative, ".archive/")
}
