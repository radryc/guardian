# Generated CDK stacks (`spec.implementation: cdk`)

Status: implemented. Scope: compile an intent's declarative assets into a
composed TypeScript AWS CDK app built from per-asset templates, and deploy it
as one CloudFormation stack.

## Model

An intent can opt in with:

```yaml
spec:
  intentType: standard
  implementation: cdk
  targetPusher: aws-<account>
  target:
    account: "123456789012"
    region: us-east-1
  assets: [...]
```

With `implementation: cdk`:

- Every **template-backed** asset in the intent (`Network`, `Volume`,
  `ObjectStore`, `Compute`, `LoadBalancer`, `Database`, `SQLDatabase`,
  `Observability`, `AWSResource`) is composed into one TypeScript CDK app: one
  stack per intent, one template call per asset.
- Assets without a template (`Secret`, `Config`, `ImageBuild`, ...) keep their
  native drivers and run before the stack, so generated compute can reference
  native-created SSM parameters and secrets.
- `implementation` defaults to `native` (empty). The mode is per-intent
  all-or-nothing; there is no per-asset mixing.

## Templates

The CDK mapping lives in per-asset TypeScript templates under
`aws/templates/aws/v1/` (source of truth) and is embedded into Guardian by
`internal/cdkgen`. See [`templates.md`](templates.md) for the catalog format,
variant selection, store overlays and digests.

The composer (`internal/cdkgen`):

1. orders the assets (planner dependency order),
2. selects a template per asset via the catalog match rules,
3. validates required/unsupported properties from the catalog,
4. computes deterministic physical names in Go (native-driver parity),
5. emits `lib/stack.ts` importing the selected template modules and wiring
   `refs` (network, volumes, shared ECS clusters, load balancer and
   observability targets),
6. copies exactly the used template files plus `templates/_shared` into the
   payload and records `templates: [{id, version, digest, source}]` and
   `catalogVersion` in `guardian.payload.json`.

## Where the code lives

The control plane (reconciler) regenerates the app on every reconcile:

- `/partitions/<partition>/payloads/aws-generated/<intent>/`
  - `package.json`, `tsconfig.json`, `cdk.json`
  - `bin/app.ts`, `lib/stack.ts`
  - `templates/<template>/construct.ts` (only the selected templates)
  - `templates/_shared/helpers.ts`, `templates/_shared/handles.ts`
  - `guardian.payload.json` (stack name, source dir, output map, template refs)

Writes are content-hash deduplicated and stale files are pruned. Generation is
deterministic and never embeds secret material: `Secret` and `Config` values
stay in their native assets and are referenced by deterministic physical name.

## Store overlays

Templates can be overridden without rebuilding Guardian:

- `/.templates/aws/<dir>/` (global)
- `/partitions/<p>/templates/aws/<dir>/` (partition, wins over global)

Each overlay directory carries `template.yaml` (a catalog entry) plus its
module files. Invalid overlays are skipped with a warning and fall back to the
next level; the payload records the winning `source` (`embedded`, `global`,
`partition`) and the content digest. `guardianctl template list` shows the
effective catalog.

## Deployment

`common.BuildTaskFromManifest` replaces the intent's generated assets with one
synthetic `GeneratedStack` asset (native assets remain first). The AWS pusher's
`GeneratedStackDriver` stages the tree from the store and reuses the existing
CDK backend:

- `CHECK`: `cdk synth` + CDK bootstrap check
- `DIFF`: stack missing / `guardian.hash` tag mismatch / CloudFormation drift
- `APPLY`: `cdk deploy`, then maps CloudFormation outputs back to
  `assetName.outputKey` keys so cross-intent refs keep working
- `DESTROY`: delete the stack (runs before native assets because destroy
  reverses the task asset order)

## AWSResource fallback

`AWSResource` assets (`cfnType`, `properties`, optional `outputs`) render via
the `aws.fallback.cfn-resource` template: a raw CloudFormation resource for
types without a curated template yet. Scanned existing resources are marked
`existing: true` and stay observed-only until CDK import adoption lands.

## Construct parity

Templates create resources directly on the stack scope with the same construct
IDs as the historical monolithic construct library, so CloudFormation logical
IDs stay stable across the redesign. `GeneratedPhysicalNameParity` in the AWS
driver tests guards the native-driver naming contract, and
`aws/test/templates.test.ts` asserts the template-level outputs and resources.
