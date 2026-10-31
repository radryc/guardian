# Guardian TODO

Remaining work after the P0 + P1 correctness pass (October 2026).
Each item includes the reason, the files to touch, and acceptance criteria. When
an item is completed, move the summary to
[`docs/internal/changes.md`](docs/internal/changes.md) and update the
architecture/security docs if behavior changed.

Status key: `[ ]` open, `[~]` partially mitigated, `[x]` done.

Safe-default follow-ups still to flip: require UI authentication (item 2),
reference-only secrets (item 3), AWS read API TLS/scopes (item 4), IAM least
privilege (item 5), and non-root images (item 13). The AWS read API loopback
default and fail-closed OIDC/authz setup are already implemented.

---

## P0 — before shared or production use

### [ ] 1. Add leader election / CAS for control-plane writes

**Why:** every `guardiand` replica runs the reconciler, result processor,
observed collector, alerting, and onboarding loops. State and release writes
are last-writer-wins. Two replicas can double-queue tasks, archive twice, lose
release entries, and race alert state.

**Files:**
- `cmd/guardiand/main.go`
- `internal/orchestrator/reconciler/reconciler.go`
- `internal/orchestrator/dispatcher/dispatcher.go`
- `internal/orchestrator/results/processor.go`
- `internal/alerting/evaluator.go`
- `internal/observed/collector.go`

**Acceptance:**
- Only one replica reconciles a given partition at a time (lease or work claim).
- `WriteIntentState`, `WritePartitionState`, and `WriteRelease` accept and honor
  an expected version; conflicts retry or abort cleanly.
- A two-replica integration test produces no duplicate tasks, releases, or
  archives.
- Leader loss is recovered within one lease interval.

### [ ] 2. Require authentication for UI mutations by default

**Why:** `authorizeMutation` now applies the access policy, but a deployment
without OIDC/login and without an access policy still accepts anonymous
mutations. There is also no CSRF/origin check or security-header baseline.

**Files:**
- `cmd/guardiand/main.go`
- `internal/ui/server.go`
- `internal/ui/templates/index.html`
- `internal/ui/static-src/*`

**Acceptance:**
- A configurable `requireAuthentication` mode rejects anonymous non-GET requests.
- Mutations require `Origin`/`Referer` validation or a CSRF token.
- Responses set CSP, `X-Content-Type-Options`, `X-Frame-Options`/
  `frame-ancestors`, `Referrer-Policy`, and HSTS behind TLS.
- `/auth/logout` is POST (or CSRF-protected).
- Tests cover anonymous mutation, cross-origin mutation, and valid session.

### [ ] 3. Move to reference-only secrets

**Why:** Secret values are redacted from results/state/UI, but the task queue
still carries plaintext while in flight, inline `properties.value` is still
accepted, and Docker/Kubernetes still materialize plaintext env values.

**Files:**
- `internal/domain/assets/secret.go`
- `internal/pusher/secrets/`
- `internal/pusher/drivers/{docker,kubernetes,aws}/`
- `internal/ui/server.go`
- `cmd/guardianctl/partition_push.go`

**Acceptance:**
- `Secret` assets accept `secretRef` and provider-managed references; inline
  values are deprecated or rejected in shared deployments.
- Task specs/queue files never contain plaintext; pushers resolve references
  immediately before use.
- Docker uses a secret file/mount instead of `-e KEY=VALUE`; Kubernetes uses
  `Secret`/`secretKeyRef` and `imagePullSecrets`.
- Secret outputs are hashes or references only.
- Tests prove plaintext never appears in queue/state/result/archive artifacts.

### [ ] 4. Harden the AWS read API

**Why:** it now defaults to loopback, but it is still plaintext HTTP with a
single static token, no scopes/rate limits/timeouts, and it exposes
`/v1/aws/observed/diff`, which runs `npm install`/`cdk synth` on caller input.

