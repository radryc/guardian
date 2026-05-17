package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	cliformat "github.com/rydzu/ainfra/guardian/internal/cli/format"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"github.com/rydzu/ainfra/guardian/internal/compiler/manifest"
	"github.com/rydzu/ainfra/guardian/internal/compiler/planner"
	"github.com/rydzu/ainfra/guardian/internal/compiler/validator"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	historydomain "github.com/rydzu/ainfra/guardian/internal/domain/history"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	"github.com/rydzu/ainfra/guardian/internal/historyquery"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	fsstore "github.com/rydzu/ainfra/guardian/internal/store/fs"
	monofsstore "github.com/rydzu/ainfra/guardian/internal/store/monofs"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

const (
	defaultLocalGuardianURL      = "http://127.0.0.1:18090"
	defaultLocalMonoFSRouter     = "127.0.0.1:9090"
	defaultLocalMonoFSToken      = "guardian-dev-token"
	guardianDiscoveryTokenHeader = "X-Guardian-Discovery-Token"
)

var (
	guardianDiscoveryHTTPClient = &http.Client{Timeout: 500 * time.Millisecond}
	guardianDiscoveryURL        = defaultLocalGuardianURL
	guardianStoreOpener         = openStore
	rolloutTagNow               = func() time.Time { return time.Now().UTC() }
)

type autoStoreRequest struct {
	storeDir                      string
	monofsRouter                  string
	monofsToken                   string
	monofsPrincipalID             string
	monofsUseExternalAddresses    bool
	monofsUseExternalAddressesSet bool
	guardianURL                   string
	guardianDiscoveryToken        string
}

type resolvedStoreConfig struct {
	storeDir                   string
	monofsRouter               string
	monofsToken                string
	monofsPrincipalID          string
	monofsUseExternalAddresses bool
}

type guardianClientConfig struct {
	MonoFS *guardianMonoFSClientConfig `json:"monofs,omitempty"`
}

type guardianMonoFSClientConfig struct {
	RouterAddr           string `json:"routerAddr,omitempty"`
	Token                string `json:"token,omitempty"`
	UseExternalAddresses bool   `json:"useExternalAddresses"`
}

