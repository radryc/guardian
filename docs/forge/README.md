# Forge

Forge prepares a Guardian partition repository: it discovers workloads, authors
manifests and assets, validates everything offline, plans the change, and
submits it as a reviewed PR/MR through MonoFS.

Forge never writes Guardian or MonoFS partition state. Its only publish action
is a proposal. Merging at the forge is what leads to deployment.

- Enforcement model: [`enforcement.md`](enforcement.md)
- Diagnostic codes: [`codes.md`](codes.md)
- Reverse-proxy SSO: [`sso.md`](sso.md)

## Build

```bash
go build -o bin/forge ./cmd/forge      # or: make forge-build
make forge-cross                       # linux amd64/arm64, windows amd64, darwin arm64
```

The UI is embedded in the binary. No Node, WebView, or FUSE runtime is required
on the user's machine.

## Quick start

```bash
# 1. Prepare the repository (writes .guardian/config.yaml, the MonoFS
#    pipeline, the guardrail policy, and a .gitignore entry).
forge repo init --name acme-platform --source https://github.com/acme/platform

# 2. Discover workloads from Dockerfiles, compose, and Kubernetes manifests.
forge discover --root .

# 3. Author the partition. `partition new` also registers the binding in
#    .guardian/config.yaml, so this works in an empty or existing repo.
forge partition new --root . --name checkout
forge intent new --root . --partition checkout --name api \
  --pusher k8s-main --cluster k8s-main --namespace checkout
forge asset add --root . --partition checkout --intent api \
  --type Compute --name api --image ghcr.io/acme/api:1 --port 8080
forge asset add --root . --partition checkout --intent api \
  --type Compute --name api \
  --properties '{"image":"ghcr.io/acme/api:1","env":{"LOG_LEVEL":"info"},"resources":{"requests":{"cpu":"100m","memory":"128Mi"}}}'
forge probe add --root . --partition checkout --intent api \
  --asset api --path /healthz
forge probe add --root . --partition checkout --intent api \
  --name checkout-journey --asset api --type http \
  --steps '[{"name":"read","method":"GET","path":"/healthz"},{"name":"update","method":"PUT","path":"/items/${id}","expectStatus":[200]}]'
forge secret ref --root . --partition checkout --intent api \
  --asset api --env DATABASE_URL --ref monofs-secret://checkout/db
forge quota set --root . --partition checkout --profile medium --enforcement enforce
forge quota profiles --root .   # small, small-plus, medium, medium-plus, large, xlarge + custom
forge values set --root . --partition checkout --json '{"imageTag":"v1"}'
forge values set --root . --partition checkout --intent api --json '{"replicas":3}'
forge values set --root . --partition checkout --intent api --asset api --json '{"replicas":5}'
forge access template --partition checkout > access.yaml
forge access set --root . --partition checkout --file access.yaml
forge alert suggest --root . --partition checkout --write

# 4. Verify and preview.
forge validate --root . --strict
forge policy check --root .
forge plan --root . --partition checkout --format markdown

# 5. Submit through MonoFS. This opens a real PR/MR.
forge submit --root . --partition checkout \
  --message "checkout: add api" --body-file plan.md

# 6. Follow the proposal.
forge status --root . --partition checkout --live

# 7. Or use the workbench.
forge ui --root .
```

## Commands

| Command | Purpose |
| --- | --- |
| `forge repo init` | Scaffold `.guardian/config.yaml`, `.monofs/pipelines/partition-pipeline.yml`, `.forge/policy.yaml`, `.gitignore` |
| `forge repo adopt` | Import a live partition bundle into the repository |
| `forge discover` | Scan for Dockerfiles, compose services, Kubernetes workloads, and runtime files; write `.forge/profile.json` |
| `forge partition new` | Create `partitions/<name>/config.yaml` and register the partition binding in `.guardian/config.yaml` (use `--no-bind` to skip) |
| `forge intent new` | Create an intent |
| `forge asset add` / `connect` | Append an asset or a `dependsOn` edge. `dependsOn` must reference an asset in the same intent; use intent `joins` for cross-intent ordering. `--properties` / `--properties-file` accept the full catalog property set for any asset type |
| `forge probe add` | Add a Probe or a multi-step synthetic journey (`--type`, `--steps` / `--steps-file`, `--headers`, `--expect-status`) |
| `forge quota profiles` / `show` / `set` | List predefined profiles, show the current quota, or select a profile for `spec.quota` |
| `forge values show` / `set` | Read or write `${value.NAME}` values at partition, intent, or asset level |
| `forge access show` / `set` / `template` | Author the partition-scoped `access.yaml`; validation enforces that grants stay inside the partition |
| `forge secret ref` | Point a sensitive env var at a secret reference (never a value) |
| `forge alert suggest` | Generate a starter alert group with canonical metric labels |
| `forge validate` | Layered offline validation with stable `FGE` codes |
| `forge policy check` | Evaluate `.forge/policy.yaml` guardrails |
| `forge schema` | Write partition/intent JSON Schema and the asset catalog |
| `forge plan` | Read-only change plan (`--against disk` or `live`) |
| `forge submit` | Validate, build repository operations, publish a review branch through MonoFS |
| `forge revert` | `git revert` plus the same reviewed proposal path |
| `forge status` | Show locally recorded proposals and, with `--live`, refresh job state |
| `forge display bind/show/lint` | Author and lint repo-owned display-key aliases |
| `forge pipeline install` | Validate and install the MonoFS pipeline (`--apply`) |
| `forge doctor` | Check git, repo config, validation, policy, tools, and branch protection |
| `forge ui` | Serve the embedded workbench on loopback |

