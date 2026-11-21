package main

import (
	bundlepkg "github.com/radryc/guardian/internal/bundle"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
)

// Aliases keep the guardianctl call sites unchanged while the loader lives in
// internal/bundle and is shared with Forge.
type localPartitionBundle = bundlepkg.Bundle
type localIntentFile = bundlepkg.IntentFile

// loadLocalPartitionBundle loads a partition directory with guardianctl's
// image-reference resolution enabled. The resolver rewrites
// ${intent.NAME.outputs.ASSET.FIELD} placeholders in memory so the pushed
// bundle is self-contained while the templates on disk stay intact.
func loadLocalPartitionBundle(dir string, includeSecrets bool) (*localPartitionBundle, error) {
	return bundlepkg.Load(dir, bundlepkg.Options{
		IncludeSecrets:    includeSecrets,
		ResolveIntentRefs: resolveIntentImageRefs,
	})
}

func loadIntentFiles(dir string) ([]localIntentFile, error) {
	return bundlepkg.LoadIntentFiles(dir)
}

func normalizeIntentManifest(raw []byte, sourcePath string, knownIntents []string) (*intentdomain.Intent, []byte, error) {
	return bundlepkg.NormalizeIntentManifest(raw, sourcePath, knownIntents)
}
