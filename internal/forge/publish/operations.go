package publish

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/radryc/guardian/internal/workspacepub"
)

// OperationOptions controls how a partition's working tree is turned into
// repository operations.
type OperationOptions struct {
	// Changed diffs the working tree against BaseRef instead of submitting
	// every file. Untracked files are included as upserts.
	Changed bool
	// BaseRef is the git ref to diff against. Empty resolves origin/main,
	// main, origin/master, master, then HEAD.
	BaseRef string
	// PartitionDir is the absolute or root-relative partition directory.
	PartitionDir string
	// Root is the repository root (or a subdirectory inside it).
	Root           string
	IncludeSecrets bool
}

// GitRoot returns the git top-level directory containing dir.
func GitRoot(ctx context.Context, dir string) (string, error) {
	out, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CanonicalPath resolves a path to its absolute, long form. Windows can return
// an 8.3 short path from the OS while git reports the long path; resolving
// symlinks and getting the final path makes them comparable.
func CanonicalPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

// ResolveBaseRef picks the comparison ref for a changed-file submission.
func ResolveBaseRef(ctx context.Context, gitRoot, preferred string) string {
	if strings.TrimSpace(preferred) != "" {
		return strings.TrimSpace(preferred)
	}
	for _, candidate := range []string{"origin/main", "main", "origin/master", "master"} {
		if _, err := runGit(ctx, gitRoot, "rev-parse", "--verify", "--quiet", candidate); err == nil {
			return candidate
		}
	}
	return "HEAD"
}

// BuildOperations turns a partition directory into MonoFS publish operations.
// Paths are relative to the git root so they land in the repository tree.
func BuildOperations(ctx context.Context, opts OperationOptions) ([]workspacepub.Operation, error) {
	partitionDir := CanonicalPath(opts.PartitionDir)
	root := CanonicalPath(opts.Root)

	gitRoot, err := GitRoot(ctx, root)
	if err != nil {
		if opts.Changed {
			return nil, fmt.Errorf("changed mode requires a git repository: %w (use --all to submit the partition tree)", err)
		}
		gitRoot = root
	} else {
		gitRoot = CanonicalPath(gitRoot)
	}
	prefix, err := filepath.Rel(gitRoot, partitionDir)
	if err != nil {
		return nil, err
	}
	prefix = filepath.ToSlash(prefix)
	if prefix == "." || prefix == "" || strings.HasPrefix(prefix, "../") {
		return nil, fmt.Errorf("partition directory %s is not inside git root %s", partitionDir, gitRoot)
	}

	if !opts.Changed {
		return allOperations(gitRoot, partitionDir, prefix, opts.IncludeSecrets)
	}
	return changedOperations(ctx, gitRoot, prefix, opts.BaseRef, opts.IncludeSecrets)
}

func allOperations(gitRoot, partitionDir, prefix string, includeSecrets bool) ([]workspacepub.Operation, error) {
	ops := make([]workspacepub.Operation, 0)
	err := filepath.WalkDir(partitionDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == partitionDir {
			return nil
		}
		rel, err := filepath.Rel(gitRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !underPrefix(rel, prefix) {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(rel, prefix, includeSecrets) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(rel, prefix, includeSecrets) {
			return nil
		}
		content, mode, err := readRepoFile(gitRoot, rel)
		if err != nil {
			return err
		}
		ops = append(ops, workspacepub.Operation{
			Kind:    workspacepub.OperationUpsert,
			Path:    rel,
			Content: content,
			Mode:    mode,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortOperations(ops)
	return ops, nil
}

func changedOperations(ctx context.Context, gitRoot, prefix, baseRef string, includeSecrets bool) ([]workspacepub.Operation, error) {
	baseRef = ResolveBaseRef(ctx, gitRoot, baseRef)
	out, err := runGit(ctx, gitRoot, "diff", "--name-status", "-z", baseRef, "--", prefix)
	if err != nil {
		return nil, fmt.Errorf("git diff against %s: %w", baseRef, err)
	}

	ops := map[string]workspacepub.Operation{}
	addDelete := func(path string) {
		if shouldSkipFile(path, prefix, includeSecrets) || !underPrefix(path, prefix) {
			return
		}
		ops[path] = workspacepub.Operation{Kind: workspacepub.OperationDelete, Path: path}
	}
	addUpsert := func(path string) error {
		if shouldSkipFile(path, prefix, includeSecrets) || !underPrefix(path, prefix) {
			return nil
		}
		content, mode, err := readRepoFile(gitRoot, path)
		if err != nil {
			return err
		}
		ops[path] = workspacepub.Operation{
			Kind:    workspacepub.OperationUpsert,
			Path:    path,
			Content: content,
			Mode:    mode,
		}
		return nil
	}

	for _, entry := range parseNameStatus(out) {
		switch entry.status {
		case "D":
			addDelete(entry.path)
		case "R", "C":
			if entry.status == "R" {
				addDelete(entry.oldPath)
			}
			if err := addUpsert(entry.path); err != nil {
				return nil, err
			}
		default: // A, M, T, U, X
			if err := addUpsert(entry.path); err != nil {
				return nil, err
			}
		}
	}

	untracked, err := runGit(ctx, gitRoot, "ls-files", "-z", "--others", "--exclude-standard", "--", prefix)
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	for _, path := range splitNUL(untracked) {
		if path == "" {
			continue
		}
		if err := addUpsert(path); err != nil {
			return nil, err
		}
	}

	result := make([]workspacepub.Operation, 0, len(ops))
	for _, op := range ops {
		result = append(result, op)
	}
	sortOperations(result)
	return result, nil
}

type nameStatusEntry struct {
	status  string
	path    string
	oldPath string
}

// parseNameStatus parses `git diff --name-status -z` output.
func parseNameStatus(raw string) []nameStatusEntry {
	fields := splitNUL(raw)
	entries := make([]nameStatusEntry, 0, len(fields)/2)
	for i := 0; i < len(fields); {
		status := strings.TrimSpace(fields[i])
		if status == "" {
			break
		}
		kind := status[:1]
		if kind == "R" || kind == "C" {
			if i+2 >= len(fields) {
				break
			}
			entries = append(entries, nameStatusEntry{status: kind, oldPath: fields[i+1], path: fields[i+2]})
			i += 3
			continue
		}
		if i+1 >= len(fields) {
			break
		}
		entries = append(entries, nameStatusEntry{status: kind, path: fields[i+1]})
		i += 2
	}
	return entries
}

func splitNUL(raw string) []string {
	raw = strings.TrimSuffix(raw, "\x00")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\x00")
}

func readRepoFile(gitRoot, repoPath string) ([]byte, int64, error) {
	full := filepath.Join(gitRoot, filepath.FromSlash(repoPath))
	info, err := os.Stat(full)
	if err != nil {
		return nil, 0, err
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, 0, err
	}
	return content, int64(info.Mode().Perm()), nil
}

func underPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func shouldSkipDir(rel, prefix string, includeSecrets bool) bool {
	trimmed := strings.TrimPrefix(rel, prefix+"/")
	if trimmed == ".state" || strings.HasPrefix(trimmed, ".state/") {
		return true
	}
	if !includeSecrets && (trimmed == "secrets" || strings.HasPrefix(trimmed, "secrets/")) {
		return true
	}
	return false
}

func shouldSkipFile(rel, prefix string, includeSecrets bool) bool {
	trimmed := strings.TrimPrefix(rel, prefix+"/")
	if strings.HasPrefix(trimmed, ".state/") || trimmed == ".state" {
		return true
	}
	if !includeSecrets && (strings.HasPrefix(trimmed, "secrets/") || trimmed == "secrets") {
		return true
	}
	// Editor and OS noise never belongs in a partition bundle.
	base := filepath.Base(trimmed)
	if base == ".DS_Store" || strings.HasSuffix(base, ".swp") || strings.HasSuffix(base, "~") {
		return true
	}
	return false
}

func sortOperations(ops []workspacepub.Operation) {
	sort.Slice(ops, func(i, j int) bool { return ops[i].Path < ops[j].Path })
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
