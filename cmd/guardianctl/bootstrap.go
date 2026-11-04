package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/radryc/guardian/internal/bootstrap"
	"github.com/radryc/guardian/internal/cli/command"
)

// bootstrapReg holds a command registration descriptor.
type bootstrapReg struct {
	Group string
	Name  string
	Cmd   *command.Command
}

// devCommands returns all development-only commands (bootstrap + setup).
// These do NOT require a running Guardian store — they work with kubectl + docker directly.
func devCommands() []bootstrapReg {
	return []bootstrapReg{
		{Group: "dev", Name: "setup", Cmd: setupCommand()},
		{Group: "dev", Name: "build", Cmd: devBuildCommand()},
		{Group: "dev", Name: "deploy", Cmd: devDeployCommand()},
		{Group: "dev", Name: "init", Cmd: devInitCommand()},
		{Group: "dev", Name: "stop", Cmd: devStopCommand()},
		{Group: "dev", Name: "destroy", Cmd: devDestroyCommand()},
		{Group: "dev", Name: "status", Cmd: devStatusCommand()},
		{Group: "dev", Name: "stamp-urls", Cmd: devStampURLsCommand()},
		{Group: "dev", Name: "configure-registry", Cmd: devConfigureRegistryCommand()},
	}
}

func devBuildCommand() *command.Command {
	flags := flag.NewFlagSet("dev build", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	configPath := flags.String("config", "", "path to bootstrap.yaml (default: auto-detect)")

	return &command.Command{
		Description: "Build Docker images for dev components via partition intents",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			bootstrapDir := findBootstrapDir()
			if bootstrapDir != "" {
				tag := resolveDevTag(ctx)
				fmt.Fprintf(os.Stderr, "=== building bootstrap images via partition intents (tag: %s) ===\n", tag)
				if err := buildImages(ctx, bootstrapDir, *dryRun, tag); err != nil {
					return fmt.Errorf("image build: %w", err)
				}
				return nil
			}

			// Fallback: legacy hardcoded builds
			cfg, err := loadBootstrapConfig(*configPath)
			if err != nil {
				return err
			}
			tag := resolveDevTag(ctx)
			applyDevTag(cfg, tag)
			writeDevTag(tag)
			fmt.Printf("=== building images with tag %s ===\n", tag)
			fmt.Println("=== building storage images ===")
			if err := bootstrap.BuildMonoFSImages(ctx, cfg, *dryRun); err != nil {
				return err
			}
			fmt.Println("=== building guardian images ===")
			if err := bootstrap.BuildGuardianImages(ctx, cfg, *dryRun); err != nil {
				return err
			}
			return nil
		},
	}
}

func devDeployCommand() *command.Command {
	flags := flag.NewFlagSet("dev deploy", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	configPath := flags.String("config", "", "path to bootstrap.yaml (default: auto-detect)")
	skipStorage := flags.Bool("skip-storage", false, "skip storage phase deployment")
	skipGuardian := flags.Bool("skip-guardian", false, "skip guardian phase deployment")
	noWait := flags.Bool("no-wait", false, "skip waiting for rollout completion")

	return &command.Command{
		Description: "Deploy dev templates into Kubernetes (storage + guardian)",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg, err := loadBootstrapConfig(*configPath)
			if err != nil {
				return err
			}

			if tag := readDevTag(); tag != "" {
				applyDevTag(cfg, tag)
				fmt.Printf("=== deploying with tag %s ===\n", tag)
			}

			env, err := bootstrap.ComputeEnv(cfg)
			if err != nil {
				return err
			}

			if !*dryRun {
				key, _ := bootstrap.LoadOrGenerateEncryptionKey()
				tokenB64 := env["MONOFS_TOKEN_B64"]
				token, _ := base64.StdEncoding.DecodeString(tokenB64)
				if err := bootstrap.WriteSharedPartitionSecrets(key, string(token)); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: shared partition secrets sync failed: %v\n", err)
				}
			}

			storageDir, guardianDir := bootstrap.TemplateDirs()

			if !*skipStorage {
				if err := deployStorageTemplates(ctx, cfg, env, storageDir, *dryRun); err != nil {
					return err
				}
				if !*noWait {
					if err := waitStorageDeployments(ctx, cfg, *dryRun); err != nil {
						return err
					}
					if err := waitLBEdgeDeployments(ctx, cfg, *dryRun); err != nil {
						return err
					}
				}
			}

			if !*skipGuardian {
				if err := deployGuardianTemplates(ctx, cfg, env, guardianDir, *dryRun); err != nil {
					return err
				}
			}

			// Configure containerd on kind nodes to pull from monofs-registry
			ctxName, _ := bootstrap.RunCapture(ctx, "kubectl", "config", "current-context")
			if strings.HasPrefix(ctxName, "kind-") {
				fmt.Println("=== configuring containerd for registry ===")
				if err := bootstrap.ConfigureKindRegistryForContainerd(ctx, strings.TrimPrefix(ctxName, "kind-"),
					cfg.Guardian.LocalRegistry.Namespace, cfg.Guardian.LocalRegistry.Name, *dryRun); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: containerd registry config failed: %v\n", err)
				}
			}

			// Patch CoreDNS so registry.strata.local resolves cluster-wide.
			if !*skipStorage {
				if err := bootstrap.PatchCoreDNSForRegistry(ctx, *dryRun); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: CoreDNS patch failed: %v\n", err)
				}
			}

			return nil
		},
	}
}

