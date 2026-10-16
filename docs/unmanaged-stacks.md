# Unmanaged stacks and repo association

Status: implemented (detection + association preparation; MonoFS MR publishing
is the integration point). Scope: the AWS → Guardian → MonoFS → repo flow that
turns live CloudFormation stacks into repository-backed partitions, and the
repo → MonoFS → deploy import that closes the loop.

## Detection

`internal/unmanaged` classifies every live stack a pusher reports:

- **fully managed** — an intent covers the stack *and* its partition has a repo
  binding; not listed.
- **missing-repo** — an intent covers the stack but no repo binding feeds the
  partition; the panel offers "associate repo" for the existing partition.
- **unmanaged** — no intent covers the stack; associating a repo creates the
  partition snapshot.

Intent coverage is derived from the store:

- observed intents: the `CDKStack` asset payload manifest `stackName`,
- generated (`implementation: cdk`) intents: `guardian-<partition>-<intent>`.

Repo bindings are read from `/.registry/repos/*.json`.

```
GET  /api/aws/unmanaged
POST /api/aws/unmanaged/{stack}/associate
     {"repoUrl","branch","partition","path","pusher","account","region"}
```

The UI panel ("Unmanaged Stacks") lists the results and prompts for an existing
repository URL on "Associate repo".

## Association bundle

`BuildAssociation` (in `internal/unmanaged`) prepares, without touching AWS:

- an observed snapshot bundle from the live CloudFormation template
  (`observedgen.Snapshot`): `config.yaml`, one observed intent, the payload
  manifest, and a minimal `cdk.out` (assembly manifest + template file). The
  desired template equals the live template, so the stack reconciles as
  `Observed` immediately after import;
- a repo registration (`registry.Repo`) with a binding to the partition;
- the review branch name `guardian/adopt-<partition>-<stack>` and the files
  keyed by repo path (`guardian/partitions/<partition>/...`).

The service then publishes the bundle through MonoFS
(`internal/workspacepub` → router `UploadWorkspaceBundle` + `PublishWorkspace`):

- workspace id = the adoption name; branch strategy = `per_repo_branch`, so
  the pushed review branch is deterministic
  (`monofs/guardian/adopt-<partition>-<stack>/<repo>`); re-runs update the same
  branch instead of opening duplicates;
- an empty base branch means the remote default branch (MonoFS resolves HEAD,
  falling back to `main`/`master`); an empty base commit publishes on top of
  the current remote head and lets the push guard against races;
- MonoFS opens the MR/PR for the review branch (GitHub/GitLab) and returns the
  URL, which Guardian stores on the association and surfaces in the UI. Forge
  tokens come from `MONOFS_GITHUB_TOKEN` / `MONOFS_GITLAB_TOKEN` with optional
  per-repo overrides `MONOFS_GITHUB_TOKEN_<OWNER>_<REPO>` /
  `MONOFS_GITLAB_TOKEN_<OWNER>_<REPO>` (for example `EXAMPLE_STRATA`); without
  a token MonoFS still pushes the branch and returns a compare URL.

Publish failures do not roll back the repo registration: the error is recorded
as `publishError` on the association so the operator can retry.

After the MR merges, the normal loop applies: MonoFS spies/refreshes the repo,
Guardian imports the partition, the watcher reconciles, and the observed
collector reports `Observed`.

## Repo → MonoFS → deploy import

`internal/repoimport` imports a repository's `guardian/partitions/<name>/**`
into the canonical partition namespace:

- local checkouts (`LocalSource`) and MonoFS repo display paths
  (`StoreSource`, logical `/.repos/<display-path>`) are supported;
- files are compared with the current store content and only changed files are
  written, so an unchanged repository does not wake the partition watcher;
- guardrails match `guardianctl repo sync`: partition name must match the
  directory, inline `Secret.value` is rejected, `secrets/**` is never imported,
  unknown directories/files are warnings.

```bash
guardianctl repo import --name strata-aws                  # local source
guardianctl repo import --name strata-aws --partition prod
guardianctl repo import --name strata-aws --source /.repos/github.com/example/strata
```

Git URLs are mapped to `/.repos/<host>/<path>` (for example
`https://github.com/example/strata.git` →
`/.repos/github.com/example/strata`). MonoFS storage nodes resolve reads by
path, so the Guardian principal reads repo display paths directly through this
mapping (the guardian-side mapping lives in `internal/store/monofs`).

## Triggering

The store write is the event: the partition watcher (`SubscribeGuardianChanges`
plus debounce) fires a reconcile, and the reconciler re-queues tasks for intents
whose desired version differs from the applied version or whose state is
failing/drifted. `ReconcileAll` (the periodic full reconcile loop in
`guardiand`) is the backstop, so a missed event self-heals without a direct
MonoFS → Guardian push.