type lazyStore struct {
	mu     sync.Mutex
	opened bool
	store  guardianapi.Store
	closer interface{ Close() error }
	err    error
	opener func(context.Context) (guardianapi.Store, interface{ Close() error }, error)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootFlags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	configureRootUsage(rootFlags)
	storeDir := rootFlags.String("store-dir", "", "filesystem-backed Guardian store")
	monofsRouter := rootFlags.String("monofs-router", "", "MonoFS router gRPC address reachable from this machine")
	monofsToken := rootFlags.String("monofs-token", "", "MonoFS guardian token")
	monofsPrincipalID := rootFlags.String("monofs-principal-id", "guardianctl", "MonoFS guardian principal ID")
	monofsUseExternalAddresses := rootFlags.Bool("monofs-use-external-addresses", false, "prefer MonoFS external addresses when refreshing cluster info")
	guardianURL := rootFlags.String("guardian-url", firstNonEmptyEnv("GUARDIAN_URL", "GUARDIAN_API_URL"), "Guardian UI/API base URL used to auto-discover MonoFS access for local port-forwards, load balancers, or ingress")
	guardianDiscoveryToken := rootFlags.String("guardian-discovery-token", firstNonEmptyEnv("GUARDIAN_DISCOVERY_TOKEN", "GUARDIAN_CLIENT_DISCOVERY_TOKEN"), "token for authenticated Guardian discovery when the UI is exposed beyond loopback")
	formatFlag := rootFlags.String("format", string(cliformat.FormatText), "output format: text|json")
	if err := rootFlags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *storeDir != "" && *monofsRouter != "" {
		fmt.Fprintln(os.Stderr, "provide at most one of --store-dir or --monofs-router")
		os.Exit(2)
	}

	store := newAutoStore(autoStoreRequest{
		storeDir:                      *storeDir,
		monofsRouter:                  *monofsRouter,
		monofsToken:                   *monofsToken,
		monofsPrincipalID:             *monofsPrincipalID,
		monofsUseExternalAddresses:    *monofsUseExternalAddresses,
		monofsUseExternalAddressesSet: flagWasProvided(rootFlags, "monofs-use-external-addresses"),
		guardianURL:                   *guardianURL,
		guardianDiscoveryToken:        *guardianDiscoveryToken,
	})
	var storeCloser interface{ Close() error } = store
	if storeCloser != nil {
		defer func() {
			if err := storeCloser.Close(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}()
	}
	printer := &output.Printer{Format: cliformat.Format(*formatFlag), Writer: os.Stdout}
	reg := registerCommands(store, printer)
	if err := reg.Run(ctx, rootFlags.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func openStore(ctx context.Context, storeDir, monofsRouter, monofsToken, monofsPrincipalID string, monofsUseExternalAddresses bool) (guardianapi.Store, interface{ Close() error }, error) {
	if storeDir == "" && monofsRouter == "" {
		// No store configured — commands that require a store will fail when
		// they attempt to use it.
		return nil, nil, nil
	}
	if monofsRouter != "" {
		store, client, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
			RouterAddr:           monofsRouter,
			Token:                monofsToken,
			PrincipalID:          monofsPrincipalID,
			Role:                 "cli",
			ClientIDPrefix:       "guardian-cli",
			Version:              "guardianctl",
			UseExternalAddresses: monofsUseExternalAddresses,
			Writable:             true,
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

func newAutoStore(req autoStoreRequest) *lazyStore {
	return &lazyStore{opener: func(ctx context.Context) (guardianapi.Store, interface{ Close() error }, error) {
		resolved, err := resolveStoreConfig(ctx, req)
		if err != nil {
			return nil, nil, err
		}
		return guardianStoreOpener(ctx, resolved.storeDir, resolved.monofsRouter, resolved.monofsToken, resolved.monofsPrincipalID, resolved.monofsUseExternalAddresses)
	}}
}

func resolveStoreConfig(ctx context.Context, req autoStoreRequest) (resolvedStoreConfig, error) {
	resolved := resolvedStoreConfig{monofsPrincipalID: strings.TrimSpace(req.monofsPrincipalID)}
	if resolved.monofsPrincipalID == "" {
		resolved.monofsPrincipalID = "guardianctl"
	}
	if strings.TrimSpace(req.storeDir) != "" && strings.TrimSpace(req.monofsRouter) != "" {
		return resolvedStoreConfig{}, fmt.Errorf("provide at most one of --store-dir or --monofs-router")
	}
	if strings.TrimSpace(req.storeDir) != "" {
		resolved.storeDir = strings.TrimSpace(req.storeDir)
		return resolved, nil
	}

	explicitMonofs := strings.TrimSpace(req.monofsRouter) != "" || strings.TrimSpace(req.monofsToken) != "" || req.monofsUseExternalAddressesSet
	if !explicitMonofs && strings.TrimSpace(req.guardianURL) == "" {
		envStoreDir := strings.TrimSpace(os.Getenv("GUARDIAN_STORE_DIR"))
		if envStoreDir != "" {
			resolved.storeDir = envStoreDir
			return resolved, nil
		}
	}

	if envRouter := strings.TrimSpace(os.Getenv("GUARDIAN_MONOFS_ROUTER")); envRouter != "" {
		resolved.monofsRouter = envRouter
	}
	if envToken := strings.TrimSpace(os.Getenv("GUARDIAN_MONOFS_TOKEN")); envToken != "" {
		resolved.monofsToken = envToken
	}
	if envPrincipal := strings.TrimSpace(os.Getenv("GUARDIAN_MONOFS_PRINCIPAL")); envPrincipal != "" && strings.TrimSpace(req.monofsPrincipalID) == "" {
		resolved.monofsPrincipalID = envPrincipal
	}
	envUseExternal, envUseExternalSet, err := parseBoolEnv("GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES")
	if err != nil {
		return resolvedStoreConfig{}, err
	}
	if envUseExternalSet {
		resolved.monofsUseExternalAddresses = envUseExternal
	}

	guardianURL := strings.TrimSpace(req.guardianURL)
	if guardianURL == "" {
		guardianURL = firstNonEmptyEnv("GUARDIAN_URL", "GUARDIAN_API_URL")
	}
	guardianDiscoveryToken := strings.TrimSpace(req.guardianDiscoveryToken)
	if guardianDiscoveryToken == "" {
		guardianDiscoveryToken = firstNonEmptyEnv("GUARDIAN_DISCOVERY_TOKEN", "GUARDIAN_CLIENT_DISCOVERY_TOKEN")
	}
	explicitGuardianURL := guardianURL != ""
	if guardianURL == "" {
		guardianURL = guardianDiscoveryURL
	}
	if guardianURL != "" {
		clientConfig, err := discoverGuardianClientConfig(ctx, guardianURL, guardianDiscoveryToken)
		if err != nil {
			if explicitGuardianURL {
				return resolvedStoreConfig{}, err
			}
		} else if clientConfig.MonoFS != nil {
			if resolved.monofsRouter == "" {
				resolved.monofsRouter = strings.TrimSpace(clientConfig.MonoFS.RouterAddr)
			}
			if resolved.monofsToken == "" {
				resolved.monofsToken = strings.TrimSpace(clientConfig.MonoFS.Token)
			}
			if !envUseExternalSet {
				resolved.monofsUseExternalAddresses = clientConfig.MonoFS.UseExternalAddresses
			}
		}
	}

	if resolved.monofsRouter == "" {
		resolved.monofsRouter = defaultLocalMonoFSRouter
	}
	if resolved.monofsToken == "" {
		resolved.monofsToken = defaultLocalMonoFSToken
	}
	if !envUseExternalSet {
		resolved.monofsUseExternalAddresses = true
	}

	if strings.TrimSpace(req.monofsRouter) != "" {
		resolved.monofsRouter = strings.TrimSpace(req.monofsRouter)
	}
	if strings.TrimSpace(req.monofsToken) != "" {
		resolved.monofsToken = strings.TrimSpace(req.monofsToken)
	}
	if req.monofsUseExternalAddressesSet {
		resolved.monofsUseExternalAddresses = req.monofsUseExternalAddresses
	}
	return resolved, nil
}

func discoverGuardianClientConfig(ctx context.Context, baseURL, discoveryToken string) (guardianClientConfig, error) {
	normalized, err := normalizeGuardianURL(baseURL)
	if err != nil {
		return guardianClientConfig{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized+"/api/client-config", nil)
	if err != nil {
		return guardianClientConfig{}, err
	}
	if token := strings.TrimSpace(discoveryToken); token != "" {
		request.Header.Set(guardianDiscoveryTokenHeader, token)
	}
	response, err := guardianDiscoveryHTTPClient.Do(request)
	if err != nil {
		return guardianClientConfig{}, fmt.Errorf("guardian discovery at %s failed: %w\n\n%s", normalized, err, guardianKubernetesDiscoveryHelp())
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return guardianClientConfig{}, guardianDiscoveryHTTPError(normalized, response)
	}
	var config guardianClientConfig
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
		return guardianClientConfig{}, fmt.Errorf("decode guardian client config: %w", err)
	}
	if config.MonoFS == nil || strings.TrimSpace(config.MonoFS.RouterAddr) == "" {
		return guardianClientConfig{}, fmt.Errorf("guardian discovery at %s did not return a usable MonoFS router address\n\nSet monofs.clientApiEndpoint in guardiand to an address reachable from the machine running guardianctl.\n\n%s", normalized, guardianKubernetesDiscoveryHelp())
	}
	return config, nil
}

func guardianDiscoveryHTTPError(baseURL string, response *http.Response) error {
	var apiErr struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(response.Body).Decode(&apiErr)
	detail := strings.TrimSpace(apiErr.Error)
	if detail != "" {
		detail = ": " + detail
	}
	switch response.StatusCode {
	case http.StatusForbidden:
		return fmt.Errorf("guardian discovery at %s was forbidden%s\n\nIf Guardian is exposed through Kubernetes, either:\n- use a local port-forward so the request comes from loopback, or\n- configure guardian.clientDiscoveryToken in guardiand and pass --guardian-discovery-token (or GUARDIAN_DISCOVERY_TOKEN) to guardianctl.\n\n%s", baseURL, detail, guardianKubernetesDiscoveryHelp())
	case http.StatusNotFound:
		return fmt.Errorf("guardian discovery at %s is unavailable%s\n\nMake sure guardiand is serving /api/client-config and has MonoFS client config enabled.\n\n%s", baseURL, detail, guardianKubernetesDiscoveryHelp())
	default:
		return fmt.Errorf("guardian discovery at %s failed: %s%s\n\n%s", baseURL, response.Status, detail, guardianKubernetesDiscoveryHelp())
	}
}

func configureRootUsage(flags *flag.FlagSet) {
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintf(out, "Usage: %s [root flags] <group> <command> [command flags]\n\n", flags.Name())
		fmt.Fprintln(out, "Connection modes:")
		fmt.Fprintln(out, "  --store-dir DIR")
		fmt.Fprintln(out, "      Use a filesystem-backed Guardian store directly.")
		fmt.Fprintln(out, "  --monofs-router HOST:PORT --monofs-token TOKEN")
		fmt.Fprintln(out, "      Connect to MonoFS directly over gRPC.")
		fmt.Fprintln(out, "  --guardian-url URL [--guardian-discovery-token TOKEN]")
		fmt.Fprintln(out, "      Ask Guardian UI/API for MonoFS client settings, then connect to MonoFS.")
		fmt.Fprintln(out, "      Env aliases: GUARDIAN_URL or GUARDIAN_API_URL, and GUARDIAN_DISCOVERY_TOKEN or GUARDIAN_CLIENT_DISCOVERY_TOKEN.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Common workflows:")
		fmt.Fprintln(out, "  guardianctl partition list")
		fmt.Fprintln(out, "  guardianctl partition push --dir ./partitions/monofs-local")
		fmt.Fprintln(out, "  guardianctl partition reconcile --partition monofs-local")
		fmt.Fprintln(out, "  guardianctl partition wait --partition monofs-local")
		fmt.Fprintln(out, "  guardianctl partition delete --partition monofs-local")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Kubernetes setup for --guardian-url:")
		fmt.Fprintln(out, indentBlock(guardianKubernetesDiscoveryHelp(), "  "))
		fmt.Fprintln(out, "Root flags:")
		flags.PrintDefaults()
	}
}

func guardianKubernetesDiscoveryHelp() string {
	return strings.TrimSpace(`To use guardianctl from outside the cluster with --guardian-url:
- expose Guardian UI with an ingress, load balancer, NodePort, or kubectl port-forward
- expose MonoFS router gRPC so the machine running guardianctl can dial it
- set monofs.clientApiEndpoint in guardiand to the externally reachable MonoFS router address
- if Guardian UI is not loopback-only, set guardian.clientDiscoveryToken in guardiand and pass the same value via --guardian-discovery-token or GUARDIAN_DISCOVERY_TOKEN

Examples:
  guardianctl --guardian-url http://127.0.0.1:18090 partition list
  guardianctl --guardian-url https://guardian.example.com --guardian-discovery-token "$TOKEN" partition list
  guardianctl --monofs-router monofs.example.com:9090 --monofs-token "$TOKEN" partition list`)
}

func normalizeGuardianURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse guardian url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("guardian url must include a host")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func parseBoolEnv(key string) (bool, bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return false, false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, false, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, true, nil
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func flagWasProvided(flags *flag.FlagSet, name string) bool {
	provided := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func (l *lazyStore) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closer == nil {
		return nil
	}
	err := l.closer.Close()
	l.closer = nil
	return err
}

func (l *lazyStore) resolve(ctx context.Context) (guardianapi.Store, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.store != nil {
		return l.store, nil
	}
	if l.opened {
		return nil, l.err
	}
	l.opened = true
	store, closer, err := l.opener(ctx)
	if err != nil {
		l.err = err
		return nil, err
	}
	l.store = store
	l.closer = closer
	return l.store, nil
}

func (l *lazyStore) ReadFile(ctx context.Context, logicalPath string) ([]byte, error) {
	store, err := l.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.ReadFile(ctx, logicalPath)
}

func (l *lazyStore) ListDir(ctx context.Context, logicalDir string) ([]guardianapi.DirEntry, error) {
	store, err := l.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.ListDir(ctx, logicalDir)
}

func (l *lazyStore) Stat(ctx context.Context, logicalPath string) (guardianapi.FileInfo, error) {
	store, err := l.resolve(ctx)
	if err != nil {
		return guardianapi.FileInfo{}, err
	}
	return store.Stat(ctx, logicalPath)
}

func (l *lazyStore) Watch(ctx context.Context, prefixes []string) (<-chan guardianapi.ChangeEvent, error) {
	store, err := l.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.Watch(ctx, prefixes)
}

func (l *lazyStore) UpsertFiles(ctx context.Context, batch guardianapi.MutationBatch) (guardianapi.BatchRevision, error) {
	store, err := l.resolve(ctx)
	if err != nil {
		return guardianapi.BatchRevision{}, err
	}
	return store.UpsertFiles(ctx, batch)
}

func (l *lazyStore) DeletePaths(ctx context.Context, batch guardianapi.DeleteBatch) (guardianapi.BatchRevision, error) {
	store, err := l.resolve(ctx)
	if err != nil {
		return guardianapi.BatchRevision{}, err
	}
	return store.DeletePaths(ctx, batch)
}

func (l *lazyStore) ListVersions(ctx context.Context, logicalPath string) ([]guardianapi.FileVersion, error) {
	store, err := l.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return store.ListVersions(ctx, logicalPath)
}

func (l *lazyStore) GetVersion(ctx context.Context, logicalPath, versionID string) (guardianapi.VersionedFile, error) {
	store, err := l.resolve(ctx)
	if err != nil {
		return guardianapi.VersionedFile{}, err
	}
	return store.GetVersion(ctx, logicalPath, versionID)
}

const storeRequiredMessage = "a store is required; pass --store-dir or --monofs-router"

func ensureStoreConfigured(store any) error {
	if store == nil {
		return fmt.Errorf(storeRequiredMessage)
	}
	return nil
}

func requireStoreCommand(store guardianapi.Store, cmd *command.Command) *command.Command {
	if cmd == nil || cmd.Run == nil {
		return cmd
	}
	originalRun := cmd.Run
	cmd.Run = func(ctx context.Context, args []string) error {
		if err := ensureStoreConfigured(store); err != nil {
			return err
		}
		return originalRun(ctx, args)
	}
	return cmd
}

func registerCommands(store guardianapi.Store, printer *output.Printer) *command.Registry {
	reg := command.New()
	storeCommand := func(cmd *command.Command) *command.Command {
		return requireStoreCommand(store, cmd)
	}

	reg.Register("partition", "init", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("partition init", flag.ContinueOnError)
		flags.SetOutput(ioDiscard{})
		partitionName := flags.String("partition", "", "partition name")
		deletionPolicy := flags.String("deletion-policy", "orphan", "orphan|destroy")
		return &command.Command{Description: "Initialize a partition", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" {
				return fmt.Errorf("--partition is required")
			}
			part := partitiondomain.Partition{
				APIVersion: "guardian/v1alpha1",
				Kind:       "Partition",
				Metadata:   partitiondomain.Metadata{Name: *partitionName},
				Spec: partitiondomain.Spec{
					DeletionPolicy: *deletionPolicy,
					Reconciliation: partitiondomain.ReconciliationSpec{Mode: "auto", Interval: "30s"},
				},
			}
			if err := validator.ValidatePartition(&part); err != nil {
				return err
			}
			data, err := yaml.Marshal(part)
			if err != nil {
				return err
			}
			result, correlationID, err := writeFile(ctx, store, paths.PartitionConfig(*partitionName), data, revisions.NewCorrelationID(), "init partition")
			if err != nil {
				return err
			}
			printMutation(printer, result, paths.PartitionConfig(*partitionName), correlationID)
			return nil
		}}
	}()))

	reg.Register("partition", "set-config", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("partition set-config", flag.ContinueOnError)
		filePath := flags.String("file", "", "partition manifest file")
		return &command.Command{Description: "Create or update only the partition config from file", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *filePath == "" {
				return fmt.Errorf("--file is required")
			}
			data, err := os.ReadFile(filepath.Clean(*filePath))
			if err != nil {
				return err
			}
			parsed, err := manifest.ParsePartition(data)
			if err != nil {
				return err
			}
			if err := validator.ValidatePartition(parsed); err != nil {
				return err
			}
			normalized, err := yaml.Marshal(parsed)
			if err != nil {
				return err
			}
			result, correlationID, err := writeFile(ctx, store, paths.PartitionConfig(parsed.Metadata.Name), normalized, revisions.NewCorrelationID(), "put partition")
			if err != nil {
				return err
			}
			printMutation(printer, result, paths.PartitionConfig(parsed.Metadata.Name), correlationID)
			return nil
		}}
	}()))

	reg.Register("partition", "push", storeCommand(partitionPushCommand(store, printer)))
	reg.Register("partition", "reconcile", storeCommand(partitionReconcileCommand(store, printer)))
	reg.Register("partition", "tag", partitionTagCommand(printer))
	reg.Register("partition", "tags", storeCommand(partitionTagsCommand(store, printer)))
	reg.Register("partition", "rollback", storeCommand(partitionRollbackCommand(store, printer)))
	reg.Register("partition", "status", storeCommand(partitionStatusCommand(store, printer)))
	reg.Register("partition", "wait", storeCommand(partitionWaitCommand(store, printer)))

	reg.Register("partition", "list", storeCommand(&command.Command{Description: "List partitions", Flags: flag.NewFlagSet("partition list", flag.ContinueOnError), Run: func(ctx context.Context, args []string) error {
		names, err := configuredPartitionNames(ctx, store)
		if err != nil {
			return err
		}
		if printer.Format == cliformat.FormatJSON {
			printer.PrintJSON(names)
			return nil
		}
		rows := make([][]string, 0, len(names))
		for _, name := range names {
			rows = append(rows, []string{name})
		}
		printer.PrintTable([]string{"PARTITION"}, rows)
		return nil
	}}))

	reg.Register("partition", "get", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("partition get", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		return &command.Command{Description: "Get a partition manifest", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" {
				return fmt.Errorf("--partition is required")
			}
			data, err := store.ReadFile(ctx, paths.PartitionConfig(*partitionName))
			if err != nil {
				return err
			}
			if printer.Format == cliformat.FormatJSON {
				parsed, err := manifest.ParsePartition(data)
				if err != nil {
					return err
				}
				printer.PrintJSON(parsed)
				return nil
			}
			printer.PrintText("%s", string(data))
			return nil
		}}
	}()))

	reg.Register("partition", "delete", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("partition delete", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		return &command.Command{Description: "Delete a partition subtree", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" {
				return fmt.Errorf("--partition is required")
			}
			correlationID := revisions.NewCorrelationID()
			files, err := walkFiles(ctx, store, paths.PartitionRoot(*partitionName))
			if err != nil {
				return err
			}
			if len(files) > 0 {
				deletes := make([]guardianapi.PathDelete, 0, len(files))
				for _, file := range files {
					deletes = append(deletes, guardianapi.PathDelete{LogicalPath: file})
				}
				if _, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
					Deletes: deletes,
					Context: guardianapi.MutationContext{PrincipalID: "guardianctl", Reason: "delete partition files", CorrelationID: correlationID},
				}); err != nil {
					return err
				}
			}
			batch, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
				Deletes: []guardianapi.PathDelete{{LogicalPath: paths.PartitionRoot(*partitionName)}},
				Context: guardianapi.MutationContext{PrincipalID: "guardianctl", Reason: "delete partition", CorrelationID: correlationID},
			})
			if err != nil {
				return err
			}
			printMutation(printer, batch, paths.PartitionRoot(*partitionName), correlationID)
			return nil
		}}
	}()))

	reg.Register("intent", "put", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("intent put", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		filePath := flags.String("file", "", "intent manifest file")
		return &command.Command{Description: "Create or update an intent", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" || *filePath == "" {
				return fmt.Errorf("--partition and --file are required")
			}
			data, err := os.ReadFile(filepath.Clean(*filePath))
			if err != nil {
				return err
			}
			parsed, err := manifest.ParseIntent(data)
			if err != nil {
				return err
			}
			knownIntents, err := listIntentNames(ctx, store, *partitionName)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			knownIntents = append(knownIntents, parsed.Metadata.Name)
			sort.Strings(knownIntents)
			knownIntents = uniqueStrings(knownIntents)
			if err := validator.ValidateIntent(parsed, knownIntents, nil); err != nil {
				return err
			}
			normalized, err := yaml.Marshal(parsed)
			if err != nil {
				return err
			}
			result, correlationID, err := writeFile(ctx, store, paths.IntentManifest(*partitionName, parsed.Metadata.Name), normalized, revisions.NewCorrelationID(), "put intent")
			if err != nil {
				return err
			}
			printMutation(printer, result, paths.IntentManifest(*partitionName, parsed.Metadata.Name), correlationID)
			return nil
		}}
	}()))

	reg.Register("intent", "delete", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("intent delete", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		intentName := flags.String("intent", "", "intent name")
		return &command.Command{Description: "Delete an intent manifest", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" || *intentName == "" {
				return fmt.Errorf("--partition and --intent are required")
			}
			correlationID := revisions.NewCorrelationID()
			batch, err := store.DeletePaths(ctx, guardianapi.DeleteBatch{
				Deletes: []guardianapi.PathDelete{{LogicalPath: paths.IntentManifest(*partitionName, *intentName)}},
				Context: guardianapi.MutationContext{PrincipalID: "guardianctl", Reason: "delete intent", CorrelationID: correlationID},
			})
			if err != nil {
				return err
			}
			printMutation(printer, batch, paths.IntentManifest(*partitionName, *intentName), correlationID)
			return nil
		}}
	}()))

	reg.Register("intent", "list", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("intent list", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		return &command.Command{Description: "List intents in a partition", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" {
				return fmt.Errorf("--partition is required")
			}
			names, err := listIntentNames(ctx, store, *partitionName)
			if err != nil {
				return err
			}
			if printer.Format == cliformat.FormatJSON {
				printer.PrintJSON(names)
				return nil
			}
			rows := make([][]string, 0, len(names))
			for _, name := range names {
				rows = append(rows, []string{name})
			}
			printer.PrintTable([]string{"INTENT"}, rows)
			return nil
		}}
	}()))

	reg.Register("intent", "validate", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("intent validate", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		intentName := flags.String("intent", "", "intent name")
		return &command.Command{Description: "Validate an intent", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" || *intentName == "" {
				return fmt.Errorf("--partition and --intent are required")
			}
			data, err := store.ReadFile(ctx, paths.IntentManifest(*partitionName, *intentName))
			if err != nil {
				return err
			}
			parsed, err := manifest.ParseIntent(data)
			if err != nil {
				return err
			}
			names, err := listIntentNames(ctx, store, *partitionName)
			if err != nil {
				return err
			}
			if err := validator.ValidateIntent(parsed, names, nil); err != nil {
				return err
			}
			if printer.Format == cliformat.FormatJSON {
				printer.PrintJSON(map[string]any{"valid": true, "intent": *intentName})
				return nil
			}
			printer.PrintText("intent %s is valid\n", *intentName)
			return nil
		}}
	}()))

	reg.Register("intent", "plan", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("intent plan", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		return &command.Command{Description: "Compile a partition plan", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" {
				return fmt.Errorf("--partition is required")
			}
			compiled, err := compilePartition(ctx, store, *partitionName)
			if err != nil {
				return err
			}
			printer.PrintJSON(compiled)
			return nil
		}}
	}()))

	reg.Register("intent", "status", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("intent status", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		intentName := flags.String("intent", "", "intent name")
		return &command.Command{Description: "Show intent state", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" || *intentName == "" {
				return fmt.Errorf("--partition and --intent are required")
			}
			state, err := common.LoadIntentState(ctx, store, *partitionName, *intentName)
			if err != nil {
				return err
			}
			printer.PrintJSON(state)
			return nil
		}}
	}()))

	reg.Register("intent", "describe", storeCommand(intentDescribeCommand(store, printer)))

	reg.Register("intent", "outputs", storeCommand(func() *command.Command {
		flags := flag.NewFlagSet("intent outputs", flag.ContinueOnError)
		partitionName := flags.String("partition", "", "partition name")
		intentName := flags.String("intent", "", "intent name")
		return &command.Command{Description: "Show intent outputs", Flags: flags, Run: func(ctx context.Context, args []string) error {
			if *partitionName == "" || *intentName == "" {
				return fmt.Errorf("--partition and --intent are required")
			}
			state, err := common.LoadIntentState(ctx, store, *partitionName, *intentName)
			if err != nil {
				return err
			}
			printer.PrintJSON(state.Outputs)
			return nil
		}}
	}()))

	reg.Register("intent", "lock", storeCommand(lockCommand(store, printer, true)))
	reg.Register("intent", "unlock", storeCommand(lockCommand(store, printer, false)))

	reg.Register("asset", "catalog", assetCatalogCommand(printer))
	reg.Register("asset", "describe", assetDescribeCommand(store, printer))

	return reg
}