func devInitCommand() *command.Command {
	flags := flag.NewFlagSet("dev init", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	configPath := flags.String("config", "", "path to bootstrap.yaml (default: auto-detect)")
	skipBuild := flags.Bool("skip-build", false, "skip image builds")

	return &command.Command{
		Description: "Full dev init: build images via partition intents -> load into kind -> deploy storage -> push/stamp -> deploy guardian",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg, err := loadBootstrapConfig(*configPath)
			if err != nil {
				return err
			}

			bootstrapDir := findBootstrapDir()
			useBootstrapIntents := bootstrapDir != ""
			tag := resolveDevTag(ctx)

			if !*skipBuild {
				applyDevTag(cfg, tag)
				writeDevTag(tag)

				if useBootstrapIntents {
					fmt.Fprintf(os.Stderr, "=== building images with tag %s ===\n", tag)
					if err := buildImages(ctx, bootstrapDir, *dryRun, tag); err != nil {
						return fmt.Errorf("image build: %w", err)
					}
				} else {
					fmt.Printf("=== building images with tag %s ===\n", tag)
					fmt.Println("=== building storage images ===")
					if err := bootstrap.BuildMonoFSImages(ctx, cfg, *dryRun); err != nil {
						return err
					}
					fmt.Println("=== building guardian images ===")
					if err := bootstrap.BuildGuardianImages(ctx, cfg, *dryRun); err != nil {
						return err
					}
				}
			} else if t := readDevTag(); t != "" {
				applyDevTag(cfg, t)
				fmt.Printf("=== deploying with tag %s ===\n", t)
			}

			// Load images into kind if applicable
			ctxName, _ := bootstrap.RunCapture(ctx, "kubectl", "config", "current-context")
			if strings.HasPrefix(ctxName, "kind-") && !*dryRun {
				fmt.Println("=== loading images into kind cluster ===")
				storeImages := []string{
					cfg.Storage.Images.Server, cfg.Storage.Images.Router,
					cfg.Storage.Images.Fetcher, cfg.Storage.Images.Search,
					cfg.Storage.Images.Registry, cfg.Storage.Images.LB,
					cfg.Storage.Images.LogQuery,
				}
				guardImages := []string{
					cfg.Guardian.Images.Guardiand, cfg.Guardian.Images.PusherK8s,
					cfg.Guardian.Images.LB,
				}
				if cfg.Guardian.Pushers.AWS.Enabled {
					guardImages = append(guardImages, cfg.Guardian.Images.PusherAws)
				}
				allImages := append(storeImages, guardImages...)
				if err := bootstrap.LoadImagesIntoKind(ctx, allImages, *dryRun); err != nil {
					return fmt.Errorf("loading images into kind: %w", err)
				}

				if err := bootstrap.EnsureKindImage(ctx, cfg.Guardian.ImageBuild.BuildKitImage, ""); err != nil {
					return fmt.Errorf("buildkitd image not loaded into kind: %w", err)
				}
			}

			env, err := bootstrap.ComputeEnv(cfg)
			if err != nil {
				return err
			}

			if !*dryRun {
				key, _ := bootstrap.LoadOrGenerateEncryptionKey()
				tokenB64 := env["MONOFS_TOKEN_B64"]
				token, _ := base64.StdEncoding.DecodeString(tokenB64)
				if err := bootstrap.WriteSharedPartitionSecrets(key, string(token)); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: shared partition secrets sync failed: %v\n", err)
				}
			}

			storageDir, guardianDir := bootstrap.TemplateDirs()

			fmt.Println("=== deploying storage ===")
			if err := deployStorageTemplates(ctx, cfg, env, storageDir, *dryRun); err != nil {
				return err
			}
			if err := waitStorageDeployments(ctx, cfg, *dryRun); err != nil {
				return err
			}
			if err := waitLBEdgeDeployments(ctx, cfg, *dryRun); err != nil {
				return err
			}

			// Configure containerd on kind nodes to pull from monofs-registry
			if strings.HasPrefix(ctxName, "kind-") {
				fmt.Println("=== configuring containerd for registry ===")
				if err := bootstrap.ConfigureKindRegistryForContainerd(ctx, strings.TrimPrefix(ctxName, "kind-"),
					cfg.Guardian.LocalRegistry.Namespace, cfg.Guardian.LocalRegistry.Name, *dryRun); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: containerd registry config failed: %v\n", err)
				}
			}

			// Patch CoreDNS so registry.strata.local resolves cluster-wide.
			if err := bootstrap.PatchCoreDNSForRegistry(ctx, *dryRun); err != nil {
				fmt.Fprintf(os.Stderr, "  WARNING: CoreDNS patch failed: %v\n", err)
			}

			// Push bootstrap images to registry now that it's running
			if useBootstrapIntents && !*skipBuild {
				fmt.Fprintln(os.Stderr, "=== pushing bootstrap images to registry ===")
				if err := pushImages(ctx, bootstrapDir, "registry.strata.local:5000", *dryRun, true); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: image push failed: %v\n", err)
				}

				fmt.Fprintln(os.Stderr, "=== stamping bootstrap intent versions ===")
				if err := stampImages(bootstrapDir, *dryRun); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: image stamp failed: %v\n", err)
				}

				// Stamp all deployed partitions with the bootstrapped image tags.
				for _, partName := range []string{
					"guardian-configs", "agent", "doctor", "k8s-top",
					"monitoring", "opentelemetry", "lb-agent", "nightwatch",
				} {
					partDir := filepath.Join(filepath.Dir(bootstrapDir), partName)
					if _, err := os.Stat(partDir); err != nil {
						continue
					}
					if err := func() error {
						state, err := loadImageState(bootstrapDir)
						if err != nil {
							return err
						}
						if err := saveImageState(partDir, state); err != nil {
							return err
						}
						return stampImages(partDir, *dryRun)
					}(); err != nil {
						fmt.Fprintf(os.Stderr, "  WARNING: %s stamp failed: %v\n", partName, err)
					} else {
						fmt.Fprintf(os.Stderr, "  ✓ stamped %s intent versions\n", partName)
					}
				}
			}

			fmt.Println("=== deploying guardian ===")
			if err := deployGuardianTemplates(ctx, cfg, env, guardianDir, *dryRun); err != nil {
				return err
			}

			fmt.Println("=== dev init complete ===")
			return nil
		},
	}
}

