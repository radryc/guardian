// Package bundle loads a Guardian partition directory (config, intents,
// payloads, alerts) into one canonical in-memory bundle.
//
// It is the single loader shared by guardianctl and Forge. Loading is
// read-only: it normalizes manifests, resolves provider-relative paths,
// optionally resolves cross-intent image references, and runs structural
// validation. Writing to a store is deliberately not part of this package.
package bundle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	"github.com/radryc/guardian/internal/paths"
)

// Bundle is a loaded partition directory.
type Bundle struct {
	Dir             string
	PartitionName   string
	ConfigContent   []byte
	IntentContents  map[string][]byte
	IntentModTimes  map[string]time.Time
	ManagedFiles    map[string][]byte
	IntentManifests map[string]*intentdomain.Intent
}

// IntentFile is one intent manifest read from disk.
type IntentFile struct {
	Path    string
	ModTime time.Time
	Content []byte
}

// Options controls loading.
type Options struct {
	// IncludeSecrets includes files under secrets/ in ManagedFiles.
	IncludeSecrets bool

	// ResolveIntentRefs, when set, rewrites intent contents in memory before
	// parsing. guardianctl uses it to resolve
	// ${intent.NAME.outputs.ASSET.FIELD} placeholders from the local image
	// state so the pushed bundle is self-contained. When nil, contents are
	// used verbatim.
	ResolveIntentRefs func(dir string, files []IntentFile) error
}

// Load reads a partition directory. config.yaml is preferred over
// partition.yaml; both are normalized and validated.
func Load(dir string, opts Options) (*Bundle, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	configPath := filepath.Join(dir, "config.yaml")
	configRaw, err := os.ReadFile(configPath)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		configPath = filepath.Join(dir, "partition.yaml")
		configRaw, err = os.ReadFile(configPath)
	}
	if err != nil {
		return nil, fmt.Errorf("read partition config: %w", err)
	}
	partitionManifest, configContent, err := NormalizePartitionManifest(configRaw, configPath)
	if err != nil {
		return nil, err
	}

	b := &Bundle{
		Dir:             dir,
		PartitionName:   partitionManifest.Metadata.Name,
		ConfigContent:   configContent,
		IntentContents:  map[string][]byte{},
		IntentModTimes:  map[string]time.Time{},
		ManagedFiles:    map[string][]byte{},
		IntentManifests: map[string]*intentdomain.Intent{},
	}
	b.ManagedFiles[paths.PartitionConfig(b.PartitionName)] = configContent

	intentFiles, err := LoadIntentFiles(dir)
	if err != nil {
		return nil, err
	}
	if opts.ResolveIntentRefs != nil {
		if err := opts.ResolveIntentRefs(dir, intentFiles); err != nil {
			return nil, fmt.Errorf("resolve intent image refs: %w", err)
		}
	}

	knownIntents := make([]string, 0, len(intentFiles))
	for _, intentFile := range intentFiles {
		intentManifest, _, err := NormalizeIntentManifest(intentFile.Content, intentFile.Path, nil)
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
		intentManifest, intentContent, err := NormalizeIntentManifest(intentFile.Content, intentFile.Path, knownIntents)
		if err != nil {
			return nil, fmt.Errorf("intent file %s: %w", intentFile.Path, err)
		}
		intentName := intentManifest.Metadata.Name
		intentPath := paths.IntentManifest(b.PartitionName, intentName)
		b.IntentContents[intentName] = intentContent
		b.IntentModTimes[intentName] = intentFile.ModTime
		b.ManagedFiles[intentPath] = intentContent
		b.IntentManifests[intentName] = intentManifest
	}

	extraFiles, err := LoadExtraFiles(dir, b.PartitionName, opts.IncludeSecrets)
	if err != nil {
		return nil, err
	}
	for logicalPath, content := range extraFiles {
		b.ManagedFiles[logicalPath] = content
	}

	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

// LoadIntentFiles reads *.yaml files from <dir>/intents in lexical order.
func LoadIntentFiles(dir string) ([]IntentFile, error) {
	intentsDir := filepath.Join(dir, "intents")
	entries, err := os.ReadDir(intentsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read intents directory: %w", err)
	}
	files := make([]IntentFile, 0)
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
		files = append(files, IntentFile{Path: intentPath, ModTime: info.ModTime(), Content: content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// LoadExtraFiles walks the partition directory and returns every managed file
// keyed by its logical Guardian path. config.yaml, partition.yaml, intents/,
// and .state/ are excluded; secrets/ is included only when includeSecrets is
// true.
func LoadExtraFiles(dir, partitionName string, includeSecrets bool) (map[string][]byte, error) {
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
		if relative == "config.yaml" || relative == "partition.yaml" {
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

// SortedContentKeys returns map keys in lexical order, which keeps generated
// writes and diffs deterministic.
func SortedContentKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ManagedPartitionFile reports whether a logical path belongs to the
// partition's managed bundle (config, intents, payloads, alerts) and may be
// removed when it disappears locally. .state/ and secrets/ are never managed.
func ManagedPartitionFile(partitionName, logicalPath string) bool {
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

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
