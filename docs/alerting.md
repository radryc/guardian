# Partition failure alerting

Guardian can notify external channels when a partition starts failing and when
it recovers. Alerts are derived server-side from the same partition state the
UI shows; they are not tied to the UI's timeout presentation.

## How it works

```
guardiand alerting evaluator
  every pollInterval:
    load derived partition state (Failing / Attention / Invalid / ...)
    track consecutive failing / healthy evaluations
    on transition -> notify channels -> write events
```

- A partition is **failing** when its derived status is `Failing` or `Invalid`
  (for example `ApplyFailed`, `DiffFailed`, `CheckFailed`, or compile errors).
- `Attention` (drift, `DriftedLocked`, `ObservedDrifted`) fires a lower-severity
  warning.
- `FailThreshold` consecutive failing evaluations are required before the first
  notification, and the same threshold of healthy evaluations before a
  recovery notification. This avoids flapping on single-pass transitions.
- While a partition stays failing, the alert is repeated every
  `RepeatInterval`; a per-partition `Cooldown` suppresses back-to-back
  notifications.

State is persisted at `/partitions/<p>/.state/notifications.json`. Delivery is
audited with `notification.sent`, `notification.failed`,
`partition.failing` and `partition.recovered` events.

## Configuration

```yaml
notifications:
  enabled: true
  pollInterval: 30s
  failThreshold: 2
  cooldown: 15m
  repeatInterval: 6h
  channels:
    - name: ops-webhook
      type: webhook
      url: https://hooks.example.com/guardian
      secretEnv: GUARDIAN_ALERT_HMAC
      timeout: 10s
  rules:
    - name: critical-only
      events: [partition.failing, partition.recovered]
      minSeverity: warning
      channels: [ops-webhook]
```

- `channels[].type` defaults to `webhook`; it is the only supported type in v1.
- `secret`/`secretEnv` signs the JSON body with HMAC-SHA256 and sets
  `X-Guardian-Signature: sha256=<hex>`.
- When no `rules` are configured, every channel receives every alert. Rules
  match on event name, minimum severity and channel selection.

## Webhook payload

```json
{
  "event": "partition.failing",
  "partition": "payments",
  "status": "firing",
  "severity": "critical",
  "summary": "3 intent(s): 2 failing, 1 healthy",
  "details": {"status": "Failing", "failingIntents": "2"},
  "firedAt": "2026-10-02T10:00:00Z"
}
```

Recovery notifications use `"event": "partition.recovered"`, `"status":
"resolved"` and include `resolvedAt`.

## Test a channel

```bash
go run ./cmd/guardianctl notify test \
  --url https://hooks.example.com/guardian \
  --secret-env GUARDIAN_ALERT_HMAC \
  --partition payments
```

## Verification

- Unit tests cover threshold arming, single-fire, recovery, rule filtering and
  webhook signing/retries.
- End-to-end: point a local webhook receiver at the config, mark an intent
  `ApplyFailed` in the store, and observe exactly one firing notification and
  one recovery notification.

## Sentinel delivery

Guardian can use the existing webhook channel to deliver partition alerts to
Sentinel. Configure the Sentinel URL and the same HMAC secret:

```yaml
notifications:
  enabled: true
  channels:
    - name: sentinel
      type: webhook
      url: http://doctor-sentinel:18082/v1/alerts/intake/guardian
      secretEnv: DOCTOR_SENTINEL_GUARDIAN_HMAC_SECRET
```

When Sentinel owns routing, disable any other Guardian notification channels to
avoid duplicate pages. Sentinel retries failed deliveries; Guardian also retries
on the next evaluation when every configured channel fails.
