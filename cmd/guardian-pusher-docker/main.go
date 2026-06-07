package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	dockerdriver "github.com/rydzu/ainfra/guardian/internal/pusher/drivers/docker"
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
	var dockerBinary string
	var dockerStateDir string
	var dockerAddHosts string
	var unclaimedTaskRetryDelay time.Duration
	flag.StringVar(&pusherName, "pusher-name", "", "pusher name")
	flag.StringVar(&cluster, "cluster", "", "target cluster handled by this worker")
	flag.StringVar(&storeDir, "store-dir", "", "filesystem-backed Guardian store")
	flag.StringVar(&workerID, "worker-id", "", "worker identifier")
	flag.StringVar(&monofsRouter, "monofs-router", "", "MonoFS router address")
	flag.StringVar(&monofsToken, "monofs-token", "", "MonoFS guardian token")
	flag.StringVar(&monofsPrincipalID, "monofs-principal-id", "", "MonoFS guardian principal ID (defaults to guardian-pusher-<pusher-name>)")
	flag.BoolVar(&monofsUseExternalAddresses, "monofs-use-external-addresses", false, "prefer MonoFS external addresses when refreshing cluster info")
	flag.StringVar(&dockerBinary, "docker-binary", "docker", "docker CLI binary to use for apply/diff/destroy")
	flag.StringVar(&dockerStateDir, "docker-state-dir", "/var/lib/guardian/pusher-docker", "host-visible directory for staged docker config and inline files")
	flag.StringVar(&dockerAddHosts, "docker-add-hosts", "", "comma-separated docker add-host mappings (host:address)")
	flag.DurationVar(&unclaimedTaskRetryDelay, "unclaimed-task-retry-delay", 15*time.Second, "minimum delay before retrying a task that could not be claimed; 0 disables backoff")
	flag.Parse()

	if pusherName == "" || cluster == "" || (storeDir == "" && monofsRouter == "") || (storeDir != "" && monofsRouter != "") {
		flag.Usage()
		os.Exit(2)
	}
	if workerID == "" {
		workerID = revisions.NewCorrelationID()
	}
	monofsPrincipalID = runtimepkg.ResolvePrincipalID(pusherName, monofsPrincipalID)
	telemetryCfg, telemetryErr := telemetry.LoadConfig("guardian-pusher-docker", monofsPrincipalID)
	if telemetryErr != nil {
		log.Fatalf("load telemetry config: %v", telemetryErr)
	}
	telemetryHandle, telemetryErr := telemetry.Setup(context.Background(), telemetryCfg)
	if telemetryErr != nil {
		log.Fatalf("setup telemetry: %v", telemetryErr)
	}
	if telemetryHandle.Enabled() {
		log.SetOutput(io.MultiWriter(os.Stderr, telemetry.NewStdLogWriter("guardian/stdlog")))
		telemetry.EmitInfo(context.Background(), "guardian/pusher/docker", "guardian docker pusher telemetry enabled")
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
			ClientIDPrefix:       "guardian-pusher-docker",
			Version:              "guardian-pusher-docker",
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

	dockerBackend, err := dockerdriver.NewCLIBackend(dockerBinary, dockerStateDir)
	if err != nil {
		log.Fatalf("configure docker backend: %v", err)
	}
	defaultExtraHosts, err := parseAddHosts(dockerAddHosts)
	if err != nil {
		log.Fatalf("parse docker add hosts: %v", err)
	}
	reg := registry.New()
	dockerdriver.Register(reg, dockerBackend, secrets.NewStoreResolver(store), dockerdriver.WithDefaultExtraHosts(defaultExtraHosts))
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

func parseAddHosts(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	out := map[string]string{}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		host, address, ok := strings.Cut(entry, "=")
		if !ok {
			host, address, ok = strings.Cut(entry, ":")
		}
		host = strings.TrimSpace(host)
		address = strings.TrimSpace(address)
		if !ok || host == "" || address == "" {
			return nil, fmt.Errorf("invalid add-host mapping %q", entry)
		}
		out[host] = address
	}
	return out, nil
}