## Environment

| Variable | Used by |
| --- | --- |
| `MONOFS_ROUTER` | `submit`, `status --live`, `plan --against live`, `pipeline install --apply`, `ui` |
| `MONOFS_TOKEN` | Same |
| `MONOFS_GITHUB_TOKEN` / `GITHUB_TOKEN` | `doctor` branch-protection check |
| `MONOFS_GITLAB_BASE_URL` | MonoFS-side PR detection for self-hosted GitLab |
| `FORGE_PROXY_AUTH_HEADER` | `ui`: identity header trusted from a loopback reverse proxy |
| `FORGE_ALLOWED_ORIGINS` | `ui`: comma-separated public origins allowed to POST through the proxy |

## Safety

- Manifests remain canonical. Display aliases are presentation only and never
  reach YAML, paths, secret refs, or PromQL.
- `forge submit` has no flag that writes `/partitions` directly.
- Repo-managed partitions refuse direct `guardianctl partition push` and
  Guardian UI saves unless an audited break-glass override is used.
- `forge validate` and `plan` are fully offline; `submit`, `status --live`,
  `plan --against live`, `repo adopt`, `pipeline install --apply`, and `ui` need
  MonoFS (the UI for live data). See the environment table above.

## Platform notes

- Linux and Windows use the same binary. Windows uses a local checkout; Linux
  can also publish from a MonoFS mount.
- Writes are atomic and LF-only. Untouched files are never reformatted.
- The UI binds loopback with a one-time session token.
- For shared access, terminate SSO at a reverse proxy and pass its identity
  header to Forge; see [`sso.md`](sso.md).
- The top-bar theme menu offers **System**, **Light**, and **Dark**; the choice
  is stored in `localStorage` and System follows the OS preference.


## Create a partition from an empty repository

`forge partition new` is safe to run before or after `forge repo init`:

- It creates `partitions/<name>/config.yaml`.
- It merges `repo.bindings[]` in `.guardian/config.yaml` (creating the config
  when absent) with `partitionPath: partitions/<name>`.
- `forge repo init` on an existing config now merges instead of refusing; pass
  `--partition` to add a binding later.
- `forge ui` has a **+ New partition** action that calls the same API
  (`POST /api/partition`).

## Value replacement

Manifests can use placeholders anywhere a string is accepted. In the UI they are
called **value overrides**:

- **Partition** tab → **Partition value overrides** applies to every intent and
  asset in the partition.
- **Partition** tab → each intent has **Intent value overrides**.
- **Assets** tab → **Asset value overrides** applies to one selected asset and
  shows the effective saved value with its source (partition/intent/asset).

Each editor takes a JSON object keyed by variable name. The preview table shows
the parsed variables and the `${value.KEY}` reference to use, with a **Remove**
button per variable. The **Remove all … overrides** buttons clear the whole
scope. For example:


```yaml
# partitions/checkout/config.yaml
spec:
  values:
    imageTag: v1
    replicas: 2

# partitions/checkout/intents/api.yaml
spec:
  values:
    replicas: 3
  assets:
    - type: Compute
      name: api
      values:
        replicas: 5
      properties:
        image: ghcr.io/acme/api:${value.imageTag}
        replicas: ${value.replicas}
```

- Precedence: partition values, then intent values, then asset values.
- Use `${value.NAME}`; nested maps use dot paths such as `${value.resources.cpu}`.
- An exact reference keeps the value's type, so `${value.replicas}` becomes a number.
- Other placeholders such as `${intent.core.outputs.tag}` and `${capture}` are left untouched.
- Guardian resolves values during compile/reconcile, so deployments use the concrete values while the repository keeps the template.
- An undefined value fails compilation with the missing key and the available keys.

## Authoring from the workbench

The UI and CLI call the same Go functions, so they produce identical manifests.

- **Assets** — the Add asset form is generated from `/api/catalog`: every
  catalog field for the selected type, including Compute resources, env, ports,
  and mounts, and Probe journey `steps`. JSON controls accept a JSON object or
  array; list controls accept comma-separated values or a JSON array.
- **Quota** — the Partition tab has a profile dropdown (built-in size classes
  plus custom `quota_profiles` from `access/base.yaml`, `access/envs/*.yaml`, or
  `.forge/quota-profiles.yaml`), an enforcement selector, and a read-only limits
  preview. Free-form profile names and inline limits are rejected.
- **Access** — the Access tab edits `partitions/<name>/access.yaml`. Saving
  validates with Guardian's repo-import rules, so cross-partition grants,
  selectors, quotas, deny rules, and platform-owned sections are rejected.

## Create an intent from the workbench

Assets attach to an intent, so the Assets tab includes a **Create intent** form
that calls `POST /api/intent` and writes the same manifest as
`forge intent new`:

- Name, target pusher, and optional cluster/namespace.
- The pusher is required, matching the CLI.
- The form appears above the asset, probe, secret, and dependency forms; those
  forms stay disabled until the partition has at least one intent.
- Creating an intent refreshes the picker immediately — no restart is needed.

The binding is what makes Guardian import the new partition. Without it,
`forge validate` and `forge submit` can succeed while Guardian never sees the
partition.