**Files:**
- `cmd/guardian-pusher-aws/main.go`
- `internal/awsread/handler.go`
- `internal/awsobserve/diff.go`
- `internal/pusher/drivers/aws/synth.go`

**Acceptance:**
- TLS or mTLS is supported and preferred; server timeouts are set.
- Authentication is mandatory for non-loopback binds; tokens have scopes and
  rotation.
- Per-caller rate limits and request/response size limits exist.
- The source-upload/synth endpoint is disabled by default or requires an
  explicit privileged scope.
- Tests cover unauthenticated, wrong-scope, and oversized requests.

### [ ] 5. Apply IAM least privilege and trust conditions

**Why:** the deploy role trusts the account root without ExternalId, the
"scoped" policy includes broad destructive/secrets actions, and generated
per-intent roles use `Resource:"*"` with `iam:CreateRole`/`PassRole`.

**Files:**
- `internal/awssetup/iam.go`, `policy.go`
- `internal/pusher/drivers/aws/execution_role.go`
- `cmd/guardianctl/aws.go`
- `internal/pusher/drivers/aws/cli_backend.go`

**Acceptance:**
- Role trust requires ExternalId and source-account/source-ARN conditions.
- Policies are resource-scoped per asset/template; no `*` secrets/IAM grants.
- A permissions boundary is attached and documented.
- Static AWS credentials are never exported to `npm`/arbitrary CDK app code.
- `aws setup` validates the role can actually be assumed.

### [ ] 6. Add task-archive retention and integrity

**Why:** task specs are now archived under `.state/tasks`, but the archive is
unbounded and unsigned.

**Files:**
- `internal/orchestrator/dispatcher/dispatcher.go`
- `internal/paths/paths.go`
- `internal/compliance/publisher.go`

**Acceptance:**
- Retention is configurable (for example, keep the last 250 specs per
  partition or N days).
- Old specs are pruned without touching the current queue.
- Archive records are signed or hash-chained and mirrored to compliance storage.

---

## P1 — correctness and operability

### [ ] 7. Finish or explicitly deprecate native AWS resources

**Why:** native Aurora has no instances/security groups/encryption; EFS has no
mount targets; Network creates only a VPC; LoadBalancer downgrades HTTPS and
orphans resources on destroy; S3 lacks encryption/public-access controls.

**Decision required:** either complete the native drivers or mark them
experimental and require `implementation: cdk` for those asset types.

**Acceptance (if completing):**
- Aurora creates writer/reader instances, SGs, encryption, backups, deletion
  protection.
- EFS creates mount targets/access points and Compute can mount it.
- Network creates subnets/IGW/NAT/routes and consumers use it.
- LB honors HTTPS/ACM and cleans up target groups/SGs/attachments.
- S3 applies SSE/KMS, public-access block, policy, lifecycle, and logging.
- Drift compares live properties instead of only the `guardian-hash` tag.

### [ ] 8. Complete Kubernetes asset coverage

**Why:** there is no Secret/Ingress/Job/StatefulSet/DaemonSet renderer, no
pruning of removed Services/ConfigMaps/policies, `K8sResource` Destroy is a
no-op, and Service selectors only use `guardian.asset`.

**Acceptance:**
- Secret, Ingress, Job, StatefulSet, and DaemonSet renderers exist.
- Service selectors are unique per intent/asset.
- Removed child resources are pruned or explicitly retained.
- `K8sResource` has ownership labels and destroy semantics.
- Integration tests run against a real/kind cluster.

### [ ] 9. Add repo-level deletion / GC

**Why:** pruning now handles files inside an existing partition, but a repo
that removes an entire partition leaves it deployed.

**Files:**
- `internal/repoimport/`
- `internal/ui/repo_import_loop.go`
- `cmd/guardianctl/repo_cmd.go`

**Acceptance:**
- A repo ownership manifest records partitions owned by each source.
- Removing a partition from the repo marks it for deletion or reports it for
  explicit approval.
- Shared partitions fed by multiple repos are not deleted by one repo's change.

### [ ] 10. Harden alert delivery and state

