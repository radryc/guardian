package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	"github.com/rydzu/ainfra/guardian/internal/observedgen"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/reconciler"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/repoconfig"
	"github.com/rydzu/ainfra/guardian/internal/repoimport"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func repoAddCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo add", flag.ContinueOnError)
	name := flags.String("name", "", "repo name")
	pusher := flags.String("pusher", "", "target pusher name")
	partition := flags.String("partition", "", "target partition name")
	source := flags.String("source", "", "git URL or local directory")
	branch := flags.String("branch", "", "git branch")
	assemblySubdir := flags.String("assembly-subdir", "cdk.out", "assembly directory relative to the repo root")
	path := flags.String("path", "", "logical MonoFS payload dir (default /partitions/<partition>/payloads/aws)")
	account := flags.String("account", "", "AWS account id override")
	region := flags.String("region", "", "AWS region override")
	description := flags.String("description", "", "free-form description")
	bindingsJSON := flags.String("bindings", "", "JSON array of partition bindings (alternative to --partition)")
	return &command.Command{Description: "Register a source repository that feeds one or more observed partitions", Flags: flags, Run: func(ctx context.Context, args []string) error {
		repo := &registry.Repo{
			Name:           *name,
			Pusher:         *pusher,
			Partition:      *partition,
			Source:         *source,
			Branch:         *branch,
			AssemblySubdir: *assemblySubdir,
			Path:           *path,
			Account:        *account,
			Region:         *region,
			Description:    *description,
		}
		if strings.TrimSpace(*bindingsJSON) != "" {
			var bindings []registry.RepoBinding
			if err := json.Unmarshal([]byte(*bindingsJSON), &bindings); err != nil {
				return fmt.Errorf("parse --bindings: %w", err)
			}
			repo.Bindings = bindings
			repo.Partition = ""
			repo.Path = ""
		}
		if err := registry.New(store).PutRepo(ctx, repo); err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(repo)
			return nil
		}
		printer.PrintText("registered repo %s -> %s via pusher %s\n", repo.Name, strings.Join(repo.PartitionNames(), ", "), repo.Pusher)
		return nil
	}}
}

func repoListCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	return &command.Command{Description: "List registered repositories", Flags: flag.NewFlagSet("repo list", flag.ContinueOnError), Run: func(ctx context.Context, args []string) error {
		repos, err := registry.New(store).ListRepos(ctx)
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(repos)
			return nil
		}
		rows := make([][]string, 0, len(repos))
		for _, repo := range repos {
			lastSync := ""
			if !repo.LastSyncedAt.IsZero() {
				lastSync = repo.LastSyncedAt.Format("2006-01-02 15:04")
			}
			rows = append(rows, []string{repo.Name, strings.Join(repo.PartitionNames(), ","), repo.Pusher, repo.Source, lastSync, repo.LastSyncError})
		}
		printer.PrintTable([]string{"NAME", "PARTITION", "PUSHER", "SOURCE", "LAST SYNC", "ERROR"}, rows)
		return nil
	}}
}

func repoRemoveCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo remove", flag.ContinueOnError)
	name := flags.String("name", "", "repo name")
	return &command.Command{Description: "Remove a registered repository", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		if err := registry.New(store).DeleteRepo(ctx, strings.TrimSpace(*name)); err != nil {
			return err
		}
		printer.PrintText("removed repo %s\n", strings.TrimSpace(*name))
		return nil
	}}
}

