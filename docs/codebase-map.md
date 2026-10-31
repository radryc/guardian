# Guardian Codebase Map

Status: October 2026, derived from source.
This is a per-file description of every Go and TypeScript file in the
repository (429 files). Test files are described by what they cover. The goal
is orientation: read this first, then open the file.

Legend:

- **Production** — compiled into a binary.
- **Test** — `_test.go`.
- **Embedded** — TypeScript template embedded into the AWS pusher.
- **Entry point** — `main` package.

---

## 1. `cmd/`

### 1.1 `cmd/guardiand` — control plane

| File | What it does |
|---|---|
| `main.go` | Entry point. Parses flags, loads config/env, sets up telemetry, opens MonoFS or memory store, constructs dispatcher/processor/reconciler, installs optional authz, starts UI/API and all background loops, handles shutdown. Contains the result worker pool, live-result safety scan, pusher-name derivation, observed/alerting constructors. |
| `envconfig.go` | Applies `GUARDIAN_PUSHERS`, observed/onboarding/repo-import env overrides; parses `name:queueDir` pushers and `GUARDIAN_ACCESS_INTERVAL`. |
| `envconfig_test.go` | Tests pusher parsing, env application, defaults untouched, invalid values ignored, nil safety. |
| `main_test.go` | Tests live-result scanning (terminal/active/reference cases), stable partition worker hashing, many-result processing; benchmark. |

### 1.2 `cmd/guardianctl` — operator CLI

| File | What it does |
|---|---|
| `main.go` | Root flags and connection modes (local dir, direct MonoFS, Guardian discovery). Defines the command registry, `store`/`filesystem` commands, `intent put/push`, `asset` describe/catalog, `partition init/set-config/tag/list`, rollback, and shared store helpers. |
| `auth.go` | `auth login/logout/whoami`: OIDC device flow, token cache in `~/.config/guardianctl/token.json`, bearer helper. |
| `access_cmd.go` | `access publish/compile/explain/who-can`: writes raw ACL files, compiles via MonoFS policy, explains grants. |
| `notify.go` | Sends a test alert to a configured webhook. |
| `publisher.go` | Builds a MonoFS workspace publisher for commands that need one. |
| `partition_push.go` | Loads a local partition bundle (config/intents/payloads/secrets), validates, writes to the store, deletes missing files when requested, emits `partition.pushed`, optionally reconciles. |
| `partition_delete.go` | Requests deferred deletion via `.deletion.json`; `--force` recursively purges without DESTROY. |
| `partition_reconcile.go` | Runs one reconcile pass for a partition (or all), optional force apply and wait. |
| `partition_rollout_diff.go` | Compares local manifests with remote intents and reports changed/added/removed intents/assets. |
| `partition_status.go` | Loads and prints partition/intent status; `wait` polls until a target status or all intents match. |
| `partition_tags.go` | Derives release tags from archived asset versions; lists tags; rolls back intents to a tag. |
| `partition_assignments.go` | Shows repo/pusher assignment for a partition. |
| `partition_validate_local.go` | Validates a local bundle offline (YAML, schema, payload references) without a store. |
| `repo_cmd.go` | Registers repos, seeds bindings, syncs/applies/ingests repo partitions, sends notify, imports from local checkout or MonoFS source. |
| `sets_cmd.go` | Creates/updates/lists virtual sets and membership. |
| `template_cmd.go` | Lists/shows embedded CDK templates and their metadata. |
| `release.go` | Orchestrates image release → tag/stamp/push → reconcile/wait, or a hard-coded “all” list. |
| `probe_cmd.go` | Lists/shows probe snapshots. |
| `pusher_cmd.go` | Adds/lists/removes platform pusher registrations (including observed API URL/token). |
| `observability_cmd.go` | Lists canonical observability identities. |
| `onboard.go` | Runs the onboarding controller once for a pusher/prefix. |
| `bootstrap.go` | Development-only `dev` commands: setup/build/deploy/init/stop/destroy/status, kind cluster and registry wiring, calls shared image helpers, URL stamping. |
| `aws.go` | `aws setup` (deploy role, policy, Roles Anywhere) and `aws init` (CFN execution policies, CDK bootstrap, ECR repos). |
| `aws_deploy.go` | `aws pusher-deploy`: `npx cdk deploy AwsPusherStack` wrapper. |
| `aws_ecr.go` | ECR login helper (`aws ecr get-login-password` → `docker login`). |
| `aws_observed.go` | `aws observed-generate`: cloud assembly → observed/managed intents. |
| `aws_scan.go` | `aws scan`: writes a scan request, polls the result, generates a draft bundle, optionally pushes/reconciles. |
| `aws_unmanaged.go` | Lists unmanaged stacks and associates one with a repo/partition. |
| `image_common.go` | Shared image manifest loading, Dockerfile resolution, build-arg resolution, state file (`.state/images.json`), tag helpers, node ref stamping. |
| `image_build.go` | `image build` plus shared `buildImages` (used by release/dev), always stamps VERSION/COMMIT/BUILD_TIME. |
| `image_push.go` | `image push` plus shared `pushImages` (retry + optional library mirror) and `computeImageTag` (fails instead of `latest`). |
| `image_release.go` | `image release`: per-directory/all orchestration using the shared build/push/stamp helpers. |
| `image_stamp.go` | `image stamp` plus shared `stampImages`: writes image tags/sourceImage into intent YAML. |
| `main_test.go` | CLI command registration/help, discovery, store/filesystem, push/diff/delete, status/wait, tag/rollback, asset describe. |
| `aws_ecr_test.go` | ECR host formatting, profile arg, dry-run account ID. |
| `aws_scan_test.go` | Draft bundle writing, CSV splitting. |
| `bootstrap_test.go` | Dev config/env/node/router, command registration, dry-run deploy/build/stop/destroy, config load. |
| `image_test.go` | Image manifest loading, context expansion, Dockerfile resolution, state round-trip, stamping, build args, node refs, tags. |
| `onboard_test.go` | Onboarding command wiring. |
| `partition_assignments_test.go` | Assignment command output. |
| `partition_push_test.go` | Push event writing for new/updated partitions. |
| `repo_cmd_test.go` | Repo seed/binding registration. |
| `repo_partition_import_test.go` | Import happy path, inline-secret rejection, configured partition path, display logical path, local checkout/store mapping. |
| `sets_cmd_test.go` | Idempotent ensure and CLI lifecycle. |

