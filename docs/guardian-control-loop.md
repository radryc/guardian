# Guardian Control Loop

This document explains how Guardian works today after the recent control-plane changes, with a focus on the path from `guardianctl partition push` plus `guardianctl partition reconcile` to task execution, result processing, UI status, and timeout behavior.

## Short version

Guardian is an asynchronous control plane built around a shared logical store.

- `guardianctl` writes partition and intent manifests into the store.
- `guardiand` watches partition config changes and reconciles them into queued tasks.
- a pusher (`guardian-pusher-local`, `guardian-pusher-docker`, `guardian-pusher-k8s`, or `guardian-pusher-aws`) claims tasks from `/.queues/<pusher>/` and executes Guardian's current pipeline: `DIFF -> CHECK -> APPLY`. `DIFF` computes configuration drift, `CHECK` validates that detected drift can be applied safely, and `APPLY` mutates the target.

Recommended queue scoping:

- AWS: run one pusher per account (default pusher name `aws-<account>`)
- Kubernetes: run one pusher per cluster (default pusher name `k8s-<cluster>`)
- `guardiand` result processing consumes those result files, advances intent state, archives successful deployments, writes events, and queues dependent work.

If the UI shows task logs ending with `task completed` but the intent remains `Queued`, `Checking`, or later flips to `TimedOut`, that means the worker finished the task and wrote a result file, but the control plane has not yet advanced state from that result.

## Core storage model

Everything important is persisted through `guardianapi.Store`.

Relevant logical paths:

- `/partitions/<partition>/config.yaml`: partition manifest.
- `/partitions/<partition>/intents/<intent>.yaml`: intent manifests.
- `/partitions/<partition>/.state/intents/<intent>.json`: per-intent state file.
- `/partitions/<partition>/.state/partition.json`: derived partition state.
- `/partitions/<partition>/.state/runtime.json`: runtime snapshot that mirrors current partition state plus all intent states.
- `/.queues/<pusher>/<taskID>.json`: queued task file.
- `/.queues/<pusher>/.claims/<taskID>.json`: optimistic task lease/claim.
- `/.queues/<pusher>/.results/<taskID>.json`: completed task result written by the worker.
- `/.archive/<partition>/<intent>/<deployment>/...`: archived successful deployment state, manifest, and logs.

The store can be memory-backed, filesystem-backed, or MonoFS-backed, but the daemon, CLI, and pushers all use the same logical contract.

## Actors and responsibilities

### `guardianctl`

`guardianctl` is an operator CLI. It uses the same store/compiler/orchestrator packages as the daemon, but it is not the long-running control plane.

The two important partition commands are:

- `guardianctl partition push --dir ...`
  Writes normalized manifests and extra partition files into the store.
  It does not reconcile.

- `guardianctl partition reconcile --partition ...`
  Runs one immediate `ReconcilePartition()` call for an already-pushed partition.
  This is still asynchronous overall. It only triggers the next reconcile step and writes queue/state files. It does not wait for the pusher to finish `DIFF`, `CHECK`, or `APPLY`, and it does not wait for the intent to become `Healthy`.

That distinction matters. Seeing `partition reconcile` succeed only means a reconcile pass was triggered.

### `guardiand`

`cmd/guardiand/main.go` is the real control-plane composition root.

It starts:

- the embedded UI/API server.
- the periodic full reconciler loop.
- the partition watcher.
- the result processor.
- optional compliance publishing.

The daemon is what keeps the system moving forward after the initial CLI write.

### Pushers

Pushers poll one queue namespace each.

- `guardian-pusher-local`
- `guardian-pusher-docker`
- `guardian-pusher-k8s`
- `guardian-pusher-aws`

Each pusher watches `/.queues/<pusher>/`, claims tasks via `/.claims/<taskID>.json`, executes the provider-specific drivers, and writes `TaskResult` JSON into `/.results/<taskID>.json`.

The AWS pusher's asset surface is a `CDKStack` asset. It stages a TypeScript CDK app from the logical store, runs CDK synth/deploy for one stack, and returns CloudFormation outputs back into intent outputs.