func assetCatalogCommand(printer *output.Printer) *command.Command {
	return &command.Command{Description: "List supported asset types", Flags: flag.NewFlagSet("asset catalog", flag.ContinueOnError), Run: func(ctx context.Context, args []string) error {
		catalog := assetdefs.Catalog()
		if printer.Format == cliformat.FormatJSON {
			printer.PrintJSON(catalog)
			return nil
		}
		rows := make([][]string, 0, len(catalog))
		for _, item := range catalog {
			rows = append(rows, []string{item.Type, item.Category, item.Title})
		}
		printer.PrintTable([]string{"TYPE", "CATEGORY", "TITLE"}, rows)
		return nil
	}}
}

type intentDescribeAsset struct {
	Name     string                  `json:"name"`
	Type     string                  `json:"type"`
	Manifest assetdomain.Spec        `json:"manifest"`
	Hints    []assetdefs.CatalogHint `json:"hints,omitempty"`
}

type intentDescribeResponse struct {
	Manifest    intentdomain.Intent     `json:"manifest"`
	OutputHints []assetdefs.CatalogHint `json:"outputHints,omitempty"`
	Assets      []intentDescribeAsset   `json:"assets,omitempty"`
}

func assetDescribeCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("asset describe", flag.ContinueOnError)
	assetType := flags.String("type", "", "asset type")
	partitionName := flags.String("partition", "", "partition name for manifest-aware hint overrides")
	intentName := flags.String("intent", "", "intent name for manifest-aware hint overrides")
	assetName := flags.String("asset", "", "asset name for manifest-aware hint overrides")
	return &command.Command{Description: "Show template fields and hints for an asset type", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*partitionName) != "" || strings.TrimSpace(*intentName) != "" || strings.TrimSpace(*assetName) != "" {
			if strings.TrimSpace(*partitionName) == "" || strings.TrimSpace(*intentName) == "" || strings.TrimSpace(*assetName) == "" {
				return fmt.Errorf("--partition, --intent, and --asset must be provided together")
			}
			parsed, err := loadIntentManifest(ctx, store, *partitionName, *intentName)
			if err != nil {
				return err
			}
			spec, ok := findIntentAsset(*parsed, *assetName)
			if !ok {
				return fmt.Errorf("asset %q not found in intent %q", *assetName, *intentName)
			}
			if strings.TrimSpace(*assetType) != "" && *assetType != spec.Type {
				return fmt.Errorf("asset %q has type %q, not %q", *assetName, spec.Type, *assetType)
			}
			item := mergedAssetCatalogTemplate(spec, parsed.Spec.Hints)
			if printer.Format == cliformat.FormatJSON {
				printer.PrintJSON(item)
				return nil
			}
			printAssetCatalogTemplate(printer, item)
			return nil
		}
		if strings.TrimSpace(*assetType) == "" {
			return fmt.Errorf("--type is required")
		}
		item, ok := assetdefs.CatalogFor(*assetType)
		if !ok {
			return fmt.Errorf("unsupported asset type %q", *assetType)
		}
		if printer.Format == cliformat.FormatJSON {
			printer.PrintJSON(item)
			return nil
		}
		printAssetCatalogTemplate(printer, item)
		return nil
	}}
}

