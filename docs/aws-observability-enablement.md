# AWS account observability — enablement guide

This guide explains what gets enabled automatically when an AWS account is
onboarded, and the small number of steps left to make CloudWatch + X-Ray appear
in Grafana alongside Doctor's native OTLP telemetry.

Enabling an account means deploying one small CloudFormation stack —
**`AwsPusherStack`** (`aws/lib/aws-pusher-stack.ts`). It contains the
`guardian-pusher-aws` worker (account scan + CDK deploys) and the read-only
observability API that Doctor federates from.

```
Grafana ── Prometheus/Loki/Zipkin ── Doctor (k8s)
                                       │  source="aws"
                                       ▼
                         guardian-pusher-aws read API (:19090)
                          ├─ CloudWatch PromQL   (SigV4, monitoring)
                          ├─ CloudWatch Logs Insights
                          └─ X-Ray
```

---

## 1. What `AwsPusherStack` provisions automatically

| # | Resource | Detail |
|---|----------|--------|
| 1 | ECS Fargate task `guardian-pusher-aws` | Task runtime + account scan runner, joins the main Strata VPC / cluster / Service Connect namespace |
| 2 | Read-only HTTP API | Listen address `:19090` via `GUARDIAN_AWSREAD_ADDR=:19090` (enabled by default) |
| 3 | Service Connect service | `guardian-awsread:19090` inside the Strata cluster namespace |
| 4 | ALB exposure | Listener rule `path = /v1/aws/*` → read API, so callers outside the cluster can reach it (disable with `-c exposeAwsReadOnAlb=false`) |
| 5 | Bearer token | Secrets Manager secret `strata/guardian/awsread-token`, auto-generated, injected into the task as `GUARDIAN_AWSREAD_TOKEN` |
| 6 | Task-role IAM | `ReadOnlyAccess` + explicit `cloudwatch:GetMetricData`, `cloudwatch:GetMetricStatistics`, `cloudwatch:ListMetrics`, `logs:StartQuery`, `logs:GetQueryResults`, `logs:DescribeLogGroups`, `logs:StopQuery`, `xray:GetServiceGraph`, `xray:GetTraceSummaries`, `xray:BatchGetTraces` |
| 7 | Exec-role IAM | Read the read-API token and the MonoFS break-glass token |
| 8 | Log group | `/ecs/strata-pusher` (14-day retention) |
| 9 | Stack outputs | `AwsReadServiceConnectAddress`, `AwsReadTokenSecretArn`, `AwsReadApiUrl` |
| 10 | Doctor default | `DOCTOR_AWS_ENABLED` already defaults to `true` |

In other words: after the pusher stack is deployed, the **AWS side** of the
integration needs no further configuration.

### Read API endpoints

| Endpoint | Purpose |
|----------|---------|
| `POST/GET /v1/aws/promql/query` | CloudWatch `/api/v1/query` (PromQL instant) |
| `POST/GET /v1/aws/promql/query_range` | CloudWatch `/api/v1/query_range` |
| `POST/GET /v1/aws/promql/series` | CloudWatch `/api/v1/series` |
| `POST/GET /v1/aws/promql/labels` | CloudWatch `/api/v1/labels` |
| `GET /v1/aws/promql/label/{name}/values` | CloudWatch `/api/v1/label/{name}/values` |
| `POST /v1/aws/logs/query` | CloudWatch Logs Insights |
| `GET /v1/aws/log-groups` | List log groups |
| `GET /v1/aws/xray/services` | X-Ray service names |
| `GET /v1/aws/xray/traces` | X-Ray trace summaries |
| `GET /v1/aws/xray/trace/{id}` | X-Ray trace as spans |
| `GET /v1/aws/metrics/query` | Direct `GetMetricData` fallback (unused by Doctor) |
| `GET /healthz` | Liveness |

All `/v1/aws/*` calls (except `/healthz`) require
`Authorization: Bearer <token>` when `GUARDIAN_AWSREAD_TOKEN` is set.

---

## 2. Enable an account (step by step)

### Prerequisites

- The main stack is deployed (`StrataStack`) and CDK is bootstrapped:
  ```bash
  cd aws
  npm install
  npm run bootstrap      # once per account/region
  ```

### Deploy the pusher (the "account enabled" construct)

```bash
cd aws
# Deploy everything (StrataStack + AwsPusherStack + DemoStack)
npm run deploy

# or just the account observability construct
npx cdk deploy AwsPusherStack
```

Options (context flags):

| Flag | Default | Meaning |
|------|---------|---------|
| `-c deployAwsPusher=true|false` | `true` | Create/skip `AwsPusherStack` |
| `-c exposeAwsReadOnAlb=true|false` | `true` | Add the `/v1/aws/*` ALB rule for cross-cluster access |

### Collect the outputs

```bash
aws cloudformation describe-stacks --stack-name AwsPusherStack \
  --query 'Stacks[0].Outputs' --output table

# Bearer token
aws secretsmanager get-secret-value \
  --secret-id strata/guardian/awsread-token \
  --query SecretString --output text
```

### Point Doctor at the read API

Set these on the `doctor-query` workload (the Doctor `query` intent), then
reconcile the partition:

