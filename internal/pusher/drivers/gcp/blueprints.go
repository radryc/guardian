package gcpdriver

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed blueprints
var blueprintFS embed.FS

// BlueprintFiles returns the files of an embedded blueprint keyed by their
// path relative to the blueprint root.
func BlueprintFiles(name string) (map[string]string, error) {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid blueprint name %q", name)
	}
	root := path.Join("blueprints", name)
	files := map[string]string{}
	err := fs.WalkDir(blueprintFS, root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := blueprintFS.ReadFile(filePath)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(filePath, root), "/")
		files[rel] = string(content)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("blueprint %q: %w", name, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("blueprint %q not found", name)
	}
	return files, nil
}

// BlueprintNames lists the embedded blueprints (used by tests).
func BlueprintNames() ([]string, error) {
	entries, err := blueprintFS.ReadDir("blueprints")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
