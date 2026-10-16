# CDK asset templates

Status: implemented. Scope: the per-asset TypeScript templates that Guardian
composes into generated CDK stacks, the catalog that selects them, and the
store overlay that overrides them without rebuilding Guardian.

## Layout

```
aws/templates/aws/v1/
  catalog.yaml                       # template registry (selection + params)
  _shared/helpers.ts                 # naming, outputs, ports, secrets helpers
  _shared/handles.ts                 # cross-template handle types
  _shared/database.ts                # Aurora engine shared by the DB variants
  network.vpc/construct.ts
  network.default/construct.ts       # implicit VPC (no Network asset)
  volume.efs/construct.ts
  objectstore.s3/construct.ts
  compute.fargate/construct.ts
  loadbalancer.alb/construct.ts
  loadbalancer.nlb/construct.ts
  database.aurora-postgres/construct.ts
  database.aurora-mysql/construct.ts
  observability.otel-sidecar/construct.ts
  fallback.cfn-resource/construct.ts
```

Each module exports a factory function that creates resources **directly on the
stack scope** with stable construct IDs:

```ts
export function computeFargate(stack: cdk.Stack, name: string, call: AssetCall): ComputeHandle
```

`AssetCall = { properties, generated, refs, partition, intent }`. `generated`
carries the deterministic physical names computed in Go; `refs` carries handles
from previously instantiated templates (network, volumes, cluster, targets).

Refresh the embedded copy after changing templates:

```bash
cd guardian && go generate ./internal/cdkgen
```

## Catalog

`catalog.yaml` declares one entry per template:

```yaml
- id: aws.compute.fargate
  assetTypes: [Compute]
  export: computeFargate
  module: compute.fargate/construct
  version: 1
  requires: [network]
  outputs: [service, launchType, cluster]
  params:
    required: [image]
    unsupported: [privileged, observeExisting]
```

- `assetTypes` maps the entry to Guardian asset types.
- `priority` + `match` rules select variants. Rules support `equals`, `in`,
  `contains`, `exists`, `notExists` over dotted paths with `[*]` wildcards, for
  example `listeners[*].protocol equals tcp` selects the NLB template.
- `outputs` are the `assetName.outputKey` keys exported as CloudFormation
  outputs; `outputPatterns` (for example `tg_%d_arn`) expand once per listener.
- `params.required` / `params.unsupported` replace the historical Go validation
  switches. Unsupported means the property must be empty/false.
- `requires: [network]` makes the composer instantiate the implicit
  `network.default` template when no Network asset exists.
- `implicit: true` entries are not selectable by asset type (the default VPC).
- `fallback: true` marks generic fallback templates (AWSResource).

The catalog replaces the previous Go switches `supportedTypes`,
`OutputsForType`, `methodName` and `validateGeneratable`.

## Store overlays (hybrid source)

The embedded catalog is the always-available baseline. Overlays layer on top
with precedence **partition > global > embedded**:

- `/.templates/aws/<dir>/template.yaml` + module files (global)
- `/partitions/<p>/templates/aws/<dir>/template.yaml` + module files
  (partition)

Overlay entries can override an existing template id or add a new one (for
example a generated fallback entry for an AWS service without a curated
template). Invalid overlays (missing id/module/export, bad match op, unreadable
files) are skipped with a warning and fall back to the next level.

Every generated payload records the winning template revision:

```json
"templates": [
  {"id": "aws.compute.fargate", "version": 1, "export": "computeFargate",
   "module": "compute.fargate/construct", "digest": "…", "source": "embedded"}
],
"catalogVersion": "v1"
```

so a deployment can be traced back to the exact template content that produced
it, even when an overlay was active.

## CLI

```bash
guardianctl template list                    # effective catalog
guardianctl template list --partition demo   # include partition overlays
guardianctl template list --type Compute
```

## Tests

- Go: `internal/cdkgen` covers catalog parsing/selection/matching, parameter
  validation, renderer output (golden text, output map, manifest), overlay
  precedence/fallback/digests, AWSResource fallback composition, and
  **CloudFormation generation**: `cfn_synth_test.go` compiles the generated
  payload with `tsc` and synthesizes it with `aws-cdk-lib`, then asserts the
  CloudFormation template (resource families, outputs coverage, stable logical
  IDs, ALB/NLB variants, RDS engines, sidecar containers, fallback resources).
  It needs node plus `aws/node_modules` (set `GUARDIAN_AWS_NODE_MODULES` to
  override; skipped with `-short`).
- TypeScript: `aws/test/templates.test.ts` synthesizes every template and
  asserts resources, outputs and handle wiring (cluster sharing, volume mounts,
  observability sidecar).
