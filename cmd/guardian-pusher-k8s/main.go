package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/buildinfo"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	kubernetesdriver "github.com/rydzu/ainfra/guardian/internal/pusher/drivers/kubernetes"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	runtimepkg "github.com/rydzu/ainfra/guardian/internal/pusher/runtime"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	fsstore "github.com/rydzu/ainfra/guardian/internal/store/fs"
	monofsstore "github.com/rydzu/ainfra/guardian/internal/store/monofs"
	"github.com/rydzu/ainfra/guardian/internal/telemetry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
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
	var unclaimedTaskRetryDelay time.Duration
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
	flag.DurationVar(&unclaimedTaskRetryDelay, "unclaimed-task-retry-delay", 15*time.Second, "minimum delay before retrying a task that could not be claimed; 0 disables backoff")
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
	telemetryHandle, telemetryErr := telemetry.Setup(context.Background(), telemetryCfg)
	if telemetryErr != nil {
		log.Fatalf("setup telemetry: %v", telemetryErr)
	}
	if telemetryHandle.Enabled() {
		log.SetOutput(io.MultiWriter(os.Stderr, telemetry.NewStdLogWriter("guardian/stdlog")))
		telemetry.EmitInfo(context.Background(), "guardian/pusher/k8s", "guardian kubernetes pusher telemetry enabled")
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

	kubeBackend, err := kubernetesdriver.NewCLIBackend(kubectlBinary, kubeconfig, kubeContext)
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
		CanHandle: func(t *taskdomain.Task) bool {
			return strings.EqualFold(strings.TrimSpace(t.Target.Cluster), cluster) &&
				strings.EqualFold(strings.TrimSpace(t.TargetPusher), pusherName)
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runtime.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
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

func defaultPusherNameForCluster(cluster string) string {
	name := scopeLabel(cluster)
	if strings.HasPrefix(name, "k8s-") || strings.HasPrefix(name, "kubernetes-") {
		return name
	}
	return "k8s-" + name
}