func devStopCommand() *command.Command {
	flags := flag.NewFlagSet("dev stop", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	configPath := flags.String("config", "", "path to bootstrap.yaml (default: auto-detect)")

	return &command.Command{
		Description: "Scale all dev deployments to zero",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg, err := loadBootstrapConfig(*configPath)
			if err != nil {
				return err
			}

			// Stop docker pusher
			bootstrap.Run(ctx, *dryRun, "docker", "stop", "guardian-pusher-docker")
			bootstrap.Run(ctx, *dryRun, "docker", "rm", "guardian-pusher-docker")

			// Scale down storage
			if err := bootstrap.ScaleDeployments(ctx, cfg.Storage.Namespace, 0, *dryRun); err != nil {
				return err
			}
			// Scale down guardian
			if err := bootstrap.ScaleDeployments(ctx, cfg.Guardian.Namespace, 0, *dryRun); err != nil {
				return err
			}

			return nil
		},
	}
}

func devDestroyCommand() *command.Command {
	flags := flag.NewFlagSet("dev destroy", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	configPath := flags.String("config", "", "path to bootstrap.yaml (default: auto-detect)")

	return &command.Command{
		Description: "Delete dev namespaces (monofs, guardian, lb-edge)",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg, err := loadBootstrapConfig(*configPath)
			if err != nil {
				return err
			}

			// Stop docker pusher
			bootstrap.Run(ctx, *dryRun, "docker", "stop", "guardian-pusher-docker")
			bootstrap.Run(ctx, *dryRun, "docker", "rm", "guardian-pusher-docker")

			// Clear service finalizers
			bootstrap.PatchServiceFinalizers(ctx, cfg.Storage.Namespace, *dryRun)
			bootstrap.PatchServiceFinalizers(ctx, cfg.Guardian.Namespace, *dryRun)

			// Delete cluster role bindings
			bootstrap.DeleteClusterRoleBinding(ctx, "guardian-cluster-admin", *dryRun)
			bootstrap.DeleteClusterRoleBinding(ctx, "guardian-pusher-cluster-admin", *dryRun)

			// Delete namespaces
			bootstrap.DeleteNamespace(ctx, cfg.Storage.Namespace, *dryRun)
			bootstrap.DeleteNamespace(ctx, cfg.Guardian.Namespace, *dryRun)
			bootstrap.DeleteNamespace(ctx, cfg.LB.Namespace, *dryRun)

			return nil
		},
	}
}

func devStatusCommand() *command.Command {
	flags := flag.NewFlagSet("dev status", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	configPath := flags.String("config", "", "path to bootstrap.yaml (default: auto-detect)")

	return &command.Command{
		Description: "Show dev component health",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg, err := loadBootstrapConfig(*configPath)
			if err != nil {
				return err
			}

			for _, ns := range []string{cfg.Storage.Namespace, cfg.Guardian.Namespace, cfg.LB.Namespace} {
				fmt.Printf("--- namespace: %s ---\n", ns)
				if err := bootstrap.Run(ctx, false, "kubectl", "-n", ns, "get", "pods"); err != nil {
					fmt.Fprintf(os.Stderr, "  (no pods)\n")
				}
				fmt.Println()
			}

			return nil
		},
	}
}

