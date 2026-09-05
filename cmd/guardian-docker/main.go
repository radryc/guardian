// guardian-docker is a standalone CLI for inspecting, generating, and diffing
// Guardian-managed Docker resources.
//
// Usage:
//
//	guardian-docker [--docker <path>] [--format text|json] <command> [flags]
//
// Commands:
//
//	generate  Discover guardian-managed Docker resources and print as asset specs.
//	          Flags: [--partition <name>]
//
//	diff      Show structural diff between desired partition state (from store)
//	          and live Docker state.
//	          Flags: --partition <name>  --store-dir <dir> | --monofs-router <url>
//	                 [--monofs-token <tok>] [--monofs-principal-id <id>]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	dockerdriver "github.com/rydzu/ainfra/guardian/internal/pusher/drivers/docker"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	fsstore "github.com/rydzu/ainfra/guardian/internal/store/fs"
	monofsstore "github.com/rydzu/ainfra/guardian/internal/store/monofs"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Global flags.
	global := flag.NewFlagSet("guardian-docker", flag.ContinueOnError)
	dockerBin := global.String("docker", "docker", "path to the docker binary")
	format := global.String("format", "text", "output format: text|json")
	if err := global.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	args := global.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(2)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "generate":
		if err := runGenerate(ctx, *dockerBin, *format, rest); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "diff":
		if err := runDiff(ctx, *dockerBin, *format, rest); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `guardian-docker [--docker PATH] [--format text|json] <command>

Commands:
  generate [--partition NAME]
      Discover guardian-managed Docker resources and print as Guardian asset specs.

  diff --partition NAME (--store-dir DIR | --monofs-router URL)
              [--monofs-token TOKEN] [--monofs-principal-id ID]
      Show structural diff between desired partition state and live Docker state.`)
}

// ---------------------------------------------------------------------------
// generate subcommand
// ---------------------------------------------------------------------------

// assetGroup mirrors the intent YAML structure for display/generation.
type assetGroup struct {
	Partition string            `yaml:"partition" json:"partition"`
	Intent    string            `yaml:"intent"    json:"intent"`
	Assets    []discoveredAsset `yaml:"assets"    json:"assets"`
}

type discoveredAsset struct {
	Type       string         `yaml:"type"                 json:"type"`
	Name       string         `yaml:"name"                 json:"name"`
	Properties map[string]any `yaml:"properties,omitempty" json:"properties,omitempty"`
}

func runGenerate(ctx context.Context, dockerBin, format string, args []string) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	partitionFilter := fs.String("partition", "", "filter by partition name (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	backend, cleanup, err := newBackend(dockerBin)
	if err != nil {
		return err
	}
	defer cleanup()

	snapshot, err := backend.SnapshotGuardianResources()
	if err != nil {
		return fmt.Errorf("snapshot docker resources: %w", err)
	}

	groups := buildAssetGroups(snapshot, *partitionFilter)
	keys := sortedKeys(groups)

	if format == "json" {
		out := make([]assetGroup, 0, len(keys))
		for _, k := range keys {
			out = append(out, *groups[k])
		}
		return printJSON(out)
	}

	if len(keys) == 0 {
		fmt.Println("no guardian-managed Docker resources found")
		return nil
	}
	for _, k := range keys {
		data, err := yaml.Marshal(groups[k])
		if err != nil {
			return err
		}
		fmt.Printf("---\n%s", string(data))
	}
	return nil
}

func buildAssetGroups(snapshot *dockerdriver.GuardianSnapshot, partitionFilter string) map[string]*assetGroup {
	groups := map[string]*assetGroup{}
	ensure := func(partition, intent string) *assetGroup {
		k := partition + "/" + intent
		if g, ok := groups[k]; ok {
			return g
		}
		g := &assetGroup{Partition: partition, Intent: intent}
		groups[k] = g
		return g
	}

	for _, v := range snapshot.Volumes {
		p, i, a := v.Labels["guardian.partition"], v.Labels["guardian.intent"], v.Labels["guardian.asset"]
		if p == "" || i == "" || a == "" {
			continue
		}
		if partitionFilter != "" && p != partitionFilter {
			continue
		}
		ensure(p, i).Assets = append(groups[p+"/"+i].Assets, discoveredAsset{
			Type:       "Volume",
			Name:       a,
			Properties: dockerdriver.VolumeToProperties(v),
		})
	}

	for _, n := range snapshot.Networks {
		p, i, a := n.Labels["guardian.partition"], n.Labels["guardian.intent"], n.Labels["guardian.asset"]
		if p == "" || i == "" || a == "" {
			continue
		}
		if partitionFilter != "" && p != partitionFilter {
			continue
		}
		ensure(p, i).Assets = append(groups[p+"/"+i].Assets, discoveredAsset{
			Type:       "Network",
			Name:       a,
			Properties: dockerdriver.NetworkToProperties(n),
		})
	}

	for _, c := range snapshot.Containers {
		p, i, a := c.Labels["guardian.partition"], c.Labels["guardian.intent"], c.Labels["guardian.asset"]
		if p == "" || i == "" || a == "" {
			continue
		}
		if partitionFilter != "" && p != partitionFilter {
			continue
		}
		assetType := c.Labels["guardian.type"]
		if assetType == "" {
			assetType = "Compute"
		}
		ensure(p, i).Assets = append(groups[p+"/"+i].Assets, discoveredAsset{
			Type:       assetType,
			Name:       a,
			Properties: dockerdriver.ContainerToProperties(c),
		})
	}
	return groups
}