**Why:** zero/unknown channels can arm state, partial channel success suppresses
retry until the repeat interval, and replicas race on `notifications.json`.

**Acceptance:**
- Alerts are retried per channel with idempotency keys.
- State writes use CAS.
- A channel failure does not mark the alert delivered.
- Tests cover partial failure, replay, and two-replica races.

### [ ] 11. Fix history/rollout scalability

**Why:** rollout/history reads scan archives and manifests before applying
limits; the archive index is unused; overview reads full detail for every
partition.

**Acceptance:**
- A summary projection (written at reconcile time) powers `/api/overview`.
- History filters/limits are applied during the scan.
- Archive index is either written and used or removed.
- A 100-partition benchmark meets an agreed latency budget.

### [ ] 12. Complete compliance storage

**Why:** the S3 publisher drops errors, has no retry, no SSE/KMS, and no Object
Lock.

**Acceptance:**
- Publish failures are retried and surfaced.
- SSE/KMS, Object Lock, and bucket policy are configurable.
- `Close` is idempotent and cannot panic under concurrent publish.

### [ ] 13. Harden build and release

**Why:** images run as root; several Makefile targets are empty; CI only runs
tests; the release workflow deletes `go.sum`; version labels are inconsistent.

**Acceptance:**
- Non-root users, read-only rootfs, dropped capabilities, and explicit state/
  socket mounts in all images.
- `make guardian-up/down/logs`, `deploy-s3`, and `dogfood-up` work or are
  removed.
- CI builds images, generates SBOMs, scans vulnerabilities, signs artifacts, and
  never deletes `go.sum`.
- Version/commit/build-time labels are set consistently.

### [ ] 14. Add probe history and failure limits

**Why:** only the latest probe snapshot is stored; storeless mode can panic;
probe HTTP transports are created per request.

**Acceptance:**
- Probe results have a bounded history/time window.
- Storeless mode handles a nil store safely.
- Transports are reused and closed on shutdown.
- Per-target error budgets/circuit breaking are documented.

---

## P2 — simplification and maintainability

### [ ] 15. Split the AWS `BackendAPI` god interface

Replace the 43-method interface with per-resource interfaces (`ComputeBackend`,
`NetworkBackend`, …). Remove the fake/CLI/composite stub boilerplate.

### [ ] 16. Remove the compiler/task-build double parse

Serialize the compiled plan into the task instead of re-reading and
re-validating the raw manifest. This also closes the TOCTOU window.

### [ ] 17. Split giant files

- `internal/ui/data.go` (≈3k lines) → overview/detail/history/topology/redact.
- `internal/ui/static-src/builder.ts` and `main.ts` → feature modules.
- Docker/Kubernetes `drivers.go` → one file per asset type.
- `cmd/guardianctl/main.go` and `bootstrap.go` → command modules/dev tool.
- `internal/awsscan/scanner.go` → one file per service.

### [ ] 18. Unify caches, pagination, and helpers

- One TTL/watch cache for partitions, rollouts, probes, identities.
- One pagination helper with pinned semantics.
- One map-clone helper, one partition-name lister, one sanitizer set.

### [ ] 19. Derive the asset catalog from the typed specs

Generate UI catalog fields from the Go asset specs (or add a test that every
catalog path exists in the typed spec). Remove the `Database`/`SQLDatabase`
alias duplication.

### [ ] 20. Extract a CLI bundle service

`partition push`, `validate`, `tag`, `rollback`, and `repo sync` should share
one bundle load/validate/write service instead of reimplementing marshal/parse
cycles.

---

## Verification checklist for every item

- [ ] Unit tests for the new behavior and failure paths.
- [ ] At least one integration/concurrency test where applicable.
- [ ] `go test ./cmd/... ./internal/...` passes.
- [ ] Architecture/security docs updated if behavior or defaults change.
- [ ] `docs/internal/changes.md` updated with files and tests.
- [ ] No new plaintext secrets or unvalidated path inputs.
