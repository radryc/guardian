# AWS account scan and import

Guardian can discover existing AWS resources and turn them into partition
intents through the UI. The scan runs on `guardian-pusher-aws`, so the daemon
never needs AWS credentials: Guardian's AWS existence stays confined to the
pusher, exactly like deployments.

The design follows `guardian-docker generate` (live snapshot → asset specs) but
is UI-triggered and async, like the rest of the Guardian control plane.

## How it works

```
UI (guardiand)                       guardian-pusher-aws                AWS
     |                                        |                        |
     | POST /api/scans                        |                        |
     |--> /.scans/<pusher>/requests/<id>.json |                        |
     |                                        | claim (.claims/)       |
     |                                        |--> STS + service APIs ->|
     |                                        |<-- resources + tags ----|
     |<-- /.scans/<pusher>/.results/<id>.json |                        |
     |                                        |                        |
     | GET /api/scans (poll)                  |                        |
     | GET /api/scans/{id}/bundle?partition=  |                        |
     | PUT /api/partitions/{name}/bundle      |                        |
     | POST /api/partitions/{name}/reconcile  |                        |
```

Storage layout:

- `/.scans/<pusher>/requests/<scanID>.json`: `AWSScanRequest` written by the UI.
- `/.scans/<pusher>/.claims/<scanID>.json`: optimistic claim (2h lease).
- `/.scans/<pusher>/.results/<scanID>.json`: `AWSScanResult` written by the pusher.

The scan request/result loop intentionally mirrors the task queue semantics
(claim file with `ExpectedVersionID: absent`, lease expiry, result file as the
completion marker) but is fully separate from `/.queues/`: scans never touch the
intent state machine.

## Authentication

The scanner uses the pusher's existing credentials path:

- default AWS SDK chain first
- `--assume-role-name` (default `GuardianCdkDeployRole`) assumed into
  `arn:aws:iam::<account>:role/<name>` when set

See `docs/aws-pusher.md` for the full setup. The scan verifies the resolved
identity before listing anything and fails fast if an assumed role lands in the
wrong account.

## What the scan collects

Two levels:

1. **Mappable resources** via direct service APIs (rich data, needed for
   intent generation):

   | AWS service | Guardian asset type |
   |---|---|
   | S3 buckets | `ObjectStore` |
   | EFS filesystems | `Volume` |
   | SSM parameters (String) | `Config` |
   | Secrets Manager secrets | `Secret` |
   | ECS services (+ task definitions) | `Compute` |
   | ELBv2 load balancers (+ listeners, target groups) | `LoadBalancer` |

   CloudFormation stacks are also listed to detect Guardian-managed stacks
   (via `guardian-*` tags) and to group resources by stack.

2. **Inventory** (optional, `Full inventory` toggle) via the Cloud Control API
   (`ListResources`) and the CloudFormation registry (`ListTypes`), filtered by
   include/exclude patterns like `AWS::EC2::*`. This is the
   `aws-list-resources` style account-wide listing. Types covered by the direct
   APIs and well-known AWS-owned defaults (for example `alias/aws/*` KMS
   aliases and `AWS-*` SSM documents) are filtered out. Inventory is read-only
   and never becomes assets in v1.

Regions: `all` (enabled regions via `ec2:DescribeRegions`) or an explicit list.
A pusher pinned with `--region` only scans that region.

## Import semantics (adopt in-sync)

Generated assets use explicit-name adoption fields so the first reconcile
reports **InSync without mutating AWS**:

| Asset type | Adoption field | Diff while unmanaged |
|---|---|---|
| `ObjectStore` | `existingBucket` | versioning matches desired |
| `Volume` | `existingID` | filesystem exists |
| `Config` | `existingParameter` | parameter value matches content |
| `Secret` | `existingSecret` | secret exists (value is never read into the store) |
| `LoadBalancer` | `existingName` | type/scheme match |
| `Compute` | `observeExisting` + `existingServiceName` + `cluster` | service exists, desired count matches |