func devStampURLsCommand() *command.Command {
	flags := flag.NewFlagSet("dev stamp-urls", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dryRun := flags.Bool("dry-run", false, "print without writing")
	configPath := flags.String("config", "", "path to bootstrap.yaml (default: auto-detect)")
	rootDir := flags.String("root", "", "path to stratatools repo root")

	return &command.Command{
		Description: "Resolve external URLs and stamp them into partition configs",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg, err := loadBootstrapConfig(*configPath)
			if err != nil {
				return err
			}

			root := *rootDir
			if root == "" {
				root = findRootDir()
			}

			lbNamespace := cfg.LB.Namespace
			guardianUIPort := cfg.Guardian.UIPort

			// Resolve lb-edge endpoint for guardian UI
			uiEndpoint := bootstrap.LbEdgeEndpoint(lbNamespace, "guardian-ui", guardianUIPort)
			guardianUIURL := ""
			if uiEndpoint != "" {
				guardianUIURL = "http://" + uiEndpoint
			}

			// Resolve lb-edge endpoint for MonoFS gRPC
			monofsGRPCEndpoint := bootstrap.LbEdgeEndpoint(lbNamespace, "grpc", "9090")

			if *dryRun {
				fmt.Printf("would stamp guardian UI URL: %s\n", guardianUIURL)
				fmt.Printf("would stamp MonoFS client API endpoint: %s\n", monofsGRPCEndpoint)
				return nil
			}

			// Stamp guardian-control-plane.yaml
			gcp := filepath.Join(root, "partitions", "guardian-configs", "intents", "guardian-control-plane.yaml")
			if guardianUIURL != "" {
				fmt.Printf("stamping GUARDIAN_UI_BASE_URL=%s into %s\n", guardianUIURL, gcp)
				bootstrap.SetEnvInIntent(gcp, "GUARDIAN_UI_BASE_URL", guardianUIURL)
			}
			if monofsGRPCEndpoint != "" {
				fmt.Printf("stamping GUARDIAN_MONOFS_CLIENT_API_ENDPOINT=%s into %s\n", monofsGRPCEndpoint, gcp)
				bootstrap.SetEnvInIntent(gcp, "GUARDIAN_MONOFS_CLIENT_API_ENDPOINT", monofsGRPCEndpoint)
			}

			// Stamp guardian docker pusher
			gdp := filepath.Join(root, "partitions", "guardian-configs", "intents", "guardian-docker-pusher.yaml")
			if monofsGRPCEndpoint != "" {
				fmt.Printf("stamping GUARDIAN_MONOFS_ROUTER=%s into %s\n", monofsGRPCEndpoint, gdp)
				bootstrap.SetEnvInIntent(gdp, "GUARDIAN_MONOFS_ROUTER", monofsGRPCEndpoint)
			}

			// Stamp guardian-configs/config.yaml
			gcfg := filepath.Join(root, "partitions", "guardian-configs", "config.yaml")
			if guardianUIURL != "" {
				fmt.Printf("stamping guardian_ui_base_url=%s into %s\n", guardianUIURL, gcfg)
				bootstrap.SetTopLevelKey(gcfg, "guardian_ui_base_url", guardianUIURL)
			}

			// Stamp doctor/query.yaml
			dq := filepath.Join(root, "partitions", "doctor", "intents", "query.yaml")
			if guardianUIURL != "" {
				fmt.Printf("stamping GUARDIAN_UI_BASE_URL=%s into %s\n", guardianUIURL, dq)
				bootstrap.SetEnvInIntent(dq, "GUARDIAN_UI_BASE_URL", guardianUIURL)
			}

			// Stamp doctor/config.yaml
			dcfg := filepath.Join(root, "partitions", "doctor", "config.yaml")
			dip := bootstrap.KubectlGetServiceIP(cfg.Guardian.Namespace, "doctor-query-external")
			if dip != "" {
				durl := "http://" + dip + ":8080"
				fmt.Printf("stamping doctor_query_base_url=%s into %s\n", durl, dcfg)
				bootstrap.SetTopLevelKey(dcfg, "doctor_query_base_url", durl)
			}

			return nil
		},
	}
}

func devConfigureRegistryCommand() *command.Command {
	flags := flag.NewFlagSet("dev configure-registry", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	configPath := flags.String("config", "", "path to bootstrap.yaml (default: auto-detect)")

	return &command.Command{
		Description: "Configure containerd on kind nodes to pull from monofs-registry (fixes ImagePullBackOff after release)",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			cfg, err := loadBootstrapConfig(*configPath)
			if err != nil {
				return err
			}

			ctxName, _ := bootstrap.RunCapture(ctx, "kubectl", "config", "current-context")
			if !strings.HasPrefix(ctxName, "kind-") {
				return fmt.Errorf("not a kind cluster context: %s", ctxName)
			}

			kindClusterName := strings.TrimPrefix(ctxName, "kind-")
			return bootstrap.ConfigureKindRegistryForContainerd(ctx, kindClusterName,
				cfg.Guardian.LocalRegistry.Namespace, cfg.Guardian.LocalRegistry.Name, *dryRun)
		},
	}
}

func setupCommand() *command.Command {
	flags := flag.NewFlagSet("dev setup", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	autoKind := flags.Bool("auto-kind", true, "auto-create kind cluster if none reachable")
	kindWorkers := flags.Int("kind-workers", 2, "number of kind worker nodes")
	reposDir := flags.String("repos-dir", "", "parent directory for cloning sibling repos")
	_ = reposDir // TODO: implement repo cloning

	return &command.Command{
		Description: "Check tools, clone sibling repos, create kind cluster, generate encryption key",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			fmt.Println("=== checking required tools ===")
			tools := []string{"git", "docker", "kubectl", "go", "kind"}
			for _, tool := range tools {
				if _, err := bootstrap.RunCapture(ctx, "which", tool); err != nil {
					fmt.Fprintf(os.Stderr, "  WARNING: %s not found in PATH\n", tool)
				} else {
					fmt.Printf("  OK: %s\n", tool)
				}
			}

			// Check Docker daemon
			if _, err := bootstrap.RunCapture(ctx, "docker", "info"); err != nil {
				fmt.Fprintf(os.Stderr, "  WARNING: docker daemon not reachable\n")
			}

			// Check kubectl connectivity
			ctxName, _ := bootstrap.RunCapture(ctx, "kubectl", "config", "current-context")
			if ctxName == "" {
				fmt.Println("  no kubectl context available")
				if *autoKind {
					fmt.Println("=== creating kind cluster ===")
					if err := createKindCluster(ctx, *dryRun, *kindWorkers); err != nil {
						return fmt.Errorf("creating kind cluster: %w", err)
					}
					if !*dryRun {
						ensureDockerNetwork(ctx, "kind", *dryRun)
					}
				}
			} else {
				fmt.Printf("  kubectl context: %s\n", ctxName)
				if strings.HasPrefix(ctxName, "kind-") {
					ensureDockerNetwork(ctx, "kind", *dryRun)
				}
			}

			// Load or generate encryption key
			fmt.Println("=== MonoFS encryption key ===")
			key, err := bootstrap.LoadOrGenerateEncryptionKey()
			if err != nil {
				return fmt.Errorf("encryption key: %w", err)
			}
			fmt.Printf("  persistent: %s\n", bootstrap.PersistentKeyPath())
			if *dryRun {
				fmt.Printf("  would write key to ../monofs/.env and shared partition\n")
			} else {
				if err := bootstrap.WriteMonofsEnv(key); err != nil {
					return fmt.Errorf("writing monofs .env: %w", err)
				}
				fmt.Println("  written to ../monofs/.env")
				if err := bootstrap.WriteSharedPartitionSecrets(key, ""); err != nil {
					return fmt.Errorf("writing shared partition secrets: %w", err)
				}
				fmt.Println("  written to stratatools/partitions/shared/secrets/")
			}

			fmt.Println("=== setup complete ===")
			return nil
		},
	}
}