// repoAssignCommand replaces a repo's partition bindings without touching its
// source or credentials.
func repoAssignCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo assign", flag.ContinueOnError)
	name := flags.String("name", "", "repo name")
	bindingsJSON := flags.String("bindings", "", `JSON array of bindings, e.g. [{"partition":"prod","selector":"Prod-*"}]`)
	partitions := flags.String("partition", "", "comma-separated partition names (simple bindings)")
	selector := flags.String("selector", "", "stack selector applied to every --partition")
	path := flags.String("path", "", "payload dir applied to every --partition")
	account := flags.String("account", "", "account override applied to every --partition")
	region := flags.String("region", "", "region override applied to every --partition")
	return &command.Command{Description: "Assign existing partitions to a repository (multiple partitions, one selector each)", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		bindings, err := parseRepoBindings(*bindingsJSON, *partitions, *selector, *path, *account, *region)
		if err != nil {
			return err
		}
		if len(bindings) == 0 {
			return fmt.Errorf("provide --bindings or --partition")
		}
		registryStore := registry.New(store)
		repo, err := registryStore.GetRepo(ctx, strings.TrimSpace(*name))
		if err != nil {
			return err
		}
		repo.Bindings = bindings
		repo.Partition = ""
		repo.Path = ""
		if err := registryStore.PutRepo(ctx, repo); err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(repo)
			return nil
		}
		printer.PrintText("assigned repo %s to %s\n", repo.Name, strings.Join(repo.PartitionNames(), ", "))
		return nil
	}}
}

func parseRepoBindings(raw, partitionsCSV, selector, path, account, region string) ([]registry.RepoBinding, error) {
	if strings.TrimSpace(raw) != "" {
		var bindings []registry.RepoBinding
		if err := json.Unmarshal([]byte(raw), &bindings); err != nil {
			return nil, fmt.Errorf("parse --bindings: %w", err)
		}
		return bindings, nil
	}
	out := make([]registry.RepoBinding, 0)
	for _, partition := range strings.Split(partitionsCSV, ",") {
		partition = strings.TrimSpace(partition)
		if partition == "" {
			continue
		}
		out = append(out, registry.RepoBinding{
			Partition: partition,
			Selector:  strings.TrimSpace(selector),
			Path:      strings.TrimSpace(path),
			Account:   strings.TrimSpace(account),
			Region:    strings.TrimSpace(region),
		})
	}
	return out, nil
}

func repoSyncCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo sync", flag.ContinueOnError)
	name := flags.String("name", "", "repo name (optional when the checkout has .guardian/config.yaml)")
	dir := flags.String("dir", "", "use an existing local checkout instead of cloning")
	assembly := flags.String("assembly", "", "use an existing cloud assembly instead of synthesizing")
	synth := flags.Bool("synth", false, "run the package manager and cdk synth when no assembly exists")
	reconcile := flags.Bool("reconcile", true, "trigger a reconcile after pushing the bundle")
	applyConfig := flags.Bool("apply-config", true, "apply .guardian/config.yaml from the checkout (registers pusher and repo)")
	return &command.Command{Description: "Ingest a repository and push its observed partition bundle", Flags: flags, Run: func(ctx context.Context, args []string) error {
		registryStore := registry.New(store)
		repoName := strings.TrimSpace(*name)
		var repo *registry.Repo
		if repoName != "" {
			loaded, err := registryStore.GetRepo(ctx, repoName)
			if err != nil {
				return err
			}
			repo = loaded
		}
		sourceDir, cleanup, err := materializeRepoSource(ctx, repo, *dir)
		if err != nil {
			return err
		}
		defer cleanup()

		if *applyConfig {
			appliedName, err := applyRepoConfig(ctx, registryStore, sourceDir)
			if err != nil {
				return err
			}
			if repoName == "" {
				repoName = appliedName
			}
		}
		if repoName == "" {
			return fmt.Errorf("--name is required when the checkout has no .guardian/config.yaml with repo.name")
		}
		if repo == nil || repo.Name != repoName {
			loaded, err := registryStore.GetRepo(ctx, repoName)
			if err != nil {
				return err
			}
			repo = loaded
		}
		return syncRepoBundle(ctx, store, registryStore, repo, sourceDir, *assembly, *synth, *reconcile, printer)
	}}
}

func repoApplyCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo apply", flag.ContinueOnError)
	dir := flags.String("dir", ".", "repository checkout containing .guardian/config.yaml")
	source := flags.String("source", "", "git URL to record when the config omits repo.source (defaults to the origin remote)")
	assembly := flags.String("assembly", "", "use an existing cloud assembly instead of synthesizing")
	synth := flags.Bool("synth", false, "run the package manager and cdk synth when no assembly exists")
	reconcile := flags.Bool("reconcile", true, "trigger a reconcile after pushing the bundle")
	return &command.Command{Description: "Apply repo config: register pusher/repo and ingest the partition", Flags: flags, Run: func(ctx context.Context, args []string) error {
		sourceDir, err := filepath.Abs(filepath.Clean(*dir))
		if err != nil {
			return err
		}
		if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
			return fmt.Errorf("%s is not a directory", sourceDir)
		}
		registryStore := registry.New(store)
		sourceOverride := strings.TrimSpace(*source)
		if sourceOverride == "" {
			sourceOverride = gitRemoteURL(ctx, sourceDir)
		}
		config, configPath, found, err := repoconfig.Load(sourceDir)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("no %s found in %s", repoconfig.FileName, sourceDir)
		}
		if err := applyRepoConfigValues(ctx, registryStore, config, sourceOverride); err != nil {
			return err
		}
		if config.Repo.Name == "" {
			return fmt.Errorf("%s must set repo.name", configPath)
		}
		repo, err := registryStore.GetRepo(ctx, config.Repo.Name)
		if err != nil {
			return err
		}
		if printer.Format != "json" {
			printer.PrintText("applied %s: pusher=%s repo=%s partitions=%s\n", configPath, config.Pusher.Name, repo.Name, strings.Join(repo.PartitionNames(), ", "))
		}
		return syncRepoBundle(ctx, store, registryStore, repo, sourceDir, *assembly, *synth, *reconcile, printer)
	}}
}

func repoIngestCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo ingest", flag.ContinueOnError)
	name := flags.String("name", "", "repo name")
	assembly := flags.String("assembly", "", "logical MonoFS path of the cloud assembly (cdk.out)")
	reconcile := flags.Bool("reconcile", true, "reconcile every bound partition after ingest")
	return &command.Command{Description: "Generate observed intents from an assembly already in MonoFS", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		registryStore := registry.New(store)
		repo, err := registryStore.GetRepo(ctx, strings.TrimSpace(*name))
		if err != nil {
			return err
		}
		created, err := ingestRegisteredRepo(ctx, store, registryStore, repo, *assembly)
		if err != nil {
			return err
		}
		if *reconcile {
			if err := reconcileRepoPartitions(ctx, store, repo); err != nil {
				return err
			}
		}
		if printer.Format == "json" {
			printer.PrintJSON(map[string]any{"repo": repo.Name, "partitions": repo.PartitionNames(), "intents": created})
			return nil
		}
		printer.PrintText("ingested repo %s -> %s (%d intent(s))\n", repo.Name, strings.Join(repo.PartitionNames(), ", "), len(created))
		return nil
	}}
}

// repoImportCommand imports a repository's guardian/partitions/** bundle from
// either a local checkout or the MonoFS repo display path (/.repos/<path>)
// into the canonical partition namespace, then optionally reconciles the
// affected partitions. It is the manual counterpart of the level-triggered
// Guardian-side repo import loop.
func repoImportCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo import", flag.ContinueOnError)
	name := flags.String("name", "", "repo name")
	source := flags.String("source", "", "override source: local directory or /.repos/<display-path>")
	partition := flags.String("partition", "", "limit the import to one partition")
	reconcile := flags.Bool("reconcile", true, "trigger a reconcile when files changed")
	return &command.Command{Description: "Import guardian/partitions/** from a repo into the partition namespace", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		registryStore := registry.New(store)
		repo, err := registryStore.GetRepo(ctx, strings.TrimSpace(*name))
		if err != nil {
			return err
		}
		src, err := repoSources(store, repo, *source)
		if err != nil {
			return err
		}
		result, err := repoimport.Import(ctx, src, store, repoimport.Options{
			RepoName:  repo.Name,
			Partition: strings.TrimSpace(*partition),
		})
		if err != nil {
			return err
		}
		if *reconcile && result.Changed {
			if err := reconcileRepoPartitions(ctx, store, repo); err != nil {
				return err
			}
		}
		if printer.Format == "json" {
			printer.PrintJSON(result)
			return nil
		}
		for _, warning := range result.Warnings {
			printer.PrintText("warning: %s\n", warning)
		}
		printer.PrintText("imported repo %s: %d partition(s) changed, %d file(s) written, %d unchanged\n",
			repo.Name, len(result.Imported), result.FilesWritten, len(result.Skipped))
		return nil
	}}
}

