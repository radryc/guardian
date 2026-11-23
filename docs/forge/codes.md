# Forge diagnostic codes

Codes are stable. Messages may improve; the code is the contract. Every
diagnostic also carries `severity`, `partition`, `file`, `path`, and an optional
`hint`. JSON output is canonical; a UI may add a cosmetic `displayLabel`.

This page lists only codes **currently emitted** by the Forge code. Reserved
ranges are intentionally broad.

| Range | Domain |
| --- | --- |
| `FGE0xxx` | Generic/informational |
| `FGE1xxx` | Schema and syntax |
| `FGE2xxx` | Partition and intent semantics |
| `FGE3xxx` | Cross-reference, graph, and scope |
| `FGE4xxx` | Policy and guardrails |
| `FGE5xxx` | Alerts |
| `FGE6xxx` | Security and secrets |
| `FGE7xxx` | Environment, platform, and pipeline |
| `FGE8xxx` | Display keys and profiles |

## Emitted codes

| Code | Severity | Meaning | Source |
| --- | --- | --- | --- |
| `FGE0000` | info | Generic diagnostic with no more specific code | `internal/forge/diagnostic` |
| `FGE1000` | error | Generic bundle load failure | `internal/forge/validate/validate.go` |
| `FGE1001` | error | YAML parse failure, annotated with the source line | `internal/forge/validate/validate.go` |
| `FGE2001` | error | Invalid partition/intent/asset name (must match `^[a-z][a-z0-9-]*$`) | `internal/forge/validate/validate.go` |
| `FGE2002` | error | Missing or invalid `target` placement | `internal/forge/validate/validate.go` |
| `FGE2005` | error | Duplicate `metadata.name` across intent files | `internal/forge/validate/validate.go` |
| `FGE3003` | error | Asset payload references a missing file | `internal/forge/validate/validate.go` |
| `FGE3004` | error | Asset type is unknown to the registry | `internal/forge/validate/validate.go` |
| `FGE3010` | warning | Intent contains unresolved `${intent...}` output references | `internal/forge/validate/validate.go` |
| `FGE4000` | error | Partition could not be loaded for policy evaluation | `internal/forge/policy/guardrails.go` |
| `FGE4001` | error | Guardrail policy violation | `internal/forge/policy/guardrails.go` |
| `FGE5001` | error | Alert file does not parse | `internal/forge/validate/validate.go` |
| `FGE5002` | error | Alert group `partition` does not match the directory | `internal/forge/validate/validate.go` |
| `FGE5003` | error | Alert group/rule is missing a name or expression | `internal/forge/validate/validate.go` |
| `FGE5004` | warning | Alert rule has no `annotations.summary` | `internal/forge/validate/validate.go` |
| `FGE5005` | warning | Alert file version is not `1` | `internal/forge/validate/validate.go` |
| `FGE6001` | error | Inline secret value where a `secret_ref` is required | `internal/forge/policy/guardrails.go` |
| `FGE7001` | warning | Known asset type outside the Forge v1 editing surface | `internal/forge/validate/validate.go` |
| `FGE7002` | error | Workspace discovery failed (root is not a repo/partition directory) | `internal/forge/validate/validate.go` |
| `FGE8003` | error | Invalid display key (regex, reserved word, duplicate, empty) | `internal/forge/display/display.go` |

## Reserved / planned codes

These codes appear in older drafts but are not emitted by the current code.
Do not rely on them until they appear in the table above:

`FGE5010` (display key in PromQL), `FGE7000` (no partitions discovered),
`FGE7010` (base moved), `FGE8001` (unknown canonical key), `FGE8002` (alias to
an unassigned partition), `FGE8004` (display key leaked into a manifest),
`FGE8005` (display profile unavailable), `FGE8006` (invalid per-surface key).

## Interpreting results

- `forge validate` returns exit code `1` when any `error` is present, or when
  `--strict` promotes a warning.
- `forge policy check` returns `1` on any `FGE4xxx`/`FGE6xxx` error.
- `forge display lint` returns `1` on any `FGE8xxx` error.
- `forge doctor` returns `1` on a failed check, or on a warning with `--strict`.