// --- Helpers ---

func loadBootstrapConfig(configPath string) (*bootstrap.Config, error) {
	if configPath == "" {
		configPath = findConfigPath()
	}
	return bootstrap.LoadConfig(configPath)
}

func findConfigPath() string {
	// Try common locations
	candidates := []string{
		"deploy/bootstrap/bootstrap.yaml",
		"../stratatools/deploy/bootstrap/bootstrap.yaml",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	// Default
	root := findRootDir()
	return filepath.Join(root, "deploy", "bootstrap", "bootstrap.yaml")
}

func findRootDir() string {
	if r := os.Getenv("ST_ROOT"); r != "" {
		if strings.HasPrefix(r, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				r = filepath.Join(home, r[2:])
			}
		}
		return r
	}
	// Search from CWD first, then from binary path.
	cwd, _ := os.Getwd()
	if dir := findRootFrom(cwd); dir != "." {
		return dir
	}
	if exe, err := os.Executable(); err == nil {
		if dir := findRootFrom(filepath.Dir(exe)); dir != "." {
			return dir
		}
	}
	return "."
}

func findRootFrom(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "deploy", "bootstrap")); err == nil {
			return dir
		}
		// Check nested patterns first (more specific > less specific)
		for _, sub := range []string{"strata/stratatools", "projects/stratatools", "src/stratatools"} {
			if _, err := os.Stat(filepath.Join(dir, sub, "pyproject.toml")); err == nil {
				return filepath.Join(dir, sub)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "stratatools", "pyproject.toml")); err == nil {
			return filepath.Join(dir, "stratatools")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}

func deployStorageTemplates(ctx context.Context, cfg *bootstrap.Config, env bootstrap.Env, storageDir string, dryRun bool) error {
	ns := cfg.Storage.Namespace
	_ = cfg.Storage.NodeNames // used indirectly via ComputeNodeEnv
	routerSuffixes := cfg.Storage.RouterSuffixes

	fmt.Printf("=== deploying storage to namespace %s ===\n", ns)

	// Namespace and secrets
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "namespace.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "secret.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "rbac-repowatch.yaml"), env, nil, dryRun)

	// ConfigMaps
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "configmap-fetcher-s3.yaml"), env, nil, dryRun)

	// FUSE device plugin — must be deployed early so nodes expose sretoolhub.com/fuse
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "k8s-fuse-plugin.yaml"), env, nil, dryRun)

	// PVCs
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "pvc-minio.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplatesForEach(ctx, filepath.Join(storageDir, "pvc-fetcher.yaml"), env,
		func(s string) bootstrap.Env { return bootstrap.Env{"SUFFIX": s} },
		[]string{"a", "b"}, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "pvc-search-index.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplatesForEach(ctx, filepath.Join(storageDir, "pvc-node.yaml"), env,
		func(s string) bootstrap.Env { return bootstrap.Env{"SUFFIX": s} },
		[]string{"a", "b", "c", "d", "e"}, dryRun)

	// Deployments
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "deploy-minio.yaml"), env, nil, dryRun)

	// Wait for MinIO to be ready before deploying fetchers
	if !dryRun {
		if err := bootstrap.WaitForDeployment(ctx, ns, "minio", dryRun); err != nil {
			return fmt.Errorf("waiting for minio: %w", err)
		}
	}
	bootstrap.ApplyTemplatesForEach(ctx, filepath.Join(storageDir, "deploy-fetcher.yaml"), env,
		func(s string) bootstrap.Env { return bootstrap.Env{"SUFFIX": s} },
		[]string{"a", "b"}, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "deploy-search-index.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplatesForEach(ctx, filepath.Join(storageDir, "deploy-node.yaml"), env,
		func(s string) bootstrap.Env {
			return bootstrap.ComputeNodeEnv(cfg, s)
		},
		[]string{"a", "b", "c", "d", "e"}, dryRun)
	bootstrap.ApplyTemplatesForEach(ctx, filepath.Join(storageDir, "deploy-router.yaml"), env,
		func(s string) bootstrap.Env {
			return bootstrap.ComputeRouterEnv(cfg, s, "")
		},
		routerSuffixes, dryRun)

	// LB-edge namespace and RBAC
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "ns-lb-edge.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "rbac-lb-agent.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "configmap-lb-port-sync.yaml"), env, nil, dryRun)

	// LB-edge deployments
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "deploy-lb-k8s-agent.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "deploy-lb-port-sync.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "deploy-haproxy.yaml"), env, nil, dryRun)

	// Services
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "svc-minio.yaml"), env, nil, dryRun)
	createMinioBucket(ctx, cfg.Storage.Namespace, dryRun)
	bootstrap.ApplyTemplatesForEach(ctx, filepath.Join(storageDir, "svc-fetcher.yaml"), env,
		func(s string) bootstrap.Env { return bootstrap.Env{"SUFFIX": s} },
		[]string{"a", "b"}, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "svc-search-index.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplatesForEach(ctx, filepath.Join(storageDir, "svc-node.yaml"), env,
		func(s string) bootstrap.Env {
			return bootstrap.ComputeNodeEnv(cfg, s)
		},
		[]string{"a", "b", "c", "d", "e"}, dryRun)
	bootstrap.ApplyTemplatesForEach(ctx, filepath.Join(storageDir, "svc-router.yaml"), env,
		func(s string) bootstrap.Env { return bootstrap.Env{"SUFFIX": s} },
		routerSuffixes, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "svc-haproxy.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "deploy-registry.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "svc-registry.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(storageDir, "svc-registry-external.yaml"), env, nil, dryRun)

	return nil
}

