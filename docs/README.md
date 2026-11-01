# Guardian Documentation

This documentation was regenerated from the source code in October 2026. The
previous document set was moved to [`../docs_backup/`](../docs_backup/) and is
not maintained.

## Current documents

| Document | Contents |
|---|---|
| [Architecture](architecture.md) | System context, components, domain model, store namespace, control plane, compiler, pushers, AWS subsystem, UI, packaging |
| [Logical flows](logical-flow.md) | End-to-end flows: push, repo import, reconcile, compile, task lifecycle, state machine, deletion, rollback, observed, scan, onboarding, probes, access policy, alerting, image release, auth |
| [Security architecture](security-architecture.md) | Trust boundaries, identity/authn, authorization matrix, secrets, transport, threat model, operator checklist |
| [Codebase map](codebase-map.md) | One-line description of every Go/TypeScript file (423 files) |

## Internal findings

These are intentionally separate from the architecture documents. They track
work that is missing, duplicated, or unnecessarily complex.

| Document | Contents |
|---|---|
| [Missing pieces](internal/missing.md) | HA gaps, authz gaps, secret handling, AWS/Kubernetes capability gaps, GitOps pruning, build/release, testing |
| [Duplication](internal/duplication.md) | Importers, image pipelines, AWS native vs CDK, asset drivers, history models, compiler/task build, entrypoints, caches |
| [Simplification](internal/simplification.md) | God objects, giant files, result pipeline, state model, drift, overlays, config, CLI/bootstrap structure |
| [Changes applied](internal/changes.md) | What was fixed in the P0+P1 pass, with files/tests, and what remains |
| [TODO](../TODO.md) | Prioritized remaining work with acceptance criteria |

## Source of truth

The code is the source of truth. If a document disagrees with the code, fix the
document. Each finding includes file/line references so it can be verified and
closed.
