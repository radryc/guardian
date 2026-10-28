package reconciler

import (
	"context"
	"log"
	"sort"
	"strings"

	"github.com/radryc/guardian/internal/cdkgen"
	"github.com/radryc/guardian/internal/compiler/planner"
	intentdomain "github.com/radryc/guardian/internal/domain/intent"
	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/internal/versioning/digest"
)

func cdkIntentFingerprint(partition, name string, compiledIntent *planner.CompiledIntent) string {
	if compiledIntent == nil || compiledIntent.Spec == nil {
		return ""
	}
	specHash := compiledIntent.IntentSpecHash
	if specHash == "" {
		specHash = digest.MustNormalizedHash(compiledIntent.Spec.Spec)
	}
	return partition + "|" + name + "|" + specHash + "|" + strings.Join(compiledIntent.AssetOrder, ",")
}

// syncGeneratedAWSPayloads compiles every spec.implementation=cdk intent into a
// TypeScript CDK app and stores it under the partition's generated payload
// path. The AWS pusher later stages that tree from the store and deploys it as
// a single CloudFormation stack. Generation errors are returned per intent so
// the caller can block just that intent.
//
// Templates come from the embedded baseline catalog plus the store overlay
// (partition > global > embedded); overlay problems are warnings and fall back
// to the next level.
func (r *Reconciler) syncGeneratedAWSPayloads(ctx context.Context, partition string, compiled *planner.CompiledPartition) map[string]error {
	errorsByIntent := map[string]error{}
	if compiled == nil {
		return errorsByIntent
	}
	provider := cdkgen.NewOverlayProvider(ctx, r.store, partition, nil)
	names := make([]string, 0, len(compiled.Intents))
	for name := range compiled.Intents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return errorsByIntent
		}
		compiledIntent := compiled.Intents[name]
		if compiledIntent == nil || compiledIntent.Spec == nil {
			continue
		}
		spec := compiledIntent.Spec.Spec
		if spec.Implementation != intentdomain.ImplementationCDK {
			continue
		}
		if !cdkgen.HasGeneratedAssets(spec.Assets) {
			continue
		}
		memoKey := partition + "/" + name
		fp := cdkIntentFingerprint(partition, name, compiledIntent)
		if fp != "" {
			r.cdkMemoMu.RLock()
			prevFP := r.cdkPayloadHashes[memoKey]
			r.cdkMemoMu.RUnlock()
			if prevFP == fp {
				continue
			}
		}
		result, err := cdkgen.Generate(cdkgen.Input{
			Partition: partition,
			Intent:    name,
			Account:   spec.Target.Account,
			Region:    spec.Target.Region,
			SourceDir: paths.GeneratedAWSPayloadDir(partition, name),
			Assets:    spec.Assets,
			Order:     compiledIntent.AssetOrder,
			Templates: provider,
		})
		if err != nil {
			errorsByIntent[name] = err
			log.Printf("reconciler: partition=%s intent=%s generate CDK stack failed: %v", partition, name, err)
			continue
		}
		for _, warning := range result.Warnings {
			log.Printf("reconciler: partition=%s intent=%s template overlay warning: %s", partition, name, warning)
		}
		if !result.Generated {
			continue
		}
		if err := r.dispatcher.SyncGeneratedPayload(ctx, paths.GeneratedAWSPayloadDir(partition, name), result.Files, "store generated CDK stack payload"); err != nil {
			errorsByIntent[name] = err
			log.Printf("reconciler: partition=%s intent=%s store generated CDK stack failed: %v", partition, name, err)
			continue
		}
		if fp != "" {
			r.cdkMemoMu.Lock()
			if r.cdkPayloadHashes == nil {
				r.cdkPayloadHashes = make(map[string]string)
			}
			r.cdkPayloadHashes[memoKey] = fp
			r.cdkMemoMu.Unlock()
		}
		log.Printf("reconciler: partition=%s intent=%s generated CDK stack %s (%d assets, %d files)",
			partition, name, result.Manifest.StackName, len(result.GeneratedAssets), len(result.Files))
	}
	return errorsByIntent
}
