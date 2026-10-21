# Customer observed onboarding

Guardian can onboard existing customer CloudFormation stacks without mutating
them. The customer keeps deploying their own CDK app; Guardian gets read-only
visibility, probes and CloudWatch/X-Ray telemetry.

## Pieces

| Piece | Where | Purpose |
|---|---|---|
| `GuardianObserve` | customer CDK stack | Publishes the onboarding descriptor, tags the stack, adds a CloudWatch agent sidecar |
| `AwsObservedPusherStack` | customer AWS account | Storeless observed pusher: CloudFormation events, read API, SSM descriptor reader, probes |
| `guardianctl aws onboard` | Guardian control plane | Pulls descriptors and creates observe-only partitions/intents |
| Onboarding controller | `guardiand` | Optional periodic version of the CLI |
| Grafana dashboard | Guardian registry | One generated dashboard per partition |

The pusher never dials MonoFS or Guardian. Everything is pull-based: Guardian
reads the pusher's `/v1/aws/onboarding`, `/v1/aws/probes`, `/v1/aws/observed/*`,
PromQL, Logs Insights and X-Ray endpoints.

## Customer construct

```ts
import { GuardianObserve } from './lib/guardian-observe';

new GuardianObserve(this, 'Guardian', {
  partition: 'customer-acme-prod-2', // one per account environment
  intent: 'app',
  repo: {
    url: 'https://github.com/acme/app.git',
    branch: 'main',
    selector: 'CustomerAppStack',
  },
  publish: { mode: 'store' }, // or { mode: 'repo', repo: 'guardian-onboarding' }
  probes: [
    { name: 'health', type: 'http', url: alb.loadBalancerDnsName, path: '/healthz' },
  ],
  telemetry: { logGroup: logGroup.logGroupName },
  targets: [service],
});
```

The construct writes `/guardian/onboarding/<stack>` with the descriptor, adds
stack tags, and adds a CloudWatch agent sidecar to the target ECS services. The
agent listens on `localhost:4317`/`4318` and forwards OTLP metrics, logs and
traces to the CloudWatch OTLP endpoints using its task role.

The construct does **not** create the pusher or the Guardian partition. The
pusher is an account-level stack. The partition is created asynchronously by
Guardian after it pulls the descriptor.

## Pusher

Deploy one `AwsObservedPusherStack` per account/region. It runs
`--observed-only`, reads descriptors from `/guardian/onboarding`, exposes probe
results, and forwards its own probe telemetry through a CloudWatch agent
sidecar. It has no MonoFS token.

Register it once:

```bash
guardianctl pusher add \
  --name aws-<account> \
  --account <account> \
  --region <region> \
  --url <AwsReadApiUrl> \
  --token <read token>
```

## Onboarding

```bash
guardianctl aws onboard --pusher aws-<account> --reconcile
guardianctl partition wait --partition customer-acme-prod-2 --status Observed
```

Or enable the controller:

```yaml
guardian:
  onboardInterval: 1m
```

For each descriptor Guardian:

1. fetches the live CloudFormation template from the pusher;
2. writes an observe-only partition and `CDKStack` intent into MonoFS;
3. writes the descriptor audit record and the latest probe snapshot;
4. generates a per-partition Grafana dashboard;
5. optionally commits the bundle to a Guardian-owned repository when
   `publish.mode: repo`.

The partition is `Observed` immediately because the desired template is the
live template. Replace the snapshot with the authored CDK app later if you want
drift against source instead of drift against the live baseline.

## CloudWatch OTLP

CloudWatch now ingests OTLP metrics, logs and traces natively:

| Signal | Endpoint | Query |
|---|---|---|
| Metrics | `https://monitoring.<region>.amazonaws.com/v1/metrics` | PromQL |
| Logs | `https://logs.<region>.amazonaws.com/v1/logs` | Logs Insights |
| Traces | `https://xray.<region>.amazonaws.com/v1/traces` | X-Ray / Transaction Search |

The CloudWatch agent handles SigV4, endpoint selection and log group headers.
OTLP metric labels are exposed as `@resource.*`; for example
`guardian.partition` becomes `@resource.guardian.partition`. Use quoted metric
names for dotted OTLP names: `{"http.server.request.duration"}`.

Prerequisites:

- CloudWatch agent `1.300070.0` or later for the OTLP receiver.
- Traces: enable CloudWatch Transaction Search.
- Optional: enable CloudWatch OTel enrichment to get `@aws.tag.*` labels from
  resource tags.

## Demo

`demo.sh onboard` (or `demo.sh test --scenario 8`) deploys the customer stack
into demo accounts 2 and 3, starts storeless observed pushers, materializes both
partitions, checks probes, dashboards, CloudWatch logs/X-Ray and verifies
`ObservedDrifted` after an out-of-band SSM change.
