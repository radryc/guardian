package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"github.com/rydzu/ainfra/guardian/internal/compiler/planner"
	"github.com/rydzu/ainfra/guardian/internal/compiler/validator"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

var yamlLinePattern = regexp.MustCompile(`line (\d+):`)

type localPartitionBundle struct {
	Dir             string
	PartitionName   string
	ConfigContent   []byte
	IntentContents  map[string][]byte
	IntentModTimes  map[string]time.Time
	ManagedFiles    map[string][]byte
	IntentManifests map[string]*intentdomain.Intent
}

type partitionPushResult struct {
	Success             bool                   `json:"success"`
	Partition           string                 `json:"partition"`
	CorrelationID       string                 `json:"correlationId"`
	RemoveMissing       bool                   `json:"removeMissing"`
	WriteBatchRevision  string                 `json:"writeBatchRevisionId,omitempty"`
	DeleteBatchRevision string                 `json:"deleteBatchRevisionId,omitempty"`
	Written             []string               `json:"written"`
	Skipped             []string               `json:"skipped"`
	Deleted             []string               `json:"deleted"`
	Rollouts            []partitionRolloutDiff `json:"rollouts,omitempty"`
}

func partitionPushCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition push", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dir := flags.String("dir", "", "local partition directory")
	includeSecrets := flags.Bool("include-secrets", false, "include files under secrets/ in the managed bundle")
	removeMissing := flags.Bool("remove-missing", true, "remove managed files not present locally")
	return &command.Command{Description: "Push a whole partition directory", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if *dir == "" {
			return fmt.Errorf("--dir is required")
		}
		bundle, err := loadLocalPartitionBundle(filepath.Clean(*dir), *includeSecrets)
		if err != nil {
			return err
		}
		result, err := pushPartitionBundle(ctx, store, bundle, *removeMissing)
		if err != nil {
			return err
		}
		printPartitionPushResult(printer, result)
		return nil
	}}
}

func loadLocalPartitionBundle(dir string, includeSecrets bool) (*localPartitionBundle, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	configPath := filepath.Join(dir, "config.yaml")
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read partition config: %w", err)
	}
	partitionManifest, configContent, err := normalizePartitionManifest(configRaw, configPath)
	if err != nil {
		return nil, err
	}

	bundle := &localPartitionBundle{
		Dir:             dir,
		PartitionName:   partitionManifest.Metadata.Name,
		ConfigContent:   configContent,
		IntentContents:  map[string][]byte{},
		IntentModTimes:  map[string]time.Time{},
		ManagedFiles:    map[string][]byte{},
		IntentManifests: map[string]*intentdomain.Intent{},
	}
	bundle.ManagedFiles[paths.PartitionConfig(bundle.PartitionName)] = configContent

	intentFiles, err := loadIntentFiles(dir)
	if err != nil {
		return nil, err
	}
	knownIntents := make([]string, 0, len(intentFiles))
	for _, intentFile := range intentFiles {
		intentManifest, _, err := normalizeIntentManifest(intentFile.Content, intentFile.Path, nil)
		if err != nil {
			return nil, fmt.Errorf("intent file %s: %w", intentFile.Path, err)
		}
		knownIntents = append(knownIntents, intentManifest.Metadata.Name)
	}
	sort.Strings(knownIntents)
	knownIntents = uniqueStrings(knownIntents)
	if len(knownIntents) != len(intentFiles) {
		return nil, fmt.Errorf("duplicate intent metadata.name in %s", filepath.Join(dir, "intents"))
	}

	for _, intentFile := range intentFiles {
		intentManifest, intentContent, err := normalizeIntentManifest(intentFile.Content, intentFile.Path, knownIntents)
		if err != nil {
			return nil, fmt.Errorf("intent file %s: %w", intentFile.Path, err)
		}
		intentName := intentManifest.Metadata.Name
		intentPath := paths.IntentManifest(bundle.PartitionName, intentName)
		bundle.IntentContents[intentName] = intentContent
		bundle.IntentModTimes[intentName] = intentFile.ModTime
		bundle.ManagedFiles[intentPath] = intentContent
		bundle.IntentManifests[intentName] = intentManifest
	}

	extraFiles, err := loadExtraFiles(dir, bundle.PartitionName, includeSecrets)
	if err != nil {
		return nil, err
	}
	for logicalPath, content := range extraFiles {
		bundle.ManagedFiles[logicalPath] = content
	}

	if err := validateLocalPartitionBundle(bundle); err != nil {
		return nil, err
	}
	return bundle, nil
}

