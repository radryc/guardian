# Copilot instructions for Guardian

## Build and test commands

- This module targets **Go 1.25** and `go.mod` has `replace github.com/radryc/monofs => ../monofs`, so local builds/tests expect a sibling `../monofs` checkout.
- Build everything: `go build ./...`
- Build the main binaries directly: `go build ./cmd/guardiand ./cmd/guardianctl ./cmd/guardian-pusher-local ./cmd/guardian-pusher-docker ./cmd/guardian-pusher-k8s`
- Run the full test suite: `go test ./...`
- Run a single test: `go test ./internal/orchestrator/reconciler -run '^TestEndToEndReconcileFlow$'`
- Single-test pattern for any package: `go test ./path/to/package -run '^TestName$'`
- List operational make targets: `make help`
- Main dogfood/runtime targets from the Makefile:
  - `make guardian-up`
  - `make pusher-docker-up`
  - `make pusher-k8s-up`
  - `make dogfood-up`
  - `make dogfood-status`
- `make` workflows use `MONOFS_DIR ?= ../monofs`; `make dogfood-up` auto-generates and reuses a local MonoFS encryption key unless `MONOFS_ENCRYPTION_KEY` is set to override it.
- Push a repo-managed partition bundle with `go run ./cmd/guardianctl --store-dir <store> partition push --dir ./partitions/monofs-local` (or `--monofs-router ... --monofs-token ...` against MonoFS).
- No repository-defined lint target or lint config is present today.

## High-level architecture

- Guardian is a Go control plane layered on a logical filesystem contract in `pkg/guardianapi/types.go`. `internal/store/fs`, `internal/store/memory`, and `internal/store/monofs` all implement that same read/watch/write interface, so the daemon, CLI, and pushers all share one storage boundary.
- `cmd/guardiand/main.go` is the real control-plane composition root. It opens the store, optionally enables S3 compliance publishing, starts the embedded UI/API server, runs the partition watcher, runs the reconciler loop, and processes pusher results from queue result files.
- The compile pipeline is spread across `internal/compiler/manifest`, `validator`, `resolver`, `dag`, and `planner`. `planner.Compile` builds:
  - a macro-DAG from intent `spec.joins`
  - a micro-DAG from asset `dependsOn`
  - resolved asset properties using `${intent.<name>.outputs.<key>}` placeholders when upstream outputs already exist
- The orchestrator writes work into the logical store, not an in-memory queue. `internal/orchestrator/dispatcher` writes task files to `/.queues/<pusher>/<taskID>.json` and state files under `/partitions/<partition>/.state/...`. `internal/orchestrator/results` consumes `/.queues/<pusher>/.results/*.json`, advances intent status, archives successful deployments under `/.archive/...`, and queues dependent intents when upstream outputs become healthy.
- Pusher binaries (`cmd/guardian-pusher-*`) are deliberately thin. They register provider-specific drivers, poll a queue dir through `internal/pusher/runtime`, claim tasks with optimistic CAS, run `CHECK -> DIFF -> APPLY` or reverse-order `DESTROY`, and write result files back to the store.
- `cmd/guardianctl/main.go` uses the same store/compiler/orchestrator primitives as production code. It is a good reference for correct manifest writes, rollback/version access, and operator workflows.
- Whole-partition repo sync now lives in `guardianctl partition push`. It normalizes manifests, validates the local bundle, uploads only changed files, and prunes removed repo-managed files while leaving partition `.state` and `secrets/` intact.
- The UI is not a separate SPA build. `internal/ui/server.go` serves embedded templates/static assets and calls the same store/dispatcher codepaths as the daemon.
- `design.txt` explains the intended Asset / Intent / Partition / Pusher model, and the current code largely follows it. `partitions/monofs-local/` is the main concrete reference for a repo-managed local multi-intent bundle.

## Key conventions

- Use `internal/paths` helpers for every logical path. Guardian code works with absolute logical paths rooted at `/partitions`, `/.queues`, and `/.archive`; `internal/store/monofs/adapter.go` remaps those to MonoFS physical paths under `guardian/...` and `guardian-system/...`.
- Treat `guardianapi.Store` as the boundary for all persistence and watches. Do not bypass it with ad-hoc filesystem logic when touching orchestrator, CLI, UI, or pusher code.
- Every write/delete should carry `guardianapi.MutationContext` with a meaningful `PrincipalID`, `Reason`, and usually a fresh correlation ID. Dispatcher, UI, CLI, and pusher code all rely on that metadata for version history and audit events.
- Manifest validation is strict. Use `internal/compiler/manifest` and `internal/compiler/validator` instead of hand-rolling YAML handling. Supported manifests are `apiVersion: guardian/v1alpha1` with `kind: Partition` or `kind: Intent`, and names must match `^[a-z][a-z0-9-]*$`.
- Orchestration logic belongs in the compiler/reconciler, not in drivers. Intent `joins` gate whole-intent progression; asset `dependsOn` controls per-intent execution order. Pushers should assume they receive an already ordered task.
- Output interpolation is string-based. Asset properties can reference `${intent.<name>.outputs.<key>}`. `resolver.ResolveProperties` substitutes known outputs, but `planner.Compile` preserves unresolved placeholders so downstream intents can still be compiled before upstream APPLY finishes.
- Successful APPLY results expose both plain output keys and `<asset>.<key>` namespaced keys. Preserve that shape when extending result handling or writing new drivers.
- `locked: true` is runtime behavior, not just UI metadata: DIFF still runs, but drift must stop at `DriftedLocked` and must not queue APPLY. The integration expectations live in `internal/orchestrator/reconciler/reconciler_integration_test.go` and `internal/orchestrator/results/processor_test.go`.
- Queue ownership uses optimistic locking. `internal/pusher/runtime` claims `/.claims/<taskID>.json` with `ExpectedVersionID: "absent"`; preserve that CAS pattern instead of adding side channels for task ownership.
- Provider-specific payload overlays are first-class. Assets can point at extra YAML via `asset.payload`; Docker drivers load the `docker` key, Kubernetes drivers load `k8s` / `kubernetes`, and the payload is merged onto the generic asset spec. Preserve that behavior in `internal/pusher/driverutil/driverutil.go`.
- Secrets are meant to stay late-bound. Keep `secret_ref` values in manifests/properties and let provider drivers resolve them through `internal/pusher/secrets`; do not move secret resolution into planner/orchestrator code or log resolved secret values.
- Determinism matters. Planner, resolver, and driver utilities sort names/map keys before hashing, walking refs, or materializing config files so version IDs and tests stay stable; preserve that behavior when editing compiler or driver code.
- Tests commonly use `internal/store/memory` as the canonical fake store for end-to-end control-plane flows. Prefer that over mocks when adding reconciler, runtime, UI, or result-processor coverage.
- The schema/examples include partition `spec.defaults` and reconciliation metadata like `mode: manual` and `jitterPercent`, but current runtime behavior does not apply partition defaults during intent compilation and does not use jitter in the daemon loop. Today, intents still need their own `targetPusher` / `target`, and actual reconcile cadence comes from the daemon configuration.