## End-to-end flow

### 1. Partition manifests are written

The initial trigger is usually one of:

- `guardianctl partition push`
- `guardianctl partition reconcile`
- a direct write through the UI or another automation path

These writes land under `/partitions/<partition>/...`.

### 2. Partition watcher triggers reconciliation

`guardiand` watches partition config/intents and debounces writes.

After the debounce window it calls `reconciler.ReconcilePartition(partition, force=false)`.

Separately, `guardianctl partition reconcile` also calls `ReconcilePartition(partition, force=true)` directly in-process.

That means a push followed quickly by `partition reconcile` can have two reconciliation triggers close together:

- the CLI-triggered reconcile
- the daemon partition watcher reacting to the same manifest writes

This is why `partition reconcile` should be treated as a trigger, not as a synchronous deployment transaction.

### 3. Reconciler compiles desired state

The reconciler:

- loads the partition manifest.
- loads intent manifests.
- loads current intent states and outputs.
- runs `planner.Compile()`.
- writes derived partition state.
- for each intent whose dependencies are healthy and which does not already have an active task, queues a `DIFF` task.

The queue write is a logical file write to:

- `/.queues/<targetPusher>/<taskID>.json`

At the same time the reconciler writes or updates:

- the intent state.
- the derived partition state.
- the partition runtime snapshot.

### 4. Pusher executes the task

The relevant pusher claims the task and runs asset driver logic.

The normal progression in the current runtime is:

- `DIFF`: compare desired intent state with live deployment state and emit a drift report.
- `CHECK`: only for intents where `DIFF` reported drift; validate that Guardian can apply that drift safely.
- `APPLY`: execute the mutation when `CHECK` succeeded.

For delete flows it can run `DESTROY`.

When execution ends, the worker writes a result file to:

- `/.queues/<pusher>/.results/<taskID>.json`

The task result contains:

- task metadata
- task status
- drift report
- outputs
- task logs

### 5. Result processor advances control-plane state

`guardiand` result processing is what turns a completed worker result into the next state transition.

For example:

- successful `DIFF` either marks the intent healthy or queues `CHECK`
- successful `CHECK` queues `APPLY`
- successful `APPLY` marks the intent healthy, archives the deployment, writes deploy events, and queues dependents

This means the worker finishing is not the end of the deployment flow. The control plane must still process the result file.

Once a task chain starts, Guardian now reuses the queued task payload across `DIFF -> CHECK -> APPLY` instead of rereading and recompiling the manifest between phases. That keeps each reconcile chain tied to one resolved asset graph and cuts extra store reads on drifted intents.

## Runtime snapshots and derived partition state

Recent changes introduced a partition runtime snapshot at:

- `/partitions/<partition>/.state/runtime.json`

This snapshot is now the fast path for loading current runtime state.

Today:

- `LoadAllIntentStates()` loads from the runtime snapshot first.
- `LoadPartitionState()` loads the standalone partition state file first and falls back to reconstructing from the runtime snapshot.
- dispatcher writes keep intent state, partition state, and runtime snapshot synchronized.

This reduced repeated full scans of `/.state/intents/` and made UI/status loading cheaper.

## Why the UI can show `task completed` and still look stuck

The UI reads task logs directly from the latest result file for the current `LastTaskID`.

That means:

- `task completed` in the log view proves that the worker wrote a result file.
- it does not prove that `guardiand` has processed that result yet.

If the result processor has not advanced the intent state, the UI can still show:

- `Queued`
- `Checking`
- `Comparing`
- `Pushing`
- eventually `TimedOut`

even though the visible task logs end in success.

In practice that symptom means:

- the pusher finished
- the result exists
- the control plane has not applied the result transition yet

## Timeout semantics

UI timeout is derived from two facts:

- the intent still appears to have an active task
- `LastQueuedAt` is older than `guardian.staleTaskAfter`

`HasActiveTask()` currently treats these as signs of an active task:

- queue task file exists
- claim file exists for an in-flight status
- result file exists for an in-flight status