### 1.3 Pusher binaries

| File | What it does |
|---|---|
| `cmd/guardian-pusher-aws/main.go` | AWS worker: flags/env, telemetry, MonoFS/FS store, composite SDK+CLI backend, driver registration, runtime with account/region `CanHandle`, scan runner, SQS observed-event consumer, probe runner, onboarding SSM reader, observed read API (loopback `:19090` default), observed-only mode, graceful shutdown. |
| `cmd/guardian-pusher-aws/main_test.go` | Loopback listen-address classification test. |
| `cmd/guardian-pusher-docker/main.go` | Docker worker: CLI backend, driver registration, runtime, probes, state dir, telemetry. |
| `cmd/guardian-pusher-docker/main_test.go` | Tests Docker driver host/port parsing (`parseAddHosts`). |
| `cmd/guardian-pusher-k8s/main.go` | Kubernetes worker: client-go or kubectl backend selection, driver registration, runtime, probes, telemetry. |
| `cmd/guardian-pusher-k8s/main_test.go` | Tests backend selection logic. |
| `cmd/guardian-pusher-local/main.go` | In-process reference worker: local Compute/Database/Probe drivers backed by in-memory maps; runtime + probes. |
| `cmd/guardian-docker/main.go` | Standalone Docker inspect/diff CLI: snapshots Guardian-labelled resources, parses inspect JSON, converts to asset properties, diffs Network/Volume/Compute. |

### 1.4 Tools

| File | What it does |
|---|---|
| `tools/devscan/main.go` | Development scan helper: writes a scan request to a store and prints the result. |
| `tools/awscheck/main.go` | AWS credential/identity smoke check (`sts get-caller-identity`). |

---

## 2. `internal/access`

| File | What it does |
|---|---|
| `catalog.go` | Loads partition names/labels into the policy catalog and computes a digest for change detection. |
| `service.go` | Compiles `/.access` ACL + imports against the catalog; staged disabled/observe/enforce modes; `CanSee`, `IsAdmin`, `CanModify`, `FilterPartitions`, `Explain`, `PolicyJSON`, background refresh. |
| `service_test.go` | Visibility, observe mode, fail-closed, disabled permissive, roles, explain/JSON. |

## 3. `internal/alerting`

| File | What it does |
|---|---|
| `evaluator.go` | Polls partition runtimes, classifies failures, applies threshold/cooldown/repeat rules, matches channels/rules, dispatches webhooks, persists `notifications.json`, writes events. |
| `evaluator_test.go` | Fire once/recover, rule matching, webhook end-to-end. |

## 4. `internal/authz`

| File | What it does |
|---|---|
| `guard.go` | Control-plane modify guard backed by MonoFS grant store; service principal + scoped evaluator; file/inline grant loading. |
| `guard_test.go` | Maintainer/admin/anonymous, observe mode, scoping, empty store. |

## 5. `internal/awsgen`

| File | What it does |
|---|---|
| `generate.go` | Converts a `ScanResult` into a partition draft: groups resources by stack/resource, maps six kinds to assets, dedupes names, records adoptions, emits managed/unmapped/warnings. |
| `generate_test.go` | Grouping/filtering, existing adoptions, unmapped resources, region suffix collision, sanitization. |

## 6. `internal/awsobserve`

| File | What it does |
|---|---|
| `types.go` | Wire types: stack state, resources, drifted resource, event, source constants. |
| `cache.go` | Thread-safe in-memory observed cache keyed account/region/stack, bounded event history, list/get/apply. |
| `consumer.go` | SQS long-poll consumer: parse message, apply to cache, delete message; poison-message handling. |
| `events.go` | EventBridge envelope/detail parser for CloudFormation stack status and drift events. |
| `refresh.go` | Live CloudFormation refresher: per-region config cache, assume role, describe stacks, drift detection, templates/resources, SSM client. |
| `service.go` | Combines cache + refresher; list/get/events/refresh/template/resources with account guard. |
| `diff.go` | Source-upload diff: synth uploaded source, inventory desired template, fetch live template, compare. |
| `awsobserve_test.go` | Event parsing, cache merge/bound, SQS deletion. |
| `refresh_test.go` | Stack tags/outputs mapping. |

## 7. `internal/awsread`

| File | What it does |
|---|---|
| `reader.go` | Public read contract for Doctor: metric/log/trace/alarm DTOs and the `Reader` interface. |
| `aws_reader.go` | AWS SDK implementation: CloudWatch metrics/alarms, Logs Insights, X-Ray, per-region config cache, Metrics Insights query. |
| `handler.go` | HTTP handler and route switch for `/v1/aws/*`; bearer auth; meta/alarms/metrics/logs/X-Ray/PromQL/onboarding/probes/observed endpoints. |
| `promql.go` | SigV4 PromQL proxy to CloudWatch's Prometheus-compatible API; query normalization. |
| `promql_fallback.go` | Empty/unsupported PromQL fallback to ListMetrics + GetMetricData/Metrics Insights; time parsing/helpers. |
| `promql_label_values.go` | `__name__` label-values fallback from `ListMetrics`. |
| `handler_test.go` | Route/auth tests, log body, metric dimensions, X-Ray parsing, observed/probes, label fallback. |
| `promql_normalize_test.go` | Metric-selector normalization tables. |

## 8. `internal/awsscan`

| File | What it does |
|---|---|
| `types.go` | Scan request/claim/result wire types and resource DTOs (S3, EFS, SSM, Secrets, ECS, ELBv2, CFN, CloudControl inventory). |
| `queue.go` | `ScanRunner`: poll requests, shard, validate account, CAS claim (2h lease), stale-claim cleanup, execute with timeout, write result, release claim. |
| `scanner.go` | Account scanner: identity check, regions, S3/EFS/SSM/Secrets/ECS/ELBv2/CloudFormation, optional CloudControl inventory, tags, managed detection, filters, summary. |
| `queue_test.go` | Execute once, skip on result, account mismatch, expired/live claim, filters, prefixes, tags, region handling. |
| `fsstore_test.go` | End-to-end runner against a filesystem store. |