// repoSources picks a source for the repo import: an explicit local checkout,
// the repo's local source, or the /.repos/<display-path> MonoFS mapping.
func repoSources(store guardianapi.Store, repo *registry.Repo, override string) (repoimport.Source, error) {
	source := firstNonEmpty(override, repo.Source)
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("repo %s has no source; pass --source", repo.Name)
	}
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		return repoimport.NewLocalSource(source), nil
	}
	logical := strings.TrimSpace(source)
	if !strings.HasPrefix(logical, "/") {
		logical = repoDisplayLogicalPath(source)
		if logical == "" {
			return nil, fmt.Errorf("cannot derive a MonoFS display path from source %q; pass --source /.repos/<display-path>", source)
		}
	}
	return repoimport.NewStoreSource(store, logical), nil
}

// repoDisplayLogicalPath converts a git URL into the MonoFS /.repos logical
// path (for example https://github.com/example/strata.git ->
// /.repos/github.com/example/strata).
func repoDisplayLogicalPath(source string) string {
	display := repoconfig.DisplayPathFromSource(source)
	if display == "" {
		return ""
	}
	return "/.repos/" + display
}

func repoNotifyCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo notify", flag.ContinueOnError)
	name := flags.String("name", "", "repo name")
	commit := flags.String("commit", "", "merge commit SHA")
	noIngest := flags.Bool("no-ingest", false, "only record the notification, do not regenerate intents")
	reconcile := flags.Bool("reconcile", true, "reconcile every bound partition after ingest")
	return &command.Command{Description: "Record a CI merge notification and regenerate intents from the stored assembly", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		registryStore := registry.New(store)
		repo, err := registryStore.GetRepo(ctx, strings.TrimSpace(*name))
		if err != nil {
			return err
		}
		if err := registryStore.RecordNotification(ctx, repo.Name, *commit); err != nil {
			return err
		}
		created := []string(nil)
		if !*noIngest {
			created, err = ingestRegisteredRepo(ctx, store, registryStore, repo, "")
			if err != nil {
				return err
			}
			if *reconcile {
				if err := reconcileRepoPartitions(ctx, store, repo); err != nil {
					return err
				}
			}
		}
		if printer.Format == "json" {
			printer.PrintJSON(map[string]any{"repo": repo.Name, "commit": *commit, "intents": created})
			return nil
		}
		printer.PrintText("notified repo %s (commit %s), %d intent(s)\n", repo.Name, valueOrNone(*commit), len(created))
		return nil
	}}
}

// reconcileRepoPartitions auto-deploys every partition bound to a repo by
// running an explicit reconcile for each binding target.
func reconcileRepoPartitions(ctx context.Context, store guardianapi.Store, repo *registry.Repo) error {
	var firstErr error
	for _, partition := range repo.PartitionNames() {
		disp := dispatcher.NewDispatcher(store, "guardianctl")
		recon := reconciler.NewReconciler(store, disp, time.Minute)
		if err := recon.ReconcilePartition(ctx, partition, true); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("reconcile %s: %w", partition, err)
		}
	}
	return firstErr
}