func intentDescribeCommand(store guardianapi.Store, printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("intent describe", flag.ContinueOnError)
	partitionName := flags.String("partition", "", "partition name")
	intentName := flags.String("intent", "", "intent name")
	return &command.Command{Description: "Show manifest-aware output and asset hints for an intent", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*partitionName) == "" || strings.TrimSpace(*intentName) == "" {
			return fmt.Errorf("--partition and --intent are required")
		}
		parsed, err := loadIntentManifest(ctx, store, *partitionName, *intentName)
		if err != nil {
			return err
		}
		response := buildIntentDescribeResponse(*parsed)
		if printer.Format == cliformat.FormatJSON {
			printer.PrintJSON(response)
			return nil
		}
		printIntentDescribeResponse(printer, response)
		return nil
	}}
}

func buildIntentDescribeResponse(intent intentdomain.Intent) intentDescribeResponse {
	response := intentDescribeResponse{
		Manifest:    intent,
		OutputHints: assetdefs.ResolveIntentOutputHints(intent.Spec.Hints),
		Assets:      make([]intentDescribeAsset, 0, len(intent.Spec.Assets)),
	}
	for _, asset := range intent.Spec.Assets {
		response.Assets = append(response.Assets, intentDescribeAsset{
			Name:     asset.Name,
			Type:     asset.Type,
			Manifest: asset,
			Hints:    assetdefs.ResolveAssetHints(asset.Type, asset.Name, asset.Hints, intent.Spec.Hints),
		})
	}
	return response
}