## 9. `internal/awssetup`

| File | What it does |
|---|---|
| `config.go` | AWS setup config: profile/region/account, role/policy names, output paths. |
| `identity.go` | STS caller identity resolution/formatting. |
| `bootstrap.go` | CDK bootstrap status check and `cdk bootstrap` execution; force delete/recreate. |
| `iam.go` | Deploy role/policy creation, trust policy parsing/merge, pass-role grants, Roles Anywhere trust update. |
| `policy.go` | Scoped deploy policy and CFN execution policy documents. |
| `rolesanywhere.go` | Local CA/client cert generation, trust anchor/profile creation/discovery, AWS config profile writing. |
| `iam_test.go` | Trust policy parsing/merge and role helpers. |
| `policy_test.go` | Deploy/execution policy JSON. |
| `rolesanywhere_test.go` | Cert generation, naming, trust anchor helpers. |

## 10. `internal/bootstrap`

| File | What it does |
|---|---|
| `config.go` | Dev bootstrap config schema/defaults, template rendering inputs, env-derived values, image names, namespaces. |
| `deploy.go` | Renders/applies Kubernetes templates, builds/loads images, creates namespaces/registry, deploys MonoFS/Guardian/pusher. |
| `env.go` | Computes bootstrap environment (tokens, encryption key, S3/MinIO settings), fetcher/OTel wiring, URL stamping helpers. |
| `env_test.go` | Config/env computations, OTel wiring (one test currently fails against external templates). |

## 11. `internal/buildinfo`

| File | What it does |
|---|---|
| `buildinfo.go` | Version, commit, branch, build date variables populated by ldflags; `Current()` accessor and display strings. |

## 12. `internal/cdkgen`

| File | What it does |
|---|---|
| `catalog.go` | Parses embedded `catalog.yaml`; template selection by asset type/properties/match rules; output definitions; property validation; template params. |
| `embed.go` | `//go:embed` of the AWS v1 template tree. |
| `generate.go` | Generates a deterministic TypeScript CDK app per intent: selects templates, assigns variables, resolves refs/network, renders stack, writes payload manifest, computes required IAM actions, supports fallback resources. |
| `overlay.go` | Loads global/partition template overlays with precedence partition > global > embedded; effective catalog and file map. |
| `provider.go` | Template provider abstraction, digest, policy parsing, template/platform/catalog IAM actions. |
| `catalog_test.go` | Embedded catalog validity, variants/matchers, outputs, validation, digest stability. |
| `generate_test.go` | Generation, naming, network hoist, refs, variants, unsupported assets, fallback, outputs, TS rendering. |
| `overlay_test.go` | Overlay precedence, invalid fallback, new templates, shared override, store errors, digest changes. |
| `cfn_synth_test.go` | End-to-end `tsc` + `cdk synth` of generated payloads; resource/IAM assertions. |

### Embedded templates (`internal/cdkgen/embed/templates/aws/v1`)

All are TypeScript CDK constructs embedded into the pusher. Each template
directory also contains a `policy.yaml` (not listed as code) declaring the IAM
actions the generated stack needs.

| File | What it does |
|---|---|
| `_shared/helpers.ts` | Shared CDK helpers: output IDs, construct IDs, CPU/memory parsing, OTEL identity/env, secrets, health checks, sizing, DB name sanitization. |
| `_shared/handles.ts` | Cross-template handle interfaces used to connect Compute to LB/DB/Network. |
| `_shared/database.ts` | Shared Aurora implementation used by both database variants. |
| `compute.fargate/construct.ts` | ECS Fargate cluster/task/service, SG, log group, execution role, container env/config/secret mounts, volumes, health checks, public subnet/IP. |
| `database.aurora-mysql/construct.ts` | Aurora MySQL cluster via shared database helper, generated secret, public subnets, destroy policy. |
| `database.aurora-postgres/construct.ts` | Aurora PostgreSQL variant. |
| `fallback.cfn-resource/construct.ts` | Raw `CfnResource` for `AWSResource` with arbitrary type/properties. |
| `loadbalancer.alb/construct.ts` | ALB, target groups/listeners from Compute handles, SG rules. |
| `loadbalancer.nlb/construct.ts` | NLB TCP variant. |
| `network.default/construct.ts` | Implicit public-only VPC used when a template requires a network but none is declared. |
| `network.vpc/construct.ts` | Explicit public-only VPC with IGW/routes and outputs. |
| `objectstore.s3/construct.ts` | S3 bucket with versioning, S3-managed encryption, enforceSSL, auto-delete, destroy policy. |
| `observability.otel-sidecar/construct.ts` | OTel collector sidecar/gateway injection, EMF/CloudWatch/X-Ray exporters, Service Connect. |
| `volume.efs/construct.ts` | Encrypted EFS filesystem, mount targets, NFS SG. |

## 13. `internal/cdktemplate`

| File | What it does |
|---|---|
| `compare.go` | Normalizes JSON/YAML CFN templates and compares resource Type/Properties/Condition/DeletionPolicy; hash. |
| `inventory.go` | Builds resource inventory with DependsOn/Ref/GetAtt/Sub edges, sorted IDs, parent/relations. |
| `relations.go` | Derives semantic parent/typed relation edges for ECS/ELB/Lambda/EC2 resources. |
| `yaml.go` | YAML template parser with CloudFormation short-tag expansion. |
| `compare_test.go` | YAML/JSON equality, hash normalization, intrinsic expansion. |
| `inventory_test.go` | Dependency derivation, parameter refs ignored, YAML. |
| `relations_test.go` | ECS/ELB/Lambda/EC2 parent/relations, DependsOn fallback. |

## 14. `internal/cli`

| File | What it does |
|---|---|
| `command/registry.go` | Tiny command registry: register groups/commands, run by args, root help, default subcommand handling. |
| `command/help.go` | Help rendering, extended help, group usage, flag formatting. |
| `command/registry_test.go` | Registration/lookup/duplicate/help behavior. |
| `format/format.go` | Human formatting helpers (bytes, durations, status labels). |
| `output/output.go` | `Printer`: table/JSON/text output; mutation reports. |

## 15. `internal/clitoken`

