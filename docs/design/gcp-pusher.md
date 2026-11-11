# ADR: GCP pusher and GCP federation

Status: accepted (Phase 0 validated 2026-11-06)
Decisions: bundled **OpenTofu**, **Workload Identity Federation**, **8 asset
drivers**, **one pusher per GCP project**, **GCP federation in Doctor**.

## Context

The AWS pusher is six subsystems: task drivers, a CDK deployment backend,
observed state, a read API, an inventory scanner, and bootstrap/identity.
Doctor federates the read API's CloudWatch/X-Ray data into the same Grafana
dataspace as native OTLP telemetry.

GCP needs the same surface without shipping HashiCorp Terraform (BSL) and
without long-lived service-account keys.

## Decisions

### 1. Deployment engine: bundled OpenTofu (MPL-2.0)

- `tofu` is vendored into `Dockerfile.pusher-gcp`, version-pinned and checksum
  verified.
- Terraform state lives in a per-project GCS bucket with object versioning and
  CMEK. Each asset owns its own state object at
  `guardian/<partition>/<intent>/<asset>/default.tfstate`; sharing one state
  across assets would make an apply of one asset destroy its siblings.
- State is additionally encrypted with OpenTofu native encryption
  (`gcp_kms` key provider, `aes_gcm` method), so secrets in state are protected
  even if bucket IAM is misconfigured.
- Blueprints are embedded in the binary (`embed.FS`) and copied into a
  per-intent workspace at synth time. A standalone `gcp/` repo mirrors them for
  demos; CI diffs the two.

Phase 0 verified on project `strata-gcp-dev`, region `europe-west1`:

- `tofu init` with the GCS backend and `gcp_kms` encryption configuration.
- `tofu plan` → `apply` → `output` → `destroy` of a `google_storage_bucket`.
- At rest, the state object contained `encrypted_data` and did **not** contain
  the bucket name or `google_storage_bucket`, confirming payload encryption.

Encryption block that works with OpenTofu 1.9.1:

```hcl
encryption {
  key_provider "gcp_kms" "state" {
    kms_encryption_key = "projects/<p>/locations/<r>/keyRings/<ring>/cryptoKeys/<key>"
    key_length         = 32
  }
  method "aes_gcm" "state" {
    keys = key_provider.gcp_kms.state
  }
  state { method = method.aes_gcm.state }
  plan  { method = method.aes_gcm.state }
}
```

Note: the provider argument is `kms_encryption_key` (not `kms_key`) and
`key_length` is required.

### 2. Authentication: WIF, no keys

- `guardianctl gcp setup` creates a workload identity pool/provider, a
  per-project `guardian-deployer` service account, and the scoped role bindings.
- ADC is the service account itself (WIF external-account config or GKE
  Workload Identity). The `google` provider therefore does **not** need
  `impersonate_service_account`; adding it would try to impersonate the SA from
  itself.
- Local testing uses
  `gcloud auth application-default login --impersonate-service-account=<sa>`,
  which yields the same ADC shape as production.

### 3. Tenancy: project-level

- `Target.Account` carries the project ID; `Target.Region` carries the region.
- No task-schema change, no runtime fork, no queue-layout change.
- `CanHandle` matches `target.account == project` and the region list.

### 4. Drivers: eight

Network (VPC), ObjectStore (GCS), Secret (Secret Manager), Compute (Cloud Run),
SQLDatabase (Cloud SQL), Volume (Filestore), LoadBalancer, Observability.

### 5. Doctor federation mirrors AWS

- Same pipeline: Guardian discovery → catalog → planner → bounded fan-out →
  merge.
- `Provider="gcp"` maps to `Backend.Kind="gcp"`.
- Queries select the GCP upstream with `source="gcp"`; `gcp_project`,
  `gcp_region`, `gcp_log_name` are planner predicates.
- Read API `/v1/gcp/*` mirrors `/v1/aws/*`; endpoints stay provider-specific so
  the AWS path is untouched.

### 6. Topology resource bridge

- Guardian's observed collector polls `/v1/gcp/observed/stacks` for every
  standard intent targeting a GCP pusher and writes the Cloud Asset Inventory
  resources into `IntentState.Resources`.
- Each durable resource becomes a flat synthetic asset at the same level as the
  authored assets, mirroring AWS observed stacks. `Provider="gcp"` and the
  Cloud Asset Inventory type are stored on the observation.
- Cloud Run revisions are retained in the inventory as ephemeral resources but
  are hidden from the topology. The newest revision is shown as
  `Latest revision` on the `run.googleapis.com/Service` node.
- The Kubernetes pusher exposes the same shape at
  `/v1/k8s/observed/stacks`; managed Deployments, Services, ConfigMaps, PVCs,
  NetworkPolicies, Secrets, ServiceAccounts, and Ingresses become
  `Provider="kubernetes"` observations. Pods and ReplicaSets are intentionally
  excluded as ephemeral deployment artifacts.

## Consequences

- No BSL dependency; OpenTofu is MPL-2.0 and provider licenses are MPL-2.0.
- The pusher image grows by roughly the `tofu` binary (~90 MB), consistent with
  the AWS image already shipping Node.js and the CDK.
- Diff quality is exact (`plan -detailed-exitcode`), unlike a managed
  Infrastructure Manager preview.
- We own state security: bucket IAM, CMEK, and native encryption.
- Deployment is synchronous inside the task lease; the existing 5-minute lease
  with renewal covers long applies. A per-asset `flock` (kept outside the
  ephemeral workspace) prevents concurrent operations on the same state.

## Open questions

- Cloud Trace has no full-text search; document the limitation or add a
  BigQuery trace sink later.
- Provider upgrades (`hashicorp/google`) need a lock-file refresh runbook.
- Per-intent service accounts for least privilege are a Phase 2 refinement; v1
  uses one deployer SA per project.