func printAssetCatalogTemplate(printer *output.Printer, item assetdefs.CatalogTemplate) {
	printer.PrintText("%s - %s\n", item.Type, item.Title)
	printer.PrintText("Category: %s\n", item.Category)
	if item.Description != "" {
		printer.PrintText("Description: %s\n", item.Description)
	}
	if len(item.Template) > 0 {
		raw, err := yaml.Marshal(item.Template)
		if err == nil {
			printer.PrintText("\nTemplate:\n%s", indentBlock(string(raw), "  "))
		}
	}
	if len(item.Hints) > 0 {
		printer.PrintText("\nHints:\n")
		for _, hint := range item.Hints {
			label := hint.Path
			if hint.Title != "" {
				label = fmt.Sprintf("%s (%s)", hint.Path, hint.Title)
			}
			printer.PrintText("  - %s: %s\n", label, hint.Description)
		}
	}
}

func printIntentDescribeResponse(printer *output.Printer, response intentDescribeResponse) {
	printer.PrintText("Intent %s\n", response.Manifest.Metadata.Name)
	if len(response.OutputHints) > 0 {
		printer.PrintText("\nOutput hints:\n")
		for _, hint := range response.OutputHints {
			label := hint.Path
			if hint.Title != "" {
				label = fmt.Sprintf("%s (%s)", hint.Path, hint.Title)
			}
			printer.PrintText("  - %s: %s\n", label, hint.Description)
		}
	}
	if len(response.Assets) > 0 {
		printer.PrintText("\nAssets:\n")
		for _, asset := range response.Assets {
			printer.PrintText("  %s (%s)\n", asset.Name, asset.Type)
			raw, err := yaml.Marshal(asset.Manifest)
			if err == nil {
				printer.PrintText("    manifest:\n%s", indentBlock(string(raw), "      "))
			}
			for _, hint := range asset.Hints {
				label := hint.Path
				if hint.Title != "" {
					label = fmt.Sprintf("%s (%s)", hint.Path, hint.Title)
				}
				printer.PrintText("    - %s: %s\n", label, hint.Description)
			}
		}
	}
}