| File | What it does |
|---|---|
| `token.go` | Loads/saves the CLI OIDC token cache; `Bearer` resolution; expiry hints (no refresh). |

## 16. `internal/compiler`

| File | What it does |
|---|---|
| `dag/dag.go` | Generic dependency graph with cycle detection and deterministic topological sort. |
| `dag/dag_test.go` | Sort/cycle/duplicate tests. |
| `manifest/parser.go` | Strict partition/intent YAML parser (`apiVersion`/`kind`, single document). |
| `manifest/parser_test.go` | Valid/invalid kinds, YAML errors, name handling. |
| `planner/planner.go` | Compile pipeline: parse, validate partition, iterate intents, validate assets, inject observability, resolve properties, hash/version, topo-sort intents/assets, build `CompiledPartition`. |
| `planner/planner_test.go` | Compile ordering, outputs, revisions, invalid manifests, skip/dep handling. |
| `planner/observability_test.go` | Synthetic observability injection rules. |
| `resolver/resolver.go` | Finds/parses `${...outputs...}` references and resolves them from current outputs. |
| `resolver/resolver_test.go` | Reference syntax, nested values, deferral, invalid refs. |
| `validator/validator.go` | Partition/intent schema validation: names, policies, modes, intervals, targets, joins, asset names. |
| `validator/assets.go` | Per-asset validation dispatch, payload path checks, dependency/cycle/type checks, hint validation. |
| `validator/validator_test.go` | Valid/invalid partitions/intents/assets, payload/dependency rules. |

## 17. `internal/compliance`

| File | What it does |
|---|---|
| `publisher.go` | No-op publisher and async S3 publisher: queues logical-path payloads, uploads in a single goroutine, key construction, close. No retry/SSE. |

## 18. `internal/config`

| File | What it does |
|---|---|
| `config.go` | Guardian config schema (MonoFS, Guardian, Compliance, Pushers, Notifications), defaults, YAML load, token resolution. |
| `config_test.go` | Defaults/load/token tests. |

## 19. `internal/dashboardgen`

| File | What it does |
|---|---|
| `render.go` | Builds Grafana dashboard JSON: overview, probes, Lambda/ECS metrics, logs, traces, query cheatsheet. |
| `render_test.go` | Partition signal/required-partition tests. |

## 20. `internal/domain`

### 20.1 `domain/asset`

| File | What it does |
|---|---|
| `asset.go` | Wire types: `Spec`, `Hint`, `State` (name, type, version, dependsOn, flow groups, payload, properties, skip). |
| `catalog.go` | Asset type constants and `KnownTypes()`. |
| `config.go` | Config format normalization (`text`, `yaml`, `json`, `binary`, `toml`, `ini`, `cfg`). |

### 20.2 `domain/assets` (typed specs + validators + UI catalog)

| File | What it does |
|---|---|
| `registry.go` | Type -> typed-spec registry; `Decode` via JSON round-trip; `Validate` dispatch; catalog lookup; hint resolution. |
| `helpers.go` | Shared property decoding/validation helpers (strings, ints, durations, refs). |
| `catalog.go` | UI catalog: titles/descriptions/icons/categories, default templates, fields, hints for every asset type. Large (~680 lines). |
| `compute.go` | Compute spec: image, replicas, ports, env, mounts, host binds, resources, health check, security context, networking. |
| `imagebuild.go` | ImageBuild spec: repository, registry, context, Dockerfile, target, platform, args, source image. |
| `cdkstack.go` | CDKStack spec: source type/dir, assembly, app command, entrypoint, stack name, package manager, context. |
| `sqldatabase.go` | SQLDatabase/Database spec: engine, version, size, storage, credentials, network, backup. |
| `volume.go` | Volume spec: size, access mode, ephemeral, storage class, EFS/S3 options. |
| `config.go` | Config spec: files/content, mounts, source. |
| `network.go` | Network spec: CIDR, internal/external, subnets, ports. |
| `secret.go` | Secret spec: inline value vs `secretRef`, mount/env usage. |
| `loadbalancer.go` | LoadBalancer spec: type/scheme/listeners/targets/health/cert. |
| `traefikroute.go` | TraefikRoute spec: hostname, target Compute, ports, TLS. |
| `objectstore.go` | ObjectStore spec: name, versioning, encryption, public access, lifecycle. |
| `observability.go` | Observability spec: provider, mode, targets, endpoint, CloudWatch/X-Ray. |
| `probe.go` | Probe spec: single check or journey, protocol/port/path, assertions, retries, interval, auth. |
| `k8sresource.go` | K8sResource spec: raw manifests, namespace, prune/ownership flags. |
| `awsresource.go` | AWSResource fallback spec: CFN type/properties/existing flag. |
| `*_test.go` | Per-type validation and catalog tests (`secret_test.go`, `probe_test.go`, `imagebuild_test.go`, `awsresource_test.go`, `traefikroute_test.go`, `compute_dynamic_hostname_test.go`, `registry_test.go`). |

### 20.3 Other domain packages

| File | What it does |
|---|---|
| `history/events.go` | Event record type and event names/details. |
| `history/history.go` | Deployment record and timeline types. |
| `intent/intent.go` | Intent manifest and type/implementation constants. |
| `partition/partition.go` | Partition manifest, reconciliation/defaults/probes/observability specs. |
| `release/release.go` | `ReleaseRecord`, `PartitionReleaseLog`, clone helpers. |
| `release/release_test.go` | Release record clone/merge semantics. |
| `state/state.go` | Partition/intent state, 17 statuses, resource observations, timestamps, deep clones. |
| `state/partition_status.go` | Aggregate partition status metrics/normalization/health roll-up helpers. |
| `state/partition_status_test.go` | Status roll-up/metrics tests. |
| `target/target.go` | Placement (cluster, namespace, region, account) and matching helpers. |
| `task/task.go` | Task, abstract asset, claim, result, drift/health/readiness/log types. |
| `task/redact.go` | Secret asset output redaction helpers (`RedactedValue`, `RedactSecretOutputs`, `RedactSecretStrings`). |

## 21. `internal/historyquery`