// ingestRegisteredRepo regenerates observed intents for a registered repo from
// an assembly already in MonoFS. One bundle is generated per partition binding.
func ingestRegisteredRepo(ctx context.Context, store guardianapi.Store, registryStore *registry.Store, repo *registry.Repo, assemblyOverride string) ([]string, error) {
	pusher, err := registryStore.GetPusher(ctx, repo.Pusher)
	if err != nil {
		return nil, err
	}
	created := make([]string, 0)
	for _, binding := range repo.BindingList() {
		account := firstNonEmpty(binding.Account, repo.Account, pusher.Account)
		region := firstNonEmpty(binding.Region, repo.Region, pusher.Region)
		if account == "" || region == "" {
			return nil, fmt.Errorf("repo %s: account and region must be set on the binding, repo or pusher", repo.Name)
		}
		assemblyDir := strings.TrimSpace(assemblyOverride)
		if assemblyDir == "" {
			assemblyDir, err = resolveStoreAssembly(ctx, store, repo, binding)
			if err != nil {
				_ = registryStore.RecordSync(ctx, repo.Name, "", err)
				return nil, err
			}
		}
		intents, err := observedgen.GenerateToStore(ctx, store, observedgen.StoreOptions{
			AssemblyDir: assemblyDir,
			Partition:   binding.Partition,
			Account:     account,
			Region:      region,
			Pusher:      repo.Pusher,
			PayloadDir:  binding.PayloadDir(),
			Selector:    binding.Selector,
		})
		if err != nil {
			_ = registryStore.RecordSync(ctx, repo.Name, "", err)
			return nil, err
		}
		created = append(created, intents...)
	}
	if err := registryStore.RecordSync(ctx, repo.Name, "", nil); err != nil {
		return nil, err
	}
	return created, nil
}

// syncRepoBundle imports each binding's repo-authored partition bundle
// (guardian/partitions/<name>/) and, when a cloud assembly is available,
// generates the observed bundle for it. Missing intents are preserved because a
// partition can be fed by several repos/bindings. Partition authoring is
// credential-free: inline Secret values are rejected.
func syncRepoBundle(ctx context.Context, store guardianapi.Store, registryStore *registry.Store, repo *registry.Repo, sourceDir, assembly string, synth, reconcile bool, printer *output.Printer) error {
	pusher, err := registryStore.GetPusher(ctx, repo.Pusher)
	if err != nil {
		return err
	}
	commit := gitHead(ctx, sourceDir)

	assemblyDir, assemblyErr := resolveAssemblyDir(ctx, repo, sourceDir, assembly, synth)
	if assemblyErr != nil {
		assemblyDir = ""
	}

	bindings := repo.BindingList()
	if len(bindings) == 0 {
		err := fmt.Errorf("repo %s has no partition bindings", repo.Name)
		_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
		return err
	}

	created := make([]string, 0)
	for _, binding := range bindings {
		imported, err := importRepoPartition(ctx, store, sourceDir, binding.Partition)
		if err != nil {
			_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
			return err
		}
		if assemblyDir == "" {
			if !imported {
				err := fmt.Errorf("repo %s partition %s: no guardian/partitions/%s and no cloud assembly: %w",
					repo.Name, binding.Partition, binding.Partition, assemblyErr)
				_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
				return err
			}
			continue
		}
		account := firstNonEmpty(binding.Account, repo.Account, pusher.Account)
		region := firstNonEmpty(binding.Region, repo.Region, pusher.Region)
		if account == "" || region == "" {
			err := fmt.Errorf("repo %s binding %s: account and region must be set on the binding, repo or pusher", repo.Name, binding.Partition)
			_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
			return err
		}
		outDir, err := os.MkdirTemp("", "guardian-observed-bundle-*")
		if err != nil {
			return err
		}
		intents, err := observedgen.Generate(observedgen.Options{
			AssemblyDir: assemblyDir,
			OutDir:      outDir,
			Partition:   binding.Partition,
			Account:     account,
			Region:      region,
			Pusher:      repo.Pusher,
			PayloadDir:  binding.PayloadDir(),
			Selector:    binding.Selector,
		})
		if err != nil {
			os.RemoveAll(outDir)
			_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
			return err
		}
		bundle, err := loadLocalPartitionBundle(outDir, false)
		if err != nil {
			os.RemoveAll(outDir)
			_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
			return err
		}
		// Observed generation is not authoritative for partition settings: keep
		// the authored or existing config when the partition already exists.
		preserveExistingPartitionConfig(ctx, store, bundle)
		if _, err := pushPartitionBundle(ctx, store, bundle, false); err != nil {
			os.RemoveAll(outDir)
			_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
			return err
		}
		os.RemoveAll(outDir)
		created = append(created, intents...)
	}
	if reconcile {
		if err := reconcileRepoPartitions(ctx, store, repo); err != nil {
			_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
			return err
		}
	}
	if err := registryStore.RecordSync(ctx, repo.Name, commit, nil); err != nil {
		return err
	}
	if printer.Format == "json" {
		printer.PrintJSON(map[string]any{
			"repo": repo.Name, "partitions": repo.PartitionNames(), "commit": commit, "intents": created,
		})
		return nil
	}
	printer.PrintText("synced repo %s -> %s (%d intent(s), commit %s)\n", repo.Name, strings.Join(repo.PartitionNames(), ", "), len(created), valueOrNone(commit))
	return nil
}

