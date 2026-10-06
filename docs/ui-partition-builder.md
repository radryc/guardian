# UI Design: Visual Partition Builder

Status: draft for review. Scope: a drag-and-drop canvas in the Guardian web UI for
creating and editing a partition, its intents, and their assets.

## 1. Goal

Today the UI can only *look* at partitions. Creating one means hand-writing
partition/intent YAML and running `guardianctl partition push`; the "New"
button in the sidebar just shows a toast telling you to do that
(`internal/ui/static-src/main.ts:2475`). The catalog already describes every
asset type, its fields, and its typed references
(`internal/domain/assets/catalog.go`), but nothing consumes the
`asset-ref` / `asset-refs` controls (`compute.networks`,
`loadBalancer.targets`, `traefikRoute.target`, `*.volume`, `*.config`, …).

The builder should make this possible without reading a single manifest:

> drag an asset from the palette onto the canvas, drag a line from one asset to
> another to connect them, fix whatever the Problems panel complains about,
> press Save.

Three direct-manipulation verbs, everything else in one inspector panel:

1. **Drag in** — palette → canvas creates an asset.
2. **Drag around** — node → move; frame → reassign intent.
3. **Drag together** — connector socket → node creates a typed connection.

YAML stays available behind a "Raw" toggle for power users, but is never the
primary surface.

## 2. Mental model

The canvas edits one partition at a time. The hierarchy is real, not cosmetic:

```
Partition            deletion policy, reconciliation, defaults
 └── Intent          placement boundary = targetPusher + target{cluster,namespace,region,account},
                     type (standard|observed), locked, hints
      └── Asset      type + properties + payload + dependsOn + skip
```

Consequences that shape the UX:

- **Assets belong to exactly one intent.** References between assets are
  resolved *within* an intent only (`ValidationContext.AssetTypes` is built
  per-intent in `internal/compiler/validator/assets.go:97`).
- **Cross-intent data flow is a different mechanism**: property values may
  contain `${intent.<x>.outputs.<y>}` placeholders, resolved by the compiler
  (`internal/compiler/resolver/resolver.go:10`). Builder shows these as dotted
  edges but never creates them via plain asset-to-asset drag.
- **Asset order within an intent is `dependsOn`, not references.** The planner
  topologically sorts on `DependsOn` only (`internal/compiler/planner/planner.go:121`).
  A Compute mounting a Volume without `dependsOn` may be applied first. The
  builder must manage this for the user (see §6.5).
- **Intents can depend on each other** via `spec.joins` (DAG). Rendered as
  frame-to-frame "join" edges.

## 3. The canvas

New top-level tab **Build** next to Topology, plus entry points:

- Sidebar **New** button → opens builder in "new partition" mode (§10).
- Partition hero in Overview → **Edit** (greyed out with a tooltip if the
  partition is read-only; locked intents render locked but editable with a
  banner).

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Build  my-partition            [Auto-layout] [Validate ✓] [?]   [Save ▾]  │
├──────────────┬───────────────────────────────────────────────┬─────────────┤
│  PALETTE     │  CANVAS                                       │  INSPECTOR  │
│  [search…]   │ ┌─ intent: api ── pusher:k8s-prod ─────────┐  │  api-api    │
│              │ │                                           │  │  Compute    │
│  Compute     │ │   ┌────────┐   ┌─────────────┐            │  │             │
│   🧠 Compute │ │   │ volume │   │ 🧠 api-api  │            │  │  Image      │
│   📦 K8s res │ │   │ 💾data │◄──┤ ports: http │            │  │  [ghcr…]    │
│  Storage     │ │   └────────┘   │  ● net      │            │  │             │
│   💾 Volume  │ │                │  ● volume   │            │  │  Ports   +  │
│   🗄️ Database│ │   ┌────────┐   └─────────────┘            │  │  http:8080  │
│  Network     │ │   │ 🔗 net │─────────● mount             │  │             │
│   🔗 Network │ │   └────────┘   ┌─────────────┐            │  │  Networks + │
│   🌐 LB edge │ │                │ 🌐 api-lb   │            │  │  [net]      │
│  Config      │ │                │  ● target   │            │  │             │
│   🧾 Config  │ │                └─────────────┘            │  │  Advanced ▸ │
│              │ └───────────────────────────────────────────┘  │             │
│  [+ blueprints]                                              │  [Raw YAML] │
├──────────────┴───────────────────────────────────────────────┴─────────────┤
│ Problems (2)  ⚠ api-lb: needs at least one target   ✕ api-api: image empty │
└────────────────────────────────────────────────────────────────────────────┘
```

- **Palette** (left, collapsible): catalog types grouped by `Category`, with
  search. Drag a chip onto the canvas to create an asset with catalog template
  defaults; click also works (click-to-add at viewport center). Types
  unavailable under the target pusher are shown dimmed with a reason tooltip
  (phase 4).
- **Canvas** (center): SVG like the existing topology view; reuses its
  card/edge/zoom/pan code (`internal/ui/static-src/topology.ts`). Intent
  frames are large rounded rects behind the nodes.
- **Inspector** (right): catalog-driven form for the selected node/edge/frame.
  Starts on "Essentials" only (required + common fields), with an **Advanced**
  disclosure for the rest and a **Raw YAML** tab for that asset.
- **Problems strip** (bottom): live validation results, clickable (§8).

Draft state is local — the builder never writes to the store until Save.

## 4. Interaction 1 — drag an asset in

- Drag from palette; canvas shows intent frames highlighting as drop targets.
  Dropping inside a frame adds to that intent. Dropping on empty canvas:
  - partition has no intents → create an intent named after the asset
    (`api-api`), pre-filled with the partition's default target pusher, and a
    gentle nudge on the frame header: "Set target pusher".
  - partition has intents → small popover: "Add to…" list of intents + "New
    intent…". The last choice is remembered for the session; holding `Alt`
    repeats the last intent without asking.
- Asset gets an auto-generated unique name (slug of catalog title, suffixed
  `-2`, `-3`), matching `^[a-z][a-z0-9-]*$` (`validator.go:15`). Name is
  editable inline on the card and in the inspector; **renaming also rewrites**
  every intra-intent reference and every hint path `assets.<name>.<field>`
  atomically.
- Creation seeds `properties` from `CatalogTemplate.Template` and shows a
  transient "what next?" hint on the new card, e.g. Compute → "Connect a
  Network, then set the image".

## 5. Interaction 2 — move and group

- **Move**: drag a node; edges redraw live (existing pointer-event code in
  `topology.ts:257-312`). Positions are draft-only, saved in localStorage, with
  **Auto-layout** (layered left→right by references+dependencies) available.
- **Intent frames**: header shows editable name, target pusher picker, and
  placement fields (cluster / namespace / region / account, driven by pusher
  kind). Frame body auto-sizes to contents. Frame footer shows a count
  (`4 assets · 1 warning`).
- **Reassign intent**: drag a node from one frame to another. If that would
  break references (the asset is a provider/consumer of assets staying behind),
  block the drop and offer a dialog: "Bring the 2 referenced assets too",
  "Duplicate into that intent", or "Cancel".
- **Join intents**: drag from the socket at the right edge of a frame header to
  another frame → dashed arrow, writes `spec.joins`. Cycles are rejected with a
  red edge and toast (`joinGraph.TopologicalSort` must succeed,
  `planner.go:157`).
- **Multi-select**: rubber-band select; group move; group delete.
- **Locked intents**: canvas draws a lock badge; edits inside open a confirm
  once per session ("This intent is marked locked. Edit anyway?").
- **Observed intents** (`spec.intentType: observed`): AWS-only and report-only
  (`internal/domain/intent/intent.go:13`). Editable but always badged
  "Observed — never applied".

## 6. Interaction 3 — connect assets (the core)

### 6.1 Connectors are typed sockets, derived from the domain

Every property that the validator checks with `validateAssetRef`
(`internal/domain/assets/helpers.go:77`) becomes a socket on the *consumer*
card. Hovering a card reveals its sockets on the right edge, labelled with
intent, e.g. Compute: `network`, `volume`, `config`; LoadBalancer: `target`,
`config`, `network`; Traefik route: `target`.

The full matrix (provider → consumer, and the property written) is fixed by the
domain today:

| Provider type | Consumer type | Property written | Extra questions (smart-fill) |
|---|---|---|---|
| Network | Compute, Database, LoadBalancer, ObjectStore, Observability | `networks += <name>` | — |
| Volume | Compute | `volumeMounts += {volume, path, readOnly}` | mount path (default `/mnt/<name>`), read-only |
| Volume | Database, ObjectStore, Observability | `volume = <name>` (single) | replaces existing |
| Config | Compute | `configMounts += {config, path, readOnly}` | mount path (default `/etc/<name>`), read-only |
| Config | Database, LoadBalancer, ObjectStore, Observability | `config = <name>` (single) | — |
| Compute | LoadBalancer | `targets += <name>` + optional listener | which named port to route; auto-add listener `{name, port, protocol}` if missing |
| Compute | Traefik route | `target = <name>`, `portName = <port>` | port name (required when >1); suggest hostname `<compute>.strata` |

`Volume`, `Config`, `Network`, `Secret`, `ImageBuild`, `CDKStack`,
`K8sResource` are providers only (no consumer sockets). Compute/DB/etc. are
consumers only. No type can be both in a single field, so connection direction
is never ambiguous.

### 6.2 The drag

- Start on a socket (or anywhere on a card, which reveals sockets on hover) and
  drag. While dragging:
  - **valid targets** pulse with an accent glow and the cursor is a "+";
  - **invalid cards** dim, cursor `not-allowed`, and a small tooltip explains
    the rule: "A Volume can't mount a Compute";
  - a live bezier snaps to the nearest socket.
- Drop on a card body or directly on its socket. Releasing on empty canvas
  opens a fuzzy palette: "Create a **Network** and connect" (only types valid
  for this socket).
- **Drop-on-socket works too**: dragging a provider card shows a socket ring on
  every compatible consumer, so both mental directions work.
- **Click-to-connect** twin for accessibility: click a socket → compatible
  cards highlight → click one. `Esc` cancels.

### 6.3 Smart-fill popovers

A connection is not always just a name. On drop, show a small popover anchored
to the new edge with *only* the missing answers, each defaulted:

- Compute←Volume: `path: /mnt/data`, `readOnly: false` → **[Connect]**.
- LoadBalancer←Compute: port dropdown (`http:8080`, …). On confirm, if no
  listener targets that port, append one.
- Traefik←Compute: port dropdown + suggested `hostname: api-api.strata`.
- Everything else: no popover; connect instantly with an undo toast
  ("Connected data → api-api · Undo").

Every generated value is a normal editable field in the inspector; the popover
never hides what it wrote. "Advanced" in the popover jumps to the inspector at
that property.

### 6.4 Edge semantics

- Solid edge = property reference (within intent). Deleting the edge removes
  the property entry (or clears the single field). Deleting a consumer removes
  its inbound edges with it.
- Deleting a provider that still has consumers is blocked with a dialog:
  "Volume data is used by api-api (mount /mnt/data). Delete anyway and
  unlink?" — cascade unlink, never silently leave dangling refs.
- Duplicate connections are prevented (same provider already in `networks`):
  the existing edge flashes instead of adding a second entry.
- An asset can connect to many providers and vice versa, matching the
  `asset-refs` (many) vs `asset-ref` (single) distinction in the catalog.

### 6.5 Apply order (dependsOn) is derived, then editable

Because references do not imply ordering, every new edge also adds the provider
to the consumer's `dependsOn` by default ("wait for `data` before applying
`api-api`"). The edge popover has a toggle **"Apply after <provider>"**, on by
default; turning it off marks the edge "parallel" (dashed). On save,
`dependsOn` is written as: user-authored deps (preserved) ∪ auto deps of
enabled edges. An auto-managed dep is removed only when its last referencing
edge is removed. The Save review diff calls out every `dependsOn` change so
nothing changes invisibly, and cycles are impossible because the dependency
graph follows the reference graph which the UI keeps acyclic per intent.

### 6.6 Cross-intent wiring (phase 3)

Dragging between frames does **not** create a reference. Instead:

- Dropping a card near a frame edge offers "**Use an output from `other-intent`**".
  If the field being edited supports text interpolation (e.g. Compute `env`
  values), the inspector opens with an output picker
  (`<intent> → <output key>`) that inserts `${intent.x.outputs.y}`.
- Existing placeholders in property values render as dotted, read-only
  "outputRef" edges between the intent frame and the consumer card, matching
  what `buildTopology` already derives (`internal/ui/data.go:1266`).
- Intent output *declarations* (`spec.hints` with `outputs.<key>`) are edited in
  the frame inspector as a simple key/description list.

## 7. Inspector

One panel, three modes, all catalog-driven:

- **Asset**: type badge + icon; name; **Essentials** (fields without which the
  asset is invalid), then **Advanced**. Controls map from `CatalogField.Control`:
  `text`, `number`, `boolean` (toggle), `select` (dropdown), `list` (chip
  editor), `textarea`, `json` (structured key/value grid with a raw-JSON
  toggle), `asset-ref` / `asset-refs` (connection chips; this is where the
  same connections can be created/removed without the canvas).
  Field `Description` becomes inline help; `Placeholder` the input
  placeholder; `Hints` become tooltips, including intent-scoped and
  asset-scoped hints (`ResolveAssetHints`).
  `payload` (provider → logical path) gets a path input with a browse hint.
- **Intent**: name, target pusher (dropdown of `GET /api/pushers`), placement,
  type, locked, joins (chips), output declarations, hints.
- **Partition**: labels, deletion policy, reconciliation mode/interval/jitter,
  defaults (target pusher/placement).

A header strip shows the draft's dirty state and the **last save** timestamp +
version IDs.

## 8. Validation and Problems

Two layers:

- **Instant (client)**: name rules, required fields, duplicate names, dangling
  refs, socket type compatibility, join/dependency cycles. Errors paint the
  field/card/edge immediately.
- **Authoritative (server)**: debounced `POST /api/partitions/{name}/validate`
  (§13) runs the real compiler validator and returns structured issues:
  `{severity, intent, asset, field, message}`. This catches subtleties the
  client mirror will inevitably drift on.

Problems strip at the bottom: grouped by intent, click → select + scroll +
pulse the offending card/field; "Fix" buttons for mechanically fixable issues
(e.g. a LoadBalancer with no target jumps into connect mode; an empty image
focuses the field). Save is enabled with warnings, disabled with errors
("Save anyway" remains available behind a confirm while the partition is new).

## 9. Drafts, review, save

- **Draft**: the builder model autosaves to
  `localStorage["guardian.builder.v1.<partition>"]` every change. Sidebar shows
  a dot; closing/reloading restores the draft and offers Discard.
- **Undo/redo**: full command stack (`Cmd/Ctrl+Z`, `Shift+Cmd/Ctrl+Z`) covering
  node/edge/field edits. Structural edits (delete asset, move intent) are single
  undo steps.
- **Review & Save** (`Cmd/Ctrl+S`): a modal diff, per intent —
  `+ asset`, `- asset`, `~ field` — plus partition changes, `dependsOn` deltas,
  and the validation summary. Confirm → `PUT /api/partitions/{name}/bundle`
  with the full partition manifest, every changed intent manifest, and the
  `expectedVersionID`s loaded at open time (`SaveBundleRequest`,
  `internal/ui/data.go:346`). Unchanged intents are sent untouched so their
  version IDs are preserved; `removeMissingIntents` carries deletions.
- **New partition**: Save creates it directly (no `guardianctl` step needed);
  name is collected in a small first-run card, default reconciliation `auto`.
- **Version conflict**: if the server rejects an expected-version mismatch,
  show "This partition changed while you were editing": **Reload theirs**
  (discard draft), **Keep mine** (reload, reapply my structural changes onto
  their manifests, re-validate — field-level 3-way merge where cheap, prompt
  per conflicting asset otherwise), or **Save as copy** (new name).
- After save, optionally offer **Reconcile now** (existing
  `POST …/reconcile`) instead of waiting for the loop.

## 10. Empty state and blueprints

First open of a new partition shows blueprint cards rather than a blank canvas:

- **Empty** — just the partition settings card.
- **Local web app** — Network + Compute (`http:8080`) + LoadBalancer +
  optional Volume, joined by connections, one intent `api`, target pusher
  preselected if only one is registered.
- **Service + database** — Network + Compute + SQLDatabase + Volume + Config,
  with `env` wiring hinted.
- **Image build + deploy** — ImageBuild + Compute + LoadBalancer.
- **AWS CDK stack** — a single CDKStack asset with `payload.aws` path field.
- **Observed AWS partition** — observed intent, no apply; locks the frame type.

Blueprints are plain functions creating nodes, edges, and pre-set positions; no
server support needed.

## 11. Keyboard and accessibility

- `Tab`/`Shift+Tab`: cycle nodes; `Enter`: inspector; `Space`: select.
- `c`: connect mode for the selected node (cycle valid partners, `Enter`
  confirms).
- `n`: focus palette search; `/`: command palette (add asset, jump to node,
  auto-layout).
- `Delete`/`Backspace`: remove selection (with the unlink dialog if needed).
- `Cmd/Ctrl+Z` / `Shift+Cmd/Ctrl+Z`: undo/redo. `Cmd/Ctrl+S`: review & save.
  `Esc`: cancel drag/connect/popover.
- Sockets have `aria-label`s ("Connect api-api to a network"); every canvas
  action is available via inspector chips, so the builder is fully usable
  without pointer drags.

## 12. Visual language

Reuse the existing dark theme and topology cards to make Build feel native:
category accent colors from the catalog icon/category, status dots hidden
(draft has no health yet; show a **stale** badge if the saved partition changed
underneath), sockets as small circles on card edges that grow on hover,
compatibility highlighting via the existing accent palette. Sensible micro-motion:
edges animate in, invalid drops shake once. No new color system.

## 13. Technical plan

### Client (`internal/ui/static-src/`)

New `builder/` module, no framework (matches the codebase's vanilla TS +
template-literal DOM style):

- `builder/model.ts` — draft model (partition + intents + assets with
  positions), mutations, undo/redo command stack, localStorage persistence,
  manifest ⇄ model mapping, `dependsOn` derivation.
- `builder/canvas.ts` — SVG render, pan/zoom, node drag, frames, edge routing;
  reuses/extends `topology.ts` geometry (`computePositions`,
  `edgePath`, `updateEdgePaths` — export these).
- `builder/connections.ts` — compatibility matrix, socket resolution,
  smart-fill popovers, dependency auto-management.
- `builder/inspector.ts` — catalog-driven form renderer (all `Control` kinds).
- `builder/palette.ts`, `builder/blueprints.ts`, `builder/diff.ts`,
  `builder/validate.ts`.
- Wire-up: new `builderPanel` in both `index.html` files, new `state.builder`
  slice in `state.ts`, new nav item, "New partition" handler replacement in
  `main.ts:2475`.

### Server (`internal/ui/`)

1. **Catalog connectors** — extend `CatalogTemplate` with an optional
   `connectors` array so the UI and the validator share one source of truth:
   `{key, label, control, refTypes, many, propertyPath, defaultPath}`.
   Initially this can be a small static table in the `assets` package mirroring
   the `validateAssetRef` calls; longer term, fold it into the asset
   definitions. This removes the need for a hard-coded matrix in the client.
2. **Validate endpoint** — `POST /api/partitions/{name}/validate`, body like
   `SaveBundleRequest`; runs the compiler validator and returns
   `{errors:[…], warnings:[…]}` with `intent`/`asset`/`field`/`message`.
   Also return structured errors from `saveBundle` so a rejected Save can point
   at fields instead of a single string.
3. **No new persistence.** Drafts live in the browser; saved manifests stay the
   source of truth. `GET /api/partitions/{name}` already returns each intent's
   full `manifest` + `manifestVersionID` (`internal/ui/data.go:94-130`), which
   is exactly the builder's seed.
4. **Pusher capabilities** (phase 4) — expose supported asset types per
   pusher in `GET /api/pushers` to grey out impossible drops.

## 14. Rules and edge cases

- Asset names unique per intent (not per partition); references are by name.
- Renaming is atomic across refs, hints, joins unaffected.
- Deleting an asset cascades unlink (with confirmation listing consumers).
- Moving an asset across intents blocks when refs would cross the boundary;
  offer bring-along/duplicate.
- Duplicating an asset copies properties and positions, strips inbound refs
  (or rewires if the provider is duplicated too — ask).
- A partition cannot save with zero intents or an intent with no assets; the
  builder blocks Save and explains.
- `observed` intents cannot target non-AWS pushers (validator rule); the
  picker filters.
- Provider assets with no consumer are fine (Network, Volume alone); warn only
  when they exist solely to satisfy a removed consumer.
- The partition can change server-side while the builder is open; poll
  `GET /api/partitions/{name}` every 20s (existing refresh cadence) and raise
  the conflict badge in §9 rather than yanking the draft.

## 15. Phased rollout

1. **MVP**: Build tab for an existing partition — palette, drop, inspector,
   chips for refs, client validation, review diff, save via `PUT bundle`.
2. **Connections**: sockets + drag-to-connect + smart-fill + `dependsOn`
   auto-management + Problems strip with server validation.
3. **Frames & polish**: intent create/reassign/joins, auto-layout, undo/redo,
   blueprints, keyboard/accessibility, cross-intent output wiring.
4. **Capabilities & extras**: pusher-aware palette, "Save as copy", duplicate
   assets, raw YAML round-trip safety, template/blueprint registry.

## 16. Open questions

- Do we want a "new asset created and unreferenced" warning, or silence until
  Save? (Current design: a one-line hint on the card, no Problem entry.)
- Should auto-derived `dependsOn` be written into manifests, or kept as UI
  ordering metadata? Current proposal writes it (with review diff) because the
  planner only orders on it.
- Multi-user editing: polling + conflict dialog is the v1 assumption; a
  `batchRevisionID`-based optimistic lock may be enough.
- Do pushers ever support a strict subset of asset types worth surfacing now?
