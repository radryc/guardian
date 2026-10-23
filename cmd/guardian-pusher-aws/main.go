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

	"github.com/rydzu/ainfra/guardian/internal/awsobserve"
	"github.com/rydzu/ainfra/guardian/internal/awsread"
	"github.com/rydzu/ainfra/guardian/internal/awsscan"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/onboarding"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	awsdriver "github.com/rydzu/ainfra/guardian/internal/pusher/drivers/aws"
	"github.com/rydzu/ainfra/guardian/internal/pusher/probes"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	runtimepkg "github.com/rydzu/ainfra/guardian/internal/pusher/runtime"
	"github.com/rydzu/ainfra/guardian/internal/pusher/secrets"
	fsstore "github.com/rydzu/ainfra/guardian/internal/store/fs"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
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
	var cfnExecutionRole string
	var unclaimedTaskRetryDelay time.Duration
	var maxConcurrency int
	var shardIndex int
	var shardCount int
	var awsReadAddr string
	var awsReadToken string
	var awsReadDisabled bool
	var eventsQueueURL string
	var eventsDisabled bool
	var observedOnly bool
	var onboardingSSMPrefix string
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
	flag.StringVar(&cfnExecutionRole, "cfn-execution-role", envOr("GUARDIAN_CFN_EXECUTION_ROLE", "per-intent"), "CloudFormation execution role: per-intent (role per partition/intent with only the declared template actions) or shared (account bootstrap role)")
	flag.DurationVar(&unclaimedTaskRetryDelay, "unclaimed-task-retry-delay", 15*time.Second, "minimum delay before retrying a task that could not be claimed; 0 disables backoff")
	flag.IntVar(&maxConcurrency, "max-concurrency", 0, "maximum tasks executed concurrently (0 = auto from CPU count)")
	flag.IntVar(&shardIndex, "shard-index", 0, "shard index for this replica when sharding a shared queue")
	flag.IntVar(&shardCount, "shard-count", 1, "number of shards sharing the queue; 1 disables sharding")
	flag.StringVar(&awsReadAddr, "awsread-addr", envOr("GUARDIAN_AWSREAD_ADDR", ":19090"), "HTTP listen address for the read-only CloudWatch/X-Ray API (empty disables)")
	flag.StringVar(&awsReadToken, "awsread-token", os.Getenv("GUARDIAN_AWSREAD_TOKEN"), "bearer token required by the AWS read API (empty disables auth)")
	flag.BoolVar(&awsReadDisabled, "awsread-disabled", false, "disable the read-only CloudWatch/X-Ray API")
	flag.StringVar(&eventsQueueURL, "events-queue-url", os.Getenv("GUARDIAN_AWS_EVENTS_QUEUE_URL"), "SQS queue URL carrying CloudFormation EventBridge events (empty disables)")
	flag.BoolVar(&eventsDisabled, "events-disabled", false, "disable the CloudFormation EventBridge event consumer")
	flag.BoolVar(&observedOnly, "observed-only", false, "run only the observed-state API and event consumer (no MonoFS task queue)")
	flag.StringVar(&onboardingSSMPrefix, "onboarding-ssm-prefix", "", "SSM parameter prefix containing customer onboarding descriptors (empty disables)")
	flag.Parse()

	if account == "" {
		flag.Usage()
		os.Exit(2)
	}
	if observedOnly {
		if storeDir != "" || monofsRouter != "" {
			log.Fatalf("--observed-only does not use a Guardian store; remove --store-dir/--monofs-router")
		}
	} else if (storeDir == "") == (monofsRouter == "") {
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
	telemetryCfg.ServiceName = "guardian.pusher." + pusherName
	telemetryCfg.ResourceAttributes = map[string]string{
		"service.alias":        pusherName,
		"guardian.pusher":      pusherName,
		"guardian.provider":    "aws",
		"guardian.account":     account,
		"guardian.region":      region,
		"guardian.shard.index": fmt.Sprintf("%d", shardIndex),
		"guardian.shard.count": fmt.Sprintf("%d", shardCount),
	}
	telemetryHandle, telemetryErr := telemetry.Setup(context.Background(), telemetryCfg)
	if telemetryErr != nil {
		log.Fatalf("setup telemetry: %v", telemetryErr)
	}
	if telemetryHandle.Enabled() {
		log.SetOutput(io.MultiWriter(os.Stderr, telemetry.NewStdLogWriter("guardian.stdlog")))
		telemetry.EmitInfo(context.Background(), "guardian.pusher.aws", "guardian aws pusher telemetry enabled")
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
	if observedOnly {
		store = memory.New()
	} else if monofsRouter != "" {
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

	observedCache := awsobserve.NewCache(0)
	observedService := awsobserve.NewService(observedCache, awsobserve.NewRefresher(account, assumeRoleName, assumeRoleExternalID))
	observedService.StateDir = awsStateDir

	var (
		runtimeRunner *runtimepkg.Runtime
		scanRunner    *awsscan.ScanRunner
	)
	if !observedOnly {
		backend, err := awsdriver.NewCLIBackend(cdkBinary, awsStateDir, assumeRoleName, assumeRoleExternalID, bootstrapStackName)
		if err != nil {
			log.Fatalf("configure aws backend: %v", err)
		}
		sdkBackend := awsdriver.NewAWSBackend("", region, false)
		composite := awsdriver.NewCompositeBackend(backend, sdkBackend)
		reg := registry.New()
		awsdriver.RegisterWithOptions(reg, composite, secrets.NewStoreResolver(store), awsdriver.Options{
			ExecutionRoleMode: cfnExecutionRole,
		})
		runtimeRunner = &runtimepkg.Runtime{
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
		scanRunner = &awsscan.ScanRunner{
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
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if !observedOnly {
		// Observed-only pushers intentionally run without a Guardian store;
		// their registration stays managed by guardianctl. Only task-queue
		// pushers publish liveness heartbeats into the store they own.
		runtimepkg.StartRegistration(ctx, store, runtimepkg.PusherInfo{
			Name:           pusherName,
			Provider:       "aws",
			Account:        account,
			Region:         region,
			ShardIndex:     shardIndex,
			ShardCount:     shardCount,
			Capabilities:   []string{"metrics", "logs", "traces", "probes", "observed", "cloudwatch"},
			OTLPEndpoint:   telemetryCfg.Endpoint,
			CollectorURL:   probes.HTTPEndpoint(telemetryCfg.Endpoint),
			WorkerID:       workerID,
			ObservedAPIURL: envOr("GUARDIAN_OBSERVED_API_URL", ""),
		})
	}

	var onboardingReader onboarding.Reader
	if prefix := strings.TrimSpace(onboardingSSMPrefix); prefix != "" {
		if observedService == nil || observedService.Refresher == nil {
			log.Printf("onboarding SSM reader disabled: observed refresher is not configured")
		} else if strings.TrimSpace(region) == "" {
			log.Printf("onboarding SSM reader disabled: --region is required to read %s", prefix)
		} else if client, err := observedService.Refresher.SSMClient(ctx, region); err != nil {
			log.Printf("onboarding SSM reader disabled: %v", err)
		} else {
			onboardingReader = &onboarding.SSMReader{
				Client:  client,
				Prefix:  prefix,
				Account: account,
				Region:  region,
			}
			log.Printf("onboarding descriptor reader enabled: %s", prefix)
		}
	}

	probeCfg := probes.ConfigFromEnv()
	probeCfg.Store = store
	probeCfg.Resolver = secrets.NewStoreResolver(store)
	probeCfg.Pusher = pusherName
	probeCfg.Provider = "aws"
	probeCfg.Account = account
	probeCfg.Region = region
	probeCfg.WorkerID = workerID
	probeCfg.ShardIndex = shardIndex
	probeCfg.ShardCount = shardCount
	if probeCfg.OTLPEndpoint == "" {
		probeCfg.OTLPEndpoint = probes.HTTPEndpoint(telemetryCfg.Endpoint)
	}
	if onboardingReader != nil {
		probeCfg.ExternalTargets = func(ctx context.Context) ([]probes.Target, error) {
			descriptors, err := onboardingReader.List(ctx)
			if err != nil {
				return nil, err
			}
			return probes.TargetsFromOnboarding(pusherName, descriptors), nil
		}
	}
	probeRunner := probes.New(probeCfg)
	go func() {
		if err := probeRunner.Run(ctx); err != nil && err != context.Canceled {
			log.Printf("probe runner stopped: %v", err)
		}
	}()

	taskErr := make(chan error, 1)
	scanErr := make(chan error, 1)
	readErr := make(chan error, 1)
	eventsErr := make(chan error, 1)
	goroutines := 0
	if runtimeRunner != nil {
		goroutines += 2
		go func() {
			taskErr <- runtimeRunner.Run(ctx)
		}()
		go func() {
			scanErr <- scanRunner.Run(ctx)
		}()
	}

	eventsEnabled := !eventsDisabled && strings.TrimSpace(eventsQueueURL) != ""
	if eventsEnabled {
		sqsClient, err := awsobserve.NewSQSClient(ctx, region)
		if err != nil {
			log.Fatalf("configure observed event consumer: %v", err)
		}
		consumer := &awsobserve.Consumer{
			Client:   sqsClient,
			QueueURL: eventsQueueURL,
			Cache:    observedCache,
		}
		goroutines++
		go func() {
			log.Printf("observed event consumer polling %s", eventsQueueURL)
			eventsErr <- consumer.Run(ctx)
		}()
	} else {
		log.Printf("observed event consumer disabled (set --events-queue-url or GUARDIAN_AWS_EVENTS_QUEUE_URL to enable)")
	}

	if !awsReadDisabled && strings.TrimSpace(awsReadAddr) != "" {
		readerConfig := awsread.Config{
			Account:       account,
			DefaultRegion: region,
			Regions:       splitRegions(region),
		}
		reader := awsread.NewAWSReader(readerConfig)
		promQL := awsread.NewPromQLClient(readerConfig)
		readServer := &http.Server{
			Addr: awsReadAddr,
			Handler: awsread.NewHandler(reader, awsReadToken).
				WithPromQL(promQL).
				WithObserved(observedService).
				WithOnboarding(onboardingReader).
				WithProbes(probeRunner).
				WithIdentity(pusherName, account, region),
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

	if observedOnly && goroutines == 0 {
		log.Fatalf("--observed-only requires the read API (--awsread-addr) or the event consumer (--events-queue-url)")
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
		case err := <-eventsErr:
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
