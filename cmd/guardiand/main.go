package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/buildinfo"
	"github.com/rydzu/ainfra/guardian/internal/compliance"
	"github.com/rydzu/ainfra/guardian/internal/config"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/common"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/dispatcher"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/reconciler"
	"github.com/rydzu/ainfra/guardian/internal/orchestrator/results"
	watcherpkg "github.com/rydzu/ainfra/guardian/internal/orchestrator/watcher"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	monofsstore "github.com/rydzu/ainfra/guardian/internal/store/monofs"
	"github.com/rydzu/ainfra/guardian/internal/telemetry"
	uiuipkg "github.com/rydzu/ainfra/guardian/internal/ui"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	// fastResultScanInterval is used when active tasks are in flight.
	// It acts as a tight safety net for missed watch events.
	fastResultScanInterval = 15 * time.Second
	// idleResultScanInterval is used when no active tasks are observed.
	// It keeps costs low during quiet periods.
	idleResultScanInterval = 2 * time.Minute
)

func main() {
	var configPath string
	var reconcileInterval string
	var uiListen string
	flag.StringVar(&configPath, "config", "", "path to guardian config file")
	flag.StringVar(&reconcileInterval, "reconcile-interval", "10m", "interval between full Guardian reconcile cycles")
	flag.StringVar(&uiListen, "ui-listen", "", "listen address for Guardian UI/API (for example :8090)")
	flag.Parse()

	cfg := config.Default()
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		cfg = loaded
	}
	if strings.TrimSpace(uiListen) != "" {
		cfg.Guardian.UIListenAddress = uiListen
	}
	if flagProvided("reconcile-interval") {
		cfg.Guardian.ReconcileInterval = reconcileInterval
	}
	// Allow env override for stale task timeout without requiring a config file rebuild.
	if v := strings.TrimSpace(os.Getenv("GUARDIAN_STALE_TASK_AFTER")); v != "" {
		cfg.Guardian.StaleTaskAfter = v
	}

	telemetryCfg, err := telemetry.LoadConfig("guardiand", cfg.Guardian.PrincipalID)
	if err != nil {
		log.Fatalf("load telemetry config: %v", err)
	}
	telemetryHandle, err := telemetry.Setup(context.Background(), telemetryCfg)
	if err != nil {
		log.Fatalf("setup telemetry: %v", err)
	}
	if telemetryHandle.Enabled() {
		log.SetOutput(io.MultiWriter(os.Stderr, telemetry.NewStdLogWriter("guardian/stdlog")))
		telemetry.EmitInfo(context.Background(), "guardian/guardiand", "guardiand telemetry enabled")
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := telemetryHandle.Shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "shutdown telemetry: %v\n", err)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		store        guardianapi.Store
		storeCloser  interface{ Close() error }
		clientConfig uiuipkg.ClientConfig
	)
	if cfg.MonoFS.APIEndpoint != "" {
		clientRouterAddr := strings.TrimSpace(cfg.MonoFS.ClientAPIEndpoint)
		if clientRouterAddr == "" {
			clientRouterAddr = cfg.MonoFS.APIEndpoint
		}
		clientConfig.MonoFS = &uiuipkg.MonoFSClientConfig{
			RouterAddr:           clientRouterAddr,
			Token:                cfg.MonoFS.Token,
			UseExternalAddresses: cfg.MonoFS.DiscoveryUseExternalAddresses(),
		}
		openedStore, client, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
			RouterAddr:           cfg.MonoFS.APIEndpoint,
			Token:                cfg.MonoFS.Token,
			PrincipalID:          cfg.MonoFS.PrincipalID,
			Role:                 "control-plane",
			BaseURL:              resolveHTTPBaseURL(cfg.Guardian.UIBaseURL, cfg.Guardian.UIListenAddress),
			ClientIDPrefix:       "guardian-control-plane",
			Version:              buildinfo.Current().Version,
			MountPoint:           cfg.MonoFS.MountPath,
			UseExternalAddresses: cfg.MonoFS.UseExternalAddresses,
			Writable:             true,
		})
		if err != nil {
			log.Fatalf("open monofs store: %v", err)
		}
		store = openedStore
		storeCloser = client
	} else {
		store = memory.New()
	}
	if storeCloser != nil {
		defer func() {
			if err := storeCloser.Close(); err != nil {
				log.Printf("close monofs client: %v", err)
			}
		}()
	}

	dispatcher := dispatcher.NewDispatcher(store, cfg.Guardian.PrincipalID)
	compliancePublisher := compliance.NewNoop()
	if cfg.Compliance.S3Bucket != "" {
		publisher, err := compliance.NewS3Publisher(ctx, compliance.Config{
			S3Bucket:       cfg.Compliance.S3Bucket,
			S3Region:       cfg.Compliance.S3Region,
			S3Prefix:       cfg.Compliance.S3Prefix,
			S3Endpoint:     cfg.Compliance.S3Endpoint,
			ForcePathStyle: cfg.Compliance.ForcePathStyle,
		})
		if err != nil {
			log.Fatalf("open compliance publisher: %v", err)
		}
		compliancePublisher = publisher
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := compliancePublisher.Close(closeCtx); err != nil {
			log.Printf("close compliance publisher: %v", err)
		}
	}()
	dispatcher.SetCompliancePublisher(compliancePublisher)
	if err := dispatcher.SeedRuntimeMetrics(ctx); err != nil && ctx.Err() == nil {
		log.Printf("dispatcher: seed runtime metrics: %v", err)
	}
	staleTaskDuration, err := time.ParseDuration(cfg.Guardian.StaleTaskAfter)
	if err != nil {
		log.Fatalf("invalid stale task timeout: %v", err)
	}
	processor := results.NewProcessor(store, dispatcher)
	recon := reconciler.NewReconcilerWithOptions(store, dispatcher, cfg.Guardian.ReconcileInterval, staleTaskDuration)
	watcher := &watcherpkg.Watcher{}
	var wg sync.WaitGroup

	if strings.TrimSpace(cfg.Guardian.UIListenAddress) != "" {
		uiServer, err := uiuipkg.New(uiuipkg.Options{
			Store:             store,
			Dispatcher:        dispatcher,
			PrincipalID:       cfg.Guardian.PrincipalID,
			Pushers:           pusherNames(cfg.Pushers),
			StaleTaskAfter:    staleTaskDuration,
			ClientConfig:      clientConfig,
			ClientConfigToken: cfg.Guardian.ClientDiscoveryToken,
		})
		if err != nil {
			log.Fatalf("build ui server: %v", err)
		}
		httpServer := &http.Server{
			Addr:    cfg.Guardian.UIListenAddress,
			Handler: otelhttp.NewHandler(uiServer, "guardian.ui"),
		}
		runHTTPServer(ctx, &wg, httpServer)
		log.Printf("guardian ui listening on %s", cfg.Guardian.UIListenAddress)
	}
	run := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(ctx); err != nil && err != context.Canceled {
				log.Printf("%s stopped: %v", name, err)
			}
		}()
	}

	run("reconciler", recon.Run)
	run("partition-watcher", func(ctx context.Context) error {
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			ch, err := watcher.Watch(ctx, store, nil, time.Duration(cfg.Guardian.DebounceMs)*time.Millisecond)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("partition-watcher: Watch error (retrying in 5s): %v", err)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
				}
				continue
			}
			// Reconnected — run a full reconcile so any changes missed during the
			// outage are applied before we start watching for new ones.
			if err := recon.ReconcileAll(ctx); err != nil && ctx.Err() == nil {
				log.Printf("partition-watcher: post-reconnect reconcile error: %v", err)
			}
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case partition, ok := <-ch:
					if !ok {
						log.Printf("partition-watcher: Watch stream closed, reconnecting")
						goto reconnect
					}
					if err := recon.ReconcilePartition(ctx, partition, false); err != nil {
						log.Printf("reconcile %s: %v", partition, err)
					}
				}
			}
		reconnect:
		}
	})
	run("result-processor", func(ctx context.Context) error {
		pushers := pusherNames(cfg.Pushers)
		watchPrefixes := resultWatchPrefixes(pushers)
		scanReason := "startup"
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Establish Watch FIRST so any results written during the startup scan
			// accumulate in the channel buffer and are not missed.
			events, err := store.Watch(ctx, watchPrefixes)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				log.Printf("result-processor: Watch error (retrying in 5s): %v", err)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
				}
				continue
			}

			// Prime the initial scan interval: start fast if tasks are already
		// in flight (e.g. daemon restart mid-rollout), idle otherwise.
		initialLive, err := processLiveResultFiles(ctx, store, processor, pushers, scanReason)
			if err != nil {
				log.Printf("result-processor: %s scan failed: %v", scanReason, err)
			}
			scanReason = "reconnect"
			// Watch delivery is best-effort in the distributed store. Periodically
			// rescan active result files so a missed .results event cannot leave an
			// in-flight task stuck until the stale-task timeout elapses.
			// The interval is adaptive: fast (15s) while tasks are active, slow (2m) when idle.
			scanInterval := idleResultScanInterval
			if initialLive > 0 || err != nil {
				scanInterval = fastResultScanInterval
			}
			scanTicker := time.NewTicker(scanInterval)

			for {
				select {
				case <-ctx.Done():
					scanTicker.Stop()
					return ctx.Err()
				case <-scanTicker.C:
					liveCount, scanErr := processLiveResultFiles(ctx, store, processor, pushers, "periodic")
					if scanErr != nil {
						log.Printf("result-processor: periodic scan failed: %v", scanErr)
						// On error we cannot tell whether tasks are active; keep
						// current interval rather than downgrading to idle.
						break
					}
					newInterval := idleResultScanInterval
					if liveCount > 0 {
						newInterval = fastResultScanInterval
					}
					if newInterval != scanInterval {
						scanTicker.Reset(newInterval)
						scanInterval = newInterval
						log.Printf("result-processor: scan interval adjusted to %s (live tasks: %d)", scanInterval, liveCount)
					}
				case event, ok := <-events:
					if !ok {
						scanTicker.Stop()
						log.Printf("result-processor: Watch channel closed, reconnecting")
						goto reconnect
					}
					if !isResultPath(event.LogicalPath) {
						continue
					}
					var result taskdomain.TaskResult
					if err := readJSON(ctx, store, event.LogicalPath, &result); err != nil {
						log.Printf("read result %s: %v", event.LogicalPath, err)
						continue
					}
					if err := processor.ProcessResult(ctx, &result); err != nil {
						log.Printf("process result %s: %v", event.LogicalPath, err)
					}
					// A result just arrived via watch: tasks are clearly active.
					// Arm fast scan so any sibling results missed by the watcher
					// are caught quickly.
					if scanInterval != fastResultScanInterval {
						scanTicker.Reset(fastResultScanInterval)
						scanInterval = fastResultScanInterval
					}
				}
			}
		reconnect:
		}
	})

	<-ctx.Done()
	wg.Wait()
}