// ---------------------------------------------------------------------------
// diff subcommand
// ---------------------------------------------------------------------------

// assetDiffResult is the outcome of comparing one asset's desired vs actual state.
type assetDiffResult struct {
	AssetName string               `json:"assetName"`
	AssetType string               `json:"assetType"`
	Status    string               `json:"status"` // IN_SYNC | DRIFTED | MISSING
	Resources []resourceDiffResult `json:"resources,omitempty"`
}

// resourceDiffResult holds field-level diffs for a single Docker resource.
type resourceDiffResult struct {
	ResourceName string                   `json:"resourceName"`
	Status       string                   `json:"status"` // IN_SYNC | DRIFTED | MISSING
	Fields       []dockerdriver.DiffField `json:"fields,omitempty"`
}

// intentDiffResult aggregates diff results for one intent.
type intentDiffResult struct {
	Intent  string            `json:"intent"`
	Cluster string            `json:"cluster"`
	Assets  []assetDiffResult `json:"assets"`
}

// partitionDiffResult is the top-level output of the diff command.
type partitionDiffResult struct {
	Partition string             `json:"partition"`
	Intents   []intentDiffResult `json:"intents"`
}

func runDiff(ctx context.Context, dockerBin, format string, args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	partitionName := fs.String("partition", "", "partition name (required)")
	storeDir := fs.String("store-dir", "", "filesystem-backed Guardian store directory")
	monofsRouter := fs.String("monofs-router", "", "MonoFS router address")
	monofsToken := fs.String("monofs-token", "", "MonoFS token")
	monofsPrincipal := fs.String("monofs-principal-id", "guardian-docker", "MonoFS principal ID")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *partitionName == "" {
		return fmt.Errorf("--partition is required for diff")
	}
	if *storeDir == "" && *monofsRouter == "" {
		return fmt.Errorf("provide --store-dir or --monofs-router to read desired state")
	}
	if *storeDir != "" && *monofsRouter != "" {
		return fmt.Errorf("provide at most one of --store-dir or --monofs-router")
	}

	store, closer, err := openStore(ctx, *storeDir, *monofsRouter, *monofsToken, *monofsPrincipal)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if closer != nil {
		defer closer.Close() //nolint:errcheck
	}

	backend, cleanup, err := newBackend(dockerBin)
	if err != nil {
		return err
	}
	defer cleanup()

	// Snapshot live Docker state for the partition.
	snapshot, err := backend.SnapshotGuardianResources()
	if err != nil {
		return fmt.Errorf("snapshot docker resources: %w", err)
	}
	liveContainers := indexByName(snapshot.Containers)
	liveVolumes := indexVolumesByName(snapshot.Volumes)
	liveNetworks := indexNetworksByName(snapshot.Networks)

	// List intents in the partition from the store.
	intentNames, err := listIntentNames(ctx, store, *partitionName)
	if err != nil {
		return fmt.Errorf("list intents: %w", err)
	}

	result := partitionDiffResult{Partition: *partitionName}

	for _, intentName := range intentNames {
		data, err := store.ReadFile(ctx, paths.IntentManifest(*partitionName, intentName))
		if err != nil {
			return fmt.Errorf("read intent %q: %w", intentName, err)
		}
		intent, err := manifest.ParseIntent(data)
		if err != nil {
			return fmt.Errorf("parse intent %q: %w", intentName, err)
		}

		target := intent.Spec.Target
		cluster := target.Cluster
		if cluster == "" {
			cluster = "local"
		}

		intentResult := intentDiffResult{Intent: intentName, Cluster: cluster}

		for _, assetSpec := range intent.Spec.Assets {
			assetRes, err := diffAsset(*partitionName, intentName, target, assetSpec,
				liveContainers, liveVolumes, liveNetworks)
			if err != nil {
				return fmt.Errorf("diff asset %q in intent %q: %w", assetSpec.Name, intentName, err)
			}
			intentResult.Assets = append(intentResult.Assets, assetRes)
		}

		result.Intents = append(result.Intents, intentResult)
	}

	if format == "json" {
		return printJSON(result)
	}
	printDiffText(result)
	return nil
}