That design is intentional: if the worker finished but the control plane has not consumed the result file yet, the task is still operationally in-flight from Guardian's perspective.

So `TimedOut` does not mean the worker literally never ran. It often means the worker finished but the control plane did not progress past the result file in time.

## Failure mode: completed result but no state advance

The exact symptom you reported is:

- UI shows `Queued`
- UI shows operation `Check`
- task logs show all assets checked successfully
- logs end with `task completed`
- later the intent times out

That means the stuck point is after result write and before state transition.

The important boundary is the result processor in `guardiand`.

There are only a few possibilities at that point:

- `guardiand` did not receive the `.results` change event.
- `guardiand` received the event but failed while reading or processing the result.
- another reconcile path rewrote `LastTaskID` so the completed result was treated as stale.

## Current robustness behavior

The result processor used to perform a broad periodic scan, then was tightened to watch `/.results` directly and rescan only on startup/reconnect.

That reduced unnecessary background work, but it also made the system more sensitive to missed `.results` notifications.

The control plane now has a lightweight periodic live-result rescan again, but only for active task IDs and only across result directories, not the whole queue root.

That rescan exists to recover from cases where:

- a distributed watch event is missed
- the MonoFS watch stream drops and reconnect timing is unlucky
- a result file already exists but the event was not consumed by the running daemon

It is intentionally a recovery path, not the primary mechanism.

## Current command semantics

Use these mental models:

- `partition push`: write desired state only
- `partition reconcile`: trigger one reconcile pass only, optionally followed by `partition wait`
- background `guardiand`: the actual long-running loop that watches, reconciles, and processes results

None of the CLI commands should be read as "wait until the infrastructure is healthy".

## How to debug a stuck task now

When you see `task completed` in the UI but Guardian later times out, inspect in this order:

1. Check the current intent state file.
   - `/partitions/<partition>/.state/intents/<intent>.json`
   - confirm `status`, `lastTaskID`, `targetPusher`, and `lastError`

2. Check whether the result file exists for that `lastTaskID`.
   - `/.queues/<pusher>/.results/<taskID>.json`

3. Check whether the queue task file or claim file still exists.
   - `/.queues/<pusher>/<taskID>.json`
   - `/.queues/<pusher>/.claims/<taskID>.json`

4. Check `guardiand` logs for result processing errors.
   Look for lines beginning with:
   - `process result`
   - `read result`
   - `result-processor:`

5. Check whether another task ID replaced the one that completed.
   If `lastTaskID` moved to a newer task before the completed result was processed, the older result may have been treated as stale.

6. Check whether the configured pusher names match the intent `targetPusher`.
   `guardiand` only watches result directories for configured pushers.

## Code map for this flow

- `cmd/guardianctl/partition_push.go`
  - `partition push`

- `cmd/guardianctl/partition_reconcile.go`
  - `partition reconcile`

- `cmd/guardiand/main.go`
  - daemon composition root
  - partition watcher
  - result processor loop

- `internal/orchestrator/reconciler/reconciler.go`
  - compile and queue work

- `internal/orchestrator/dispatcher/dispatcher.go`
  - queue tasks
  - write intent/partition/runtime state
  - archive deployments
  - write events

- `internal/orchestrator/results/processor.go`
  - consume result files and advance state machine

- `internal/orchestrator/common/common.go`
  - `HasActiveTask()`

- `internal/orchestrator/common/runtime.go`
  - runtime snapshot loading

- `internal/pusher/runtime/runtime.go`
  - claim, execute, and write `TaskResult`

- `internal/ui/data.go`
  - timeout classification
  - current activity/log views

## Practical takeaway

The important conceptual split is:

- worker success
- control-plane success

The worker becomes successful when it writes `/.results/<taskID>.json`.

The control plane becomes successful only after `guardiand` consumes that result, updates intent state, possibly queues the next task, and eventually reaches `Healthy` or another terminal status.

If you remember only one thing, remember this:

`task completed` in the UI logs means "the worker wrote a result", not "Guardian finished reconciliation".
