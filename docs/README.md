# Guardian Documentation

This documentation is maintained alongside the source and reviewed against it.
When a document disagrees with the implementation, the implementation wins and
the document is corrected.

## Documents

| Document | Contents |
|---|---|
| [Architecture](architecture.md) | System context, components, domain model, store namespace, control plane, compiler, pushers, AWS and GCP subsystems, display profiles, audit, UI, packaging |
| [Logical flows](logical-flow.md) | End-to-end flows: push, repo import, reconcile, compile, task lifecycle, state machine, deletion, rollback, observed, scan, onboarding, probes, access policy, alerting, image release, auth |
| [Security architecture](security-architecture.md) | Trust boundaries, identity/authn, authorization matrix, secrets, transport, audit anchoring, threat model, operator checklist |
| [Codebase map](codebase-map.md) | One-line description of the main Go/TypeScript files (see the coverage note at the top) |
| [Forge](forge/README.md) | Repository workbench commands, offline behavior, and validation |
| [Forge codes](forge/codes.md) | Diagnostics emitted by Forge validation |
| [Forge enforcement](forge/enforcement.md) | Repo-managed enforcement, principals, and break-glass |
| [Forge SSO](forge/sso.md) | Forge UI SSO and trusted-header setup |

The code is the source of truth. Line references and file inventories in these
documents are navigation aids, not stable API contracts.