| Env | Value |
|-----|-------|
| `DOCTOR_AWS_ENABLED` | `true` (already the default) |
| `DOCTOR_AWS_BACKEND_URL` | `http://<AwsReadApiUrl host>` (ALB) or `http://guardian-awsread:19090` (in-cluster) |
| `DOCTOR_AWS_BACKEND_TOKEN` | the secret value above |

Nothing else is required — Grafana reuses the existing Prometheus/Loki/Zipkin
datasources. The **AWS** dashboard folder and the `source="aws"` routing are
already provisioned by the `monitoring` partition.

---

## 3. Network paths (pick one)

| Path | When | `DOCTOR_AWS_BACKEND_URL` |
|------|------|--------------------------|
| Service Connect | Doctor runs in the same ECS cluster/VPC namespace | `http://guardian-awsread:19090` |
| Strata ALB | Doctor runs elsewhere but can reach the Strata edge | `http://<AwsReadApiUrl host>` |
| Private link / peering | Regulated/private networks | add your own NLB/PrivateLink and use its DNS |

The ALB is internet-facing, so the bearer token is mandatory in that mode. Keep
`exposeAwsReadOnAlb=false` if you provide private connectivity instead.

---

## 4. Querying in Grafana

All three signals are reachable through the existing datasources:

- **Metrics** — full PromQL through CloudWatch:
  ```promql
  sum(rate({CPUUtilization, "@instrumentation.@name"="cloudwatch.aws/ec2", source="aws"}[5m]))
  ```
  `source="aws"` tells Doctor to route to CloudWatch; Doctor strips it and
  forwards the rest verbatim.

- **Logs** — Logs Insights via the Loki datasource:
  ```logql
  {source="aws", aws_log_group="/ecs/strata"} |~ "error"
  ```

- **Traces** — X-Ray appears in the Zipkin datasource (service list, search and
  trace waterfall).

Vended AWS metrics versus OTLP-ingested metrics:

| Source | Metric name | Distinguishing labels |
|--------|-------------|-----------------------|
| Vended AWS metrics (OTel enrichment on) | original CloudWatch name, e.g. `CPUUtilization` | dimensions as datapoint labels (`InstanceId`, `FunctionName`, …), `"@instrumentation.@name"="cloudwatch.aws/<service>"`, `"@aws.tag.*"` |
| OTLP metrics ingested into CloudWatch | dotted name, quote it: `{"http.server.active_requests"}` | `"@resource.service.name"`, `"@datapoint.*"`, `"@aws.region"` |

PromQL in CloudWatch requires a metric name in the selector; selecting by labels
alone is not supported.

---

## 5. Verification

```bash
# 1. Pusher liveness through the ALB
curl -fsS "http://<AwsReadApiUrl host>/healthz"

# 2. Signed CloudWatch PromQL via the read API
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "http://<host>/v1/aws/promql/query?query=%7BCPUUtilization%7D"

# 3. End-to-end through Doctor
DOCTOR_QUERY_URL=http://localhost:18080 \
DOCTOR_TENANT=default \
AWS_NAMESPACE=AWS/EC2 AWS_METRIC=CPUUtilization \
  ./doctor/scripts/verify-aws-federation.sh
```

In Grafana: open **AWS → AWS - CloudWatch & X-Ray**, set the *instrumentation
scope* (`cloudwatch.aws/ec2`) and *metric* (`CPUUtilization`) variables.

---

## 6. What remains manual

| Item | Why | How |
|------|-----|-----|
| Doctor env + reconcile | Doctor lives in Kubernetes, not in the pusher stack | set `DOCTOR_AWS_*` and reconcile the `doctor` partition |
| Cross-network connectivity | Depends on where Doctor runs | Service Connect, ALB, or private link |
| OTel enrichment of vended metrics | AWS-side, per account/region | enable per AWS docs, then vended metrics are PromQL-queryable |
| Region pinning | One pusher can serve all regions or one | `--region` / `GUARDIAN_REGION` (defaults to stack region) |
| ALB exposure | Some environments prefer private only | `-c exposeAwsReadOnAlb=false` and use Service Connect/NLB |

---

## 7. Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `401 unauthorized` from the read API | token mismatch | re-read `strata/guardian/awsread-token` and set `DOCTOR_AWS_BACKEND_TOKEN` |
| `502` on `/v1/aws/promql/*` | pusher cannot sign/reach CloudWatch | check task-role IAM, region, and pusher logs in `/ecs/strata-pusher` |
| Empty metric panels | metric/scope mismatch, wrong region, or vended metric enrichment off | set the instrumentation scope variable, enable OTel enrichment |
| `404` from the ALB on `/v1/aws/*` | ALB exposure disabled or stack not redeployed | deploy with `-c exposeAwsReadOnAlb=true` |
| `source` appears in CloudWatch errors | metric selector only had `source` (no metric name) | include a metric name, e.g. `{CPUUtilization, source="aws"}` |

See also: [`aws-pusher.md`](aws-pusher.md) (pusher setup, scan/import) and
[`doctor/README.md`](../../doctor/README.md) (federation config and verification).
