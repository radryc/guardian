// Package workspace locates Guardian partition directories in a repository.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PartitionRef is one discovered partition directory.
type PartitionRef struct {
	Name string
	Dir  string
}

// Discover finds partition directories under root.
//
// Resolution order:
//  1. root itself when it contains config.yaml or partition.yaml;
//  2. root/partitions/*;
//  3. immediate children of root that contain config.yaml or partition.yaml.
//
// Directories whose name starts with "_" are reserved for the bootstrap
// exception and are skipped.
func Discover(root string) ([]PartitionRef, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("read root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root is not a directory: %s", root)
	}

	if hasPartitionConfig(root) {
		return []PartitionRef{{Name: filepath.Base(root), Dir: root}}, nil
	}

	candidates := root
	partitionsDir := filepath.Join(root, "partitions")
	if info, err := os.Stat(partitionsDir); err == nil && info.IsDir() {
		candidates = partitionsDir
	}

	entries, err := os.ReadDir(candidates)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", candidates, err)
	}

	refs := make([]PartitionRef, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "_") {
			continue
		}
		dir := filepath.Join(candidates, name)
		if !hasPartitionConfig(dir) {
			continue
		}
		refs = append(refs, PartitionRef{Name: name, Dir: dir})
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no partition directories found under %s", candidates)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

func hasPartitionConfig(dir string) bool {
	for _, name := range []string{"config.yaml", "partition.yaml"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
