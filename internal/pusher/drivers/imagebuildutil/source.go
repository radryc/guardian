package imagebuildutil

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type SourceFileSnapshot struct {
	Path    string
	Content string
}

func StageSourceTree(ctx context.Context, store guardianapi.ReadStore, logicalDir string) (string, []SourceFileSnapshot, func(), error) {
	if store == nil {
		return "", nil, noopCleanup, fmt.Errorf("read store is required to stage image sources")
	}
	cleanDir := path.Clean(strings.TrimSpace(logicalDir))
	if cleanDir == "." || !strings.HasPrefix(cleanDir, "/") {
		return "", nil, noopCleanup, fmt.Errorf("sourceDir %q must be an absolute logical path", logicalDir)
	}
	workspaceDir, err := os.MkdirTemp("", "guardian-imagebuild-*")
	if err != nil {
		return "", nil, noopCleanup, fmt.Errorf("create temp workspace: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(workspaceDir)
	}
	files, err := walkStoreFiles(ctx, store, cleanDir)
	if err != nil {
		cleanup()
		return "", nil, noopCleanup, err
	}
	if len(files) == 0 {
		cleanup()
		return "", nil, noopCleanup, fmt.Errorf("sourceDir %q does not contain any files", logicalDir)
	}
	snapshots := make([]SourceFileSnapshot, 0, len(files))
	for _, logicalPath := range files {
		content, err := store.ReadFile(ctx, logicalPath)
		if err != nil {
			cleanup()
			return "", nil, noopCleanup, fmt.Errorf("read source file %s: %w", logicalPath, err)
		}
		relPath := strings.TrimPrefix(strings.TrimPrefix(logicalPath, cleanDir), "/")
		if relPath == "" || relPath == logicalPath {
			relPath = path.Base(logicalPath)
		}
		destPath := filepath.Join(workspaceDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			cleanup()
			return "", nil, noopCleanup, fmt.Errorf("create workspace dir for %s: %w", logicalPath, err)
		}
		if err := os.WriteFile(destPath, content, 0o644); err != nil {
			cleanup()
			return "", nil, noopCleanup, fmt.Errorf("write workspace file %s: %w", destPath, err)
		}
		snapshots = append(snapshots, SourceFileSnapshot{Path: path.Clean(relPath), Content: string(content)})
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Path < snapshots[j].Path })
	return workspaceDir, snapshots, cleanup, nil
}

func walkStoreFiles(ctx context.Context, store guardianapi.ReadStore, logicalDir string) ([]string, error) {
	entries, err := store.ListDir(ctx, logicalDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, entry := range entries {
		child := path.Join(logicalDir, entry.Name)
		if entry.IsDir {
			nested, err := walkStoreFiles(ctx, store, child)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		out = append(out, child)
	}
	sort.Strings(out)
	return out, nil
}

func noopCleanup() {}