func flagProvided(name string) bool {
	provided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func runHTTPServer(ctx context.Context, wg *sync.WaitGroup, server *http.Server) {
	if server == nil {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil && err != context.Canceled {
				log.Printf("shutdown ui server: %v", err)
			}
		}()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("ui-server stopped: %v", err)
		}
	}()
}

func pusherNames(configs []config.PusherConfig) []string {
	names := make([]string, 0, len(configs))
	for _, cfg := range configs {
		if strings.TrimSpace(cfg.Name) == "" {
			continue
		}
		names = append(names, cfg.Name)
	}
	if len(names) == 0 {
		return []string{"local"}
	}
	return names
}

func resultWatchPrefixes(pushers []string) []string {
	prefixes := make([]string, 0, len(pushers))
	for _, pusher := range pushers {
		prefixes = append(prefixes, paths.QueueResultsDir(pusher))
	}
	if len(prefixes) == 0 {
		return []string{paths.QueueResultsDir("local")}
	}
	return prefixes
}

func processLiveResultFiles(ctx context.Context, store guardianapi.Store, processor *results.Processor, pushers []string, reason string) (int, error) {
	liveTaskIDs := make(map[string]bool)
	partEntries, err := store.ListDir(ctx, paths.PartitionsRoot())
	if err != nil {
		return 0, err
	}
	for _, pe := range partEntries {
		if !pe.IsDir {
			continue
		}
		runtime, loadErr := common.LoadPartitionRuntime(ctx, store, pe.Name)
		if loadErr != nil {
			continue
		}
		for intentName, state := range runtime.Intents {
			if state == nil || state.LastTaskID == "" {
				continue
			}
			activeTask, activeErr := common.HasActiveTask(ctx, store, state)
			if activeErr != nil {
				log.Printf("result-processor: %s scan active-task check %s/%s: %v", reason, pe.Name, intentName, activeErr)
				continue
			}
			if activeTask {
				liveTaskIDs[state.LastTaskID] = true
			}
		}
	}
	log.Printf("result-processor: %s scan covering %d live task IDs", reason, len(liveTaskIDs))
	for _, pn := range pushers {
		entries, scanErr := store.ListDir(ctx, paths.QueueResultsDir(pn))
		if scanErr != nil {
			log.Printf("result-processor: %s scan %s: %v", reason, pn, scanErr)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
				continue
			}
			taskID := strings.TrimSuffix(entry.Name, ".json")
			if !liveTaskIDs[taskID] {
				continue
			}
			resultPath := paths.QueueResult(pn, taskID)
			var result taskdomain.TaskResult
			if err := readJSON(ctx, store, resultPath, &result); err != nil {
				log.Printf("result-processor: %s scan read %s: %v", reason, resultPath, err)
				continue
			}
			if err := processor.ProcessResult(ctx, &result); err != nil {
				log.Printf("result-processor: %s scan process %s: %v", reason, resultPath, err)
			}
		}
	}
	log.Printf("result-processor: %s scan complete", reason)
	return len(liveTaskIDs), nil
}

func resolveHTTPBaseURL(explicit, listen string) string {
	raw := strings.TrimSpace(explicit)
	if raw == "" {
		raw = strings.TrimSpace(listen)
	}
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimSpace(parsed.Hostname())
	port := strings.TrimSpace(parsed.Port())
	switch host {
	case "", "0.0.0.0", "::":
		host = "localhost"
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else {
		parsed.Host = host
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	if parsed.Path == "/" {
		parsed.Path = ""
	} else {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func isResultPath(logicalPath string) bool {
	return strings.Contains(logicalPath, "/.results/") && strings.HasSuffix(logicalPath, ".json")
}

func readJSON(ctx context.Context, store guardianapi.ReadStore, logicalPath string, out any) error {
	data, err := store.ReadFile(ctx, logicalPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
