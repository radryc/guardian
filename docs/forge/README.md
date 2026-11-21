# Forge

Forge prepares a Guardian partition repository: it discovers workloads, authors
manifests and assets, validates everything offline, plans the change, and
submits it as a reviewed PR/MR through MonoFS.

Forge never writes Guardian or MonoFS partition state. Its only publish action
is a proposal. Merging at the forge is what leads to deployment.

- Design: [`../design/forge.md`](../design/forge.md)
- Enforcement model: [`enforcement.md`](enforcement.md)
- Diagnostic codes: [`codes.md`](codes.md)

## Build

```bash
go build -o bin/forge ./cmd/forge      # or: make forge-build
make forge-cross                       # linux/windows/darwin, amd64/arm64
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
forge probe add --root . --partition checkout --intent api \
  --asset api --path /healthz
forge secret ref --root . --partition checkout --intent api \
  --asset api --env DATABASE_URL --ref monofs-secret://checkout/db
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
| `forge asset add` / `connect` | Append an asset or a `dependsOn` edge |
| `forge probe add` | Add a Probe targeting an existing asset |
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

## Safety

- Manifests remain canonical. Display aliases are presentation only and never
  reach YAML, paths, secret refs, or PromQL.
- `forge submit` has no flag that writes `/partitions` directly.
- Repo-managed partitions refuse direct `guardianctl partition push` and
  Guardian UI saves unless an audited break-glass override is used.
- `forge validate` and `plan` are fully offline; only `submit`, `status --live`,
  and `plan --against live` need MonoFS.

## Platform notes

- Linux and Windows use the same binary. Windows uses a local checkout; Linux
  can also publish from a MonoFS mount.
- Writes are atomic and LF-only. Untouched files are never reformatted.
- The UI binds loopback with a one-time session token.


## Create a partition from an empty repository

`forge partition new` is safe to run before or after `forge repo init`:

- It creates `partitions/<name>/config.yaml`.
- It merges `repo.bindings[]` in `.guardian/config.yaml` (creating the config
  when absent) with `partitionPath: partitions/<name>`.
- `forge repo init` on an existing config now merges instead of refusing; pass
  `--partition` to add a binding later.
- `forge ui` has a **+ New partition** action that calls the same API
  (`POST /api/partition`).

The binding is what makes Guardian import the new partition. Without it,
`forge validate` and `forge submit` can succeed while Guardian never sees the
partition.
