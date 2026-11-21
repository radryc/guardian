# Guardian

> Part of the **Strata** platform.

Guardian is a declarative deployment control plane. Desired state lives in
MonoFS as partitions and intents; Guardian compiles them into typed assets,
computes drift, and queues tasks; pusher workers claim those tasks and execute
DIFF → CHECK → APPLY (or DESTROY) against Kubernetes, Docker, AWS, GCP, or a
local backend.

Guardian has no database of its own: **MonoFS is the store and the message
bus**. Partitions, intents, runtime state, queues, archives, and the platform
registry are all versioned files under the `guardian/` namespace.

See [`docs/architecture.md`](docs/architecture.md) for the full architecture,
[`docs/logical-flow.md`](docs/logical-flow.md) for end-to-end flows, and
[`docs/security-architecture.md`](docs/security-architecture.md) for the threat
model and trust boundaries.

## Components

| Binary | Role |
| --- | --- |
| `guardiand` | Control plane: reconciler, result processor, observed collector, alerting, UI/API |
| `guardianctl` | Operator CLI: partitions, intents, repos, pushers, AWS, images, bootstrap |
| `forge` | Repository workbench: discover, author, validate, plan, and submit partition changes as reviewed PRs |
| `guardian-pusher-k8s` | Kubernetes driver (client-go or kubectl), optional observed read API |
| `guardian-pusher-docker` | Docker driver |
| `guardian-pusher-aws` | AWS driver (CDK + SDK), account scans, observed read API (`:19090`) |
| `guardian-pusher-gcp` | GCP driver (OpenTofu + SDK), Cloud Asset Inventory, GCP read API (`:19091`) |
| `guardian-pusher-local` | In-process reference drivers for dev/dogfood |
| `guardian-docker` | Inspect/diff guardian-labelled Docker resources |

## Core concepts

- **Partition** — ownership unit with a deletion policy (`orphan`/`destroy`)
  and reconciliation mode (`auto`/`manual`/`readonly`, interval or cron).
- **Intent** — deployable unit. `standard` intents run DIFF → CHECK → APPLY;
  `observed` intents are AWS report-only and never mutate anything.
  `implementation: native` uses provider drivers; `implementation: cdk`
  compiles CDK-expressible assets into one generated TypeScript stack.
- **Asset** — typed building block: Compute, ImageBuild, CDKStack, Database,
  SQLDatabase, Volume, Config, Network, Secret, LoadBalancer, TraefikRoute,
  ObjectStore, Observability, Probe (single check or synthetic journey),
  K8sResource.
- **Rollout / rollback** — spec changes on applied intents become deployment
  revisions; failed applies record a rollback and preserve the pushed manifest
  for retry, while operators can restore archived tags explicitly.

### Authoring (Forge)

Forge prepares the repository before Guardian sees it: it discovers workloads,
authors partition manifests and assets, validates offline, and submits the change
as a reviewed PR/MR through MonoFS. It never writes Guardian state.

```bash
make forge-build
./bin/forge repo init --name acme-platform --source https://github.com/acme/platform
./bin/forge discover --root .
./bin/forge validate --root . --strict
./bin/forge submit --root . --partition checkout --message "checkout: add api"
./bin/forge ui --root .
```

See [docs/design/forge.md](docs/design/forge.md) for the design and
[docs/forge/README.md](docs/forge/README.md) for the command reference.

### Secret mounts (`compute.secretMounts`)

The asset surface is provider-agnostic: a Compute mounts a **Secret asset**.
Drivers resolve that asset to the native resource underneath.

```yaml
assets:
  - type: Secret
    name: router-tls
    properties:
      # either provide a value/secretRef (Guardian creates it)...
      secretRef: monofs-secret://shared/router-tls
      key: tls.crt
      # ...or adopt one created outside Guardian:
      # existingSecret: strata-tls-monofs-router

  - type: Compute
    name: router
    dependsOn: [router-tls]
    properties:
      image: monofs-router:latest
      secretMounts:
        - secret: router-tls              # Secret asset name
          path: /var/run/secrets/strata-tls
          readOnly: true
        - secret: router-tls
          path: /etc/tls/tls.crt
          file: tls.crt                   # optional single-file mount
```

