# Guardian Documentation

This documentation is derived from the source code and is updated with the
code. When a document disagrees with the implementation, fix the document.

## Documents

| Document | Contents |
|---|---|
| [Architecture](architecture.md) | System context, components, domain model, store namespace, control plane, compiler, pushers, AWS and GCP subsystems, UI, packaging |
| [Logical flows](logical-flow.md) | End-to-end flows: push, repo import, reconcile, compile, task lifecycle, state machine, deletion, rollback, observed, scan, onboarding, probes, access policy, alerting, image release, auth |
| [Security architecture](security-architecture.md) | Trust boundaries, identity/authn, authorization matrix, secrets, transport, threat model, operator checklist |
| [Codebase map](codebase-map.md) | One-line description of every Go/TypeScript file |
| [GCP pusher ADR](design/gcp-pusher.md) | Accepted design for the GCP pusher, OpenTofu backend, identity, and federation |

The code is the source of truth. Line references in these documents are
navigation aids, not stable API contracts.
