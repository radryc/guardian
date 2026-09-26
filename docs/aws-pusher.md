# AWS pusher

Phase 1 adds `guardian-pusher-aws` and a `CDKStack` asset type.

## How authentication works

`guardian-pusher-aws` uses the normal AWS SDK credential chain first:

- environment variables
- shared config / shared credentials files
- AWS SSO / profile-based auth
- EC2 or ECS task role credentials when running inside AWS

After it has those base credentials, the pusher does one of two things:

1. If `--assume-role-name` is set, it assumes:

   `arn:aws:iam::<target.account>:role/<assume-role-name>`

   This is the default and recommended mode.

2. If `--assume-role-name` is empty, it uses the base credentials directly.

That means the pusher itself does **not** need to run inside the target AWS account. It only needs credentials that can assume the target-account deploy role.

## Where the pusher should run

Two deployment models are supported.

### Recommended: run it in AWS

Run `guardian-pusher-aws` in a dedicated tools or control-plane account, for example on:

- ECS/Fargate
- EC2
- EKS

Why this is better:

- long-lived worker
- no dependency on a developer laptop
- easier to attach a stable runtime role
- better network path for AWS APIs and future private assets

In this model:

- the runtime role in the tools account is trusted by the target account's `GuardianCdkDeployRole`
- the pusher assumes that target-account role for each deployment

### Acceptable for development: run it locally

You can also run it on your workstation.

In that case:

- authenticate with `aws sso login`, a profile, or environment credentials
- trust your current IAM user or IAM role in the target account's `GuardianCdkDeployRole`

This is good for development and early testing, but it is not the preferred steady-state deployment model.

## What must exist in AWS

For each target account and region, the pusher expects:

1. a deploy role, default name `GuardianCdkDeployRole`
2. CDK bootstrap stack, default name `CDKToolkit`

Use stratatools setup for both prerequisites:

```bash
cd ../stratatools
uv run st-aws-setup --aws-profile prod-admin --aws-default-region eu-west-1
```

What it does:

1. creates or updates Roles Anywhere trust anchor and profile
2. creates or updates local deploy IAM role and policy
3. creates or updates CloudFormation execution role
4. prints credential_process snippet for `~/.aws/config`

Defaults:

- role name: `GuardianCdkDeployRole`
- profile name: `LocalDeploymentProfile`
- trust anchor name: `LocalDeploymentAnchor`

**Security note:** `AdministratorAccess` is overly broad. Tighten this to the minimum permissions required for your CDK stacks before using in production.

## Example pusher startup

```bash
./guardian-pusher-aws \
  --account 123456789012 \
  --region eu-west-1 \
  --assume-role-name GuardianCdkDeployRole \
  --store-dir /path/to/store
```

`--pusher-name` is optional. When omitted, Guardian now defaults it to:

- `aws-<account>` (for example `aws-123456789012`)

This gives one queue namespace per AWS account by default. `--region` is now an
optional runtime filter:

- set `--region` to pin one worker to one region
- omit `--region` to let the account-scoped worker handle all regions in that account

Or with MonoFS:

```bash
./guardian-pusher-aws \
  --account 123456789012 \
  --assume-role-name GuardianCdkDeployRole \
  --monofs-router host:9090 \
  --monofs-token '...'
```

For the full account enablement flow (what the pusher stack provisions
automatically and how to wire Doctor), see
[`aws-observability-enablement.md`](aws-observability-enablement.md).

## Read-only CloudWatch / X-Ray API

The pusher also serves a read-only observability API so Doctor can federate AWS
telemetry into the same Grafana dataspace as OTLP data.

Enable it with `--awsread-addr` (default `:19090`, or `GUARDIAN_AWSREAD_ADDR`)
and optionally protect it with `--awsread-token` / `GUARDIAN_AWSREAD_TOKEN`
(`Authorization: Bearer <token>`). Disable with `--awsread-disabled`.

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Liveness |
| `GET /v1/aws/metrics?namespace=&metric=` | Discover CloudWatch metrics |
| `GET /v1/aws/metrics/query?namespace=&metric=&stat=&period=&from=&to=&dim.<Name>=<Value>` | Query a CloudWatch metric series |
| `GET /v1/aws/log-groups` | List CloudWatch log groups |
| `POST /v1/aws/logs/query` | Run a Logs Insights query (`{"log_groups":[...],"query":"...","limit":N}`) |
| `GET /v1/aws/xray/services` | List X-Ray service names |
| `GET /v1/aws/xray/traces?service=&from=&to=&limit=` | List X-Ray trace summaries |
| `GET /v1/aws/xray/trace/{traceId}` | Fetch an X-Ray trace as spans |
| `GET/POST /v1/aws/promql/query` | Proxy to CloudWatch's `/api/v1/query` (PromQL instant) |
| `GET/POST /v1/aws/promql/query_range` | Proxy to CloudWatch's `/api/v1/query_range` |
| `GET/POST /v1/aws/promql/series` | Proxy to CloudWatch's `/api/v1/series` |
| `GET/POST /v1/aws/promql/labels` | Proxy to CloudWatch's `/api/v1/labels` |
| `GET /v1/aws/promql/label/{name}/values` | Proxy to CloudWatch's `/api/v1/label/{name}/values` |

The PromQL endpoints proxy CloudWatch's [Prometheus-compatible
API](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch-PromQL.html)
using SigV4 (service `monitoring`). This gives Doctor full PromQL over
CloudWatch metrics instead of a lossy `GetMetricData` translation. An optional
`region` parameter selects the region (defaults to the pusher's region); it is
not forwarded to CloudWatch.

The task role needs `cloudwatch:GetMetricData`, `cloudwatch:ListMetrics`,
`logs:StartQuery`, `logs:GetQueryResults`, `logs:DescribeLogGroups`,
`xray:GetServiceGraph`, `xray:GetTraceSummaries` and `xray:BatchGetTraces`
(`ReadOnlyAccess` plus the explicit statement added in `aws-pusher-stack.ts`).

## CDK asset shape

Example intent asset:

```yaml
- type: CDKStack
  name: network
  payload:
    aws: /partitions/demo/payloads/aws/network/stack.yaml
  properties:
    context:
      envName: prod
    env:
      TEAM_TOKEN:
        secret_ref: monofs-secret://shared/team-token
```

Example payload manifest:

```yaml
sourceType: cdk-ts
sourceDir: /partitions/demo/payloads/aws/network/src
entrypoint: bin/app.ts
stackName: guardian-demo-network
stackID: NetworkStack
packageManager: npm
outputMap:
  vpcId: VpcId
```

### Prebuilt assembly mode (skip synth)

When CI already synthesized a Cloud Assembly, you can point the AWS payload at
that staged directory and the pusher will run CHECK/DIFF/APPLY without running
`npm install` or `cdk synth`.

Requirements:

- `prebuiltAssemblyDir` must be an absolute logical path in the Guardian store
- the directory must include `manifest.json`
- exactly one of `sourceDir` or `prebuiltAssemblyDir` must be set

Example prebuilt manifest:

```yaml
sourceType: cdk-ts
prebuiltAssemblyDir: /partitions/demo/payloads/aws/network/cdk.out
stackName: guardian-demo-network
stackID: NetworkStack
packageManager: none
outputMap:
  vpcId: VpcId
```