type localIntentFile struct {
	Path    string
	ModTime time.Time
	Content []byte
}

func loadIntentFiles(dir string) ([]localIntentFile, error) {
	intentsDir := filepath.Join(dir, "intents")
	entries, err := os.ReadDir(intentsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read intents directory: %w", err)
	}
	files := make([]localIntentFile, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		intentPath := filepath.Join(intentsDir, entry.Name())
		content, err := os.ReadFile(intentPath)
		if err != nil {
			return nil, fmt.Errorf("read intent file %s: %w", intentPath, err)
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat intent file %s: %w", intentPath, err)
		}
		files = append(files, localIntentFile{Path: intentPath, ModTime: info.ModTime(), Content: content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func loadExtraFiles(dir, partitionName string, includeSecrets bool) (map[string][]byte, error) {
	files := map[string][]byte{}
	intentsDir := filepath.Join(dir, "intents")
	secretsDir := filepath.Join(dir, "secrets")
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		if d.IsDir() {
			switch path {
			case intentsDir:
				return fs.SkipDir
			case filepath.Join(dir, ".state"):
				return fs.SkipDir
			case secretsDir:
				if includeSecrets {
					return nil
				}
				return fs.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "config.yaml" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[paths.PartitionRoot(partitionName)+"/"+relative] = content
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk partition files: %w", err)
	}
	return files, nil
}

func normalizePartitionManifest(raw []byte, sourcePath string) (*partitiondomain.Partition, []byte, error) {
	var part partitiondomain.Partition
	if err := yaml.Unmarshal(raw, &part); err != nil {
		return nil, nil, annotateYAMLManifestError("partition", sourcePath, raw, err)
	}
	part.APIVersion = coalesceString(part.APIVersion, "guardian/v1alpha1")
	part.Kind = coalesceString(part.Kind, "Partition")
	normalized, err := yaml.Marshal(part)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := manifest.ParsePartition(normalized)
	if err != nil {
		return nil, nil, fmt.Errorf("partition manifest %s: %w", sourcePath, err)
	}
	if err := validator.ValidatePartition(parsed); err != nil {
		return nil, nil, fmt.Errorf("partition manifest %s: %w", sourcePath, err)
	}
	return parsed, normalized, nil
}

func normalizeIntentManifest(raw []byte, sourcePath string, knownIntents []string) (*intentdomain.Intent, []byte, error) {
	var intent intentdomain.Intent
	if err := yaml.Unmarshal(raw, &intent); err != nil {
		return nil, nil, annotateYAMLManifestError("intent", sourcePath, raw, err)
	}
	intent.APIVersion = coalesceString(intent.APIVersion, "guardian/v1alpha1")
	intent.Kind = coalesceString(intent.Kind, "Intent")
	normalized, err := yaml.Marshal(intent)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := manifest.ParseIntent(normalized)
	if err != nil {
		return nil, nil, fmt.Errorf("intent manifest %s: %w", sourcePath, err)
	}
	if knownIntents != nil {
		if err := validator.ValidateIntent(parsed, knownIntents, nil); err != nil {
			return nil, nil, fmt.Errorf("intent manifest %s: %w", sourcePath, err)
		}
	}
	normalized, err = yaml.Marshal(parsed)
	if err != nil {
		return nil, nil, err
	}
	return parsed, normalized, nil
}

func annotateYAMLManifestError(kind, sourcePath string, raw []byte, err error) error {
	message := fmt.Sprintf("parse %s %s: %v", kind, sourcePath, err)
	lineNumber := extractYAMLLineNumber(err)
	if lineNumber <= 0 {
		return errors.New(message)
	}
	lineText, ok := sourceLine(raw, lineNumber)
	if !ok {
		return errors.New(message)
	}
	return fmt.Errorf("%s\n  %d | %s", message, lineNumber, lineText)
}

func extractYAMLLineNumber(err error) int {
	match := yamlLinePattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return 0
	}
	lineNumber, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return 0
	}
	return lineNumber
}

func sourceLine(raw []byte, lineNumber int) (string, bool) {
	if lineNumber <= 0 {
		return "", false
	}
	lines := bytes.Split(raw, []byte("\n"))
	if lineNumber > len(lines) {
		return "", false
	}
	return strings.TrimRight(string(lines[lineNumber-1]), "\r"), true
}

func validateLocalPartitionBundle(bundle *localPartitionBundle) error {
	intentVersions := make(map[string]string, len(bundle.IntentContents))
	for name := range bundle.IntentContents {
		intentVersions[name] = "local-" + name
	}
	if _, err := planner.Compile(context.Background(), planner.CompileInput{
		PartitionName:    bundle.PartitionName,
		ConfigContent:    bundle.ConfigContent,
		IntentContents:   bundle.IntentContents,
		IntentVersionIDs: intentVersions,
		IntentModTimes:   bundle.IntentModTimes,
		ConfigVersionID:  "local-config",
		CurrentOutputs:   map[string]map[string]string{},
	}); err != nil {
		return err
	}

	partitionRoot := paths.PartitionRoot(bundle.PartitionName) + "/"
	for intentName, intentManifest := range bundle.IntentManifests {
		for _, asset := range intentManifest.Spec.Assets {
			for provider, logicalPath := range asset.Payload {
				if !strings.HasPrefix(logicalPath, partitionRoot) {
					continue
				}
				if _, ok := bundle.ManagedFiles[logicalPath]; !ok {
					return fmt.Errorf("intent %s asset %s payload %s references missing file %s", intentName, asset.Name, provider, logicalPath)
				}
			}
		}
	}
	return nil
}

func pushPartitionBundle(ctx context.Context, store guardianapi.Store, bundle *localPartitionBundle, removeMissing bool) (*partitionPushResult, error) {
	rollouts, err := planPartitionRolloutDiff(ctx, store, bundle, removeMissing)
	if err != nil {
		return nil, err
	}
	remoteFiles, err := walkFiles(ctx, store, paths.PartitionRoot(bundle.PartitionName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		remoteFiles = nil
	}

	remoteSet := make(map[string]struct{}, len(remoteFiles))
	for _, logicalPath := range remoteFiles {
		remoteSet[logicalPath] = struct{}{}
	}

	writePaths := make([]string, 0)
	skippedPaths := make([]string, 0)
	writes := make([]guardianapi.PathWrite, 0)
	for _, logicalPath := range sortedContentKeys(bundle.ManagedFiles) {
		content := bundle.ManagedFiles[logicalPath]
		if _, ok := remoteSet[logicalPath]; !ok {
			writes = append(writes, guardianapi.PathWrite{
				LogicalPath:       logicalPath,
				Content:           content,
				ExpectedVersionID: "absent",
			})
			writePaths = append(writePaths, logicalPath)
			continue
		}

		currentContent, err := store.ReadFile(ctx, logicalPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writes = append(writes, guardianapi.PathWrite{
					LogicalPath:       logicalPath,
					Content:           content,
					ExpectedVersionID: "absent",
				})
				writePaths = append(writePaths, logicalPath)
				continue
			}
			return nil, err
		}
		if bytes.Equal(currentContent, content) {
			skippedPaths = append(skippedPaths, logicalPath)
			continue
		}
		info, err := store.Stat(ctx, logicalPath)
		if err != nil {
			return nil, err
		}
		writes = append(writes, guardianapi.PathWrite{
			LogicalPath:       logicalPath,
			Content:           content,
			ExpectedVersionID: info.VersionID,
		})
		writePaths = append(writePaths, logicalPath)
	}

	deletes := make([]guardianapi.PathDelete, 0)
	deletedPaths := make([]string, 0)
	if removeMissing {
		for _, logicalPath := range remoteFiles {
			if !managedPartitionFile(bundle.PartitionName, logicalPath) {
				continue
			}
			if _, ok := bundle.ManagedFiles[logicalPath]; ok {
				continue
			}
			info, err := store.Stat(ctx, logicalPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, err
			}
			deletes = append(deletes, guardianapi.PathDelete{
				LogicalPath:       logicalPath,
				ExpectedVersionID: info.VersionID,
			})
			deletedPaths = append(deletedPaths, logicalPath)
		}
	}

	correlationID := revisions.NewCorrelationID()
	result := &partitionPushResult{
		Success:       true,
		Partition:     bundle.PartitionName,
		CorrelationID: correlationID,
		RemoveMissing: removeMissing,
		Written:       writePaths,
		Skipped:       skippedPaths,
		Deleted:       deletedPaths,
		Rollouts:      rollouts,
	}

	if len(writes) > 0 {
		batch, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: writes,
			Context: guardianapi.MutationContext{
				PrincipalID:   "guardianctl",
				Reason:        "push partition bundle",
				CorrelationID: correlationID,
			},
		})
		if err != nil {
			return nil, err
		}
		result.WriteBatchRevision = batch.BatchRevisionID
	}
	if len(deletes) > 0 {
		batch, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
			Deletes: deletes,
			Context: guardianapi.MutationContext{
				PrincipalID:   "guardianctl",
				Reason:        "push partition bundle",
				CorrelationID: correlationID,
			},
		})
		if err != nil {
			return nil, err
		}
		result.DeleteBatchRevision = batch.BatchRevisionID
	}
	return result, nil
}

