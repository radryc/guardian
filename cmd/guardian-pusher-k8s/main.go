package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/radryc/guardian/internal/buildinfo"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/k8sobserve"
	"github.com/radryc/guardian/internal/k8sread"
	"github.com/radryc/guardian/internal/paths"
	kubernetesdriver "github.com/radryc/guardian/internal/pusher/drivers/kubernetes"
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
	var cluster string
	var storeDir string
	var workerID string
	var monofsRouter string
	var monofsToken string
	var monofsPrincipalID string
	var monofsUseExternalAddresses bool
	var kubectlBinary string
	var kubeconfig string
	var kubeContext string
	var backendMode string
	var unclaimedTaskRetryDelay time.Duration
	var maxConcurrency int
	var taskTimeout time.Duration
	var shardIndex int
	var shardCount int
	var readAddr string
	var readToken string
	var readTokenFile string
	var readDisabled bool
	flag.StringVar(&pusherName, "pusher-name", "", "pusher name (default: k8s-<cluster>)")
	flag.StringVar(&cluster, "cluster", "", "target cluster handled by this worker")
	flag.StringVar(&storeDir, "store-dir", "", "filesystem-backed Guardian store")
	flag.StringVar(&workerID, "worker-id", "", "worker identifier")
	flag.StringVar(&monofsRouter, "monofs-router", "", "MonoFS router address")
	flag.StringVar(&monofsToken, "monofs-token", "", "MonoFS guardian token")
	flag.StringVar(&monofsPrincipalID, "monofs-principal-id", "", "MonoFS guardian principal ID (defaults to guardian-pusher-<pusher-name>)")
	flag.BoolVar(&monofsUseExternalAddresses, "monofs-use-external-addresses", false, "prefer MonoFS external addresses when refreshing cluster info")
	flag.StringVar(&kubectlBinary, "kubectl-binary", "kubectl", "kubectl binary to use for apply/diff/destroy")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "optional kubeconfig path for kubectl")
	flag.StringVar(&kubeContext, "kube-context", "", "optional kubectl context override")
	flag.StringVar(&backendMode, "backend", "auto", "kubernetes backend: auto|client|kubectl (auto prefers client-go and falls back to kubectl)")
	flag.DurationVar(&unclaimedTaskRetryDelay, "unclaimed-task-retry-delay", 15*time.Second, "minimum delay before retrying a task that could not be claimed; 0 disables backoff")
	flag.IntVar(&maxConcurrency, "max-concurrency", 0, "maximum tasks executed concurrently (0 = auto from CPU count)")
	flag.DurationVar(&taskTimeout, "task-timeout", 0, "maximum duration for a single task before it is cancelled; 0 disables (keep below GUARDIAN_STALE_TASK_AFTER)")
	flag.IntVar(&shardIndex, "shard-index", 0, "shard index for this replica when sharding a shared queue")
	flag.IntVar(&shardCount, "shard-count", 1, "number of shards sharing the queue; 1 disables sharding")
	flag.StringVar(&readAddr, "read-addr", os.Getenv("GUARDIAN_K8SREAD_ADDR"), "HTTP listen address for the read-only Kubernetes resource API (empty disables)")
	flag.StringVar(&readToken, "read-token", os.Getenv("GUARDIAN_K8SREAD_TOKEN"), "bearer token required by the Kubernetes read API")
	flag.StringVar(&readTokenFile, "read-token-file", os.Getenv("GUARDIAN_K8SREAD_TOKEN_FILE"), "file containing the bearer token; re-read on each request")
	flag.BoolVar(&readDisabled, "read-disabled", false, "disable the Kubernetes read API")
	flag.Parse()

	if cluster == "" || (storeDir == "" && monofsRouter == "") || (storeDir != "" && monofsRouter != "") {
		flag.Usage()
		os.Exit(2)
	}
	if pusherName == "" {
		pusherName = defaultPusherNameForCluster(cluster)
	}
	if workerID == "" {
		workerID = revisions.NewCorrelationID()
	}
	monofsPrincipalID = runtimepkg.ResolvePrincipalID(pusherName, monofsPrincipalID)
	telemetryCfg, telemetryErr := telemetry.LoadConfig("guardian-pusher-k8s", monofsPrincipalID)
	if telemetryErr != nil {
		log.Fatalf("load telemetry config: %v", telemetryErr)
	}
	telemetryCfg.ServiceName = "guardian.pusher." + pusherName
	telemetryCfg.ResourceAttributes = map[string]string{
		"service.alias":          pusherName,
		"guardian.asset.type":    "Compute",
		"guardian.identity.kind": "platform",
		"guardian.pusher":        pusherName,
		"guardian.cluster":       cluster,
		"guardian.provider":      "kubernetes",
		"guardian.shard.index":   fmt.Sprintf("%d", shardIndex),
		"guardian.shard.count":   fmt.Sprintf("%d", shardCount),
	}
	telemetryHandle, telemetryErr := telemetry.Setup(context.Background(), telemetryCfg)
	if telemetryErr != nil {
		log.Fatalf("setup telemetry: %v", telemetryErr)
	}
	if telemetryHandle.Enabled() {
		log.SetOutput(io.MultiWriter(os.Stderr, telemetry.NewStdLogWriter("guardian.stdlog")))
		telemetry.EmitInfo(context.Background(), "guardian.pusher.k8s", "guardian kubernetes pusher telemetry enabled")
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
			ClientIDPrefix:       "guardian-pusher-k8s",
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

	kubeBackend, err := newKubeBackend(backendMode, kubectlBinary, kubeconfig, kubeContext)
	if err != nil {
		log.Fatalf("configure kubernetes backend: %v", err)
	}
	reg := registry.New()
	kubernetesdriver.Register(reg, kubeBackend, secrets.NewStoreResolver(store))
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
			return strings.EqualFold(strings.TrimSpace(t.Target.Cluster), cluster) &&
				strings.EqualFold(strings.TrimSpace(t.TargetPusher), pusherName)
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var inventoryService *k8sobserve.Service
	if lister, ok := kubeBackend.(k8sobserve.ResourceLister); ok {
		inventoryService = k8sobserve.NewService(lister, cluster)
		go inventoryService.Run(ctx)
	} else {
		log.Printf("kubernetes: backend does not support managed resource inventory")
	}

	observedAPIURL := ""
	if inventoryService != nil && !readDisabled && strings.TrimSpace(readAddr) != "" {
		token := strings.TrimSpace(readToken)
		if token == "" && strings.TrimSpace(readTokenFile) != "" {
			if content, err := os.ReadFile(readTokenFile); err == nil {
				token = strings.TrimSpace(string(content))
			}
		}
		if token == "" && !isLoopbackListen(readAddr) {
			log.Printf("kubernetes: read API is listening on %s without a bearer token; configure --read-token or --read-token-file", readAddr)
		} else {
			handler := k8sread.NewHandler(inventoryService, pusherName, cluster, token)
			if strings.TrimSpace(readTokenFile) != "" {
				handler = handler.WithTokenFile(readTokenFile)
			}
			server := &http.Server{
				Addr:              readAddr,
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      2 * time.Minute,
				IdleTimeout:       2 * time.Minute,
			}
			go func() {
				<-ctx.Done()
				_ = server.Shutdown(context.Background())
			}()
			go func() {
				log.Printf("kubernetes read API listening on %s", readAddr)
				if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
					log.Printf("kubernetes read API stopped: %v", serveErr)
				}
			}()
			observedAPIURL = advertisedReadURL(readAddr)
		}
	}

	runtimepkg.StartRegistration(ctx, store, runtimepkg.PusherInfo{
		Name:           pusherName,
		Provider:       "kubernetes",
		Cluster:        cluster,
		KubeContext:    kubeContext,
		ShardIndex:     shardIndex,
		ShardCount:     shardCount,
		Capabilities:   []string{"metrics", "logs", "traces", "probes", "observed"},
		OTLPEndpoint:   telemetryCfg.Endpoint,
		CollectorURL:   probes.HTTPEndpoint(telemetryCfg.Endpoint),
		ObservedAPIURL: observedAPIURL,
		Token:          readToken,
		WorkerID:       workerID,
	})

	probeCfg := probes.ConfigFromEnv()
	probeCfg.Store = store
	probeCfg.Resolver = secrets.NewStoreResolver(store)
	probeCfg.Pusher = pusherName
	probeCfg.Cluster = cluster
	probeCfg.Provider = "kubernetes"
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

var scopeLabelSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

func newKubeBackend(mode, kubectlBinary, kubeconfig, kubeContext string) (kubernetesdriver.BackendAPI, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "kubectl":
		return kubernetesdriver.NewCLIBackend(kubectlBinary, kubeconfig, kubeContext)
	case "client", "client-go", "clientgo":
		return kubernetesdriver.NewClientBackend(kubeconfig, kubeContext)
	default:
		backend, err := kubernetesdriver.NewClientBackend(kubeconfig, kubeContext)
		if err == nil {
			log.Printf("kubernetes: using client-go backend (kubeconfig=%q context=%q)", kubeconfig, kubeContext)
			return backend, nil
		}
		log.Printf("kubernetes: client-go backend unavailable (%v); falling back to kubectl", err)
		return kubernetesdriver.NewCLIBackend(kubectlBinary, kubeconfig, kubeContext)
	}
}

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

func defaultPusherNameForCluster(cluster string) string {
	name := scopeLabel(cluster)
	if strings.HasPrefix(name, "k8s-") || strings.HasPrefix(name, "kubernetes-") {
		return name
	}
	return "k8s-" + name
}

func advertisedReadURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return ""
	}
	podIP := strings.TrimSpace(os.Getenv("POD_IP"))
	if podIP != "" {
		host = podIP
	} else if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, port)
}

func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