| File | What it does |
|---|---|
| `deployments.go` | Deployment filter/index; load via release-log fast path then archive scan; paging. |
| `releases.go` | Load release log/latest release; convert release record to deployment record. |
| `rollouts.go` | Load/collapse archive deployments into rollout views; diff/summary/version selection. |
| `asset_diff.go` | Diff two intent snapshots into added/updated/removed assets. |
| `deployments_test.go` | Archive index preference, release-log fast path/fallback, paging. |
| `releases_test.go` | Release log/latest release loading and record conversion. |
| `rollouts_test.go` | Rollout diff/collapse/self-heal/current semantics. |

## 22. `internal/notify`

| File | What it does |
|---|---|
| `notify.go` | Severity ranking, `Alert`, `Notifier` interface. |
| `webhook.go` | JSON webhook notifier with HMAC-SHA256 signature and bounded exponential retries. |
| `webhook_test.go` | Signature body and retry behavior. |

## 23. `internal/observability`

| File | What it does |
|---|---|
| `identity.go` | Canonical `<partition>.<intent>.<asset>` service identity; 200-char truncation with hash suffix. |
| `catalog.go` | Scans partitions/intents, resolves defaults, emits identity records with runtime classification. |
| `catalog_test.go` | Defaults/identity derivation. |

## 24. `internal/observed`

| File | What it does |
|---|---|
| `types.go` | Stack payload subset and snapshot wire types. |
| `client.go` | HTTP client for the pusher observed API (stacks/templates/resources/refresh/diff/events/onboarding/probes), bearer token, size cap, timeout. |
| `collector.go` | Observed-intent loop: discover endpoints/assets, refresh, diff desired vs live, write state/snapshots/events. |
| `diff.go` | Template comparison, hash, health mapping. |
| `resolver.go` | Static + registry endpoint resolution with TTL cache. |
| `resources.go` | Build/merge resource observations and synthetic asset names from templates, changed keys, live resources. |
| `collector_test.go` | In-sync/drift/diff-failed, inventory, source upload, no queueing. |
| `resolver_test.go` | Static/registry precedence, non-AWS skip, fallback. |
| `resources_test.go` | Status mapping, added/removed, dedupe, changed parser. |
| `summary_test.go` | Drift summary merge/truncation. |

## 25. `internal/observedgen`

| File | What it does |
|---|---|
| `generate.go` | Local CDK cloud assembly → observed/managed partition bundle; stack filtering, asset copy, intent naming/slug. |
| `snapshot.go` | One live CFN template → observed bundle; normalization. |
| `storegen.go` | Store-resident assembly → store bundle (config/intents/payloads/manifest/templates). |
| `generate_test.go` | Observed vs managed, managed-only asset copy, dedupe, mode errors. |
| `snapshot_test.go` | Bundle shape, labels, YAML normalization, validation. |
| `storegen_test.go` | Store ingest, config preservation, asset copy, slug. |

## 26. `internal/onboard` and `internal/onboarding`

| File | What it does |
|---|---|
| `onboard/controller.go` | Periodic controller that pulls descriptors and republishes observed partitions. |
| `onboard/service.go` | Validates descriptors, materializes/updates partition and observed intents, records audit, reconciles. |
| `onboard/service_test.go` | Descriptor materialization/validation. |
| `onboarding/types.go` | Descriptor schema, normalization, region/account/selector validation. |
| `onboarding/types_test.go` | Descriptor validation/round-trip. |

## 27. `internal/orchestrator`

### 27.1 `common`

| File | What it does |
|---|---|
| `common.go` | Shared helpers: load state, outputs, dependency health, in-flight detection, status preservation, task building from manifests, generated-stack substitution, clones. |
| `runtime.go` | Loads a partition runtime (partition + all intent states) and normalizes metrics/status. |
| `runtime_metrics.go` | Prometheus counters for runtime loads and intent state counts. |
| `common_test.go` | Status transitions, generated-stack substitution, authoritative state scan. |
| `generated_test.go` | CDK task building. |
| `runtime_test.go` | Runtime load/normalize. |

### 27.2 `dispatcher`

| File | What it does |
|---|---|
| `dispatcher.go` | Write side of the control plane: queue tasks (plus audit copy); write/delete state; events; archive deployments; release log; caches; compliance publish; generated payload sync; forget partition. |
| `metrics.go` | Prometheus counters/gauges for writes, bytes, skips, deletes, statuses. |
| `dispatcher_test.go` | Serialization/runtime derivation. |
| `release_test.go` | Release monotonicity/concurrency. |
| `dispatcher_bench_test.go` | Write-byte benchmark. |

### 27.3 `reconciler`

| File | What it does |
|---|---|
| `reconciler.go` | Scheduling and reconcile orchestration: full/per-partition reconcile, compile, task selection, stale tasks, removed intents, release recovery, quartz jobs, priorities, sharding. |
| `deletion.go` | Deletion marker, destroy/orphan flow, queue purge, recursive partition purge. |
| `schedule.go` | Priority heap/pool, trigger parsing (duration/cron), deterministic offset, quartz jobs. |
| `generated.go` | Generates and prunes CDK payloads; fingerprint memo. |
| `reconciler_integration_test.go` | End-to-end DIFF/CHECK/APPLY/DESTROY, lock/readonly/manual/deletion/parallel. |
| `authorizer_test.go` | Auth gate behavior. |
| `generated_test.go` | CDK generation/pruning. |
| `schedule_test.go` | Trigger/pool/priority/aging. |
| `schedule_integration_test.go` | Scheduler integration. |

### 27.4 `results`

| File | What it does |
|---|---|
| `processor.go` | Applies task results: identity/provenance checks, stale check, secret redaction, per-op transitions, follow-up queueing, rollback, archive/release, dependents, cleanup. |
| `asset_metrics.go` | Records asset health/readiness observations as Prometheus series. |
| `processor_test.go` | Success/failure/observed/ rollback/task reuse/metrics. |
| `asset_metrics_test.go` | Metric replacement/cleanup. |

### 27.5 `watcher`

| File | What it does |
|---|---|
| `watcher.go` | Watches partition config/intent paths, debounces per partition, emits partition names. |
| `watcher_test.go` | Config trigger, state-event ignore. |

## 28. `internal/paths`