func deploysStorage() []string {
	return []string{
		"minio", "fetcher-a", "fetcher-b", "search-index",
		"node-a", "node-b", "node-c", "node-d", "node-e",
		"router-a", "router-b", "monofs-registry",
	}
}

func createMinioBucket(ctx context.Context, storageNamespace string, dryRun bool) {
	awsAccessKey := os.Getenv("MINIO_ACCESS_KEY")
	if awsAccessKey == "" {
		awsAccessKey = "minioadmin"
	}
	awsSecretKey := os.Getenv("MINIO_SECRET_KEY")
	if awsSecretKey == "" {
		awsSecretKey = "minioadmin"
	}

	// Pre-load the mc image into kind so the ephemeral pod can use it.
	// Use bitnamilegacy because Docker Hub/quay require authentication for minio/mc.
	mcImage := "docker.io/bitnamilegacy/minio-client:latest"
	ctxName, _ := bootstrap.RunCapture(ctx, "kubectl", "config", "current-context")
	if strings.HasPrefix(ctxName, "kind-") && !dryRun {
		fmt.Fprintf(os.Stderr, "  loading %s into kind...\n", mcImage)
		if err := bootstrap.EnsureKindImage(ctx, mcImage, ""); err != nil {
			fmt.Fprintf(os.Stderr, "  WARNING: could not load minio/mc into kind: %v\n", err)
		}
	}

	script := fmt.Sprintf(
		`mc alias set local http://minio.%s.svc.cluster.local:9000 %s %s && mc mb --ignore-existing local/monofs`,
		storageNamespace, awsAccessKey, awsSecretKey,
	)
	if dryRun {
		_ = bootstrap.Run(ctx, dryRun, "kubectl", "-n", storageNamespace,
			"run", "minio-init-bucket", "--rm", "-i", "--restart=Never",
			"--image="+mcImage, "--command", "--", "sh", "-c", script)
		return
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			wait := time.Duration(attempt) * 2 * time.Second
			fmt.Fprintf(os.Stderr, "  retrying minio bucket init in %v (attempt %d/5)...\n", wait, attempt+1)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return
			}
		}
		lastErr = bootstrap.Run(ctx, dryRun, "kubectl", "-n", storageNamespace,
			"run", "minio-init-bucket", "--rm", "-i", "--restart=Never",
			"--image="+mcImage, "--command", "--", "sh", "-c", script)
		if lastErr == nil {
			fmt.Fprintf(os.Stderr, "  minio bucket monofs ready\n")
			return
		}
	}
	fmt.Fprintf(os.Stderr, "  WARNING: minio bucket bootstrap failed after retries: %v\n", lastErr)
}

func waitStorageDeployments(ctx context.Context, cfg *bootstrap.Config, dryRun bool) error {
	for _, d := range deploysStorage() {
		if err := bootstrap.WaitForDeployment(ctx, cfg.Storage.Namespace, d, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func waitLBEdgeDeployments(ctx context.Context, cfg *bootstrap.Config, dryRun bool) error {
	if err := bootstrap.WaitForDeployment(ctx, cfg.LB.Namespace, "monofs-haproxy", dryRun); err != nil {
		return err
	}
	return nil
}

func deployGuardianTemplates(ctx context.Context, cfg *bootstrap.Config, env bootstrap.Env, guardianDir string, dryRun bool) error {
	ns := cfg.Guardian.Namespace

	// Resolve the local registry ClusterIP for hostAliases
	if env["LOCAL_REGISTRY_HOST_ALIASES"] == "" {
		env["LOCAL_REGISTRY_HOST_ALIASES"] = bootstrap.ComputeLocalRegistryHostAliases(cfg)
	}

	fmt.Printf("=== deploying guardian to namespace %s ===\n", ns)

	bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "namespace.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "secret.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "rbac.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "svc-guardian-ui.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "deploy-guardiand.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "deploy-pusher-k8s.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "deploy-buildkitd.yaml"), env, nil, dryRun)
	bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "svc-buildkitd.yaml"), env, nil, dryRun)

	if cfg.Guardian.Pushers.AWS.Enabled {
		bootstrap.ApplyTemplate(ctx, filepath.Join(guardianDir, "deploy-pusher-aws.yaml"), env, nil, dryRun)
	}

	// Deploy docker pusher as Docker container (outside K8s)
	_ = deployDockerPusher(ctx, cfg, dryRun)

	// Stamp URLs into partition configs
	_ = bootstrapStampURLsSilent(ctx, cfg, dryRun)

	return nil
}

