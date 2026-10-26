# AWS observed intents

`observed` is an AWS-only, **report-only** intent type. Guardian watches
CloudFormation stacks, compares the live state with the desired CDK app stored
in MonoFS, and reports drift, health and CloudFormation events. It never queues
`APPLY`, never archives a deployment and never mutates AWS for these intents.

The connection topology is deliberately pull-based:

- **Guardian initiates every connection to the pusher.** The pusher never dials
  MonoFS or Guardian.
- EventBridge and SQS live entirely inside AWS and only feed the pusher's
  in-memory observed cache.
- Guardian reads the desired CDK app from the MonoFS partition bundle and the
  observed state from the pusher's read API, then writes snapshots and intent
  state back into MonoFS itself.

```
GitHub (aws/) ──sync (guardianctl partition push)──▶ MonoFS
  /partitions/<p>/config.yaml
  /partitions/<p>/intents/<stack>.yaml          intentType: observed
  /partitions/<p>/payloads/aws/cdk.out/...      desired templates

AWS account (AwsPusherStack)
  CloudFormation ──▶ EventBridge ──▶ SQS ──▶ pusher observed cache
                                              └─ :19090 /v1/aws/observed/*  (passive)

guardiand
  observed collector ──HTTP──▶ pusher observed API   (CHECK: latest state)
                     ──diff──▶ desired cdk.out vs observed template/drift
                     ──write──▶ /partitions/<p>/.observed/... + IntentState
                                 Observed / ObservedDrifted   (no APPLY, ever)
```

## End-to-end flow (repo → MonoFS → Guardian → AWS)

1. Author the CDK app and the pusher stack in your repository and deploy the
   pusher (`AwsObservedPusherStack` for your own account, or
   `guardianctl aws pusher-deploy` for the Strata app). The stack outputs the
   observed read API URL and token secret.
2. Add `.guardian/config.yaml` to the repository declaring the pusher URL,
   account/region and the partition/repo binding, then merge it.
3. On merge, CI runs `guardianctl repo apply --dir . --synth` (templates in
   [`docs/ci/`](ci/)). This upserts the pusher/repo records, synthesizes the CDK
   app, generates one `observed` intent per stack and pushes the partition
   bundle into MonoFS. A notify-only variant can mirror the repo into MonoFS
   (`POST /api/ingest`) and call `POST /api/repos/{name}/notify`.
4. guardiand's partition watcher sees the bundle and reconciles. Observed
   intents skip the task pipeline; the observed collector polls the pusher read
   API, diffs desired templates against live CloudFormation state/drift and
   records `Observed` / `ObservedDrifted` in MonoFS. Monitoring continues from
   then on.

Two important clarifications:

- Intents are generated from the **CDK app in the repository**; AWS is the
  observed side. Guardian does not discover stacks from AWS to create observed
  intents (that is the separate Scan & Import flow for adoption assets).
- MonoFS does not watch GitHub by itself. The merge trigger is the CI job, or
  MonoFS's own `--auto-refresh` poll / forge webhook routes (which need the
  MonoFS bearer token when auth is enforced).

For stacks that have no repository yet (unmanaged / missing-repo), Guardian can
prepare an observed snapshot and an adoption branch from the live template; see
[`unmanaged-stacks.md`](unmanaged-stacks.md).

## Intent manifest

One observed intent per CloudFormation stack. The `CDKStack` asset points at a
stack payload manifest inside the partition bundle:

```yaml
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: aws-pusher-stack
spec:
  intentType: observed
  targetPusher: aws-123456789012
  target:
    account: "123456789012"
    region: eu-west-1
  assets:
    - type: CDKStack
      name: aws-pusher-stack
      payload:
        aws: /partitions/strata-aws/payloads/aws/aws-pusher-stack.yaml
```

Payload manifest:

```yaml
sourceType: cdk-ts
prebuiltAssemblyDir: /partitions/strata-aws/payloads/aws/cdk.out
stackName: AwsPusherStack
stackID: AwsPusherStack
packageManager: none
```

Observed intents normally use a prebuilt Cloud Assembly (`cdk.out`) so Guardian
can diff templates without running Node/CDK. If only `sourceDir` is present,
Guardian uploads the source tree to the pusher, which synthesizes it and returns
the diff (see `POST /v1/aws/observed/diff` below).