| File | What it does |
|---|---|
| `paths.go` | All logical store path helpers (partitions, state, queues, scans, registry, access, archive, observed, sets, probes, task audit, repo ownership). |
| `validate.go` | Strict name/task-ID validation used at CLI, UI, repo-import, and result boundaries. |
| `paths_test.go` | Path construction/traversal behavior. |
| `validate_test.go` | Segment/task-ID/env traversal rejection. |

## 29. `internal/pusher`

### 29.1 `pusher/drivers` (local reference drivers)

| File | What it does |
|---|---|
| `compute.go` | In-memory Compute driver (image/running state), no real infrastructure. |
| `database.go` | In-memory Database driver (engine/url), no real connectivity. |
| `helpers.go` | `stringProperty` helper. |

### 29.2 `pusher/drivers/aws`

| File | What it does |
|---|---|
| `drivers.go` | Driver registration (12 drivers + probe), `baseDriver`, execution-role handling, CDKStack and Secret drivers. |
| `backend_api.go` | 43-method backend god interface + all AWS resource DTOs (EFS, VPC, DB cluster, parameter, secret, ECS, LB, target group, listener, bucket, log group, stack). |
| `backend.go` | In-memory test double with canned stacks/resources/drift. |
| `aws_backend.go` | AWS SDK backend: EFS, SSM, Secrets Manager, ECS/cluster/task/service/roles, ELBv2, S3, log groups, CFN stubs, VPC, RDS/Aurora. |
| `cli_backend.go` | CDK/CloudFormation CLI backend: stage source, `cdk synth/deploy`, stack get/drift/delete, EFS/VPC stubs. |
| `composite_backend.go` | Delegates CDK/CFN methods to CLI backend and resource methods to SDK backend; execution-role pass-through. |
| `compute.go` | ECS Fargate Compute driver: desired task/service, env/secrets, ports, volumes, diff/apply/destroy. |
| `network.go` | VPC Network driver. |
| `loadbalancer.go` | ALB/NLB driver: listeners, target groups, ECS attachment. |
| `objectstore.go` | S3 ObjectStore driver. |
| `sqldatabase.go` | Aurora SQLDatabase/Database driver (alias `typeName`). |
| `volume.go` | EFS Volume driver. |
| `config.go` | SSM Parameter Store Config driver. |
| `observability.go` | ADOT/OTel ECS task role/sidecar/daemon driver. |
| `generated_stack.go` | GeneratedStack driver: stage generated CDK tree, hash, per-intent role, synth/deploy, drift. |
| `payload.go` | CDK payload schema, desired hash, staging validation. |
| `execution_role.go` | Per-intent CFN execution role name/trust/policy split, ensure/delete. |
| `naming.go` | Deterministic AWS physical names/tags. |
| `synth.go` | `SynthTemplateFromFiles` for observed diff/source upload. |
| `adoption_test.go` | Existing-resource adoption behavior. |
| `cli_backend_test.go` | CLI backend command/env/stack behavior. |
| `drivers_test.go` | Driver end-to-end through runtime with fake backend. |
| `execution_role_test.go` | Role policy/trust. |
| `generated_stack_test.go` | Generated stack staging/hash/deploy. |
| `network_test.go` | VPC driver. |
| `observability_test.go` | OTel driver. |
| `sqldatabase_test.go` | Aurora driver. |
| `synth_test.go` | Source synth/diff. |

### 29.3 `pusher/drivers/docker`

| File | What it does |
|---|---|
| `backend_api.go` | Docker backend interface: network/volume/config/container CRUD, list by asset. |
| `backend.go` | In-memory Docker backend with clone helpers (drops ShmSize/GPUs). |
| `drivers.go` | All Docker drivers + registration + naming/hash/decoders. |
| `desired.go` | Builds desired container/network snapshots for `guardian-docker diff`. |
| `payload.go` | Loads/merges `payload.docker.yaml`, hashes with payload, applies overlays. |
| `inspect.go` | Runs `docker inspect`, parses containers/volumes/networks, converts to asset properties (including env values). |
| `inspect_diff.go` | Structural container/network drift comparison and detailed field diff. |
| `cli_backend.go` | Docker CLI backend: run/rm/network/volume, state dir for config/inline files, port conflict checks, error messages with argv. |
| `imagebuild.go` | ImageBuild driver: checks docker info and registry image presence; no build in Apply. |
| `imagebuild_backend.go` | Docker build/push/load/tag/manifest inspect helpers (mostly not called by the driver). |
| `drivers_test.go` | Driver end-to-end through runtime with memory backend; payload/secret/diff/destroy/HAProxy. |
| `dynamic_hostname_test.go` | Host port suppression for dynamic hostnames. |
| `imagebuild_test.go` | ImageBuild observed state error propagation. |
| `inspect_diff_test.go` | Inspect parsing, property mapping, structural drift. |

### 29.4 `pusher/drivers/kubernetes`

| File | What it does |
|---|---|
| `backend_api.go` | Kubernetes backend interface: ConfigMap/PVC/Deployment/Service/NetworkPolicy CRUD, pod events, raw apply. |
| `backend.go` | In-memory Kubernetes backend and object models (clone drops RunAsUser). |
| `drivers.go` | All Kubernetes drivers + registration + decoders/naming/manifests inputs. |
| `manifest.go` | Pure manifest builders: ConfigMap, PVC, Service, NetworkPolicy, Deployment, inline ConfigMap. |
| `network.go` | Network asset → NetworkPolicy; Compute joins by label. |
| `payload.go` | Loads/merges `payload.k8s.yaml`/`payload.kubernetes.yaml`. |
| `decode.go` | Shared decoders for Kubernetes objects; hash annotation/label lookup. |
| `cli_backend.go` | kubectl backend: apply JSON via stdin, namespace creation, pod failure/events. |
| `client_backend.go` | client-go dynamic backend: server-side apply (`Force: true`), discovery mapper, namespace, pod/events, raw multi-doc apply. |
| `imagebuild.go` | ImageBuild driver: registry presence only. |
| `imagebuild_backend.go` | BuildKit Job builder/runner, context tar, kubectl cp, log streaming, registry HEAD. |
| `drivers_test.go` | Driver end-to-end through runtime with memory backend; payload/config/hostPath/crashloop/network/LB. |
| `dynamic_hostname_test.go` | Service port suppression. |
| `imagebuild_test.go` | ImageBuild observed state. |
| `imagebuild_backend_test.go` | Build context tar contents. |
| `network_test.go` | NetworkPolicy apply/diff/destroy. |
| `client_backend_test.go` | Kubeconfig/apply round-trips, waiting classification, selector stability. |