func deployDockerPusher(ctx context.Context, cfg *bootstrap.Config, dryRun bool) error {
	fmt.Println("=== deploying docker pusher as Docker container ===")
	lbNS := cfg.LB.Namespace
	router := bootstrap.LbEdgeEndpoint(lbNS, "grpc", "9090")
	if router == "" {
		router = "127.0.0.1:9090"
	}

	token := ""
	if !dryRun {
		token = bootstrap.KubectlGetSecretData(cfg.Guardian.Namespace, "guardian-secrets", "monofs-token")
	}

	name := "guardian-pusher-docker"
	image := cfg.Guardian.Images.PusherDocker

	stateDir, err := dockerPusherStateDir()
	if err != nil {
		return err
	}
	if !dryRun {
		if err := os.MkdirAll(stateDir, 0o770); err != nil {
			return fmt.Errorf("create docker pusher state dir %q: %w", stateDir, err)
		}
		// MkdirAll is subject to umask, so make the group access explicit:
		// the container's non-root user is granted this group below.
		if err := os.Chmod(stateDir, 0o770); err != nil {
			return fmt.Errorf("chmod docker pusher state dir %q: %w", stateDir, err)
		}
	}

	// Remove existing container
	bootstrap.Run(ctx, dryRun, "docker", "rm", "-f", name)

	args := []string{
		"run", "-d",
		"--restart=unless-stopped",
		"--name", name,
		"--network", "host",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		// Mount the state dir at the same absolute path on both sides: the
		// host Docker daemon resolves bind-mount sources on the host, so
		// staged config files must be visible there at the path the pusher
		// passes to `docker run`.
		"-v", fmt.Sprintf("%s:%s", stateDir, stateDir),
		"-e", fmt.Sprintf("GUARDIAN_STATE_DIR=%s", stateDir),
		"-e", fmt.Sprintf("GUARDIAN_PUSHER_NAME=%s", cfg.Guardian.Pushers.Docker.Name),
		"-e", "GUARDIAN_CLUSTER=docker-main",
		"-e", fmt.Sprintf("GUARDIAN_MONOFS_ROUTER=%s", router),
		"-e", fmt.Sprintf("GUARDIAN_MONOFS_TOKEN=%s", token),
		"-e", "GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES=true",
	}
	// The pusher runs as the image's non-root guardian user, so grant it the
	// socket's and state directory's owning groups.
	groupIDs := make([]int, 0, 2)
	if gid, ok := fileGroupID("/var/run/docker.sock"); ok {
		groupIDs = append(groupIDs, gid)
	}
	if gid, ok := fileGroupID(stateDir); ok {
		groupIDs = append(groupIDs, gid)
	}
	seen := make(map[int]bool, len(groupIDs))
	for _, gid := range groupIDs {
		if seen[gid] {
			continue
		}
		seen[gid] = true
		args = append(args, "--group-add", strconv.Itoa(gid))
	}
	args = append(args, image)
	return bootstrap.Run(ctx, dryRun, "docker", args...)
}

// dockerPusherStateDir returns the host-visible directory used to stage
// Docker config and inline files. GUARDIAN_DOCKER_STATE_DIR overrides the
// default under the current user's home directory.
func dockerPusherStateDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("GUARDIAN_DOCKER_STATE_DIR")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for docker pusher state: %w", err)
	}
	return filepath.Join(home, ".guardian", "pusher-docker"), nil
}

// fileGroupID returns the group ID that owns path.
func fileGroupID(path string) (int, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Gid), true
}

func bootstrapStampURLsSilent(ctx context.Context, cfg *bootstrap.Config, dryRun bool) error {
	lbNS := cfg.LB.Namespace
	uiPort := cfg.Guardian.UIPort
	root := findRootDir()

	uiEndpoint := bootstrap.LbEdgeEndpoint(lbNS, "guardian-ui", uiPort)
	guardianUIURL := ""
	if uiEndpoint != "" {
		guardianUIURL = "http://" + uiEndpoint
	}

	monofsGRPCEndpoint := bootstrap.LbEdgeEndpoint(lbNS, "grpc", "9090")
	if monofsGRPCEndpoint == "" {
		monofsGRPCEndpoint = cfg.Guardian.Monofs.Router
	}

	if dryRun || root == "." {
		return nil
	}

	gcp := filepath.Join(root, "partitions", "guardian-configs", "intents", "guardian-control-plane.yaml")
	gdp := filepath.Join(root, "partitions", "guardian-configs", "intents", "guardian-docker-pusher.yaml")
	gcfg := filepath.Join(root, "partitions", "guardian-configs", "config.yaml")
	dq := filepath.Join(root, "partitions", "doctor", "intents", "query.yaml")
	dcfg := filepath.Join(root, "partitions", "doctor", "config.yaml")

	if guardianUIURL != "" {
		bootstrap.SetEnvInIntent(gcp, "GUARDIAN_UI_BASE_URL", guardianUIURL)
		bootstrap.SetTopLevelKey(gcfg, "guardian_ui_base_url", guardianUIURL)
		bootstrap.SetEnvInIntent(dq, "GUARDIAN_UI_BASE_URL", guardianUIURL)
	}

	if monofsGRPCEndpoint != "" {
		bootstrap.SetEnvInIntent(gcp, "GUARDIAN_MONOFS_CLIENT_API_ENDPOINT", monofsGRPCEndpoint)
		bootstrap.SetEnvInIntent(gdp, "GUARDIAN_MONOFS_ROUTER", monofsGRPCEndpoint)
	}

	dip := bootstrap.KubectlGetServiceIP(cfg.Guardian.Namespace, "doctor-query-external")
	if dip != "" {
		bootstrap.SetTopLevelKey(dcfg, "doctor_query_base_url", "http://"+dip+":8080")
	}

	return nil
}

