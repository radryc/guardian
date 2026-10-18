# Partition GitOps: repo-sourced partitions, secrets, and auto-deploy

Status: accepted. Scope: make a Git repository the reviewable source of truth for
a partition, keep secret material out of GitHub, and deploy the partition
automatically when the repository is ingested.

## Problem

Partitions should be reviewed and versioned with the code they deploy, but the
Guardian registry (`.registry`) and partition bundles can contain credentials
(pusher tokens, git URLs with userinfo, inline `Secret.value`). Committing those
to a repository publishes them to everyone with read access. At the same time a
merge should be enough to roll the partition out — no manual reconcile step.

## Model

Two planes of state:

- **Intent plane (GitHub / app repo)** — credential-free desired state:
  - `.guardian/config.yaml`: pusher registration by `tokenEnv`, repo bindings.
  - `guardian/partitions/<name>/partition.yaml`: partition settings.
  - `guardian/partitions/<name>/intents/*.yaml`: intent manifests.
  - `guardian/partitions/<name>/payloads/**`: payload files.
  - Secret-backed values use `secretRef` / `tokenEnv`, never inline values.
- **Access plane (MonoFS + cluster)** — secret values and credentials:
  - `/partitions/<name>/secrets/**` in MonoFS (not published), resolved at apply
    time by the pusher (`internal/pusher/secrets`).
  - `/.registry` pusher tokens and repo source URLs (not published).
  - Local, uncommitted overlays for development.

A repo can feed several partitions and a partition several repos; bindings carry
a stack selector, payload path and account/region override, and contain no
credentials (`registry.RepoBinding`).

Platform-wide assignments can be seeded declaratively:

```bash
guardianctl repo seed --file deploy/bootstrap/repos.yaml
```

The file is a `RepoRegistry` with a `repos:` list; each entry uses the same
fields as `guardianctl repo add`. Seeding is idempotent and is safe to run on
every bootstrap.

## Changes

### 1. Deploy after repo ingestion

`repo ingest` and `repo notify` (UI and CLI) reconcile every partition bound to
the repo when ingestion succeeds. Controlled by
`guardian.autoDeployOnIngest` (default `true`); `repo sync` keeps its
`--reconcile` flag (default `true`). The partition watcher remains as a
best-effort safety net.

### 2. Partition from the repository

When a repo contains `guardian/partitions/<name>/`, ingestion imports
`partition.yaml`, `intents/*.yaml` and `payloads/**` into `/partitions/<name>/`.
Repo-authored bundles are authoritative for the partition config. Generated
observed bundles (CDK only) preserve an existing partition config instead of
resetting it.

### 3. Secret guardrails

- Repo config rejects inline `pusher.token` / `pusher.tokenEnv` conflicts and
  credential-bearing URLs.
- Repo-sourced partitions reject inline `Secret.value`; use `secretRef`.
- MonoFS publish skips deny-listed paths (`.registry/**`,
  `**/secrets/**`, `**/*.env`, `**/*.secret.*`, `**/*credential*`) so an
  accidental commit can never be mirrored back to the remote.

### 4. Local ingestion

MonoFS routers accept filesystem ingestion (`ingestion_type=file`, including
`/api/ingest` and `/api/ingest/file`) by default; `--local-ingest=false`
disables it. This is the development path for applying local edits and local
overlays without a repository push.

### 5. Guardian-side repo import

Guardian can import `guardian/partitions/<name>/**` straight from an ingested
repository (MonoFS repo display paths via the `/.repos/<display-path>` logical
mapping) with `guardianctl repo import` or the level-triggered import loop. The
import compares content, writes only changed files, and reuses the same
guardrails as `repo sync` (partition name match, inline secret rejection,
`secrets/**` never imported). See
[`unmanaged-stacks.md`](unmanaged-stacks.md).

## Rollout

1. `guardian.autoDeployOnIngest` + explicit reconcile after ingest. **Done.**
2. `guardian/partitions/<name>/` import in repo sync and store ingest. **Done.**
3. Secret validation in repo config and partition import. **Done.**
4. MonoFS publish deny-list and `--local-ingest` flag. **Done.**
