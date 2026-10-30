# Strata observability naming contract

This is the canonical naming contract for traces, logs, metrics, and probe
telemetry. It applies to every first-party service and every Guardian-managed
asset.

## Identity

| Field | Value |
| --- | --- |
| `service.name` | `<partition>.<intent>.<asset>` |
| `service.namespace` | `<partition>` |
| `service.alias` | legacy / producer name (for example `guardian`, `doctor`, `monofs-node-a`) |
| `guardian.partition` | partition |
| `guardian.intent` | intent |
| `guardian.asset` | asset |
| `guardian.pusher` | producing pusher |
| `guardian.provider` | `kubernetes`, `docker`, `aws`, or `local` |
| `guardian.cluster` | cluster for kubernetes/docker |
| `guardian.account` / `guardian.region` | AWS target |
| `guardian.component` | binary or subsystem (`guardiand`, `doctor-ingest`, `monofs-router`, ...) |
| `guardian.service.full_name` | untruncated canonical name |
| `guardian.service.truncated` | `true` when the 200-character cap applied |

Canonical identity wins over an explicit `OTEL_SERVICE_NAME`; the explicit
value is preserved as `service.alias`.

Control-plane exceptions:

- `guardian.pusher.<pusher>` for pusher processes.
- `guardian-configs.guardian.guardiand` for `guardiand` (the bootstrap
  controller runs with `guardian.partition=guardian-configs`).

Identity belongs on the OTLP **resource**, never on metric data points.

## Query vocabulary (Doctor projection)

Prometheus, Loki, and Tempo expose the same labels:

`service`, `service_full`, `service_alias`, `tenant`, `source`,
`guardian_partition`, `guardian_intent`, `guardian_asset`,
`guardian_component`, `guardian_cluster`, `guardian_pusher`,
`guardian_provider`, `guardian_account`, `guardian_region`, `k8s_*`,
`level`, `trace_id`, `span_id`.

Legacy short keys (`partition`, `intent`, `asset`, `service_name`) are accepted
as ingestion aliases but are never emitted. Dashboards must use the canonical
names.

## Metric names

- OTel source: `<namespace>.<component>.<subject>[.<unit>]`
- Prometheus: the same name with `.` replaced by `_`.
- Counters end in `.total`; durations in `.seconds` (unit `s`); sizes in
  `.bytes`; gauges have no suffix.

Namespaces: `guardian`, `doctor`, `monofs`, `kvs`, `lb`, `k8s.top`.

Guardian components: `reconciler`, `results`, `dispatcher`, `registry`,
`pusher`, `probe`, `asset`.

Examples:

| OTel | Prometheus |
| --- | --- |
| `guardian.reconciler.partition.duration.seconds` | `guardian_reconciler_partition_duration_seconds_bucket` |
| `guardian.results.executions` | `guardian_results_executions` |
| `guardian.pusher.task.executions` | `guardian_pusher_task_executions` |
| `guardian.probe.duration.seconds` | `guardian_probe_duration_seconds` |
| `lb.proxy.connection_errors.total` | `lb_proxy_connection_errors_total` |
| `k8s.top.pod.cpu.usage.millicores` | `k8s_top_pod_cpu_usage_millicores` |

## Spans and log scopes

Span names: `<namespace>.<component>.<operation>`.
Logger/tracer scope: `<namespace>.<component>`.

| Old | New |
| --- | --- |
| `guardian.reconcile.partition` | `guardian.reconciler.partition` |
| `guardian.result.process` | `guardian.results.process` |
| `guardian.task.execute` | `guardian.pusher.task.execute` |
| `guardian.probe` | `guardian.probe.run` |
| `k8s-top.scrape` | `k8s.top.scrape` |
| `guardian/reconciler` | `guardian.reconciler` |
| `guardian/pusher/runtime` | `guardian.pusher.runtime` |
| `guardian/stdlog` | `guardian.stdlog` |
| `doctor/stdlog` | `doctor.stdlog` |
| `monofs/server` | `monofs.server` |
| `lb/edge` | `lb.edge` |
| `k8s-top/stdlog` | `k8s.top.stdlog` |

## Probes

Probe results carry the **target** identity (`service.name =
partition.intent.asset`). The pusher is producer metadata.

Metrics:

```
guardian.probe.up
guardian.probe.duration.seconds
guardian.probe.last_success.timestamp.seconds
guardian.probe.consecutive_failures
guardian.probe.state_age.seconds
guardian.probe.info
```

Metric labels: `guardian_probe_name`, `guardian_probe_type`,
`guardian_probe_mode`. `guardian_probe_target`, `guardian_probe_path`,
`guardian_probe_id`, and `guardian_probe_status` are available on the info
metric, logs, and spans. Synthetic journeys also carry
`guardian_probe_failed_step` and `guardian_probe_step_count` on logs and spans.

Scope: `guardian.probe`. Span: `guardian.probe.run`.

## Scraped Prometheus metrics

The collector promotes Guardian pod labels to metric labels so scraped metrics
carry the same identity as OTLP signals:

```
guardian_partition
guardian_intent
guardian_asset
```

Doctor promotes those to typed identity at ingest, so scraped metrics get the
canonical `service` and `service_full` values. The original scrape job name is
preserved as `service_alias`.