| Provider | Implementation underneath |
| :--- | :--- |
| Kubernetes | Creates/adopts a `Secret` and mounts it (directory, or one key via `subPath`). |
| GCP Cloud Run | Uses the Secret Manager secret and mounts it as a secret volume. |
| AWS ECS | ECS has no secret-file volume; injects the Secrets Manager reference as a task secret env var. |
| Docker | Rejected: use an env secret or a regular Volume. |

The intent never mentions a K8s Secret name, GCP secret ID, or AWS ARN.
Adoption (`existingSecret`) is the escape hatch when the secret is provisioned
externally (for example `tlsx install`).

## Prerequisites

- Go 1.26.10 (see `go.mod`).
- Sibling checkouts because of replace directives:

  ```text
  replace github.com/radryc/monofs => ../monofs
  replace github.com/radryc/kvs => ../kvs
  ```

- Node 20+ to build the UI (`make ui-build`).
- Docker (BuildKit) for container builds and the docker pusher; kubectl and a
  cluster for the k8s pusher; Node + `aws-cdk` + credentials for the AWS
  pusher.
- A sibling `sqlagregator` checkout (or `SQLAGREGATOR_REPO_DIR`) for the
  `dev`/bootstrap MonoFS and logquery image builds; those builds pass it as a
  named build context.

## Build

```bash
make ui-build                                        # TypeScript UI -> static assets
go build -o guardianctl ./cmd/guardianctl            # CLI
go build -o forge       ./cmd/forge                  # repository workbench (CLI + embedded UI)
go build -o guardiand   ./cmd/guardiand              # daemon
go build -o guardian-pusher-docker ./cmd/guardian-pusher-docker
# container images need sibling build contexts:
make guardian-build                                  # guardiand image
make pusher-docker-image-build
make pusher-k8s-image-build
make pusher-gcp-image-build
```

## Transport security (`STRATA_TLS_*`)

Every Guardian process reads the shared `STRATA_TLS_*` contract from
[`github.com/radryc/tlsx`](../tlsx). `STRATA_TLS_MODE` defaults to `off`, so
existing plaintext deployments keep working unchanged.

| Env | Meaning |
| --- | --- |
| `STRATA_TLS_MODE` | `off` (default, plaintext), `optional` (TLS when certs exist), `required` (TLS only, fail closed) |
| `STRATA_TLS_CERT_FILE` | PEM server/workload certificate (default `/var/run/secrets/strata-tls/tls.crt`) |
| `STRATA_TLS_KEY_FILE` | PEM private key (default `/var/run/secrets/strata-tls/tls.key`) |
| `STRATA_TLS_CA_FILE` | CA bundle used to verify peers (default `/var/run/secrets/strata-tls/ca.crt`) |
| `STRATA_TLS_SERVER_NAME` | Override the name clients verify in the server certificate |
| `STRATA_TLS_REQUIRE_CLIENT_CERT` | `true` (default) verifies client certificates for mTLS servers |
| `STRATA_TLS_ALLOWED_SPIFFE_IDS` | Comma-separated SPIFFE URI SAN allowlist for accepted clients |
| `STRATA_TLS_EXPECTED_SPIFFE_IDS` | Comma-separated SPIFFE URI SAN allowlist for accepted servers |

Where the contract applies:

- **guardiand UI/API**: terminates TLS itself. `GUARDIAN_UI_TLS_CERT_FILE` and
  `GUARDIAN_UI_TLS_KEY_FILE` still work; set `GUARDIAN_UI_TLS_CA_FILE` (or
  `STRATA_TLS_CA_FILE`) to require and verify client certificates.
- **Pusher read APIs** (`guardian-pusher-aws`, `-gcp`, `-k8s`): server TLS and
  optional mTLS. The existing `--awsread-tls-*` / `--read-tls-*` flags are
  retained; `STRATA_TLS_*` is preferred whenever the mode is not `off`. The
  advertised observed API URL switches to `https://` when TLS is active.
- **MonoFS gRPC clients**: `STRATA_TLS_*` supplies any setting
  `GUARDIAN_MONOFS_TLS_*` does not define. The guardian token is still never
  sent over plaintext unless `GUARDIAN_MONOFS_ALLOW_PLAINTEXT=true` is set
  explicitly with the mode off.