## Resource-level assets

The intent manifest stays stack-level: one observed intent per CloudFormation
stack and one `CDKStack` asset. Guardian computes a per-resource inventory on
every collection from the desired template and stores it on the intent state:

```json
{
  "resources": [
    {"logicalId": "OrdersTable", "cfnType": "AWS::DynamoDB::Table", "assetName": "res-orders-table", "status": "Changed"},
    {"logicalId": "OrdersQueue", "cfnType": "AWS::SQS::Queue", "assetName": "res-orders-queue", "status": "InSync"}
  ]
}
```

The UI renders each entry as a read-only synthetic `AWSResource` asset. Status
is `InSync`, `Changed`, `Added` or `Removed`, derived from the template diff
(`changed:<LogicalID>`, `added:`, `removed:`). The intent manifest is never
rewritten when the CDK app changes; the template remains the source of truth.

## Observed and managed modes

`guardianctl aws observed-generate` defaults to `--mode observed` (report-only).
`--mode managed` emits `intentType: standard` intents with the same `CDKStack`
payload; Guardian applies the stack as one unit, preserving the stack == intent
model.

Bundles created by unmanaged import use a live-template snapshot and mark the
payload `snapshot: true`. The AWS pusher refuses CHECK/DIFF/APPLY/DESTROY for a
snapshot payload, so a snapshot cannot be promoted by flipping `intentType`
alone. Managed mode requires the user's synthesized CDK app.

## Deploy a new pusher

`AwsPusherStack` is deployable per account/region. The CDK app lives in
`aws/`; the stack creates the ECS pusher, the EventBridge→SQS observation
pipeline and the read API.

```bash
guardianctl aws pusher-deploy \
  --dir ../aws \
  --profile prod-admin \
  --account 123456789012 \
  --region eu-west-1 \
  --pusher-name aws-123456789012
```

The command runs `npx cdk deploy AwsPusherStack` and prints the registration
steps. Collect the outputs:

```bash
aws cloudformation describe-stacks --stack-name AwsPusherStack \
  --query 'Stacks[0].Outputs' --output table

aws secretsmanager get-secret-value --secret-id strata/guardian/awsread-token \
  --query SecretString --output text
```

Disable the observation pipeline with `--no-observe`
(`-c deployAwsObserve=false`).

### Pusher in your own repository/account

If the CDK app lives in your own repository (not the Strata `aws/` app), use the
self-contained construct `AwsObservedPusherStack`
(`aws/lib/aws-observed-pusher-stack.ts`). It creates its own VPC/cluster/ALB,
runs `guardian-pusher-aws --observed-only` (no MonoFS dependency) and outputs
everything Guardian needs:

```ts
import { AwsObservedPusherStack } from './constructs/aws-observed-pusher-stack';

new AwsObservedPusherStack(app, 'AwsObservedPusherStack', {
  env: { account: '123456789012', region: 'eu-west-1' },
  pusherName: 'aws-123456789012',
});
```

Outputs: `PusherName`, `AwsReadApiUrl`, `AwsReadTokenSecretArn`,
`AwsObserveQueueUrl`, `EcsClusterName`, `EcsServiceName`. Set `exposeOnAlb:
false` and attach the Service Connect address (`guardian-awsread:19090`) when
Guardian runs in the same cluster/VPC.

The construct builds the pusher image from the Strata checkout by default
(`repoRoot`). In an external repository, publish the `guardian-pusher-aws`
image once and pass it explicitly:

```ts
new AwsObservedPusherStack(app, 'AwsObservedPusherStack', {
  image: ecs.ContainerImage.fromEcrRepository(repo, 'guardian-pusher-aws'),
});
```

## Register the pusher URL in Guardian

Pushers are store-backed records under `/.registry/pushers`, editable from the
UI (**Pushers & Repos**), the CLI, or the HTTP API. This is how Guardian learns
where to pull observed state from without a config rebuild.

```bash
guardianctl pusher add \
  --name aws-123456789012 \
  --account 123456789012 \
  --region eu-west-1 \
  --url http://guardian-awsread:19090 \
  --token-env GUARDIAN_AWSREAD_TOKEN

guardianctl pusher list
```

