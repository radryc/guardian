# Forge diagnostic codes

Codes are stable. Messages may improve; the code is the contract. Every
diagnostic also carries `severity`, `partition`, `file`, `path`, and an optional
`hint`. JSON output is always canonical; a UI may add a cosmetic
`displayLabel`.

| Range | Domain |
| --- | --- |
| `FGE1xxx` | Schema and syntax |
| `FGE2xxx` | Partition and intent semantics |
| `FGE3xxx` | Cross-reference, graph, and scope |
| `FGE4xxx` | Policy and guardrails |
| `FGE5xxx` | Alerts |
| `FGE6xxx` | Security and secrets |
| `FGE7xxx` | Environment, platform, and pipeline |
| `FGE8xxx` | Display keys and profiles |

## Codes

| Code | Severity | Meaning |
| --- | --- | --- |
| `FGE1000` | error | Generic bundle load failure |
| `FGE1001` | error | YAML parse failure, annotated with the source line |
| `FGE2001` | error | Invalid partition/intent/asset name |
| `FGE2002` | error | Missing or invalid target placement |
| `FGE2005` | error | Duplicate `metadata.name` across intent files |
| `FGE3003` | error | Asset payload references a missing file |
| `FGE3004` | error | Asset type is unknown to the registry |
| `FGE3010` | warning | Unresolved `${intent...}` output reference |
| `FGE4000` | error | Partition could not be loaded for policy evaluation |
| `FGE4001` | error | Guardrail policy violation |
| `FGE5001` | error | Alert file does not parse |
| `FGE5002` | error | Alert group `partition` does not match the directory |
| `FGE5003` | error | Alert group/rule is missing a name or expression |
| `FGE5004` | warning | Alert rule has no `annotations.summary` |
| `FGE5005` | warning | Alert file version is not `1` |
| `FGE5010` | error | Display key used inside a PromQL expression |
| `FGE6001` | error | Inline secret value where a `secret_ref` is required |
| `FGE7000` | error | No partition directories discovered |
| `FGE7001` | warning | Known asset type outside the Forge v1 editing surface |
| `FGE7010` | error | Base moved; rebase before submitting |
| `FGE8001` | error | Unknown canonical key in a display binding |
| `FGE8002` | error | Alias targets a partition not assigned to the repo |
| `FGE8003` | error | Invalid display key (regex, reserved word, duplicate, empty) |
| `FGE8004` | error | Display key leaked into a manifest, path, ref, or expression |
| `FGE8005` | warning | Referenced display profile is unavailable |
| `FGE8006` | error | Per-surface key invalid for that surface |

## Interpreting results

- `forge validate` returns exit code `1` when any `error` is present, or when
  `--strict` promotes a warning.
- `forge policy check` returns `1` on any `FGE4xxx`/`FGE6xxx` error.
- `forge display lint` returns `1` on any `FGE8xxx` error.
- `forge doctor` returns `1` on a failed check, or on a warning with `--strict`.
