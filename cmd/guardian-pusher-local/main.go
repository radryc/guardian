package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/pusher/drivers"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	runtimepkg "github.com/rydzu/ainfra/guardian/internal/pusher/runtime"
	fsstore "github.com/rydzu/ainfra/guardian/internal/store/fs"
	monofsstore "github.com/rydzu/ainfra/guardian/internal/store/monofs"
	"github.com/rydzu/ainfra/guardian/internal/telemetry"
	"github.com/rydzu/ainfra/guardian/internal/versioning/revisions"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

func main() {
	var pusherName string
	var storeDir string
	var workerID string
	var monofsRouter string
	var monofsToken string
	var monofsPrincipalID string
	var unclaimedTaskRetryDelay time.Duration
	flag.StringVar(&pusherName, "pusher-name", "", "pusher name")
	flag.StringVar(&storeDir, "store-dir", "", "filesystem-backed Guardian store")
	flag.StringVar(&workerID, "worker-id", "", "worker identifier")
	flag.StringVar(&monofsRouter, "monofs-router", "", "MonoFS router address")
	flag.StringVar(&monofsToken, "monofs-token", "", "MonoFS guardian token")
	flag.StringVar(&monofsPrincipalID, "monofs-principal-id", "", "MonoFS guardian principal ID (defaults to guardian-pusher-<pusher-name>)")
	flag.DurationVar(&unclaimedTaskRetryDelay, "unclaimed-task-retry-delay", 15*time.Second, "minimum delay before retrying a task that could not be claimed; 0 disables backoff")
	flag.Parse()

	if pusherName == "" || (storeDir == "" && monofsRouter == "") || (storeDir != "" && monofsRouter != "") {
		flag.Usage()
		os.Exit(2)
	}
	if workerID == "" {
		workerID = revisions.NewCorrelationID()
	}
	monofsPrincipalID = runtimepkg.ResolvePrincipalID(pusherName, monofsPrincipalID)
	telemetryCfg, telemetryErr := telemetry.LoadConfig("guardian-pusher-local", monofsPrincipalID)
	if telemetryErr != nil {
		log.Fatalf("load telemetry config: %v", telemetryErr)
	}
	telemetryHandle, telemetryErr := telemetry.Setup(context.Background(), telemetryCfg)
	if telemetryErr != nil {
		log.Fatalf("setup telemetry: %v", telemetryErr)
	}
	if telemetryHandle.Enabled() {
		log.SetOutput(io.MultiWriter(os.Stderr, telemetry.NewStdLogWriter("guardian/stdlog")))
		telemetry.EmitInfo(context.Background(), "guardian/pusher/local", "guardian local pusher telemetry enabled")
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
			RouterAddr:     monofsRouter,
			Token:          monofsToken,
			PrincipalID:    monofsPrincipalID,
			Role:           "pusher",
			ClientIDPrefix: "guardian-pusher",
			Version:        "guardian-pusher-local",
			Writable:       true,
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

	reg := registry.New()
	reg.Register(drivers.NewComputeDriver())
	dbDriver := drivers.NewDatabaseDriver()
	reg.Register(dbDriver)
	reg.RegisterAs(assetdomain.TypeSQLDatabase, dbDriver)

	runtime := &runtimepkg.Runtime{
		QueuePath:               paths.QueueDir(pusherName),
		WorkerID:                workerID,
		PrincipalID:             monofsPrincipalID,
		Store:                   store,
		Registry:                reg,
		PollInterval:            5 * time.Second,
		UnclaimedTaskRetryDelay: unclaimedTaskRetryDelay,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runtime.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}