- **Observed-state HTTP clients**: `http://` pusher URLs are upgraded to
  `https://` when the mode is `required`, using the configured CA and client
  certificate.

Local PKI for development can be generated with `tlsx`:

```bash
go run github.com/radryc/tlsx/cmd/tlsx gen --dir .tls --reuse-ca
go run github.com/radryc/tlsx/cmd/tlsx env --dir .tls --service guardian --mode required
# mount .tls/<service> read-only at /var/run/secrets/strata-tls
export STRATA_TLS_MODE=required
export STRATA_TLS_CERT_FILE=/var/run/secrets/strata-tls/tls.crt
export STRATA_TLS_KEY_FILE=/var/run/secrets/strata-tls/tls.key
export STRATA_TLS_CA_FILE=/var/run/secrets/strata-tls/ca.crt
```

Unset `STRATA_TLS_MODE` (or set it to `off`) to return to plaintext.

## Quick start

### Full local stack (MonoFS + Guardian + pusher)

```bash
make bootstrap        # build images, start MonoFS + Guardian, push base partitions
make bootstrap-down   # tear down
```

or the lighter dogfood loop:

```bash
make dogfood-up       # MonoFS + Guardian + docker pusher + example stack
make dogfood-status   # print example intent states from MonoFS
make dogfood-stop
```

### Against an existing MonoFS

Run the daemon directly:

```bash
export GUARDIAN_MONOFS_ROUTER=localhost:9090
export GUARDIAN_MONOFS_TOKEN=guardian-dev-token
export GUARDIAN_MONOFS_PRINCIPAL=guardiand
./guardiand --config guardian-config.yaml --ui-listen 127.0.0.1:18090
```

or in Docker: `make deploy-s3` (starts MonoFS and Guardian).

### Reconcile concurrency and rate limiting

The reconciler runs partitions on a bounded worker pool. By default it allows
`GOMAXPROCS*4` partitions at once (minimum 4, maximum 128), which on a large
node is effectively every partition. To avoid a startup/interval burst that
floods the store and pusher queues, cap it or enable backlog-aware pacing:

| Env | Config | Meaning |
| --- | --- | --- |
| `GUARDIAN_RECONCILE_PARALLELISM` | `guardian.reconcileParallelism` | max concurrent partition reconciles (0 = CPU default) |
| `GUARDIAN_RECONCILE_RATE_LIMIT` | `guardian.reconcileRateLimit` | fixed starts/sec (0 = off) |
| `GUARDIAN_RECONCILE_BURST` | `guardian.reconcileBurst` | token-bucket burst (0 = strict 1) |
| `GUARDIAN_RECONCILE_ADAPTIVE` | `guardian.reconcileAdaptive` | AIMD pace from queue backlog |
| `GUARDIAN_RECONCILE_BACKLOG_TARGET` | `guardian.reconcileBacklogTarget` | healthy outstanding task count (0 = 100) |
| `GUARDIAN_RECONCILE_MIN_RATE_LIMIT` | `guardian.reconcileMinRateLimit` | adaptive floor starts/sec (0 = max/10, min 0.1) |

Adaptive mode is enabled by default: it samples `/.queues/<pusher>/*.json`
every 10s and shrinks the rate when the outstanding-task backlog exceeds the
target, ramping back up when it drains. With no explicit rate configured it
caps at 1 start/sec and floors at 0.1 start/sec. Set
`guardian.reconcileAdaptive: false` (or `GUARDIAN_RECONCILE_ADAPTIVE=false`)
to opt out.

To size a fixed rate, divide the number of partitions by the sweep window:
50 partitions over 5 minutes = `50/300 = 0.17` starts/sec.

Where exposed, the pusher's own `--max-concurrency` still bounds how many
tasks execute at once; lower it too if your registry limits concurrent pushes
(the Docker pusher does not expose this flag).

Each pusher also accepts `--task-timeout` (default `0` = disabled) to cancel a
single task after a fixed duration and write a failed result. Set it below
`GUARDIAN_STALE_TASK_AFTER` so the pusher reports the timeout before Guardian
treats the task as dead and queues a replacement.

## guardianctl

`guardianctl` can connect three ways:

1. **Guardian discovery** — `GUARDIAN_API_URL` (or `GUARDIAN_URL`) points at
   the Guardian UI/API; the CLI learns the MonoFS router from
   `GET /api/client-config`. That response does not include the MonoFS token,
   so supply it separately with `GUARDIAN_MONOFS_TOKEN` or `--monofs-token`. If
   the endpoint is not loopback-only, also set
   `GUARDIAN_CLIENT_DISCOVERY_TOKEN` (`GUARDIAN_DISCOVERY_TOKEN` is accepted
   too).
2. **Direct MonoFS** — `GUARDIAN_MONOFS_ROUTER` + `GUARDIAN_MONOFS_TOKEN`.
3. **Local store dir** — `GUARDIAN_STORE_DIR` for offline inspection.

Useful commands:

```bash
./guardianctl partition list
./guardianctl partition push --dir ./partitions/my-partition    # write desired state
./guardianctl partition reconcile --partition my-partition      # run one cycle (queues work)
./guardianctl partition wait --partition my-partition           # block until healthy
./guardianctl partition delete --partition my-partition [--wait|--force]
./guardianctl intent list --partition my-partition
./guardianctl asset catalog
./guardianctl store tree --path /partitions/my-partition
./guardianctl partition tags --partition my-partition
./guardianctl partition rollback --partition my-partition --tag <tag>
```

In a Kubernetes deployment both values usually come from the bootstrap
`guardian-secrets` Secret in the Guardian namespace:

```bash
export GUARDIAN_CLIENT_DISCOVERY_TOKEN="$(
  kubectl -n guardian get secret guardian-secrets \
    -o jsonpath='{.data.client-discovery-token}' | base64 -d
)"
export GUARDIAN_MONOFS_TOKEN="$(
  kubectl -n guardian get secret guardian-secrets \
    -o jsonpath='{.data.monofs-token}' | base64 -d
)"
```

## Repo-driven partitions (GitOps)

Repositories declare their deployment surface in `.guardian/config.yaml`
(credential-free: committed secrets are rejected):

```bash
./guardianctl repo add --name my-repo --pusher docker-main \
  --partition my-partition --source https://github.com/org/my-repo.git
./guardianctl repo seed --file deploy/bootstrap/repos.yaml  # bulk assignments
./guardianctl repo sync --name my-repo --synth     # import partitions + generate observed intents
./guardianctl repo notify --name my-repo           # CI hook after a push
```

`guardian/partitions/<name>/{config.yaml,intents/*.yaml,payloads/**}` in the
repo maps to `/partitions/<name>/...` in the store.

## AWS

```bash
./guardianctl aws setup --profile prod-admin --region <r>
./guardianctl aws pusher-deploy --dir ../aws --profile prod-admin --account <id> --region <r>
./guardianctl pusher add --account <id> --region <r> --url http://guardian-awsread:19090
./guardianctl aws scan --pusher aws-<id> --partition my-partition
./guardianctl aws observed-generate --pusher aws-<id> --partition my-partition
```

The AWS pusher also serves the read API that Doctor federates (CloudWatch,
Logs Insights, X-Ray, CloudWatch PromQL, observed stacks).

## Documentation

- [Architecture](docs/architecture.md)
- [Logical flows](docs/logical-flow.md)
- [Security architecture](docs/security-architecture.md)
- [Codebase map](docs/codebase-map.md)
- [GCP pusher ADR](docs/design/gcp-pusher.md)

## Troubleshooting

- If `partition list` works, your connection settings are correct.
- `client config requires loopback access or a valid discovery token` — set
  `GUARDIAN_CLIENT_DISCOVERY_TOKEN` and make sure Guardian is configured with
  an externally reachable `monofs.clientApiEndpoint`.
- `name resolver error: produced zero addresses` — MonoFS is advertising
  Kubernetes-only node addresses; rerun the storage deploy so routers pick up
  host-reachable endpoints, or use direct `GUARDIAN_MONOFS_ROUTER`.
- In-cluster components should use `GUARDIAN_MONOFS_USE_EXTERNAL_ADDRESSES=false`;
  discovery served to `guardianctl` should use external addresses.
- Check `GUARDIAN_MONOFS_ROUTER` and `GUARDIAN_MONOFS_TOKEN` are reachable and
  correct for direct connections.