Static config (`pushers[].observedApiUrl`) still works and takes precedence over
the registry.

HTTP API (same records):

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/pushers` | GET/POST | list / upsert pushers |
| `/api/pushers/{name}` | GET/DELETE | read / remove a pusher |
| `/api/repos` | GET/POST | list / upsert repos |
| `/api/repos/{name}` | GET/DELETE | read / remove a repo |
| `/api/repos/{name}/bindings` | PUT | replace partition assignments (credential-free) |
| `/api/repos/{name}/ingest` | POST | generate intents from the stored assembly |
| `/api/repos/{name}/notify` | POST | record a CI merge notification and ingest |

## Register a repository

A repo can feed one or more partitions, and a partition can be fed by several
repos. Each binding carries its own payload path, stack selector and optional
account/region override:

```bash
guardianctl repo add \
  --name strata-aws \
  --pusher aws-123456789012 \
  --bindings '[{"partition":"prod","selector":"Prod-*"},{"partition":"staging","selector":"Stg-*"}]' \
  --source https://github.com/example/strata.git \
  --branch main \
  --assembly-subdir cdk.out

guardianctl repo assign --name strata-aws --partition prod,staging --selector 'Strata-*'
guardianctl repo list
```

`source` may be a git URL or a MonoFS logical path; it never embeds credentials.
`account`/`region` default to the pusher's values and can be overridden per
binding. The UI exposes the same assignment in **Pushers & Repos** — check the
existing partitions a repo feeds and set a selector/path per partition.

Repo records live in MonoFS under `/.registry/repos` and are the only place
credentials (pusher token, source URL) are kept. Partition manifests and
`.guardian/config.yaml` reference repos by name and carry only credential-free
bindings, so they are safe to commit or push to GitHub.

### Partition-as-code in the repository

A repository can author the partition itself:

```
.guardian/config.yaml                    # pusher (tokenEnv) + repo bindings
guardian/partitions/<name>/
  partition.yaml                         # or config.yaml
  intents/*.yaml
  payloads/**                            # copied to /partitions/<name>/payloads/**
  secrets/**                             # never imported from the repo
```

`guardianctl repo sync` / `repo apply` imports this directory and reconciles the
partition. UIs/APIs that ingest from a MonoFS logical source path do the same.
Put secret values under `/partitions/<name>/secrets/...` in MonoFS and reference
them as `secretRef: monofs-secret://<name>/<path>`; inline `Secret.value` is
rejected for repo-sourced partitions.

## Repository config (`.guardian/config.yaml`)

The repository itself can declare the pusher URL and the partition binding, so a
merge can drive registration + ingest without manual CLI steps:

```yaml
apiVersion: guardian/v1alpha1
kind: RepoConfig
pusher:
  name: aws-123456789012
  account: "123456789012"
  region: eu-west-1
  observedApiUrl: http://guardian-awsread:19090
  tokenEnv: GUARDIAN_AWSREAD_TOKEN
repo:
  name: strata-aws
  assemblySubdir: cdk.out
  bindings:
    - partition: prod
      selector: Prod-*
      path: /partitions/prod/payloads/aws
    - partition: staging
      selector: Stg-*
      region: eu-west-1
```

Bindings are credential-free by design. Use `pusher.tokenEnv` (not
`pusher.token`) so the shared config never contains a secret.

Apply it with:

```bash
guardianctl repo apply --dir . --synth
```

This upserts the pusher and repo registrations (repo source defaults to the
`origin` remote), generates one observed intent per stack, pushes the bundle and
reconciles. `guardianctl repo sync` also applies the config automatically when
the checkout contains one (`--apply-config=false` disables it).

## CI integration (merge-triggered sync)

Copy a template into your repository's CI:

- GitHub Actions: [`docs/ci/github-actions-observed.yml`](ci/github-actions-observed.yml)
- GitLab CI: [`docs/ci/gitlab-observed.yml`](ci/gitlab-observed.yml)

Two modes are supported:

1. **Full sync (recommended)** — the CI job builds `guardianctl` and runs
   `guardianctl repo apply --dir . --synth`, talking directly to MonoFS. No
   Guardian UI auth is required; the merge produces the partition bundle and the
   watcher starts monitoring.

2. **Notify-only** — for pipelines that cannot run `guardianctl`/Node:
   - `POST <monofs>/api/ingest` (bearer MonoFS token) mirrors the git repo into
     MonoFS (this is MonoFS's native git ingestion; the forge webhook routes
     `/api/webhooks/github|gitlab` also work but are behind MonoFS bearer auth,
     so CI must send the token).
   - `POST /api/repos/{name}/notify` with `{"commit","branch"}` tells Guardian a
     merge happened. Guardian records it and, if the cloud assembly is already in
     MonoFS, regenerates the partition intents; otherwise it returns `202` with
     `notified_ingest_pending` so CI can retry after MonoFS finishes.

`guardianctl repo notify --name <repo> --commit <sha>` is the CLI equivalent of
the notify endpoint.

## Ingest

Two ingest paths exist:

1. **CLI sync (git or local checkout)** — clones, optionally synthesizes, pushes
   the partition bundle and (by default) triggers a reconcile:

   ```bash
   guardianctl repo sync --name strata-aws --synth
   ```

   Use `--dir` for an existing checkout or `--assembly` for a prebuilt
   `cdk.out`. Without `--synth` the repository must already contain
   `<assembly-subdir>/manifest.json`.

2. **Store ingest (UI or CLI)** — when the cloud assembly is already in MonoFS
   (pushed by CI, or by a previous sync), regenerate intents without any local
   tooling:

   ```bash
   guardianctl repo ingest --name strata-aws
   # or POST /api/repos/strata-aws/ingest
   ```

Both paths generate one `observed` intent per CloudFormation stack, targeting
the registered pusher, and write `config.yaml`, `intents/*.yaml`, payload
manifests and a pruned `cdk.out` under the partition.

Both paths are also GitOps-aware:

- If the repository contains `guardian/partitions/<name>/`, its
  `partition.yaml`, `intents/*.yaml` and payload files are imported into
  `/partitions/<name>/`. The repository is authoritative for that config, so an
  authored `partition.yaml` wins over generated defaults.
- Generated observed bundles never reset an existing partition config: ingest
  extends instead of overwriting reconciliation, labels and defaults.
- After a successful ingest every bound partition is reconciled immediately
  (deploy). Control it with `guardian.autoDeployOnIngest` (default `true`) for
  the UI/daemon and `--reconcile=false` for `repo sync`/`repo ingest`/`repo
  notify`.
- Repo-sourced partitions reject inline `Secret.value`; use `secretRef` with
  the value stored under `/partitions/<name>/secrets/...` in MonoFS (never
  committed). `.guardian/config.yaml` rejects `pusher.token` and
  credential-bearing URLs for the same reason.

`repo sync --synth` and `POST /v1/aws/observed/diff` execute the repository's
CDK app (npm install + cdk synth) on the machine or pusher that runs them. Treat
the repository content and the pusher read API token as trusted inputs.

## Generate a bundle manually

```bash
cd aws && npm run synth
cd ../guardian
guardianctl aws observed-generate \
  --assembly ../aws/cdk.out \
  --partition strata-aws \
  --account 123456789012 \
  --region eu-west-1 \
  --out ./partitions/strata-aws
guardianctl partition push --dir ./partitions/strata-aws
```

## Pusher read API

All endpoints require the existing bearer token
(`GUARDIAN_AWSREAD_TOKEN` / `strata/guardian/awsread-token`).

| Endpoint | Purpose |
|---|---|
| `GET /v1/aws/observed/stacks?account=&region=` | list observed stacks |
| `GET /v1/aws/observed/stacks/{name}?region=` | latest state for one stack |
| `GET /v1/aws/observed/stacks/{name}/template?region=` | live CloudFormation template |
| `GET /v1/aws/observed/events?stack=&since=&limit=` | recent normalized events |
| `POST /v1/aws/observed/refresh` | force `DescribeStacks` (and optional drift) |
| `POST /v1/aws/observed/diff` | synthesize uploaded CDK source and diff it live |

The pusher consumes `CloudFormation Stack Status Change` and
`CloudFormation Drift Detection Status Change` events from the
`guardian-aws-observe` SQS queue created by `AwsPusherStack`. Live drift
detection (`DetectStackDrift`) runs only when Guardian asks for it, not on every
event. When an observed intent only has `sourceDir`, Guardian uploads the source
tree to `POST /v1/aws/observed/diff`; the pusher synthesizes it and returns the
changed resources.

## Guardian configuration

Guardian reads pusher URLs from the store registry (`/.registry/pushers`) with a
30s refresh, and falls back to static config. The collector runs whenever
`guardian.observedInterval` is set (default `5m`).

```yaml
guardian:
  observedInterval: 5m
  observedTimeout: 30s

pushers:
  - name: aws-123456789012
    queueDir: /.queues/aws-123456789012
    observedApiUrl: http://guardian-awsread:19090
    observedApiTokenEnv: GUARDIAN_AWSREAD_TOKEN
```

Wire `observedApiUrl` to the `AwsReadServiceConnectAddress`
(`guardian-awsread:19090`) when Guardian runs in the same ECS cluster, or to the
Strata ALB for external deployments. Leave `observedInterval` empty to disable
the collector.

## Status and snapshots

- `Observed` — live state matches the desired CDK app.
- `ObservedDrifted` — template resources and/or CloudFormation drift differ;
  surfaced as `attention`, never applied.
- `DiffFailed` — Guardian could not reach the pusher or read the desired
  assembly; surfaced as `failing`.

Snapshots are written under
`/partitions/<p>/.observed/<intent>/<asset>.json`, with a bounded event log in
`/partitions/<p>/.observed/<intent>/events/` (200 events per intent).

## Topology enrichment

The pusher exposes the live CloudFormation resource mapping at:

```text
GET /v1/aws/observed/stacks/{stack}/resources?account=&region=
```

Each resource carries `logicalId`, `physicalId`, `resourceType`, `status`,
`statusReason`, `driftStatus`, and `updatedAt`. Guardian joins that mapping with
the template to attach:

- physical ARNs/names to the synthetic resource nodes
- containment (`ECS::Cluster` → `Service`, `LoadBalancer` → `Listener`)
- typed edges for `routesTo`, `triggers`, `uses`, `logsTo`, and `network`
- application/observability/infrastructure tiers for the topology filter

The topology UI shows application resources and observability by default. IAM,
VPC, subnet, route, security-group, and SSM resources are hidden until
**Infrastructure** is enabled.

This pass does not yet enumerate running ECS tasks or target health. Those are
the next enrichment step and require `ecs:ListTasks`/`DescribeTasks` and
`elasticloadbalancing:DescribeTargetHealth`.

## Required permissions

The pusher task role needs, on top of `ReadOnlyAccess`:

- `sqs:ReceiveMessage`, `sqs:DeleteMessage`, `sqs:GetQueueUrl`,
  `sqs:ChangeMessageVisibility`
- `cloudformation:GetTemplate`, `cloudformation:DetectStackDrift`,
  `cloudformation:DescribeStackDriftDetectionStatus`,
  `cloudformation:DescribeStackResourceDrifts`

## Limitations (v1)

- Same account and region as the `AwsPusherStack`; multi-account observation
  needs EventBridge rules in each target account (deploy one pusher per
  account/region and register each).
- The store ingest path requires a cloud assembly (`manifest.json` plus
  templates) already in MonoFS.
- The registry stores bearer tokens inline when `--token` is used; prefer
  `--token-env` so secrets stay out of the store. The registry is the only
  place credentials live — never copy source URLs or tokens into partition
  manifests, `.guardian/config.yaml` or other GitHub-shared files.
- Repo-authored content is checked for secret material: `.guardian/config.yaml`
  rejects `pusher.token` and URLs with embedded credentials,
  `guardian/partitions/<name>/` rejects inline `Secret.value`, and MonoFS
  publish skips deny-listed paths (`**/secrets/**`, `.env`, `*.secret.*`,
  `*.pem`/`*.key`, `.registry/**`) so they can never round-trip to the remote.
- `repo sync` writes each binding's intent files but no longer removes intents
  missing from the generated bundle, because a partition can be fed by several
  repos/bindings. Remove stale intents explicitly when a stack is retired.
- Deleting a repo or pusher registration does not delete the generated
  partition; use `guardianctl partition delete` separately.