### 29.5 `pusher/drivers/probe`

| File | What it does |
|---|---|
| `probe.go` | No-op Probe driver; real execution is in `internal/pusher/probes`. |

### 29.6 `pusher/driverutil`

| File | What it does |
|---|---|
| `driverutil.go` | Shared driver helpers: asset decode, payload loading, hashing, naming, clones, ownership labels. |
| `configfiles.go` | Config content precedence (inline/config map/payload) and file staging. |
| `driverutil_test.go` | Hash/payload/naming helpers. |
| `configfiles_test.go` | Config precedence. |
| `observability_test.go` | Observability env injection. |

### 29.7 `pusher/probes`

| File | What it does |
|---|---|
| `types.go` | Config/target/step/result types, env defaults, ProbeID. |
| `run.go` | Runner scheduler: discovery tick, due targets, bounded concurrency, snapshots, telemetry. |
| `discover.go` | Builds targets from partition config, intents, state, explicit probes, outputs, onboarding; sharding/limits. |
| `exec.go` | HTTP/TCP/DNS/TLS execution with retries/timeouts. |
| `grpc.go` | gRPC health + reflection-based dynamic invocation, descriptor cache. |
| `journey.go` | Multi-step HTTP/gRPC journey engine with interpolation/captures/assertions. |
| `telemetry.go` | OTLP/HTTP metrics/logs/traces exporter for probes. |
| `onboarding.go` | Descriptor → probe targets for storeless mode. |
| `sort.go` | Deterministic target/result ordering. |
| `store.go` | Latest per-partition probe snapshot with hash suppression. |
| `probes_test.go` | HTTP/gRPC/telemetry/sort/shard/snapshot. |
| `journey_test.go` | Journey interpolation/captures/assertions. |
| `store_test.go` | Snapshot write semantics. |
| `onboarding_test.go` | Descriptor conversion. |

### 29.8 `pusher/registry`

| File | What it does |
|---|---|
| `registry.go` | Asset type → driver map, `AssetDriver` contract, observed/context optional interfaces, `AssetInput`/`AssetResult`, aliases. |

### 29.9 `pusher/runtime`

| File | What it does |
|---|---|
| `runtime.go` | Worker loop: poll/watch queue, CAS claim, lease heartbeat, sharding, concurrency, asset execution, dependency blocking, result write, OTel. |
| `registration.go` | Pusher registration/heartbeat with jitter; merges token/URL/labels. |
| `identity.go` | Principal ID resolution. |
| `runtime_test.go` | Claim CAS, operations, observed state, dependency blocking, trace redaction, expired claims, retry. |
| `runtime_concurrency_test.go` | Concurrency limit/shard ownership/benchmark. |
| `registration_test.go` | Heartbeat preserves fields; waits for URL. |
| `identity_test.go` | Principal ID cases. |

### 29.10 `pusher/secrets`

| File | What it does |
|---|---|
| `secrets.go` | Resolves `monofs-secret://<partition>/<path>` from partition secrets, strings/maps; rejects traversal; no caching/redaction. |
| `secrets_test.go` | Resolution and traversal rejection. |

## 30. `internal/registry`

| File | What it does |
|---|---|
| `types.go` | Pusher/Repo/RepoBinding models, normalization/validation, token resolution, path derivation. |
| `store.go` | Store-backed pusher/repo CRUD, pagination, summary, repo sync/notify, watch cache. |
| `metrics.go` | Periodic pusher gauges. |
| `store_test.go` | CRUD/validation/bindings/cursoring/cache. |
| `binding_paths_test.go` | Binding path defaults. |

## 31. `internal/repoconfig`, `repoimport`, `workspacepub`

| File | What it does |
|---|---|
| `repoconfig/config.go` | `.guardian/config.yaml` schema, credential-free validation, repo/partition discovery. |
| `repoconfig/displaypath_test.go` | Display path derivation. |
| `repoimport/import.go` | Shared repo bundle importer: config/intents/payloads mapping, inline-secret rejection, ownership manifest + safe pruning, custom partition paths. |
| `repoimport/import_test.go` | Import mapping/rejection/tests. |
| `workspacepub/publish.go` | Chunked repo workspace upload/publish via MonoFS; per-repo branch mode. |
| `workspacepub/publish_test.go` | Publish/chunk/status tests. |

## 32. `internal/sets`

| File | What it does |
|---|---|
| `sets.go` | Virtual set store: CRUD, hierarchy, partition membership, primary set, access-filtered views. |
| `sets_test.go` | CRUD/membership/validation. |

## 33. `internal/store`

| File | What it does |
|---|---|
| `fs/store.go` | Local filesystem store: read/list/stat, upsert/delete, `.versions` history, logical→physical confinement; no watch. |
| `memory/store.go` | In-memory store: current/history maps, watchers, CAS (`absent`/expected), tombstones. |
| `memory/store_test.go` | CRUD/watch/CAS. |
| `monofs/adapter.go` | Maps Guardian logical namespace to physical MonoFS paths; implements Store; event remap; paged/stat/watch fallbacks. |
| `monofs/grpc_client.go` | MonoFS gRPC client: router/node dialing, register/heartbeat, upsert streaming, delete/list versions/get, file/dir reads, watch, topology refresh, retries. |
| `monofs/open.go` | Builds GRPC client + adapter. |
| `monofs/publisher.go` | Workspace publisher bound to the live router. |
| `monofs/adapter_test.go` | Logical/physical mapping and traversal (build tag `monofs`). |
| `monofs/adapter_runtime_test.go` | Adapter runtime behavior. |
| `monofs/grpc_client_test.go` | Register/watch/topology/retry helpers. |

## 34. `internal/telemetry`

| File | What it does |
|---|---|
| `config.go` | Telemetry config from env: OTLP endpoint, insecure, sampling, service/resource attributes. |
| `telemetry.go` | Sets up tracer/meter/logger providers, exporters, resource attributes, shutdown. |
| `log_writer.go` | `io.Writer` that parses standard log lines and emits OTLP log records. |
| `log_writer_test.go` | Log writer behavior. |

