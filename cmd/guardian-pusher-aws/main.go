package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/awsread"
	"github.com/rydzu/ainfra/guardian/internal/awsscan"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	awsdriver "github.com/rydzu/ainfra/guardian/internal/pusher/drivers/aws"
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
	var account string
	var region string
	var storeDir string
	var workerID string
	var monofsRouter string
	var monofsToken string
	var monofsPrincipalID string
	var monofsUseExternalAddresses bool
	var cdkBinary string
	var awsStateDir string
	var assumeRoleName string
	var assumeRoleExternalID string
	var bootstrapStackName string
	var unclaimedTaskRetryDelay time.Duration
	var maxConcurrency int
	var shardIndex int
	var shardCount int
	var awsReadAddr string
	var awsReadToken string
	var awsReadDisabled bool
	flag.StringVar(&pusherName, "pusher-name", "", "pusher name (default: aws-<account>)")
	flag.StringVar(&account, "account", "", "target account handled by this worker")
	flag.StringVar(&region, "region", "", "optional target region filter handled by this worker (empty = all regions in account)")
	flag.StringVar(&storeDir, "store-dir", "", "filesystem-backed Guardian store")
	flag.StringVar(&workerID, "worker-id", "", "worker identifier")
	flag.StringVar(&monofsRouter, "monofs-router", "", "MonoFS router address")
	flag.StringVar(&monofsToken, "monofs-token", "", "MonoFS guardian token")
	flag.StringVar(&monofsPrincipalID, "monofs-principal-id", "", "MonoFS guardian principal ID (defaults to guardian-pusher-<pusher-name>)")
	flag.BoolVar(&monofsUseExternalAddresses, "monofs-use-external-addresses", false, "prefer MonoFS external addresses when refreshing cluster info")
	flag.StringVar(&cdkBinary, "cdk-binary", "cdk", "aws cdk CLI binary to use for synth/deploy")
	flag.StringVar(&awsStateDir, "aws-state-dir", "/var/lib/guardian/pusher-aws", "directory for temporary cloud assemblies and staged backend state")
	flag.StringVar(&assumeRoleName, "assume-role-name", "GuardianCdkDeployRole", "target-account IAM role name assumed before AWS operations; empty disables assume-role")
	flag.StringVar(&assumeRoleExternalID, "assume-role-external-id", "", "optional external ID passed to target-account role assumption")
	flag.StringVar(&bootstrapStackName, "bootstrap-stack-name", "CDKToolkit", "expected CDK bootstrap stack name in target accounts")
	flag.DurationVar(&unclaimedTaskRetryDelay, "unclaimed-task-retry-delay", 15*time.Second, "minimum delay before retrying a task that could not be claimed; 0 disables backoff")
	flag.IntVar(&maxConcurrency, "max-concurrency", 0, "maximum tasks executed concurrently (0 = auto from CPU count)")
	flag.IntVar(&shardIndex, "shard-index", 0, "shard index for this replica when sharding a shared queue")
	flag.IntVar(&shardCount, "shard-count", 1, "number of shards sharing the queue; 1 disables sharding")
	flag.StringVar(&awsReadAddr, "awsread-addr", envOr("GUARDIAN_AWSREAD_ADDR", ":19090"), "HTTP listen address for the read-only CloudWatch/X-Ray API (empty disables)")
	flag.StringVar(&awsReadToken, "awsread-token", os.Getenv("GUARDIAN_AWSREAD_TOKEN"), "bearer token required by the AWS read API (empty disables auth)")
	flag.BoolVar(&awsReadDisabled, "awsread-disabled", false, "disable the read-only CloudWatch/X-Ray API")
	flag.Parse()

	if account == "" || (storeDir == "" && monofsRouter == "") || (storeDir != "" && monofsRouter != "") {
		flag.Usage()
		os.Exit(2)
	}
	if pusherName == "" {
		pusherName = "aws-" + scopeLabel(account)
	}
	if workerID == "" {
		workerID = revisions.NewCorrelationID()
	}
	monofsPrincipalID = runtimepkg.ResolvePrincipalID(pusherName, monofsPrincipalID)
	telemetryCfg, telemetryErr := telemetry.LoadConfig("guardian-pusher-aws", monofsPrincipalID)
	if telemetryErr != nil {
		log.Fatalf("load telemetry config: %v", telemetryErr)
	}
	telemetryHandle, telemetryErr := telemetry.Setup(context.Background(), telemetryCfg)
	if telemetryErr != nil {
		log.Fatalf("setup telemetry: %v", telemetryErr)
	}
	if telemetryHandle.Enabled() {
		log.SetOutput(io.MultiWriter(os.Stderr, telemetry.NewStdLogWriter("guardian/stdlog")))
		telemetry.EmitInfo(context.Background(), "guardian/pusher/aws", "guardian aws pusher telemetry enabled")
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
			ClientIDPrefix:       "guardian-pusher-aws",
			Version:              "guardian-pusher-aws",
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

	backend, err := awsdriver.NewCLIBackend(cdkBinary, awsStateDir, assumeRoleName, assumeRoleExternalID, bootstrapStackName)
	if err != nil {
		log.Fatalf("configure aws backend: %v", err)
	}
	sdkBackend := awsdriver.NewAWSBackend("", region, false)
	composite := awsdriver.NewCompositeBackend(backend, sdkBackend)
	reg := registry.New()
	awsdriver.Register(reg, composite, secrets.NewStoreResolver(store))
	runtime := &runtimepkg.Runtime{
		QueuePath:               paths.QueueDir(pusherName),
		WorkerID:                workerID,
		PrincipalID:             monofsPrincipalID,
		Store:                   store,
		Registry:                reg,
		PollInterval:            5 * time.Second,
		UnclaimedTaskRetryDelay: unclaimedTaskRetryDelay,
		MaxConcurrency:          maxConcurrency,
		ShardIndex:              shardIndex,
		ShardCount:              shardCount,
		CanHandle: func(task *taskdomain.Task) bool {
			if !strings.EqualFold(strings.TrimSpace(task.Target.Account), account) {
				return false
			}
			if strings.TrimSpace(region) == "" {
				return true
			}
			return strings.EqualFold(strings.TrimSpace(task.Target.Region), region)
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	scanRunner := &awsscan.ScanRunner{
		PusherName:           pusherName,
		Account:              account,
		Region:               region,
		AssumeRoleName:       assumeRoleName,
		AssumeRoleExternalID: assumeRoleExternalID,
		WorkerID:             workerID,
		PrincipalID:          monofsPrincipalID,
		Store:                store,
		ShardIndex:           shardIndex,
		ShardCount:           shardCount,
	}

	taskErr := make(chan error, 1)
	scanErr := make(chan error, 1)
	readErr := make(chan error, 1)
	goroutines := 2
	go func() {
		taskErr <- runtime.Run(ctx)
	}()
	go func() {
		scanErr <- scanRunner.Run(ctx)
	}()

	if !awsReadDisabled && strings.TrimSpace(awsReadAddr) != "" {
		readerConfig := awsread.Config{
			Account:       account,
			DefaultRegion: region,
			Regions:       splitRegions(region),
		}
		reader := awsread.NewAWSReader(readerConfig)
		promQL := awsread.NewPromQLClient(readerConfig)
		readServer := &http.Server{
			Addr:    awsReadAddr,
			Handler: awsread.NewHandler(reader, awsReadToken).WithPromQL(promQL),
		}
		goroutines++
		go func() {
			log.Printf("aws read API listening on %s", awsReadAddr)
			go func() {
				<-ctx.Done()
				_ = readServer.Shutdown(context.Background())
			}()
			if err := readServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				readErr <- err
				return
			}
			readErr <- nil
		}()
	}

	var runErr error
	for received := 0; received < goroutines; received++ {
		select {
		case err := <-taskErr:
			if err != nil && !errors.Is(err, context.Canceled) && runErr == nil {
				runErr = err
				cancel()
			}
		case err := <-scanErr:
			if err != nil && !errors.Is(err, context.Canceled) && runErr == nil {
				runErr = err
				cancel()
			}
		case err := <-readErr:
			if err != nil && !errors.Is(err, context.Canceled) && runErr == nil {
				runErr = err
				cancel()
			}
		}
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitRegions(region string) []string {
	region = strings.TrimSpace(region)
	if region == "" {
		return nil
	}
	parts := strings.Split(region, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
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