// diffAsset compares one AssetSpec against the live Docker snapshot.
func diffAsset(
	partition, intentName string,
	target targetdomain.Placement,
	assetSpec assetdomain.Spec,
	liveContainers map[string]dockerdriver.Container,
	liveVolumes map[string]dockerdriver.Volume,
	liveNetworks map[string]dockerdriver.Network,
) (assetDiffResult, error) {
	res := assetDiffResult{AssetName: assetSpec.Name, AssetType: assetSpec.Type}

	typed, _, err := assetdefs.Decode(assetSpec)
	if err != nil {
		// Unknown asset type — report as missing rather than erroring out.
		res.Status = "MISSING"
		res.Resources = []resourceDiffResult{{ResourceName: "?", Status: "MISSING"}}
		return res, nil //nolint:nilerr
	}

	switch spec := typed.(type) {

	case *assetdefs.NetworkSpec:
		desired := dockerdriver.DesiredNetworkForDiff(assetSpec.Name, target, spec)
		actual, ok := liveNetworks[desired.Name]
		if !ok {
			res.Status = "MISSING"
			res.Resources = []resourceDiffResult{{ResourceName: desired.Name, Status: "MISSING"}}
			return res, nil
		}
		fields := dockerdriver.DetailedNetworkDiff(desired, actual)
		rr := resourceDiffResult{ResourceName: desired.Name, Status: "IN_SYNC"}
		if len(fields) > 0 {
			rr.Status = "DRIFTED"
			rr.Fields = fields
		}
		res.Resources = []resourceDiffResult{rr}
		res.Status = rr.Status

	case *assetdefs.VolumeSpec:
		volName := driverutil.ResourceName("docker-vol", target, partition, intentName, assetSpec.Name)
		actual, ok := liveVolumes[volName]
		if !ok {
			res.Status = "MISSING"
			res.Resources = []resourceDiffResult{{ResourceName: volName, Status: "MISSING"}}
			return res, nil
		}
		fields := diffVolumeFields(spec, actual)
		rr := resourceDiffResult{ResourceName: volName, Status: "IN_SYNC"}
		if len(fields) > 0 {
			rr.Status = "DRIFTED"
			rr.Fields = fields
		}
		res.Resources = []resourceDiffResult{rr}
		res.Status = rr.Status

	case *assetdefs.ComputeSpec:
		replicas := 1
		if spec.Replicas != nil && *spec.Replicas > 0 {
			replicas = *spec.Replicas
		}
		overallDrifted := false
		for idx := 0; idx < replicas; idx++ {
			ctName := driverutil.ResourceName("docker-ct", target, partition, intentName, assetSpec.Name, strconv.Itoa(idx))
			actual, ok := liveContainers[ctName]
			if !ok {
				res.Resources = append(res.Resources, resourceDiffResult{ResourceName: ctName, Status: "MISSING"})
				overallDrifted = true
				continue
			}
			desired := dockerdriver.DesiredContainerForDiff(partition, intentName, assetSpec.Name, target, spec, idx)
			fields := dockerdriver.DetailedContainerDiff(desired, actual)
			rr := resourceDiffResult{ResourceName: ctName, Status: "IN_SYNC"}
			if len(fields) > 0 {
				rr.Status = "DRIFTED"
				rr.Fields = fields
				overallDrifted = true
			}
			res.Resources = append(res.Resources, rr)
		}
		if overallDrifted {
			res.Status = "DRIFTED"
		} else {
			res.Status = "IN_SYNC"
		}

	default:
		// Asset type not handled by the Docker driver — skip silently.
		res.Status = "SKIP"
	}

	return res, nil
}

