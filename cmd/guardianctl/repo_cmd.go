package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/observedgen"
	"github.com/rydzu/ainfra/guardian/internal/registry"
	"github.com/rydzu/ainfra/guardian/internal/repoconfig"
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
	return &command.Command{Description: "Register a source repository that feeds an observed partition", Flags: flags, Run: func(ctx context.Context, args []string) error {
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
		if err := registry.New(store).PutRepo(ctx, repo); err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(repo)
			return nil
		}
		printer.PrintText("registered repo %s -> partition %s via pusher %s\n", repo.Name, repo.Partition, repo.Pusher)
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
			rows = append(rows, []string{repo.Name, repo.Partition, repo.Pusher, repo.Source, lastSync, repo.LastSyncError})
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
			printer.PrintText("applied %s: pusher=%s repo=%s partition=%s\n", configPath, config.Pusher.Name, repo.Name, repo.Partition)
		}
		return syncRepoBundle(ctx, store, registryStore, repo, sourceDir, *assembly, *synth, *reconcile, printer)
	}}
}

func repoIngestCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo ingest", flag.ContinueOnError)
	name := flags.String("name", "", "repo name")
	assembly := flags.String("assembly", "", "logical MonoFS path of the cloud assembly (cdk.out)")
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
		if printer.Format == "json" {
			printer.PrintJSON(map[string]any{"repo": repo.Name, "partition": repo.Partition, "intents": created})
			return nil
		}
		printer.PrintText("ingested repo %s -> partition %s (%d intent(s))\n", repo.Name, repo.Partition, len(created))
		return nil
	}}
}

func repoNotifyCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("repo notify", flag.ContinueOnError)
	name := flags.String("name", "", "repo name")
	commit := flags.String("commit", "", "merge commit SHA")
	noIngest := flags.Bool("no-ingest", false, "only record the notification, do not regenerate intents")
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
		}
		if printer.Format == "json" {
			printer.PrintJSON(map[string]any{"repo": repo.Name, "commit": *commit, "intents": created})
			return nil
		}
		printer.PrintText("notified repo %s (commit %s), %d intent(s)\n", repo.Name, valueOrNone(*commit), len(created))
		return nil
	}}
}

// ingestRegisteredRepo regenerates observed intents for a registered repo from
// an assembly already in MonoFS.
func ingestRegisteredRepo(ctx context.Context, store guardianapi.Store, registryStore *registry.Store, repo *registry.Repo, assemblyOverride string) ([]string, error) {
	pusher, err := registryStore.GetPusher(ctx, repo.Pusher)
	if err != nil {
		return nil, err
	}
	account := firstNonEmpty(repo.Account, pusher.Account)
	region := firstNonEmpty(repo.Region, pusher.Region)
	if account == "" || region == "" {
		return nil, fmt.Errorf("repo %s: account and region must be set on the repo or pusher", repo.Name)
	}
	assemblyDir := strings.TrimSpace(assemblyOverride)
	if assemblyDir == "" {
		assemblyDir, err = resolveStoreAssembly(ctx, store, repo)
		if err != nil {
			_ = registryStore.RecordSync(ctx, repo.Name, "", err)
			return nil, err
		}
	}
	created, err := observedgen.GenerateToStore(ctx, store, observedgen.StoreOptions{
		AssemblyDir: assemblyDir,
		Partition:   repo.Partition,
		Account:     account,
		Region:      region,
		Pusher:      repo.Pusher,
		PayloadDir:  repo.ObservedPartitionPath(),
	})
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, "", err)
		return nil, err
	}
	if err := registryStore.RecordSync(ctx, repo.Name, "", nil); err != nil {
		return nil, err
	}
	return created, nil
}

// syncRepoBundle generates and pushes the observed partition bundle for a
// materialized checkout.
func syncRepoBundle(ctx context.Context, store guardianapi.Store, registryStore *registry.Store, repo *registry.Repo, sourceDir, assembly string, synth, reconcile bool, printer *output.Printer) error {
	pusher, err := registryStore.GetPusher(ctx, repo.Pusher)
	if err != nil {
		return err
	}
	account := firstNonEmpty(repo.Account, pusher.Account)
	region := firstNonEmpty(repo.Region, pusher.Region)
	if account == "" || region == "" {
		return fmt.Errorf("repo %s: account and region must be set on the repo or pusher", repo.Name)
	}
	assemblyDir, err := resolveAssemblyDir(ctx, repo, sourceDir, assembly, synth)
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, "", err)
		return err
	}
	commit := gitHead(ctx, sourceDir)

	outDir, err := os.MkdirTemp("", "guardian-observed-bundle-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(outDir)
	created, err := observedgen.Generate(observedgen.Options{
		AssemblyDir: assemblyDir,
		OutDir:      outDir,
		Partition:   repo.Partition,
		Account:     account,
		Region:      region,
		Pusher:      repo.Pusher,
		PayloadDir:  repo.ObservedPartitionPath(),
	})
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
		return err
	}
	bundle, err := loadLocalPartitionBundle(outDir, false)
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
		return err
	}
	if _, err := pushPartitionBundle(ctx, store, bundle, true); err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
		return err
	}
	if reconcile {
		if err := reconcilePushed(ctx, store, bundle); err != nil {
			_ = registryStore.RecordSync(ctx, repo.Name, commit, err)
			return err
		}
	}
	if err := registryStore.RecordSync(ctx, repo.Name, commit, nil); err != nil {
		return err
	}
	if printer.Format == "json" {
		printer.PrintJSON(map[string]any{
			"repo": repo.Name, "partition": repo.Partition, "commit": commit, "intents": created,
		})
		return nil
	}
	printer.PrintText("synced repo %s -> partition %s (%d intent(s), commit %s)\n", repo.Name, repo.Partition, len(created), valueOrNone(commit))
	return nil
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

func resolveStoreAssembly(ctx context.Context, store guardianapi.Store, repo *registry.Repo) (string, error) {
	candidates := make([]string, 0, 4)
	if source := strings.TrimSpace(repo.Source); strings.HasPrefix(source, "/") {
		candidates = append(candidates, source, strings.TrimRight(source, "/")+"/cdk.out")
	}
	payloadDir := repo.ObservedPartitionPath()
	candidates = append(candidates, strings.TrimRight(payloadDir, "/")+"/cdk.out", payloadDir)
	for _, candidate := range candidates {
		if _, err := store.ReadFile(ctx, strings.TrimRight(candidate, "/")+"/manifest.json"); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no cloud assembly found for repo %s; push a cdk.out to %s or run `guardianctl repo sync --name %s`", repo.Name, strings.TrimRight(payloadDir, "/")+"/cdk.out", repo.Name)
}