func createKindCluster(ctx context.Context, dryRun bool, workers int) error {
	clusterName := os.Getenv("KIND_CLUSTER_NAME")
	if clusterName == "" {
		clusterName = "strata"
	}

	// Write kind config as YAML
	config := `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
nodes:
  - role: control-plane
    extraMounts:
      - hostPath: /var/run/docker.sock
        containerPath: /var/run/docker.sock
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
    extraPortMappings:
      - containerPort: 8080
        hostPort: 8080
        protocol: TCP
      - containerPort: 8090
        hostPort: 8090
        protocol: TCP
      - containerPort: 9090
        hostPort: 9090
        protocol: TCP
      - containerPort: 9091
        hostPort: 9091
        protocol: TCP
      - containerPort: 15051
        hostPort: 15051
        protocol: TCP
      - containerPort: 5000
        hostPort: 5000
        protocol: TCP
      - containerPort: 18081
        hostPort: 18081
        protocol: TCP
      - containerPort: 9002
        hostPort: 9002
        protocol: TCP
      - containerPort: 9003
        hostPort: 9003
        protocol: TCP
      - containerPort: 9004
        hostPort: 9004
        protocol: TCP
      - containerPort: 9005
        hostPort: 9005
        protocol: TCP
      - containerPort: 9006
        hostPort: 9006
        protocol: TCP
      - containerPort: 8888
        hostPort: 8888
        protocol: TCP
      - containerPort: 9191
        hostPort: 9191
        protocol: TCP
      - containerPort: 3000
        hostPort: 3000
        protocol: TCP
      - containerPort: 4317
        hostPort: 4317
        protocol: TCP
      - containerPort: 18080
        hostPort: 18080
        protocol: TCP
  - role: worker
    extraMounts:
      - hostPath: /var/run/docker.sock
        containerPath: /var/run/docker.sock
`

	// Add additional workers
	for i := 1; i < workers; i++ {
		config += `  - role: worker
    extraMounts:
      - hostPath: /var/run/docker.sock
        containerPath: /var/run/docker.sock
`
	}

	tmpFile := filepath.Join(os.TempDir(), "kind-config.yaml")
	if !dryRun {
		if err := os.WriteFile(tmpFile, []byte(config), 0644); err != nil {
			return err
		}
		defer os.Remove(tmpFile)
	}

	fmt.Printf("creating kind cluster '%s' with %d workers...\n", clusterName, workers)
	return bootstrap.Run(ctx, dryRun, "kind", "create", "cluster", "--name", clusterName, "--config", tmpFile)
}

// ensureDockerNetwork verifies a Docker network exists; creates it if missing.
func ensureDockerNetwork(ctx context.Context, network string, dryRun bool) {
	if dryRun {
		fmt.Printf("  would ensure docker network %s exists\n", network)
		return
	}
	exists, _ := bootstrap.RunCapture(ctx, "docker", "network", "inspect", network)
	if strings.TrimSpace(exists) != "" {
		return
	}
	fmt.Printf("  creating docker network %s\n", network)
	if err := bootstrap.Run(ctx, dryRun, "docker", "network", "create", network); err != nil {
		fmt.Fprintf(os.Stderr, "  WARNING: docker network create %s failed: %v\n", network, err)
	}
}

// --- Dev tag management ---

// resolveDevTag returns a unique build tag preferring the monofs repo short git
// SHA so the tag changes whenever monofs source changes (invalidating Docker cache).
// Falls back to any sibling repo SHA, then a timestamp.
func resolveDevTag(ctx context.Context) string {
	if sha, _ := bootstrap.RunCapture(ctx, "git", "rev-parse", "--short", "HEAD"); sha != "" {
		return sha
	}
	root := findRootDir()
	if root == "." {
		return time.Now().UTC().Format("20060102-150405")
	}

	// Prefer monofs repo SHA so tag changes when monofs source changes.
	monofsDir := filepath.Join(root, "monofs")
	if info, err := os.Stat(filepath.Join(monofsDir, ".git")); err == nil && info.IsDir() {
		if sha, _ := bootstrap.RunCapture(ctx, "git", "-C", monofsDir, "rev-parse", "--short", "HEAD"); sha != "" {
			return sha
		}
	}

	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == "monofs" {
				continue
			}
			gitDir := filepath.Join(root, entry.Name(), ".git")
			if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
				if sha, _ := bootstrap.RunCapture(ctx, "git", "-C", filepath.Join(root, entry.Name()), "rev-parse", "--short", "HEAD"); sha != "" {
					return sha
				}
			}
		}
	}
	return time.Now().UTC().Format("20060102-150405")
}

// writeDevTag persists the current dev tag to deploy/bootstrap/.dev-tag.
func writeDevTag(tag string) {
	root := findRootDir()
	if root == "." {
		return
	}
	if err := os.WriteFile(filepath.Join(root, "deploy", "bootstrap", ".dev-tag"), []byte(tag+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: write .dev-tag: %v\n", err)
	}
}

// readDevTag reads the persisted dev tag.
func readDevTag() string {
	root := findRootDir()
	if root == "." {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, "deploy", "bootstrap", ".dev-tag"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// applyDevTag replaces :latest with :<tag> on all locally-built image references.
func applyDevTag(cfg *bootstrap.Config, tag string) {
	suffix := ":" + tag
	s := &cfg.Storage.Images
	s.Server = strings.Replace(s.Server, ":latest", suffix, 1)
	s.Router = strings.Replace(s.Router, ":latest", suffix, 1)
	s.Fetcher = strings.Replace(s.Fetcher, ":latest", suffix, 1)
	s.Search = strings.Replace(s.Search, ":latest", suffix, 1)
	s.Registry = strings.Replace(s.Registry, ":latest", suffix, 1)
	s.LB = strings.Replace(s.LB, ":latest", suffix, 1)
	s.LogQuery = strings.Replace(s.LogQuery, ":latest", suffix, 1)
	g := &cfg.Guardian.Images
	g.Guardiand = strings.Replace(g.Guardiand, ":latest", suffix, 1)
	g.PusherK8s = strings.Replace(g.PusherK8s, ":latest", suffix, 1)
	g.PusherAws = strings.Replace(g.PusherAws, ":latest", suffix, 1)
	g.PusherDocker = strings.Replace(g.PusherDocker, ":latest", suffix, 1)
	g.LB = strings.Replace(g.LB, ":latest", suffix, 1)
}

func findBootstrapDir() string {
	bootstrapName := "_bootstrap"
	root := findRootDir()
	if root != "." {
		dir := filepath.Join(root, "partitions", bootstrapName)
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	return ""
}
