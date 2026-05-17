package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	"github.com/rydzu/ainfra/guardian/internal/paths"
)

type intentManifestSource struct {
	Name        string
	LogicalPath string
	VersionID   string
	ModTime     time.Time
	Content     []byte
	Manifest    *intentdomain.Intent
}

func (s *Server) intentManifestSources(ctx context.Context, partitionName string) ([]intentManifestSource, error) {
	entries, err := s.store.ListDir(ctx, paths.PartitionIntentsDir(partitionName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	pathsToLoad := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !isIntentManifestFileName(entry.Name) {
			continue
		}
		pathsToLoad = append(pathsToLoad, paths.PartitionIntentsDir(partitionName)+"/"+entry.Name)
	}
	sort.Strings(pathsToLoad)

	byName := make(map[string]intentManifestSource, len(pathsToLoad))
	for _, logicalPath := range pathsToLoad {
		content, err := s.store.ReadFile(ctx, logicalPath)
		if err != nil {
			return nil, err
		}
		manifest, err := parseIntent(content)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(manifest.Metadata.Name)
		if name == "" {
			return nil, fmt.Errorf("intent manifest %s is missing metadata.name", logicalPath)
		}
		info, err := s.store.Stat(ctx, logicalPath)
		if err != nil {
			return nil, err
		}
		current := intentManifestSource{
			Name:        name,
			LogicalPath: logicalPath,
			VersionID:   info.VersionID,
			ModTime:     info.ModTime,
			Content:     content,
			Manifest:    manifest,
		}
		existing, ok := byName[name]
		if !ok {
			byName[name] = current
			continue
		}
		switch {
		case preferIntentManifestSource(current, existing):
			byName[name] = current
		case preferIntentManifestSource(existing, current):
		default:
			return nil, fmt.Errorf("duplicate intent %q found at %s and %s", name, existing.LogicalPath, current.LogicalPath)
		}
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	sources := make([]intentManifestSource, 0, len(names))
	for _, name := range names {
		sources = append(sources, byName[name])
	}
	return sources, nil
}

func isIntentManifestFileName(name string) bool {
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}

func preferIntentManifestSource(candidate, current intentManifestSource) bool {
	candidateYAML := strings.HasSuffix(candidate.LogicalPath, ".yaml")
	currentYAML := strings.HasSuffix(current.LogicalPath, ".yaml")
	if candidateYAML != currentYAML {
		return candidateYAML
	}
	return false
}