// diffVolumeFields compares a VolumeSpec against an inspected Volume.
func diffVolumeFields(spec *assetdefs.VolumeSpec, actual dockerdriver.Volume) []dockerdriver.DiffField {
	var diffs []dockerdriver.DiffField
	if spec.Size != "" && spec.Size != actual.Size {
		diffs = append(diffs, dockerdriver.DiffField{Field: "size", Desired: spec.Size, Actual: actual.Size})
	}
	if spec.AccessMode != "" && spec.AccessMode != actual.AccessMode {
		diffs = append(diffs, dockerdriver.DiffField{Field: "accessMode", Desired: spec.AccessMode, Actual: actual.AccessMode})
	}
	desiredEphemeral := spec.Ephemeral != nil && *spec.Ephemeral
	if desiredEphemeral != actual.Ephemeral {
		diffs = append(diffs, dockerdriver.DiffField{
			Field:   "ephemeral",
			Desired: strconv.FormatBool(desiredEphemeral),
			Actual:  strconv.FormatBool(actual.Ephemeral),
		})
	}
	return diffs
}

// ---------------------------------------------------------------------------
// Text output for diff
// ---------------------------------------------------------------------------

func printDiffText(result partitionDiffResult) {
	fmt.Printf("=== Partition: %s ===\n", result.Partition)
	for _, intent := range result.Intents {
		fmt.Printf("\n--- Intent: %s [cluster: %s] ---\n", intent.Intent, intent.Cluster)
		for _, asset := range intent.Assets {
			switch asset.Status {
			case "IN_SYNC":
				fmt.Printf("  [✓ IN_SYNC]  %s (%s)\n", asset.AssetName, asset.AssetType)
			case "DRIFTED":
				fmt.Printf("  [✗ DRIFTED]  %s (%s)\n", asset.AssetName, asset.AssetType)
			case "MISSING":
				fmt.Printf("  [! MISSING]  %s (%s)\n", asset.AssetName, asset.AssetType)
			default:
				fmt.Printf("  [- SKIP]     %s (%s)\n", asset.AssetName, asset.AssetType)
				continue
			}
			for _, rr := range asset.Resources {
				fmt.Printf("    %s  → %s\n", rr.Status, rr.ResourceName)
				for _, f := range rr.Fields {
					fmt.Printf("      %-20s  want=%-30s  got=%s\n", f.Field, f.Desired, f.Actual)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newBackend(dockerBin string) (*dockerdriver.CLIBackend, func(), error) {
	stateDir, err := os.MkdirTemp("", "guardian-docker-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(stateDir) } //nolint:errcheck
	backend, err := dockerdriver.NewCLIBackend(dockerBin, stateDir)
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("init docker backend: %w", err)
	}
	return backend, cleanup, nil
}

func openStore(ctx context.Context, storeDir, monofsRouter, monofsToken, monofsPrincipal string) (guardianapi.Store, interface{ Close() error }, error) {
	if monofsRouter != "" {
		store, client, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
			RouterAddr:     monofsRouter,
			Token:          monofsToken,
			PrincipalID:    monofsPrincipal,
			Role:           "cli",
			ClientIDPrefix: "guardian-docker",
			Version:        "guardian-docker",
			Writable:       false,
		})
		if err != nil {
			return nil, nil, err
		}
		return store, client, nil
	}
	store, err := fsstore.Open(storeDir)
	if err != nil {
		return nil, nil, err
	}
	return store, nil, nil
}

// listIntentNames returns the names of all intents in the partition (derived
// from YAML file names in the intents directory).
func listIntentNames(ctx context.Context, store guardianapi.Store, partition string) ([]string, error) {
	entries, err := store.ListDir(ctx, paths.PartitionIntentsDir(partition))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		base := path.Base(e.Name)
		if !strings.HasSuffix(base, ".yaml") {
			continue
		}
		names = append(names, strings.TrimSuffix(base, ".yaml"))
	}
	sort.Strings(names)
	return names, nil
}

func indexByName(containers []dockerdriver.Container) map[string]dockerdriver.Container {
	m := make(map[string]dockerdriver.Container, len(containers))
	for _, c := range containers {
		m[c.Name] = c
	}
	return m
}

func indexVolumesByName(volumes []dockerdriver.Volume) map[string]dockerdriver.Volume {
	m := make(map[string]dockerdriver.Volume, len(volumes))
	for _, v := range volumes {
		m[v.Name] = v
	}
	return m
}

func indexNetworksByName(networks []dockerdriver.Network) map[string]dockerdriver.Network {
	m := make(map[string]dockerdriver.Network, len(networks))
	for _, n := range networks {
		m[n.Name] = n
	}
	return m
}

func sortedKeys(m map[string]*assetGroup) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
