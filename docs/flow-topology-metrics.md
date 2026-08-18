Guardian Flow Topology Metrics
=============================

Overview
--------
This feature enriches Guardian topology assets with flow-state metadata derived
from metrics and local Guardian observations.

Goals:
- Show data-flow state in topology (`healthy`, `busy`, `failing`, `unknown`).
- Keep reconcile behavior unchanged (read-only overlay in phase 1).
- Remain safe when Doctor is unavailable.

Safety and Dependency Boundary
------------------------------
- Guardian does not require Doctor to compute flow-state.
- Flow-state derivation uses:
  1. Monofs Prometheus metrics (when configured), and
  2. Guardian local observations as fallback.
- If metrics endpoint is unavailable or stale, Guardian keeps serving topology
  and falls back to local observations with an explanatory summary.

Configuration
-------------
Guardian runtime config (`guardian/internal/config/config.go`):

```yaml
guardian:
  enableFlowTopology: true
  flowMetricsURL: "http://monofs-router:9090/metrics"
  flowMetricsTimeout: "2s"
```

Environment overrides:
- `GUARDIAN_ENABLE_FLOW_TOPOLOGY` (`true` or `false`)
- `GUARDIAN_FLOW_METRICS_URL` (HTTP URL to metrics endpoint)
- `GUARDIAN_FLOW_METRICS_TIMEOUT` (Go duration, for example `2s`)

Bootstrap config/env plumbing also supports these keys:
- `guardian.flowMetricsURL`
- `guardian.flowMetricsTimeout`

Intent Authoring
----------------
Add optional metric groups per asset:

```yaml
apiVersion: guardian/v1alpha1
kind: Intent
metadata:
  name: workers
spec:
  targetPusher: k8s
  assets:
    - type: Compute
      name: api
      flowMetricGroups:
        - ingest
        - churn
      properties:
        image: ghcr.io/example/api:latest
```

Validation:
- Group names must be non-empty and match Guardian name rules.
- Invalid names fail validation before deploy/reconcile.

Metric Inputs (Monofs)
----------------------
Guardian consumes these Monofs router metrics:
- `monofs_router_guardian_flow_writes_total`
- `monofs_router_guardian_flow_write_bytes_total`
- `monofs_router_guardian_flow_deletes_total`
- `monofs_router_guardian_flow_delete_bytes_total`

They are labeled by:
- `partition`
- `intent`
- `path_kind`

Current Flow-State Behavior
---------------------------
When `enableFlowTopology=true` and an asset has `flowMetricGroups`:
- If recent metric deltas indicate activity in selected groups, asset flow-state becomes `busy` with source `monofs-metrics`.
- If no activity is detected, Guardian fallback observation determines state (`healthy`, `busy`, `failing`, or `unknown`) with source `guardian-observation`.
- On metrics endpoint errors, fallback observation is used and summary includes an availability note.

Rollback / Kill Switch
----------------------
Immediate logical rollback:
- Set `GUARDIAN_ENABLE_FLOW_TOPOLOGY=false` (or config equivalent).

Metrics-only rollback (keep topology overlay but disable metrics enrichment):
- Leave `enableFlowTopology=true`, clear `GUARDIAN_FLOW_METRICS_URL`.

Notes
-----
- This phase does not gate reconcile/apply decisions.
- Doctor topology can display flow metadata when present, but Doctor outages do
  not block Guardian or Monofs behavior.