func managedPartitionFile(partitionName, logicalPath string) bool {
	root := paths.PartitionRoot(partitionName)
	if logicalPath == paths.PartitionConfig(partitionName) {
		return true
	}
	prefix := root + "/"
	if !strings.HasPrefix(logicalPath, prefix) {
		return false
	}
	relative := strings.TrimPrefix(logicalPath, prefix)
	if relative == "" {
		return false
	}
	if strings.HasPrefix(relative, ".state/") || strings.HasPrefix(relative, "secrets/") {
		return false
	}
	return true
}

func sortedContentKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func printPartitionPushResult(printer *output.Printer, result *partitionPushResult) {
	if printer.Format == cliformat.FormatJSON {
		printer.PrintJSON(result)
		return
	}
	written := len(result.Written)
	skipped := len(result.Skipped)
	deleted := len(result.Deleted)

	fileWord := func(n int) string {
		if n == 1 {
			return "file"
		}
		return "files"
	}

	switch {
	case written == 0 && deleted == 0:
		printer.PrintText(
			"OK partition=%s no manifest changes (%d %s already up to date) correlation=%s\n",
			result.Partition, skipped, fileWord(skipped), result.CorrelationID,
		)
	case written > 0 && deleted == 0:
		printer.PrintText(
			"OK partition=%s wrote %d %s (%d unchanged) correlation=%s\n",
			result.Partition, written, fileWord(written), skipped, result.CorrelationID,
		)
	default:
		printer.PrintText(
			"OK partition=%s wrote %d %s, deleted %d, %d unchanged correlation=%s\n",
			result.Partition, written, fileWord(written), deleted, skipped, result.CorrelationID,
		)
	}

	// Note: rollout diff is computed against intent ASSET versions/contents,
	// not against the on-disk file count above. A change to partition metadata
	// (e.g. config.yaml endpoint label) will write a file but produce no asset
	// diff and therefore no reconcile work.
	printPartitionRolloutDiff(printer, result.Rollouts)
}

func coalesceString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