func mergedAssetCatalogTemplate(spec assetdomain.Spec, intentHints []assetdomain.Hint) assetdefs.CatalogTemplate {
	item, ok := assetdefs.CatalogFor(spec.Type)
	if !ok {
		item = assetdefs.CatalogTemplate{Type: spec.Type, Title: spec.Type}
	}
	item.Hints = assetdefs.ResolveAssetHints(spec.Type, spec.Name, spec.Hints, intentHints)
	return item
}

func loadIntentManifest(ctx context.Context, store guardianapi.Store, partitionName, intentName string) (*intentdomain.Intent, error) {
	if err := ensureStoreConfigured(store); err != nil {
		return nil, err
	}
	data, err := store.ReadFile(ctx, paths.IntentManifest(partitionName, intentName))
	if err != nil {
		return nil, err
	}
	return manifest.ParseIntent(data)
}

func findIntentAsset(intent intentdomain.Intent, assetName string) (assetdomain.Spec, bool) {
	for _, asset := range intent.Spec.Assets {
		if asset.Name == assetName {
			return asset, true
		}
	}
	return assetdomain.Spec{}, false
}

type rolloutTagAssetResult struct {
	Intent string `json:"intent"`
	Asset  string `json:"asset"`
	Path   string `json:"path"`
}

type rolloutTagResult struct {
	Success        bool                    `json:"success"`
	Dir            string                  `json:"dir"`
	Version        string                  `json:"version"`
	UpdatedIntents []string                `json:"updatedIntents,omitempty"`
	UpdatedAssets  []rolloutTagAssetResult `json:"updatedAssets,omitempty"`
}

func partitionTagCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("partition tag", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	dir := flags.String("dir", "", "local partition directory")
	version := flags.String("version", "", "version tag to write to matched assets; defaults to current UTC minute")
	intentName := flags.String("intent", "", "limit tagging to a specific intent")
	assetName := flags.String("asset", "", "limit tagging to a specific asset name")
	return &command.Command{Description: "Tag asset versions in local partition manifests", Flags: flags, Run: func(ctx context.Context, args []string) error {
		_ = ctx
		if strings.TrimSpace(*dir) == "" {
			return fmt.Errorf("--dir is required")
		}
		result, err := tagLocalRolloutManifests(filepath.Clean(*dir), strings.TrimSpace(*version), strings.TrimSpace(*intentName), strings.TrimSpace(*assetName))
		if err != nil {
			return err
		}
		if printer.Format == cliformat.FormatJSON {
			printer.PrintJSON(result)
			return nil
		}
		if len(result.UpdatedAssets) == 0 {
			printer.PrintText("no asset versions changed in %s\n", result.Dir)
			return nil
		}
		rows := make([][]string, 0, len(result.UpdatedAssets))
		for _, updated := range result.UpdatedAssets {
			rows = append(rows, []string{updated.Intent, updated.Asset, result.Version, updated.Path})
		}
		printer.PrintText("tagged %d assets in %d intent manifests with version %s\n", len(result.UpdatedAssets), len(result.UpdatedIntents), result.Version)
		printer.PrintTable([]string{"INTENT", "ASSET", "VERSION", "FILE"}, rows)
		return nil
	}}
}

func tagLocalRolloutManifests(dir, version, intentFilter, assetFilter string) (rolloutTagResult, error) {
	result := rolloutTagResult{Success: true, Dir: dir}
	if strings.TrimSpace(version) == "" {
		version = defaultRolloutTagVersion(rolloutTagNow())
	}
	result.Version = version

	intentFiles, err := loadIntentFiles(dir)
	if err != nil {
		return rolloutTagResult{}, err
	}
	if len(intentFiles) == 0 {
		return rolloutTagResult{}, fmt.Errorf("no intent manifests found under %s", filepath.Join(dir, "intents"))
	}

	knownIntents := make([]string, 0, len(intentFiles))
	for _, intentFile := range intentFiles {
		parsed, _, err := normalizeIntentManifest(intentFile.Content, intentFile.Path, nil)
		if err != nil {
			return rolloutTagResult{}, fmt.Errorf("intent file %s: %w", intentFile.Path, err)
		}
		knownIntents = append(knownIntents, parsed.Metadata.Name)
	}
	sort.Strings(knownIntents)
	knownIntents = uniqueStrings(knownIntents)

	matchedIntent := intentFilter == ""
	matchedAsset := assetFilter == ""
	updatedIntentSet := make(map[string]struct{})
	for _, intentFile := range intentFiles {
		parsed, _, err := normalizeIntentManifest(intentFile.Content, intentFile.Path, knownIntents)
		if err != nil {
			return rolloutTagResult{}, fmt.Errorf("intent file %s: %w", intentFile.Path, err)
		}
		if intentFilter != "" && parsed.Metadata.Name != intentFilter {
			continue
		}
		matchedIntent = true

		changed := false
		for idx := range parsed.Spec.Assets {
			asset := &parsed.Spec.Assets[idx]
			if assetFilter != "" && asset.Name != assetFilter {
				continue
			}
			matchedAsset = true
			if strings.TrimSpace(asset.Version) == version {
				continue
			}
			asset.Version = version
			changed = true
			result.UpdatedAssets = append(result.UpdatedAssets, rolloutTagAssetResult{
				Intent: parsed.Metadata.Name,
				Asset:  asset.Name,
				Path:   intentFile.Path,
			})
		}
		if !changed {
			continue
		}
		normalized, err := yaml.Marshal(parsed)
		if err != nil {
			return rolloutTagResult{}, err
		}
		perm := os.FileMode(0o644)
		if info, statErr := os.Stat(intentFile.Path); statErr == nil {
			perm = info.Mode().Perm()
		}
		if err := os.WriteFile(intentFile.Path, normalized, perm); err != nil {
			return rolloutTagResult{}, err
		}
		if _, ok := updatedIntentSet[parsed.Metadata.Name]; !ok {
			updatedIntentSet[parsed.Metadata.Name] = struct{}{}
			result.UpdatedIntents = append(result.UpdatedIntents, parsed.Metadata.Name)
		}
	}

	if intentFilter != "" && !matchedIntent {
		return rolloutTagResult{}, fmt.Errorf("intent %q not found under %s", intentFilter, filepath.Join(dir, "intents"))
	}
	if assetFilter != "" && !matchedAsset {
		if intentFilter != "" {
			return rolloutTagResult{}, fmt.Errorf("asset %q not found in intent %q", assetFilter, intentFilter)
		}
		return rolloutTagResult{}, fmt.Errorf("asset %q not found under %s", assetFilter, filepath.Join(dir, "intents"))
	}

	return result, nil
}

