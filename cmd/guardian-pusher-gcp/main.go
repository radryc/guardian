package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/radryc/guardian/internal/awsscan"
	"github.com/radryc/guardian/internal/buildinfo"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/gcpscan"
	"github.com/radryc/guardian/internal/paths"
	gcpdriver "github.com/radryc/guardian/internal/pusher/drivers/gcp"
	"github.com/radryc/guardian/internal/pusher/drivers/gcp/opentofu"
	"github.com/radryc/guardian/internal/pusher/probes"
	"github.com/radryc/guardian/internal/pusher/registry"
	runtimepkg "github.com/radryc/guardian/internal/pusher/runtime"
	"github.com/radryc/guardian/internal/pusher/secrets"
	fsstore "github.com/radryc/guardian/internal/store/fs"
	monofsstore "github.com/radryc/guardian/internal/store/monofs"
	"github.com/radryc/guardian/internal/telemetry"
	"github.com/radryc/guardian/internal/versioning/revisions"
	"github.com/radryc/guardian/pkg/guardianapi"
)

func main() {
	var pusherName string
	var project string
	var region string
	var storeDir string
	var workerID string
	var monofsRouter string
	var monofsToken string
	var monofsPrincipalID string
	var monofsUseExternalAddresses bool
	var tofuBinary string
	var workDir string
	var stateBucket string
	var statePrefix string
	var kmsKey string
	var unclaimedTaskRetryDelay time.Duration
	var maxConcurrency int
	var taskTimeout time.Duration
	var shardIndex int
	var shardCount int
	var readAddr string
	var readToken string
	var readTokenFile string
	var readScopes string
	var readTLSCert string
	var readTLSKey string
	var readTLSClientCA string
	var readDisabled bool
	var readAllowPlaintext bool
	flag.StringVar(&pusherName, "pusher-name", "", "pusher name (default: gcp-<project>)")
	flag.StringVar(&project, "project", os.Getenv("GUARDIAN_GCP_PROJECT"), "GCP project handled by this worker")
	flag.StringVar(&region, "region", os.Getenv("GUARDIAN_GCP_REGION"), "optional region filter handled by this worker (empty = all regions in project)")
	flag.StringVar(&storeDir, "store-dir", "", "filesystem-backed Guardian store")
	flag.StringVar(&workerID, "worker-id", "", "worker identifier")
	flag.StringVar(&monofsRouter, "monofs-router", "", "MonoFS router address")
	flag.StringVar(&monofsToken, "monofs-token", "", "MonoFS guardian token")
	flag.StringVar(&monofsPrincipalID, "monofs-principal-id", "", "MonoFS guardian principal ID (defaults to guardian-pusher-<pusher-name>)")
	flag.BoolVar(&monofsUseExternalAddresses, "monofs-use-external-addresses", false, "prefer MonoFS external addresses when refreshing cluster info")
	flag.StringVar(&tofuBinary, "tofu-binary", "tofu", "OpenTofu binary to use")
	flag.StringVar(&workDir, "work-dir", "/var/lib/guardian/pusher-gcp", "directory for per-intent OpenTofu workspaces")
	flag.StringVar(&stateBucket, "state-bucket", os.Getenv("GUARDIAN_GCP_STATE_BUCKET"), "GCS bucket for OpenTofu state")
	flag.StringVar(&statePrefix, "state-prefix", "guardian", "GCS object prefix for OpenTofu state")
	flag.StringVar(&kmsKey, "kms-key", os.Getenv("GUARDIAN_GCP_KMS_KEY"), "GCP KMS key resource name for state encryption")
	flag.DurationVar(&unclaimedTaskRetryDelay, "unclaimed-task-retry-delay", 15*time.Second, "minimum delay before retrying a task that could not be claimed; 0 disables backoff")
	flag.IntVar(&maxConcurrency, "max-concurrency", 0, "maximum tasks executed concurrently (0 = auto from CPU count)")
	flag.DurationVar(&taskTimeout, "task-timeout", 0, "maximum duration for a single task before it is cancelled; 0 disables (keep below GUARDIAN_STALE_TASK_AFTER)")
	flag.IntVar(&shardIndex, "shard-index", 0, "shard index for this replica when sharding a shared queue")
	flag.IntVar(&shardCount, "shard-count", 1, "number of shards sharing the queue; 1 disables sharding")
	flag.StringVar(&readAddr, "read-addr", envOr("GUARDIAN_GCPREAD_ADDR", "127.0.0.1:19091"), "HTTP listen address for the read-only GCP metrics/logs/traces API (empty disables)")
	flag.StringVar(&readToken, "read-token", os.Getenv("GUARDIAN_GCPREAD_TOKEN"), "bearer token required by the GCP read API")
	flag.StringVar(&readTokenFile, "read-token-file", os.Getenv("GUARDIAN_GCPREAD_TOKEN_FILE"), "file containing the bearer token; re-read on each request")
	flag.StringVar(&readScopes, "read-scopes", envOr("GUARDIAN_GCPREAD_SCOPES", "read"), "comma-separated scopes for the bearer token")
	flag.StringVar(&readTLSCert, "read-tls-cert", os.Getenv("GUARDIAN_GCPREAD_TLS_CERT"), "TLS certificate for the GCP read API")
	flag.StringVar(&readTLSKey, "read-tls-key", os.Getenv("GUARDIAN_GCPREAD_TLS_KEY"), "TLS private key for the GCP read API")
	flag.StringVar(&readTLSClientCA, "read-tls-client-ca", os.Getenv("GUARDIAN_GCPREAD_TLS_CLIENT_CA"), "CA bundle for mutual TLS client verification")
	flag.BoolVar(&readDisabled, "read-disabled", false, "disable the GCP read API")
	flag.BoolVar(&readAllowPlaintext, "read-allow-plaintext", false, "allow the read API to listen on a non-loopback address without TLS")
	flag.Parse()

	if project == "" || (storeDir == "") == (monofsRouter == "") {
		flag.Usage()
		os.Exit(2)
	}
	if pusherName == "" {
		pusherName = "gcp-" + scopeLabel(project)
	}
	if workerID == "" {
		workerID = revisions.NewCorrelationID()
	}
	monofsPrincipalID = runtimepkg.ResolvePrincipalID(pusherName, monofsPrincipalID)

	telemetryCfg, telemetryErr := telemetry.LoadConfig("guardian-pusher-gcp", monofsPrincipalID)
	if telemetryErr != nil {
		log.Fatalf("load telemetry config: %v", telemetryErr)
	}
	telemetryCfg.ServiceName = "guardian.pusher." + pusherName
	telemetryCfg.ResourceAttributes = map[string]string{
		"service.alias":          pusherName,
		"guardian.asset.type":    "Compute",
		"guardian.identity.kind": "platform",
		"guardian.pusher":        pusherName,
		"guardian.provider":      "gcp",
		"guardian.account":       project,
		"guardian.region":        region,
		"guardian.shard.index":   fmt.Sprintf("%d", shardIndex),
		"guardian.shard.count":   fmt.Sprintf("%d", shardCount),
	}
	telemetryHandle, telemetryErr := telemetry.Setup(context.Background(), telemetryCfg)
	if telemetryErr != nil {
		log.Fatalf("setup telemetry: %v", telemetryErr)
	}
	if telemetryHandle.Enabled() {
		log.SetOutput(io.MultiWriter(os.Stderr, telemetry.NewStdLogWriter("guardian.stdlog")))
		telemetry.EmitInfo(context.Background(), "guardian.pusher.gcp", "guardian gcp pusher telemetry enabled")
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := telemetryHandle.Shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "shutdown telemetry: %v\n", err)
			}
		}()
	}

	var (
		store       guardianapi.Store
		closeClient interface{ Close() error }
		err         error
	)
	if monofsRouter != "" {
		openedStore, client, openErr := monofsstore.Open(context.Background(), monofsstore.OpenConfig{
			RouterAddr:           monofsRouter,
			Token:                monofsToken,
			PrincipalID:          monofsPrincipalID,
			Role:                 "pusher",
			ClientIDPrefix:       "guardian-pusher-gcp",
			Version:              buildinfo.Current().Version,
			Writable:             true,
			UseExternalAddresses: monofsUseExternalAddresses,
		})
		if openErr != nil {
			log.Fatalf("open monofs store: %v", openErr)
		}
		store = openedStore
		closeClient = client
	} else {
		store, err = fsstore.Open(storeDir)
		if err != nil {
			log.Fatalf("open store: %v", err)
		}
	}
	if closeClient != nil {
		defer func() {
			if err := closeClient.Close(); err != nil {
				log.Printf("close monofs client: %v", err)
			}
		}()
	}

	backend, err := gcpdriver.NewOpenTofuBackend(opentofu.Config{
		Binary:          tofuBinary,
		WorkRoot:        workDir,
		StateBucket:     stateBucket,
		StatePrefixRoot: statePrefix,
		StateKMSKey:     kmsKey,
		Env: map[string]string{
			"GOOGLE_CLOUD_PROJECT": project,
		},
	})
	if err != nil {
		log.Fatalf("configure OpenTofu backend: %v", err)
	}

	reg := registry.New()
	gcpdriver.Register(reg, backend, secrets.NewStoreResolver(store))

	runtime := &runtimepkg.Runtime{
		QueuePath:               paths.QueueDir(pusherName),
		WorkerID:                workerID,
		PrincipalID:             monofsPrincipalID,
		Store:                   store,
		Registry:                reg,
		PollInterval:            5 * time.Second,
		UnclaimedTaskRetryDelay: unclaimedTaskRetryDelay,
		MaxConcurrency:          maxConcurrency,
		TaskTimeout:             taskTimeout,
		ShardIndex:              shardIndex,
		ShardCount:              shardCount,
		CanHandle: func(t *taskdomain.Task) bool {
			if !strings.EqualFold(strings.TrimSpace(t.Target.Account), project) {
				return false
			}
			if strings.TrimSpace(region) == "" {
				return true
			}
			return strings.EqualFold(strings.TrimSpace(t.Target.Region), region)
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !readDisabled {
		if _, err := startReadAPI(ctx, readAPIConfig{
			Address:        readAddr,
			Token:          readToken,
			TokenFile:      readTokenFile,
			Scopes:         readScopes,
			TLSCert:        readTLSCert,
			TLSKey:         readTLSKey,
			TLSClientCA:    readTLSClientCA,
			AllowPlaintext: readAllowPlaintext,
			Pusher:         pusherName,
			Project:        project,
			Region:         region,
			StateBucket:    stateBucket,
		}); err != nil {
			log.Fatalf("start GCP read API: %v", err)
		}
	}

	// Advertise the read API we just bound so the observed collector can find
	// it. An explicit override wins for NAT/ingress setups.
	observedAPIURL := strings.TrimSpace(os.Getenv("GUARDIAN_OBSERVED_API_URL"))
	if observedAPIURL == "" && !readDisabled {
		observedAPIURL = advertisedReadURL(readAddr)
		if observedAPIURL != "" && strings.TrimSpace(readTLSCert) != "" {
			observedAPIURL = "https" + strings.TrimPrefix(observedAPIURL, "http")
		}
	}
	registrationToken := readToken
	if registrationToken == "" && strings.TrimSpace(readTokenFile) != "" {
		if content, err := os.ReadFile(readTokenFile); err == nil {
			registrationToken = strings.TrimSpace(string(content))
		}
	}
	pusherInfo := runtimepkg.PusherInfo{
		Name:           pusherName,
		Provider:       "gcp",
		Account:        project,
		Region:         region,
		ShardIndex:     shardIndex,
		ShardCount:     shardCount,
		Capabilities:   []string{"metrics", "logs", "traces", "probes", "observed"},
		OTLPEndpoint:   telemetryCfg.Endpoint,
		CollectorURL:   probes.HTTPEndpoint(telemetryCfg.Endpoint),
		WorkerID:       workerID,
		ObservedAPIURL: observedAPIURL,
		Token:          registrationToken,
	}
	// The registry accepts a literal token or an env reference, never both.
	if pusherInfo.Token == "" {
		pusherInfo.TokenEnv = "GUARDIAN_GCPREAD_TOKEN"
	}
	runtimepkg.StartRegistration(ctx, store, pusherInfo)

	scanRunner := &awsscan.ScanRunner{
		PusherName:  pusherName,
		Account:     project,
		Region:      region,
		WorkerID:    workerID,
		PrincipalID: monofsPrincipalID,
		Store:       store,
		ShardIndex:  shardIndex,
		ShardCount:  shardCount,
		NewScanner: func() awsscan.Scanner {
			return gcpscan.NewScanner(project, region)
		},
	}
	go func() {
		if err := scanRunner.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("scan runner stopped: %v", err)
		}
	}()

	probeCfg := probes.ConfigFromEnv()
	probeCfg.Store = store
	probeCfg.Resolver = secrets.NewStoreResolver(store)
	probeCfg.Pusher = pusherName
	probeCfg.Provider = "gcp"
	probeCfg.Account = project
	probeCfg.Region = region
	probeCfg.WorkerID = workerID
	probeCfg.ShardIndex = shardIndex
	probeCfg.ShardCount = shardCount
	if probeCfg.OTLPEndpoint == "" {
		probeCfg.OTLPEndpoint = probes.HTTPEndpoint(telemetryCfg.Endpoint)
	}
	go func() {
		if err := probes.New(probeCfg).Run(ctx); err != nil && err != context.Canceled {
			log.Printf("probe runner stopped: %v", err)
		}
	}()

	if err := runtime.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

var scopeLabelSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

func scopeLabel(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	value = scopeLabelSanitizer.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "default"
	}
	return value
}

func advertisedReadURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return ""
	}
	if podIP := strings.TrimSpace(os.Getenv("POD_IP")); podIP != "" {
		host = podIP
	} else if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, port)
}