const repoPartitionsSubdir = "guardian/partitions"

// importRepoPartition pushes the repo-authored partition bundle from
// guardian/partitions/<name>/ when the checkout contains one. It returns false
// when the repo does not author that partition.
func importRepoPartition(ctx context.Context, store guardianapi.Store, sourceDir, partition string) (bool, error) {
	if strings.TrimSpace(sourceDir) == "" {
		return false, nil
	}
	dir := filepath.Join(sourceDir, filepath.FromSlash(repoPartitionsSubdir), partition)
	info, statErr := os.Stat(dir)
	if statErr != nil || !info.IsDir() {
		return false, nil
	}
	bundle, err := loadLocalPartitionBundle(dir, false)
	if err != nil {
		return true, fmt.Errorf("import partition %s: %w", partition, err)
	}
	if bundle.PartitionName != partition {
		return true, fmt.Errorf("import partition %s: manifest metadata.name %q does not match", partition, bundle.PartitionName)
	}
	if err := rejectInlineSecretValues(bundle); err != nil {
		return true, err
	}
	if _, err := pushPartitionBundle(ctx, store, bundle, false); err != nil {
		return true, err
	}
	return true, nil
}

// rejectInlineSecretValues enforces the GitOps rule that repository content
// only carries references, never secret material.
func rejectInlineSecretValues(bundle *localPartitionBundle) error {
	for intentName, manifest := range bundle.IntentManifests {
		for _, asset := range manifest.Spec.Assets {
			if asset.Type != assetdomain.TypeSecret {
				continue
			}
			value, _ := asset.Properties["value"].(string)
			if strings.TrimSpace(value) != "" {
				return fmt.Errorf("intent %s asset %s: inline Secret value is not allowed in repo-sourced partitions; use secretRef", intentName, asset.Name)
			}
		}
	}
	return nil
}

// preserveExistingPartitionConfig drops the generated partition settings when
// the partition already exists, so ingest extends instead of resetting it.
func preserveExistingPartitionConfig(ctx context.Context, store guardianapi.Store, bundle *localPartitionBundle) {
	if _, err := store.ReadFile(ctx, paths.PartitionConfig(bundle.PartitionName)); err == nil {
		delete(bundle.ManagedFiles, paths.PartitionConfig(bundle.PartitionName))
	}
}

// applyRepoConfig loads .guardian/config.yaml from dir and upserts the pusher
// and repo registrations. Returns the configured repo name, or "" when no
// config exists.
func applyRepoConfig(ctx context.Context, registryStore *registry.Store, dir string) (string, error) {
	config, _, found, err := repoconfig.Load(dir)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	sourceOverride := gitRemoteURL(ctx, dir)
	if err := applyRepoConfigValues(ctx, registryStore, config, sourceOverride); err != nil {
		return "", err
	}
	return config.Repo.Name, nil
}

func applyRepoConfigValues(ctx context.Context, registryStore *registry.Store, config *repoconfig.Config, sourceOverride string) error {
	if pusher := config.PusherRegistration(); pusher != nil {
		if err := registryStore.PutPusher(ctx, pusher); err != nil {
			return fmt.Errorf("apply pusher registration: %w", err)
		}
	}
	if repo := config.RepoRegistration(sourceOverride); repo != nil {
		if err := registryStore.PutRepo(ctx, repo); err != nil {
			return fmt.Errorf("apply repo registration: %w", err)
		}
	}
	return nil
}

