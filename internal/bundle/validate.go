package bundle

import (
	"context"
	"fmt"
	"strings"

	"github.com/radryc/guardian/internal/compiler/planner"
	"github.com/radryc/guardian/internal/paths"
)

// Validate compiles the bundle and checks that every provider-relative payload
// path referenced by an asset exists in the loaded files. It is the last step
// of Load and is safe to call again after in-memory edits.
func (b *Bundle) Validate() error {
	if b == nil {
		return fmt.Errorf("bundle is nil")
	}

	intentVersions := make(map[string]string, len(b.IntentContents))
	for name := range b.IntentContents {
		intentVersions[name] = "local-" + name
	}
	if _, err := planner.Compile(context.Background(), planner.CompileInput{
		PartitionName:    b.PartitionName,
		ConfigContent:    b.ConfigContent,
		IntentContents:   b.IntentContents,
		IntentVersionIDs: intentVersions,
		IntentModTimes:   b.IntentModTimes,
		ConfigVersionID:  "local-config",
		CurrentOutputs:   map[string]map[string]string{},
	}); err != nil {
		return err
	}

	partitionRoot := paths.PartitionRoot(b.PartitionName) + "/"
	for intentName, intentManifest := range b.IntentManifests {
		for _, asset := range intentManifest.Spec.Assets {
			for provider, logicalPath := range asset.Payload {
				if !strings.HasPrefix(logicalPath, partitionRoot) {
					continue
				}
				if _, ok := b.ManagedFiles[logicalPath]; !ok {
					return fmt.Errorf("intent %s asset %s payload %s references missing file %s", intentName, asset.Name, provider, logicalPath)
				}
			}
		}
	}
	return nil
}
