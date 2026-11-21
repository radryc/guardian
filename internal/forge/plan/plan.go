// Package plan builds a read-only change plan for a partition, either from the
// working tree or against the live MonoFS bundle.
package plan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/radryc/guardian/internal/bundle"
	"github.com/radryc/guardian/internal/forge/publish"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/workspacepub"
	"github.com/radryc/guardian/pkg/guardianapi"
)

// Operation is one planned repository change.
type Operation struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Result is the plan.
type Result struct {
	Partition  string      `json:"partition"`
	Success    bool        `json:"success"`
	Against    string      `json:"against"`
	Intents    []string    `json:"intents"`
	Assets     int         `json:"assets"`
	Operations []Operation `json:"operations"`
	Created    int         `json:"created"`
	Updated    int         `json:"updated"`
	Deleted    int         `json:"deleted"`
}

// Options controls planning.
type Options struct {
	Root           string
	PartitionDir   string
	Partition      string
	Changed        bool
	BaseRef        string
	IncludeSecrets bool
	// Store, when set, plans against the live MonoFS bundle.
	Store guardianapi.ReadStore
}

// Build computes the plan.
func Build(ctx context.Context, opts Options) (Result, error) {
	result := Result{Partition: opts.Partition, Against: "disk", Success: true}
	b, err := bundle.Load(opts.PartitionDir, bundle.Options{
		IncludeSecrets:    opts.IncludeSecrets,
		ResolveIntentRefs: bundle.ResolveImageRefs,
	})
	if err != nil {
		return result, err
	}
	for name := range b.IntentManifests {
		result.Intents = append(result.Intents, name)
	}
	sort.Strings(result.Intents)
	for _, intent := range b.IntentManifests {
		result.Assets += len(intent.Spec.Assets)
	}

	if opts.Store != nil {
		result.Against = "live"
		ops, err := liveOperations(ctx, opts, b)
		if err != nil {
			return result, err
		}
		result.Operations = ops
	} else {
		ops, err := publish.BuildOperations(ctx, publish.OperationOptions{
			Changed:        opts.Changed,
			BaseRef:        opts.BaseRef,
			Root:           opts.Root,
			PartitionDir:   opts.PartitionDir,
			IncludeSecrets: opts.IncludeSecrets,
		})
		if err != nil {
			return result, err
		}
		for _, op := range ops {
			result.Operations = append(result.Operations, Operation{Kind: op.Kind, Path: op.Path})
		}
	}
	for _, op := range result.Operations {
		switch op.Kind {
		case "upsert":
			result.Updated++
		case "delete":
			result.Deleted++
		}
	}
	return result, nil
}

func liveOperations(ctx context.Context, opts Options, b *bundle.Bundle) ([]Operation, error) {
	gitRoot := opts.Root
	if root, err := publish.GitRoot(ctx, opts.Root); err == nil {
		gitRoot = root
	}
	prefix, err := relPrefix(gitRoot, opts.PartitionDir)
	if err != nil {
		return nil, err
	}

	remotePaths, err := walkStore(ctx, opts.Store, paths.PartitionRoot(b.PartitionName))
	if err != nil {
		return nil, err
	}
	remote := map[string]bool{}
	for _, path := range remotePaths {
		remote[path] = true
	}

	ops := make([]Operation, 0)
	for _, logicalPath := range bundle.SortedContentKeys(b.ManagedFiles) {
		local := b.ManagedFiles[logicalPath]
		repoPath, relErr := repoPathFor(prefix, b.PartitionName, logicalPath)
		if relErr != nil {
			continue
		}
		if !remote[logicalPath] {
			ops = append(ops, Operation{Kind: workspacepub.OperationUpsert, Path: repoPath})
			continue
		}
		current, readErr := opts.Store.ReadFile(ctx, logicalPath)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				ops = append(ops, Operation{Kind: workspacepub.OperationUpsert, Path: repoPath})
				continue
			}
			return nil, readErr
		}
		if !bytes.Equal(current, local) {
			ops = append(ops, Operation{Kind: workspacepub.OperationUpsert, Path: repoPath})
		}
	}
	for _, logicalPath := range remotePaths {
		if _, ok := b.ManagedFiles[logicalPath]; ok {
			continue
		}
		if !bundle.ManagedPartitionFile(b.PartitionName, logicalPath) {
			continue
		}
		repoPath, relErr := repoPathFor(prefix, b.PartitionName, logicalPath)
		if relErr != nil {
			continue
		}
		ops = append(ops, Operation{Kind: workspacepub.OperationDelete, Path: repoPath})
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].Path < ops[j].Path })
	return ops, nil
}

func relPrefix(gitRoot, partitionDir string) (string, error) {
	prefix, err := filepath.Rel(publish.CanonicalPath(gitRoot), publish.CanonicalPath(partitionDir))
	if err != nil {
		return "", err
	}
	if prefix == "." || strings.HasPrefix(prefix, "../") {
		return "", fmt.Errorf("partition directory is outside the repository")
	}
	return prefix, nil
}

func repoPathFor(prefix, partitionName, logicalPath string) (string, error) {
	root := paths.PartitionRoot(partitionName) + "/"
	if !strings.HasPrefix(logicalPath, root) {
		return "", fmt.Errorf("%s is outside %s", logicalPath, root)
	}
	return prefix + "/" + strings.TrimPrefix(logicalPath, root), nil
}

func walkStore(ctx context.Context, store guardianapi.ReadStore, dir string) ([]string, error) {
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
			children, err := walkStore(ctx, store, path)
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