func materializeRepoSource(ctx context.Context, repo *registry.Repo, dir string) (string, func(), error) {
	noop := func() {}
	if strings.TrimSpace(dir) != "" {
		info, err := os.Stat(dir)
		if err != nil {
			return "", noop, err
		}
		if !info.IsDir() {
			return "", noop, fmt.Errorf("%s is not a directory", dir)
		}
		return dir, noop, nil
	}
	if repo == nil {
		return "", noop, fmt.Errorf("--dir is required when the repo is not registered")
	}
	source := strings.TrimSpace(repo.Source)
	if source == "" {
		return "", noop, fmt.Errorf("repo %s has no source and --dir was not provided", repo.Name)
	}
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		return source, noop, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", noop, fmt.Errorf("git is required to clone %s: %w", source, err)
	}
	tempDir, err := os.MkdirTemp("", "guardian-repo-*")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	args := []string{"clone", "--depth", "1"}
	if branch := strings.TrimSpace(repo.Branch); branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, source, tempDir)
	if err := runCommand(ctx, "", "git", args...); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("clone %s: %w", source, err)
	}
	return tempDir, cleanup, nil
}

func resolveAssemblyDir(ctx context.Context, repo *registry.Repo, sourceDir, assembly string, synth bool) (string, error) {
	if strings.TrimSpace(assembly) != "" {
		return assembly, nil
	}
	candidate := filepath.Join(sourceDir, repo.AssemblySubdir)
	if _, err := os.Stat(filepath.Join(candidate, "manifest.json")); err == nil {
		return candidate, nil
	}
	if !synth {
		return "", fmt.Errorf("no cloud assembly found at %s; commit one, pass --assembly, or use --synth", candidate)
	}
	if err := runRepoSynth(ctx, sourceDir, repo.AssemblySubdir); err != nil {
		return "", err
	}
	return candidate, nil
}

func runRepoSynth(ctx context.Context, dir, outputSubdir string) error {
	manager, installArgs := packageManagerForDir(dir)
	if manager != "" {
		if err := runCommand(ctx, dir, manager, installArgs...); err != nil {
			return fmt.Errorf("%s %s: %w", manager, strings.Join(installArgs, " "), err)
		}
	}
	if err := runCommand(ctx, dir, "npx", "cdk", "synth", "--output", outputSubdir, "--quiet"); err != nil {
		return fmt.Errorf("cdk synth: %w", err)
	}
	return nil
}

func packageManagerForDir(dir string) (string, []string) {
	if !fileExists(filepath.Join(dir, "package.json")) {
		return "", nil
	}
	switch {
	case fileExists(filepath.Join(dir, "pnpm-lock.yaml")):
		return "pnpm", []string{"install", "--frozen-lockfile"}
	case fileExists(filepath.Join(dir, "yarn.lock")):
		return "yarn", []string{"install", "--frozen-lockfile"}
	case fileExists(filepath.Join(dir, "package-lock.json")):
		return "npm", []string{"ci"}
	default:
		return "npm", []string{"install"}
	}
}

func runCommand(ctx context.Context, dir, binary string, args ...string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitHead(ctx context.Context, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitRemoteURL(ctx context.Context, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func valueOrNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func resolveStoreAssembly(ctx context.Context, store guardianapi.Store, repo *registry.Repo, binding registry.RepoBinding) (string, error) {
	candidates := make([]string, 0, 4)
	if source := strings.TrimSpace(repo.Source); strings.HasPrefix(source, "/") {
		candidates = append(candidates, source, strings.TrimRight(source, "/")+"/cdk.out")
	}
	payloadDir := binding.PayloadDir()
	candidates = append(candidates, strings.TrimRight(payloadDir, "/")+"/cdk.out", payloadDir)
	for _, candidate := range candidates {
		if _, err := store.ReadFile(ctx, strings.TrimRight(candidate, "/")+"/manifest.json"); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no cloud assembly found for repo %s partition %s; push a cdk.out to %s or run `guardianctl repo sync --name %s`", repo.Name, binding.Partition, strings.TrimRight(payloadDir, "/")+"/cdk.out", repo.Name)
}