## 35. `internal/ui`

| File | What it does |
|---|---|
| `server.go` | UI/API server: options, caches, mux route table, static/template serving, central mutation authz, client-config/access/M2M gates, partition routes, bundle save/validate/reconcile/delete, JSON helpers. |
| `data.go` | Read model: all UI DTOs; overview/detail/history/rollout/activity aggregation; planner compile; health/status derivation; topology; redaction; catalog. |
| `assignments.go` | Builds partition assignment view from repo bindings/pushers; warnings for mismatches. |
| `intent_manifests.go` | Reads/sorts/parses intent manifests, duplicate-name resolution (`.yaml` over `.yml`). |
| `registry.go` | Pusher/repo CRUD handlers, repo ingest/notify/bindings, repo partition import, assembly resolution, inline-secret rejection. |
| `repo_import_loop.go` | Periodic sharded/leased repo import and auto-deploy. |
| `scan.go` | AWS scan request/list/get/bundle endpoints; result lookup; draft generation. |
| `sets.go` | Virtual set CRUD/membership/move with access-filtered views. |
| `unmanaged.go` | Unmanaged stack detect/associate handlers. |
| `probes.go` | Probe list/detail from the projection cache, filters, ETag. |
| `observability.go` | Projection cache for identities/probes (watch invalidation + TTL), ETag JSON writer, identities endpoint. |
| `federation_feed.go` | Paginated M2M federation partition feed with account/region/provider derivation. |
| `pushers_m2m.go` | M2M pusher list/detail with `requireM2MAccess`. |
| `flow_evaluator.go` | Default flow-state evaluator from observations; timestamp selection. |
| `flow_metrics_evaluator.go` | Prometheus text parser + MonoFS flow counter deltas + cached snapshot/fallback. |
| `assignments_test.go` | Assignment derivation, queue-pusher registration, native fallback. |
| `data_test.go` | Event classification, deployment status, partition summary, topology containment/relations. |
| `federation_feed_test.go` | Federation pagination and AWS placement derivation. |
| `flow_evaluator_test.go` | Observation-based flow state, no-metric-group case. |
| `flow_metrics_evaluator_test.go` | MonoFS counter delta, endpoint fallback, metric line parsing. |
| `origin_test.go` | Intent/asset origin classification for adopted/observed/generated/native/CDK. |
| `pushers_m2m_test.go` | M2M pusher detail auth and pagination. |
| `registry_test.go` | Pusher/repo API, store assembly ingest, bindings, repo import, notify, native source. |
| `repo_import_loop_shard_test.go` | Shard filtering. |
| `repo_import_loop_test.go` | Changed-partition import. |
| `scan_test.go` | Scan queueing, validation, list/get/bundle round-trip, stack filter. |
| `server_test.go` | Bundle save/read/delete, index, client config/token/loopback, overview/detail/history/rollouts/cache/stale tasks/static assets/validation. |
| `sets_test.go` | Sets API and partition existence checks. |
| `ui_test.go` | Index destinations/theme/set panels, partition detail assignments, set counts. |
| `unmanaged_test.go` | Unmanaged list/detect/associate/validation. |
| `authz_test.go` | Mutation guard: partition roles, admin-only registry actions, observe mode. |
| `redact_test.go` | Secret property redaction, manifest round-trip restore, intent-name traversal rejection. |

### 35.1 `internal/ui/static-src`

| File | What it does |
|---|---|
| `main.ts` | SPA entry: global state, URL/query routing, polling, overview/detail/history/rollout/topology rendering, badges, themes, diagnostics, refresh interval. |
| `builder.ts` | Visual partition/intent/asset editor: palette, canvas, connectors, inspector, client/server validation, drafts/templates, pusher migration, undo/redo, tutorial. |
| `sets.ts` | Set tree/cards/page, health rollup, set editor/picker/delete, repo association. |
| `scan.ts` | Scan create/list/detail/bundle preview/save, polling, stack selection. |
| `registry.ts` | Pusher/repo registration forms, binding pickers, ingest/delete/assign. |
| `unmanaged.ts` | Unmanaged stack table and associate prompt. |
| `topology.ts` | Hierarchical SVG layout, drag/select, edge paths, legend. |
| `state.ts` | `AppState` interface and defaults. |
| `utils.ts` | `fetchJSON`, DOM helpers, HTML escaping, date formatting, toasts. |

Build inputs not in the code count: `index.html`, `main.css`, `guardian.png`;
served template is `internal/ui/templates/index.html`. Generated outputs
`internal/ui/static/app.js`, `app.css` are committed build artifacts.

## 36. `internal/unmanaged`

| File | What it does |
|---|---|
| `detect.go` | Lists CloudFormation stacks, filters managed, classifies against repo bindings, refreshes all stacks. |
| `associate.go` | Builds repo/partition association and review-branch payload. |
| `service.go` | Detect/associate orchestration; registers repo before publishing; records publish errors. |
| `detect_test.go` | Detection/classification tests. |

## 37. `internal/versioning`

| File | What it does |
|---|---|
| `digest/digest.go` | Content hash and normalized JSON hash. |
| `digest/digest_test.go` | Hash stability/normalization. |
| `revisions/revisions.go` | Asset version IDs, derived versions, intent/partition/deployment revision IDs, correlation IDs. |
| `revisions/revisions_test.go` | Revision determinism/format. |

## 38. `pkg/guardianapi`

| File | What it does |
|---|---|
| `types.go` | Shared Store/ReadStore/WatchStore interfaces, path writes/deletes, mutation context, batch revisions, file info/version, dir entries/pages, change events, errors. |

---

## 39. Coverage notes

- Every `.go` and `.ts` file under `cmd/`, `internal/`, `pkg/`, and `tools/`
  is listed above (429 files total).
- `internal/cdkgen/embed/templates/aws/v1/**/policy.yaml` and `catalog.yaml`
  are data assets, not code; they are described in
  [architecture](architecture.md#102-cdk-generation).
- Generated UI bundles (`internal/ui/static/app.js`, `app.css`) and mockup
  assets are build artifacts and are not individually described.
- If a file is missing from this map, treat that as a documentation bug and add
  it here rather than adding a new prose document.
