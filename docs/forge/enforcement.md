# Forge enforcement model

The rule:

> No partition change reaches Guardian except through: git branch → backend
> PR/MR → required checks → merge → MonoFS ingest → Guardian repo-import pull →
> reconcile.

Forge implements the authoring side. The gate is completed by four controls.

## 1. Required checks at the forge

MonoFS reports the pipeline outcome as a commit status:

- context: `monofs/<pipeline-name>`; Forge generates `partition-pipeline`, so
  the required check is `monofs/partition-pipeline`;
- a `pending` status is posted when the review branch is published;
- the final status is posted when the pipeline run finishes;
- without a forge token the report is a no-op.

Branch protection must require `monofs/partition-pipeline` and one review
through `CODEOWNERS`. `forge doctor` verifies both through the GitHub API and
fails when the gate is not enforced.

Single-maintainer repositories cannot self-approve. The documented mode there is
**0 required approvals + required check**; `forge doctor` reports which mode is
active.

## 2. MonoFS workspace policy

Deny direct branch strategies for partition repositories. This is a MonoFS
deployment config (`.monofs` workspace policy), not a Forge flag:

```yaml
version: 1
default: allow
rules:
  - name: partitions-review-only
    match:
      actions: [PUBLISH, SOURCE_PUSH]
      branch_strategies: [direct]
      repository_ids: ["<partition-repo-storage-id>"]
    effect: deny
    reason: POLICY_MODE_RESTRICTED
  - name: partitions-no-force
    match:
      actions: [SOURCE_PUSH]
      push_modes: [squash]
      repository_ids: ["<partition-repo-storage-id>"]
    effect: allow
```

Result: a direct push to the partition repository is rejected by policy; only
`workspace_branch` or `per_repo_branch` publishes are accepted, and those open a
review request.

## 3. MonoFS write ACL

Guardian state and imported partition bundles are written by the control-plane
principal:

- `guardian_principals.go`: `GuardianControlPlanePrincipalID = "guardiand"`;
- allow writes under `/partitions/**` and `/guardian-system` for `guardiand`
  only;
- deny writes to CLI, UI, and user principals.

If the deployment cannot distinguish the repo-import writer from the UI writer,
the fallback is an app-level provenance guard: partitions carrying a repo
binding are read-only except for writes tagged `source: repo-import`.

## 4. Break-glass is explicit and audited

Two paths remain, both visible:

| Path | Control |
| --- | --- |
| `guardianctl partition push` / `set-config` | Refuses repo-managed partitions unless `--break-glass`; writes a `partition.break_glass` history event |
| Guardian UI `PUT /api/partitions/{p}/bundle` | Refuses repo-managed partitions unless `GUARDIAN_ALLOW_REPO_MANAGED_WRITES=true` |

The bootstrap exception is `_bootstrap` and `guardianctl dev init`: nothing is
deployed yet, so no pipeline exists to merge through. It is limited by name and
never applies to a normal partition.

## 5. Rollback

Rollback is a revert PR through the same path (`forge revert` will open it).
Guardian archive restore remains break-glass.