func defaultRolloutTagVersion(now time.Time) string {
	return now.UTC().Format("20060102-1504")
}

func indentBlock(value, prefix string) string {
	trimmed := strings.TrimRight(value, "\n")
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

func deploymentFilterFromFlags(limit int, sinceValue, untilValue string) (historyquery.DeploymentFilter, error) {
	filter := historyquery.DeploymentFilter{Limit: limit}
	if strings.TrimSpace(sinceValue) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, sinceValue)
		if err != nil {
			return historyquery.DeploymentFilter{}, fmt.Errorf("--since must be RFC3339")
		}
		filter.Since = &parsed
	}
	if strings.TrimSpace(untilValue) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, untilValue)
		if err != nil {
			return historyquery.DeploymentFilter{}, fmt.Errorf("--until must be RFC3339")
		}
		filter.Until = &parsed
	}
	if err := filter.Validate(); err != nil {
		return historyquery.DeploymentFilter{}, err
	}
	return filter, nil
}

func loadRollouts(ctx context.Context, store guardianapi.ReadStore, partitionName string, filter historyquery.DeploymentFilter) ([]historyquery.RolloutRecord, error) {
	if strings.TrimSpace(partitionName) != "" {
		return historyquery.LoadPartitionRollouts(ctx, store, partitionName, filter)
	}
	names, err := configuredPartitionNames(ctx, store)
	if err != nil {
		return nil, err
	}
	rollouts := make([]historyquery.RolloutRecord, 0)
	for _, name := range names {
		items, err := historyquery.LoadPartitionRollouts(ctx, store, name, historyquery.DeploymentFilter{
			Since: filter.Since,
			Until: filter.Until,
		})
		if err != nil {
			return nil, err
		}
		rollouts = append(rollouts, items...)
	}
	sort.Slice(rollouts, func(i, j int) bool {
		if rollouts[i].CreatedAt.Equal(rollouts[j].CreatedAt) {
			if rollouts[i].Partition == rollouts[j].Partition {
				if rollouts[i].Intent == rollouts[j].Intent {
					return rollouts[i].DeploymentRevision > rollouts[j].DeploymentRevision
				}
				return rollouts[i].Intent < rollouts[j].Intent
			}
			return rollouts[i].Partition < rollouts[j].Partition
		}
		return rollouts[i].CreatedAt.After(rollouts[j].CreatedAt)
	})
	if filter.Limit > 0 && len(rollouts) > filter.Limit {
		return append([]historyquery.RolloutRecord(nil), rollouts[:filter.Limit]...), nil
	}
	return rollouts, nil
}

func formatRolloutAssets(assets []historyquery.RolloutAsset) string {
	if len(assets) == 0 {
		return ""
	}
	parts := make([]string, 0, len(assets))
	for _, asset := range assets {
		label := asset.Name
		if strings.TrimSpace(asset.Type) != "" {
			label += " [" + asset.Type + "]"
		}
		if strings.TrimSpace(asset.Change) != "" {
			label += " " + asset.Change
		}
		if strings.TrimSpace(asset.Version) != "" {
			label += " @ " + asset.Version
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "; ")
}

func lockCommand(store guardianapi.Store, printer *output.Printer, locked bool) *command.Command {
	name := "intent lock"
	description := "Lock an intent"
	if !locked {
		name = "intent unlock"
		description = "Unlock an intent"
	}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	partitionName := flags.String("partition", "", "partition name")
	intentName := flags.String("intent", "", "intent name")
	return &command.Command{Description: description, Flags: flags, Run: func(ctx context.Context, args []string) error {
		if *partitionName == "" || *intentName == "" {
			return fmt.Errorf("--partition and --intent are required")
		}
		data, err := store.ReadFile(ctx, paths.IntentManifest(*partitionName, *intentName))
		if err != nil {
			return err
		}
		parsed, err := manifest.ParseIntent(data)
		if err != nil {
			return err
		}
		parsed.Spec.Locked = locked
		normalized, err := yaml.Marshal(parsed)
		if err != nil {
			return err
		}
		batch, correlationID, err := writeFile(ctx, store, paths.IntentManifest(*partitionName, *intentName), normalized, revisions.NewCorrelationID(), name)
		if err != nil {
			return err
		}
		printMutation(printer, batch, paths.IntentManifest(*partitionName, *intentName), correlationID)
		return nil
	}}
}

func compilePartition(ctx context.Context, store guardianapi.Store, partitionName string) (*planner.CompiledPartition, error) {
	configPath := paths.PartitionConfig(partitionName)
	configContent, err := store.ReadFile(ctx, configPath)
	if err != nil {
		return nil, err
	}
	info, err := store.Stat(ctx, configPath)
	if err != nil {
		return nil, err
	}
	intentNames, err := listIntentNames(ctx, store, partitionName)
	if err != nil {
		return nil, err
	}
	contents := map[string][]byte{}
	versions := map[string]string{}
	modTimes := map[string]time.Time{}
	for _, name := range intentNames {
		path := paths.IntentManifest(partitionName, name)
		content, err := store.ReadFile(ctx, path)
		if err != nil {
			return nil, err
		}
		fileInfo, err := store.Stat(ctx, path)
		if err != nil {
			return nil, err
		}
		contents[name] = content
		versions[name] = fileInfo.VersionID
		modTimes[name] = fileInfo.ModTime
	}
	states, err := common.LoadAllIntentStates(ctx, store, partitionName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return planner.Compile(ctx, planner.CompileInput{PartitionName: partitionName, ConfigContent: configContent, IntentContents: contents, IntentVersionIDs: versions, IntentModTimes: modTimes, ConfigVersionID: info.VersionID, CurrentOutputs: common.IntentOutputs(states)})
}

func listIntentNames(ctx context.Context, store guardianapi.ReadStore, partition string) ([]string, error) {
	entries, err := store.ListDir(ctx, paths.PartitionIntentsDir(partition))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".yaml") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name, ".yaml"))
	}
	sort.Strings(names)
	return names, nil
}

