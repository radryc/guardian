# Guardian

> Part of the **Strata** platform.

Guardian is a declarative deployment control plane. Desired state lives in
MonoFS as partitions and intents; Guardian compiles them into typed assets,
computes drift, and queues tasks; pusher workers claim those tasks and execute
DIFF → CHECK → APPLY (or DESTROY) against Kubernetes, Docker, AWS, or a local
backend.

Guardian has no database of its own: **MonoFS is the store and the message
bus**. Partitions, intents, runtime state, queues, archives, and the platform
registry are all versioned files under the `guardian/` namespace.

See [`docs/architecture.md`](docs/architecture.md) for the full architecture.

## Components

| Binary | Role |
| --- | --- |
| `guardiand` | Control plane: reconciler, result processor, observed collector, alerting, UI/API |
| `guardianctl` | Operator CLI: partitions, intents, repos, pushers, AWS, images, bootstrap |
| `guardian-pusher-k8s` | Kubernetes driver (client-go or kubectl) |
| `guardian-pusher-docker` | Docker driver |
| `guardian-pusher-aws` | AWS driver (CDK + SDK), account scans, observed read API (`:19090`) |
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
  ObjectStore, Observability, K8sResource.
- **Rollout / rollback** — spec changes on applied intents become deployment
  revisions; failed applies record a rollback and preserve the pushed manifest
  for retry, while operators can restore archived tags explicitly.

## Prerequisites

- Go 1.26.4 (see `go.mod`).
- Sibling checkouts because of replace directives:

  ```text
  replace github.com/radryc/monofs => ../monofs
  replace github.com/rydzu/ainfra/kvs => ../kvs
  ```

- Node 20+ to build the UI (`make ui-build`).
- Docker (BuildKit) for container builds and the docker pusher; kubectl and a
  cluster for the k8s pusher; Node + `aws-cdk` + credentials for the AWS
  pusher.

## Build

```bash
make ui-build                                        # TypeScript UI -> static assets
go build -o guardianctl ./cmd/guardianctl            # CLI
go build -o guardiand   ./cmd/guardiand              # daemon
go build -o guardian-pusher-docker ./cmd/guardian-pusher-docker
# container images need sibling build contexts:
make guardian-build                                  # guardiand image
make pusher-docker-image-build
make pusher-k8s-image-build
```

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

## guardianctl

`guardianctl` can connect three ways:

1. **Guardian discovery** — `GUARDIAN_API_URL` (or `GUARDIAN_URL`) points at
   the Guardian UI/API; the CLI learns the MonoFS router and token from
   `GET /api/client-config`. If the endpoint is not loopback-only, also set
   `GUARDIAN_CLIENT_DISCOVERY_TOKEN` (`GUARDIAN_DISCOVERY_TOKEN` is accepted
   too).
2. **Direct MonoFS** — `GUARDIAN_MONOFS_ROUTER` + `GUARDIAN_MONOFS_TOKEN`.
3. **Local store dir** — `GUARDIAN_STORE_DIR` for offline inspection.

Useful commands:

```bash
./guardianctl partition list
./guardianctl partition push --dir ./partitions/my-partition    # write desired state
./guardianctl partition reconcile --partition my-partition      # queue one cycle (async)
./guardianctl partition wait --partition my-partition           # block until healthy
./guardianctl partition delete --partition my-partition [--wait|--force]
./guardianctl intent list --partition my-partition
./guardianctl asset catalog
./guardianctl store tree --path /partitions/my-partition
./guardianctl partition tags --partition my-partition
./guardianctl partition rollback --partition my-partition --tag <tag>
```

In a Kubernetes deployment the two secrets usually come from
`guardian-configs-secrets`:

```bash
export GUARDIAN_CLIENT_DISCOVERY_TOKEN="$(
  kubectl -n guardian-configs get secret guardian-configs-secrets \
    -o jsonpath='{.data.client-discovery-token}' | base64 -d
)"
export GUARDIAN_MONOFS_TOKEN="$(
  kubectl -n guardian-configs get secret guardian-configs-secrets \
    -o jsonpath='{.data.monofs-token}' | base64 -d
)"
```

## Repo-driven partitions (GitOps)

Repositories declare their deployment surface in `.guardian/config.yaml`
(credential-free: committed secrets are rejected):

```bash
./guardianctl repo add --name my-repo --pusher docker-main \
  --partition my-partition --source https://github.com/org/my-repo.git
./guardianctl repo sync --name my-repo --synth     # import partitions + generate observed intents
./guardianctl repo notify --name my-repo           # CI hook after a push
```

`guardian/partitions/<name>/{config.yaml,intents/*.yaml,payloads/**}` in the
repo maps to `/partitions/<name>/...` in the store. CI templates for GitHub
Actions and GitLab live in `docs/ci/`.

## AWS

```bash
./guardianctl aws setup --profile prod-admin --account <id> --region <r>
./guardianctl aws pusher-deploy --dir ../aws --profile prod-admin --account <id> --region <r>
./guardianctl pusher add --account <id> --region <r> --url http://guardian-awsread:19090
./guardianctl aws scan --pusher aws-<id> --partition my-partition
./guardianctl aws observed-generate --pusher aws-<id> --partition my-partition
```

The AWS pusher also serves the read API that Doctor federates (CloudWatch,
Logs Insights, X-Ray, CloudWatch PromQL, observed stacks).

## Documentation

- [Architecture](docs/architecture.md)
- [Control loop](docs/guardian-control-loop.md)
- [Partition GitOps](docs/partition-gitops.md)
- [AWS pusher](docs/aws-pusher.md), [AWS scan](docs/aws-scan.md),
  [AWS observed](docs/aws-observed.md)
- [Alerting](docs/alerting.md)
- [Flow topology metrics](docs/flow-topology-metrics.md)
- [UI partition builder design](docs/ui-partition-builder.md)

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
