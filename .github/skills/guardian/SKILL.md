---
name: guardian
description: "Domain-driven intent engine and infrastructure control plane on MonoFS. Use when: working on guardiand, guardianctl, pushers (local/docker/k8s), partitions, intents, assets, DAG compilation, reconciliation, drift detection, orchestrator, dispatcher, watcher, results processor, compliance publishing, secret resolution, rollback, deployment history, Guardian UI, asset drivers, or Guardian↔MonoFS store integration. Also use for partition YAML authoring, intent dependency graphs, and pusher driver development."
---

# Guardian — Domain-Driven Intent Engine

## What It Does

Guardian is a declarative infrastructure control plane layered on MonoFS that enables deploying complex infrastructure across Docker, Kubernetes, and local environments without deep cloud knowledge.

- **Partitions** = business domains (e.g., `monofs-local`)
- **Intents** = YAML blueprints declaring desired infrastructure state
- **Assets** = abstract building blocks within intents (Compute, Database, Config, Volume, Network, Service)
- **Pushers** = environment-specific executors that translate assets into real infrastructure

Core loop: **Watch → Compile → Check → Diff → Apply** (self-healing reconciliation)

Module: `github.com/rydzu/ainfra/guardian`  
Go version: 1.25  
Local dependency: `replace github.com/radryc/monofs => ../monofs`

## When to Use This Skill

- Creating or modifying partition/intent YAML manifests
- Writing or extending pusher asset drivers (Docker, K8s, local)
- Working on the orchestrator (watcher, reconciler, dispatcher, results)
- Modifying the compiler pipeline (DAG, validation, variable resolution, planner)
- Building CLI commands in `guardianctl`
- Working on the web UI or API endpoints
- Configuring Guardian↔MonoFS store integration
- Implementing secret resolution patterns
- Setting up compliance/audit archiving
- Debugging state transitions or drift detection
- Writing tests for reconciliation or driver logic

## Architecture

### Binaries

| Binary | Purpose |
|--------|---------|
| `guardiand` | Long-running daemon: watches MonoFS, compiles partitions, orchestrates reconciliation |
| `guardianctl` | Operator CLI: create/update/delete partitions+intents, status, rollback, lock/unlock |
| `guardian-pusher-local` | Reference pusher with basic Compute/Database drivers |
| `guardian-pusher-docker` | Docker/Compose pusher: networks, volumes, configs, containers |
| `guardian-pusher-k8s` | Kubernetes pusher: ConfigMaps, PVCs, Deployments, Services |

### Reconciliation Loop

```
MonoFS watch → partition/intent config change detected
  → watcher (debounced) triggers reconciliation
  → reconciler.ReconcilePartition()
    → compiler/manifest.ParsePartition() + ParseIntent()
    → compiler/validator.Validate()
    → compiler/dag.TopologicalSort() (macro-DAG: intents, micro-DAG: assets)
    → compiler/resolver.ResolveProperties() (${intent.<name>.outputs.<key>})
    → compiler/planner.Compile() → CompiledPartition
    → dispatcher.QueueTask() → write /.queues/<pusher>/<taskID>.json
```

### Pusher Execution

```
pusher/runtime polls /.queues/<pusher>/
  → tryClaimTask() (optimistic CAS: /.claims/<taskID>.json)
  → registry.AssetDriver.Check() → Diff() → Apply() (or Destroy)
  → write result to /.queues/<pusher>/.results/<taskID>.json
```

### Results Processing

```
results.ProcessResult() reads /.queues/<pusher>/.results/
  → state transitions: Check→Diff→Apply, error handling
  → auto-queues dependent intents when upstream reaches Healthy
  → archives successful deployments under /.archive/
  → compliance.Publisher.Publish() → S3 audit bucket
```

## Package Map