func writeFile(ctx context.Context, store guardianapi.WriteStore, logicalPath string, content []byte, correlationID, reason string) (guardianapi.BatchRevision, string, error) {
	batch, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: content}}, Context: guardianapi.MutationContext{PrincipalID: "guardianctl", Reason: reason, CorrelationID: correlationID}})
	return batch, correlationID, err
}

func printMutation(printer *output.Printer, batch guardianapi.BatchRevision, logicalPath, correlationID string) {
	versionID := ""
	if len(batch.Files) > 0 {
		versionID = batch.Files[len(batch.Files)-1].VersionID
	}
	printer.PrintMutation(cliformat.MutationResult{Success: true, LogicalPath: logicalPath, VersionID: versionID, BatchRevisionID: batch.BatchRevisionID, CorrelationID: correlationID})
}

func walkFiles(ctx context.Context, store guardianapi.ReadStore, logicalDir string) ([]string, error) {
	entries, err := store.ListDir(ctx, logicalDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, entry := range entries {
		child := strings.TrimRight(logicalDir, "/") + "/" + entry.Name
		if entry.IsDir {
			nested, err := walkFiles(ctx, store, child)
			if err != nil {
				return nil, err
			}
			out = append(out, nested...)
			continue
		}
		out = append(out, child)
	}
	sort.Strings(out)
	return out, nil
}

func readJSON(ctx context.Context, store guardianapi.ReadStore, logicalPath string, out any) error {
	data, err := store.ReadFile(ctx, logicalPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func loadRollbackManifest(ctx context.Context, store guardianapi.Store, partition, intent, deployment string) ([]byte, *historydomain.DeploymentRecord, error) {
	var record historydomain.DeploymentRecord
	if err := readJSON(ctx, store, paths.ArchiveState(partition, intent, deployment), &record); err != nil {
		return nil, nil, err
	}
	manifestPath := paths.ArchiveManifest(partition, intent, deployment)
	manifestContent, err := store.ReadFile(ctx, manifestPath)
	if err == nil {
		if _, parseErr := manifest.ParseIntent(manifestContent); parseErr == nil {
			return manifestContent, &record, nil
		}
	}
	if record.IntentVersionID != "" {
		version, versionErr := store.GetVersion(ctx, paths.IntentManifest(partition, intent), record.IntentVersionID)
		if versionErr == nil {
			return version.Content, &record, nil
		}
		if err == nil || !errors.Is(versionErr, os.ErrNotExist) {
			return nil, nil, versionErr
		}
	}
	if err != nil {
		return nil, nil, err
	}
	return nil, nil, fmt.Errorf("archive manifest %s does not contain a valid intent manifest", manifestPath)
}

func tailEvents(ctx context.Context, store guardianapi.Store, printer *output.Printer, partition, intent string, once bool, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	if err := tailEventsWatch(ctx, store, printer, partition, intent, once); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "watch not supported") {
		return err
	}
	return tailEventsPoll(ctx, store, printer, partition, intent, once, pollInterval)
}

func tailEventsWatch(ctx context.Context, store guardianapi.Store, printer *output.Printer, partition, intent string, once bool) error {
	prefixes := []string{paths.PartitionsRoot()}
	if partition != "" {
		prefixes = []string{paths.StateEventsDir(partition)}
	}
	ch, err := store.Watch(ctx, prefixes)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if event.Type == guardianapi.ChangeDeleted || !isEventPath(event.LogicalPath) {
				continue
			}
			if _, ok := seen[event.LogicalPath]; ok {
				continue
			}
			record, err := readEventRecord(ctx, store, event.LogicalPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return err
			}
			if intent != "" && record.Intent != intent {
				continue
			}
			seen[event.LogicalPath] = struct{}{}
			printEvent(printer, record)
			if once {
				return nil
			}
		}
	}
}

func tailEventsPoll(ctx context.Context, store guardianapi.Store, printer *output.Printer, partition, intent string, once bool, pollInterval time.Duration) error {
	seen, err := listEventFiles(ctx, store, partition)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if seen == nil {
		seen = map[string]struct{}{}
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			current, err := listEventFiles(ctx, store, partition)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return err
			}
			files := make([]string, 0, len(current))
			for logicalPath := range current {
				if _, ok := seen[logicalPath]; ok {
					continue
				}
				files = append(files, logicalPath)
			}
			sort.Strings(files)
			for _, logicalPath := range files {
				record, err := readEventRecord(ctx, store, logicalPath)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					return err
				}
				seen[logicalPath] = struct{}{}
				if intent != "" && record.Intent != intent {
					continue
				}
				printEvent(printer, record)
				if once {
					return nil
				}
			}
		}
	}
}

func listEventFiles(ctx context.Context, store guardianapi.ReadStore, partition string) (map[string]struct{}, error) {
	dirs := make([]string, 0, 1)
	if partition != "" {
		dirs = append(dirs, paths.StateEventsDir(partition))
	} else {
		names, err := configuredPartitionNames(ctx, store)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			dirs = append(dirs, paths.StateEventsDir(name))
		}
	}
	out := map[string]struct{}{}
	for _, dir := range dirs {
		entries, err := store.ListDir(ctx, dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
				continue
			}
			out[strings.TrimRight(dir, "/")+"/"+entry.Name] = struct{}{}
		}
	}
	return out, nil
}

func configuredPartitionNames(ctx context.Context, store guardianapi.ReadStore) ([]string, error) {
	if err := ensureStoreConfigured(store); err != nil {
		return nil, err
	}
	entries, err := store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir {
			continue
		}
		if _, err := store.Stat(ctx, paths.PartitionConfig(entry.Name)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names, nil
}

func readEventRecord(ctx context.Context, store guardianapi.ReadStore, logicalPath string) (*historydomain.EventRecord, error) {
	var record historydomain.EventRecord
	if err := readJSON(ctx, store, logicalPath, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func printEvent(printer *output.Printer, record *historydomain.EventRecord) {
	if printer.Format == cliformat.FormatJSON {
		printer.PrintJSON(record)
		return
	}
	target := record.Partition
	if record.Intent != "" {
		target += "/" + record.Intent
	}
	printer.PrintText("%s %-24s %-24s %s\n", record.CreatedAt.Format(time.RFC3339), target, record.Type, record.Message)
}

func isEventPath(logicalPath string) bool {
	return strings.Contains(logicalPath, "/.state/events/") && strings.HasSuffix(logicalPath, ".json")
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
