# Probes and user journeys

A `Probe` asset is the definition for a check that a pusher runs on an
interval. The pusher executes it locally (it is not applied as infrastructure),
exports the result as OTLP telemetry attributed to the target service, and
writes the latest state to MonoFS.

Two modes are supported:

- **single check** — one HTTP request, TCP connection, DNS lookup, TLS
  handshake, or standard gRPC health check;
- **synthetic journey** — an ordered list of HTTP and gRPC steps. Steps can
  capture values from responses and interpolate them into later requests.

A single `type: grpc` probe calls `grpc.health.v1.Health/Check` and requires a
`SERVING` status. Use `type: tcp` if you only want to test that the port is
open.

## Single check with a request body

`body` also works on a single-request probe, so `PUT`/`POST` checks do not need
a journey:

```yaml
- type: Probe
  name: update-item
  version: "1"
  properties:
    target: api
    type: http
    method: PUT
    path: /foo/bar
    headers:
      Content-Type: application/json
    body:
      status: ready
    expectStatus: [200]
    interval: 30s
    timeout: 3s
```

Objects are sent as JSON. Strings are sent verbatim, which is useful for
non-JSON payloads.

## Synthetic journey

The canonical example: read a resource, modify it, post the modification, then
verify the result.

```yaml
- type: Probe
  name: foo-journey
  version: "1"
  properties:
    target: api
    interval: 30s
    timeout: 5s
    steps:
      - name: read
        method: GET
        path: /foo/bar
        capture:
          foo: json:$            # capture the whole JSON response
          trace: header:X-Trace  # capture a response header

      - name: update
        method: PUT
        path: /foo/bar/${foo.id}
        headers:
          Content-Type: application/json
        body:
          id: ${foo.id}
          name: ${foo.name}
          status: modified
        expectStatus: [200]

      - name: create
        method: POST
        path: /foo/${foo.id}/events
        body:
          event: modified
          payload: ${foo}        # the whole captured object, as JSON
        expectStatus: [201]
        capture:
          eventId: json:$.id

      - name: verify
        method: GET
        path: /foo/${foo.id}/events/${eventId}
        expectBody: modified
```

Steps run in order. If a step fails, the remaining steps are skipped and the
probe result is `down` with `failedStep` set.

### Step fields

Every step supports `type` (`http`, `https`, or `grpc`), `name`, `url`,
`address`, `port`, `timeout`, and `insecureSkipVerify`. A step inherits the
probe's target address when it does not set its own `url`/`address`. HTTP steps
also inherit the probe-level `headers`; a step-level header with the same name
wins. gRPC steps use `metadata` instead of HTTP headers.

HTTP steps:

| Field | Meaning |
| --- | --- |
| `method` | HTTP method; defaults to `GET`. |
| `path` | Path relative to the probe target. May contain `${var}`. |
| `headers` | Request headers; values may contain `${var}` or secret references. |
| `body` | String or JSON value. Objects are serialized as JSON. |
| `patch` | RFC 7386 JSON merge patch applied to `body` after interpolation. `null` deletes a key. |
| `expectStatus` | Accepted HTTP statuses; defaults to 2xx/3xx. |
| `expectBody` | Substring that must appear in the response body. |
| `expectJson` | Map of JSON paths to expected values, e.g. `status: success` or `data.result.0.value.1: "1"`. |
| `capture` | Values to extract into variables (see below). |

gRPC steps:

| Field | Meaning |
| --- | --- |
| `service` | Fully-qualified gRPC service, e.g. `demo.v1.CartService`. |
| `grpcMethod` | Method name, e.g. `GetCart`. |
| `request` | JSON request object (or a string containing JSON). |
| `patch` | JSON merge patch applied to `request` after interpolation. |
| `metadata` | Outgoing gRPC metadata; values may contain `${var}` or secret references. |
| `expectStatus` | Accepted gRPC status codes; defaults to `0` (OK). |
| `expectBody` | Substring that must appear in the JSON-encoded response. |
| `expectJson` | Map of JSON paths to expected values in the JSON-encoded response. |
| `capture` | Values to extract into variables. |

`expectJson` parses the response, so it is preferred over `expectBody` for JSON
APIs; paths use the same dot/index syntax as captures.

If `service` is set but `grpcMethod` is omitted, the step performs a standard
`grpc.health.v1.Health/Check` for that service. If both are omitted, it checks
overall server health.

Arbitrary method invocation uses **gRPC server reflection** to resolve request
and response schemas, so the target server must have reflection enabled. The
response and request are JSON encoded with protobuf JSON names.

```yaml
      - name: cart
        type: grpc
        service: demo.v1.CartService
        grpcMethod: GetCart
        request:
          id: ${cartId}
        metadata:
          authorization: Bearer ${token}
        expectStatus: [0]
        capture:
          cart: json:$
```

## Captures and interpolation

`capture` maps a variable name to one of:

| Expression | Value |
| --- | --- |
| `json:$` | The whole JSON response body. |
| `json:$.foo.bar` | A JSON field. Array indexes use `[0]` or `.0`. |
| `header:X-Trace` | The first response header value. |
| `status` | HTTP status code or gRPC status code. |
| `body` | The raw response body as a string. |

Capture names must start with a letter or underscore and contain only letters,
digits, and underscores. Captured values are available to every later step as
`${name}`. A captured JSON object also supports nested access such as
`${foo.id}` or `${foo.items[0].name}`.

Placeholders work in `url`, `path`, `headers`, `body`, `patch`, `request`, and
`metadata`. There are also read-only target variables:

- `${target.url}`
- `${target.address}`
- `${target.port}`
- `${target.host}`
- `${target.scheme}`
- `${target.name}`
- `${target.service}`
- `${target.asset}`

Captured values are never persisted in probe results or telemetry; only the
capture names are recorded.

### Modifying a captured response

To take a response, change a field, and send it back, use `body: ${name}`
together with `patch` (RFC 7386 JSON merge patch; `null` deletes a key):

```yaml
      - name: read
        method: GET
        path: /foo/bar
        capture:
          foo: json:$

      - name: update
        method: POST
        path: /foo/bar/${foo.id}
        body: ${foo}
        patch:
          status: modified
          internalField: null
        expectStatus: [200]
```

Journeys run on every probe interval, so prefer idempotent methods (`GET`,
`PUT`) or a dedicated test tenant when a step creates data.

## Safety and limits

- A probe still needs a `target` asset, or explicit `url`/`address` values on
  the probe/steps.
- Explicit non-target URLs and per-step `url`/`address` overrides are treated
  as external targets and require partition `defaults.probes.allowExternal:
  true`.
- Journeys are limited to 32 steps, and each attempt has a bounded budget
  derived from the per-step timeouts (capped at two minutes).
- Journeys default to `retries: 0` because a retry reruns the whole journey
  from step 1. Set `retries` explicitly only when every step is idempotent.
- Body and response capture is limited to 1 MiB per step.

## Results

Journey results add `steps` and `failedStep` to the normal probe result:

```json
{
  "status": "down",
  "failedStep": "update",
  "steps": [
    {"name": "read", "type": "http", "method": "GET", "status": "up", "success": true, "durationMs": 4},
    {"name": "update", "type": "http", "method": "PUT", "status": "down", "success": false, "statusCode": 500, "error": "unexpected HTTP status 500", "durationMs": 3}
  ]
}
```

Aggregate `guardian.probe.*` metrics, logs, and spans keep the same target
identity as single probes. Journey logs and spans add
`guardian.probe.failed_step` and `guardian.probe.step_count`.
`guardianctl probe show --id <id>` and the read API return the step details as
JSON.