When you later edit a generated intent, DIFF reports drift and APPLY converges
the live resource by its explicit name and adds `guardian-*` tags — from that
point on the resource is hash-compared like any Guardian-managed resource.

Guard rails in v1:

- Adopted secrets refuse APPLY until you supply `value` or `secretRef`.
- Adopted load balancers are diff-only (APPLY is refused).
- Destroy is a no-op for adopted `Volume`, `Secret`, and `LoadBalancer` assets;
  `deletionPolicy: orphan` is the default for generated partitions.

## UI flow

1. **Scan & Import** panel: pick an AWS pusher, regions, optional type filters
   and inventory, then start the scan.
2. The list polls until the pusher writes the result.
3. Inspect the result: mappable resources, already-managed resources, foreign
   CloudFormation stacks, inventory, and scan errors.
4. **Build partition bundle**: choose a target partition name and grouping
   (by CloudFormation stack or one-intent-per-resource), generate the draft,
   review the proposed intents, and deselect anything you do not want.
5. Save the bundle (reuses `PUT /api/partitions/{name}/bundle`) and optionally
   trigger a reconcile. Intents reconcile in-sync and go Healthy without
   touching AWS.

Regenerating a bundle from the same scan cross-references the target
partition's existing intents and skips resources that are already imported.

## API

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/scans` | POST | queue a scan (pusher must be configured) |
| `/api/scans` | GET | list scans with status and summary |
| `/api/scans/{id}` | GET | full scan result |
| `/api/scans/{id}/bundle?partition=&grouping=&types=&regions=` | GET | generated bundle draft + `SaveBundleRequest` payload |

Scan requests can also be written directly to the store by automation; the
pusher picks up any valid `AWSScanRequest` file in its `requests/` directory.

### Example: scan account `123456`

The pusher must be scoped to the account (`--account 123456`, which defaults the
pusher name to `aws-123456`), and the request's `account` must match it. Write
the request file to `/.scans/aws-123456/requests/<scanID>.json`:

```json
{
  "apiVersion": "guardian/v1alpha1",
  "kind": "AWSScanRequest",
  "scanID": "scan-123456-001",
  "account": "123456",
  "regions": ["all"],
  "inventory": false,
  "inventoryDetail": "summary",
  "createdAt": "2026-09-15T00:00:00Z",
  "requestedBy": "automation"
}
```

The equivalent UI API call is:

```sh
curl -sX POST http://guardian/api/scans \
  -H 'Content-Type: application/json' \
  -d '{"pusher":"aws-123456","account":"123456","regions":["all"]}'
```

Poll `GET /api/scans` (or `GET /api/scans/scan-123456-001`) until the result
appears, then build a bundle with
`GET /api/scans/scan-123456-001/bundle?partition=<name>`.

## Required permissions

The scanner is read-only but broad. On top of the deploy role's existing
permissions the scan needs list/describe/read for the services above, plus
`cloudcontrol:ListResources`, `cloudformation:ListTypes`, and
`ec2:DescribeRegions` when inventory or `all` regions are used. Access-denied
per resource type is recorded as a scan error and does not abort the scan.

## Code map

- `internal/awsscan/types.go`: `ScanRequest` / `ScanResult` schema
- `internal/awsscan/scanner.go`: the scan engine (STS identity check, direct
  service scans, Cloud Control inventory, managed/tag extraction)
- `internal/awsscan/queue.go`: `ScanRunner` claim/execute loop on the pusher
- `cmd/guardian-pusher-aws/main.go`: scan runner wiring
- `internal/awsgen/generate.go`: scan result → partition/intent/asset drafts
- `internal/ui/scan.go`: scan API endpoints
- `internal/ui/static-src/scan.ts`: Scan & Import panel
- `internal/pusher/drivers/aws/*`: adoption-aware Diff/Apply/Destroy
- `internal/domain/assets/*`: adoption spec fields