| Package | Path | Purpose |
|---------|------|---------|
| **Domain** | | |
| `partition` | `internal/domain/partition/` | Partition type with DeletionPolicy (orphan/destroy), ReconciliationSpec |
| `intent` | `internal/domain/intent/` | Intent with joins (macro-DAG), targetPusher, target placement, assets |
| `asset` | `internal/domain/asset/` | Asset Spec: type, name, dependsOn (micro-DAG), properties, payload |
| `task` | `internal/domain/task/` | Task: OpCheck/OpDiff/OpApply/OpDestroy, AbstractAsset list |
| `state` | `internal/domain/state/` | 14 intent states: Invalid→Blocked→Ready→Checking→Diffing→Applying→Healthy→... |
| `history` | `internal/domain/history/` | DeploymentRecord for rollback |
| `target` | `internal/domain/target/` | Placement: cluster, namespace, region, account |
| **Compiler** | | |
| `manifest` | `internal/compiler/manifest/` | YAML parsing: ParsePartition(), ParseIntent() |
| `validator` | `internal/compiler/validator/` | Schema validation, name patterns `^[a-z][a-z0-9-]*$`, join refs |
| `dag` | `internal/compiler/dag/` | Topological sort with cycle detection (intents + assets) |
| `resolver` | `internal/compiler/resolver/` | Output ref interpolation: `${intent.<name>.outputs.<key>}` |
| `planner` | `internal/compiler/planner/` | Compile() → CompiledPartition with hash-based revisions |
| **Orchestrator** | | |
| `watcher` | `internal/orchestrator/watcher/` | MonoFS change stream subscription with debouncing |
| `reconciler` | `internal/orchestrator/reconciler/` | Continuous reconciliation loop, respects `locked` flag |
| `dispatcher` | `internal/orchestrator/dispatcher/` | Queue task writes, state updates, event logging |
| `results` | `internal/orchestrator/results/` | Task result processing, state transitions, dependency chaining |
| `common` | `internal/orchestrator/common/` | Helper: LoadIntentState, LoadAllIntentStates, IntentOutputs |
| **Pusher** | | |
| `registry` | `internal/pusher/registry/` | AssetDriver interface: Type(), Validate(), Check(), Diff(), Apply(), Destroy() |
| `runtime` | `internal/pusher/runtime/` | Queue polling, task claiming (optimistic CAS), driver routing |
| `drivers` | `internal/pusher/drivers/` | Built-in: compute, database, docker/*, kubernetes/* |
| `secrets` | `internal/pusher/secrets/` | Late-bound secret resolution: `monofs-secret://partition/path` |
| `driverutil` | `internal/pusher/driverutil/` | DecodeAsset(), AssetHash() for drift detection |
| **Store** | | |
| `monofs` | `internal/store/monofs/` | MonoFS adapter: reads via mount, writes via gRPC API |
| `fs` | `internal/store/fs/` | Local filesystem store (testing/CLI) |
| `memory` | `internal/store/memory/` | In-memory store (testing) |
| **Other** | | |
| `compliance` | `internal/compliance/` | Async S3 publisher for audit/deployment archives |
| `ui` | `internal/ui/` | Embedded web dashboard + REST API |
| `cli` | `internal/cli/` | Subcommand registry for guardianctl |
| `versioning` | `internal/versioning/` | Content-based hashing: PartitionRevision(), AssetVersionID() |
| `paths` | `internal/paths/` | All logical path construction (partitions, queues, archives) |
| `config` | `internal/config/` | Config loading: MonoFS, Guardian, Compliance, Pushers |

## Key Types & Interfaces

```go
// Storage contract — all persistence goes through this
guardianapi.Store: ReadStore + WatchStore + WriteStore
guardianapi.MutationBatch: Writes []PathWrite, Context (principalID, reason, correlationID)

// Asset driver — implement for new infrastructure providers
registry.AssetDriver: Type(), Validate(), Check(), Diff(), Apply(), Destroy()

// Domain
task.Task: taskID, partition, intent, targetPusher, assets (topo-sorted)
task.TaskResult: status (Succeeded/Failed), outputs, drift, logs
state.IntentStatus: 14 states from Invalid to Orphaned

// Secrets
secrets.Resolver: Resolve(ctx, secretRef) → value
secrets.StoreResolver: reads monofs-secret:// URIs from MonoFS
```

## Intent Status State Machine

```
Invalid → Blocked (joins not met)
        → Ready → Checking → CheckFailed
                           → Diffing → Drifted → Applying → Healthy
                                                           → ApplyFailed
                                     → DriftedLocked (locked=true)
                           → Healthy (no drift)
Destroying → Destroyed
          → Orphaned (deletionPolicy: orphan)
```

## Logical Path Structure

```
/partitions/{partition}/config.yaml              # Partition manifest
/partitions/{partition}/intents/{name}.yaml       # Intent manifests
/partitions/{partition}/.state/partition.json      # Compiled partition state
/partitions/{partition}/.state/intents/{name}.json # Intent runtime state
/partitions/{partition}/.state/tasks/{id}.json     # Task records
/partitions/{partition}/.state/events/{id}.json    # Audit events
/.queues/{pusher}/{taskID}.json                    # Pending tasks
/.queues/{pusher}/.claims/{taskID}.json            # Claim locks (CAS)
/.queues/{pusher}/.results/{taskID}.json           # Task results
/.archive/{partition}/{intent}/...                 # Deployment archives
```

## Cross-Project Integration

### Guardian → MonoFS
- MonoFS is the persistent store — all partition/intent/state/queue files live there
- `internal/store/monofs/adapter.go` remaps logical paths: `/partitions/...` → `guardian/...` (physical MonoFS paths)
- Reads go through MonoFS mount point; writes/deletes go through MonoFS gRPC API
- Change stream via `SubscribeGuardianChanges` RPC for real-time watcher updates
- Config: `GUARDIAN_MONOFS_ROUTER`, `GUARDIAN_MONOFS_TOKEN`

### Guardian → Doctor
- Doctor deployment manifests in `doctor/deploy/guardian/partitions/doctor-telemetry/`
- Guardian can manage Doctor's infrastructure lifecycle via Docker/K8s pushers
- Doctor catalog can use MonoFS (managed by Guardian) as its backend store

## Manifest Format

```yaml
# Partition: /partitions/<name>/config.yaml
apiVersion: guardian/v1alpha1
kind: Partition
metadata:
  name: my-platform
spec:
  deletionPolicy: orphan    # or "destroy"
  reconciliation:
    mode: auto               # or "manual"
    interval: 60s
    jitter: 5s

# Intent: /partitions/<name>/intents/<intent>.yaml
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: storage-core
spec:
  targetPusher: docker
  joins: []                  # [] or ["dependency-intent"]
  target:
    cluster: local
    namespace: default
  assets:
    - type: Volume
      name: data-vol
      properties:
        size: 10Gi
    - type: Compute
      name: my-service
      dependsOn: [data-vol]
      properties:
        image: nginx:latest
        ports: ["8080:80"]
      payload:
        docker:
          # Docker Compose snippet merged onto asset
```

## Build & Test

```bash
# Build
go build ./...
go build ./cmd/guardiand ./cmd/guardianctl

# Test
go test ./...
go test ./internal/orchestrator/reconciler -run '^TestEndToEndReconcileFlow$'

# Run
make guardian-up           # Guardian container
make dogfood-up            # Full stack: MonoFS + Guardian + Docker pusher
make pusher-docker-up      # Docker pusher
make dogfood-status        # Check status
make guardian-logs          # Follow logs

# Push partition
go run ./cmd/guardianctl --store-dir <store> partition push --dir ./partitions/monofs-local
```

## Conventions

- **All persistence through `guardianapi.Store`** — never bypass with ad-hoc filesystem logic
- **Use `internal/paths` helpers** for every logical path construction
- **Every write carries `MutationContext`** with PrincipalID, Reason, CorrelationID
- **Manifest validation is strict**: `apiVersion: guardian/v1alpha1`, names match `^[a-z][a-z0-9-]*$`
- **Orchestration logic in compiler/reconciler**, not in drivers — pushers receive pre-ordered tasks
- **Output interpolation is string-based**: `${intent.<name>.outputs.<key>}` — unresolved placeholders preserved
- **`locked: true` is runtime behavior**: DIFF still runs but stops at `DriftedLocked`, never queues APPLY
- **Queue ownership uses optimistic CAS**: claims via `ExpectedVersionID: "absent"`
- **Secrets are late-bound**: resolved in drivers at APPLY time via `internal/pusher/secrets`, never logged
- **Deterministic hashing**: planner/resolver sort names/keys before hashing — preserve this in all compiler code
- **Payload overlays are first-class**: Docker drivers merge `asset.payload.docker`, K8s drivers merge `asset.payload.k8s`
