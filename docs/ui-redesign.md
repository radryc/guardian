# UI redesign — simple by default, deep when needed

Status: draft for review. Scope: the Guardian web UI
(`internal/ui/templates/index.html`, `internal/ui/static-src/*`).

Mockups: `docs/mockups/overview.html`, `application.html`, `sets.html`,
`adopt.html`, `build.html`, `connections.html` (see
`docs/mockups/README.md`).

## 1. Goal

Guardian should feel like one small tool that an on-call SRE can use at 3am,
not a dashboard suite. The UI keeps the existing dark visual language and the
existing data model; this redesign simplifies **what is on screen**, not what
the product can do.

Three rules:

1. **Status first.** Every screen answers: is it healthy, what changed, what do
   I do next?
2. **One job per screen.** If a tab does not answer one of those questions, it
   belongs inside a partition, not in the top-level navigation.
3. **CLI parity.** Every action has a `guardianctl` equivalent. Developers can
   stay in the repo; the UI is the shared view, not a second source of truth.

Non-goals: a new design system, a dashboard builder, a graph editor, or
per-customer theming.

## 2. What the current UI does well (keep it)

- The dark, dense, terminal-adjacent look suits SREs.
- The partition sidebar + detail panel is the right mental model.
- Status colors and origin badges (Native CDK, User CDK, Adopted from AWS,
  Observe only) make the demo scenarios readable.
- The visual partition builder design (`docs/ui-partition-builder.md`) is a
  separate, larger feature; this document does not replace it.

## 3. Current navigation (the problem)

The top-level nav currently has eight destinations plus a partition list:

```
Applications · Topology · Rollouts · History · Scan & Import ·
Unmanaged Stacks · Pushers & Repos · Build
```

Four of those (Topology, Rollouts, History, and arguably Build) only make sense
**inside a selected partition**. Unmanaged Stacks and Scan & Import overlap.
Pushers & Repos combines two different concepts. The result is a nav that looks
like a suite and forces the user to learn the data model before doing a task.

There is also a naming mismatch: the nav says **Applications**, the page title
says **Control Center**, and the data model says **partitions**.

## 4. Proposed information architecture

Four top-level destinations:

```
┌──────────────────────────────┐
│  Overview                    │  what is deployed, what is broken
│  Connections                 │  agents, accounts, repos
│  Adopt                       │  stacks that are not owned in code yet
│  Build                       │  create/edit a partition (existing design)
└──────────────────────────────┘

Selecting an application opens a partition view:

  Summary · Topology · Rollouts · History
```

- **Overview** replaces Applications + the current Control Center overview.
- **Connections** merges Pushers & Repos into one connection view.
- **Adopt** merges Unmanaged Stacks and Scan & Import into one queue.
- **Build** stays as designed in `ui-partition-builder.md`.
- Topology / Rollouts / History become partition-scoped tabs. They no longer
  appear when no partition is selected.

This is a navigation change plus a virtual-set metadata store; every existing
panel and endpoint stays.

### Virtual sets

Partitions are grouped into **named, nested sets** for navigation. A set is
metadata only: it never changes ownership, deployment, or MonoFS paths.

```
Department X — AWS Production
  └── Payments
        demo-deploy
        demo-build
        Refunds          (empty subset)
  └── Platform
```

- One parent per set, arbitrary depth, cycles rejected.
- A partition can appear in multiple sets, with one optional **primary set**.
- Each set has a description and labels (`department`, `cloud`, `account`,
  `region`, `env`, `owner`) so “department X in AWS” is readable.
- The sidebar tree replaces the flat partition list. Counts and health roll up
  through children; children are lazy-loaded.
- Each set row shows a **segmented rollup bar** (healthy · observed · attention)
  beside the count. The set page breaks the rollup down numerically, states
  whether child sets are included, and keeps assignment (assigned/unassigned)
  separate from health so a partition is not counted twice.
- **Ungrouped** is a virtual set for partitions not in any set.
- Set metadata lives in the Guardian store (`/.registry/sets`), not in repos.
- Set pages are paginated; “Add partitions” searches by name/repo/path and
  supports multi-select. This is how thousands of partitions stay navigable.
- Unassigned is a partition state, not a set: a partition can be grouped in a
  set and still have no repository binding.

## 5. Screen designs

### 5.1 Overview — "what needs me?"

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Overview                                            [Search]  [Refresh]  │
│ 2 sets · 5 partitions · 2 healthy · 1 observed · 2 need attention        │
├──────────────────────────────────────────────────────────────────────────┤
│ NEEDS ATTENTION (2)                                                      │
│ ┌──────────────────────────────────────────────────────────────────────┐ │
│ │ ● demo-observe   ObservedDrifted   Department Y · 1 drifted resource │ │
│ │   SSM parameter /strata-demo/observe/value changed outside Guardian  │ │
│ │   [View drift]  [History]                                            │ │
│ ├──────────────────────────────────────────────────────────────────────┤ │
│ │ ● demo-new   Unassigned   Payments · Department X                    │ │
│ │   Desired state has no source of truth until a repo is bound.        │ │
│ │   [Bind repo]  [Open set]                                            │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│                                                                          │
│ SETS                                                    [Status ▾] [View all]
│ ┌───────────────────────────────────┐ ┌───────────────────────────────────┐
│ │ Department X — AWS Production     │ │ Department Y — AWS Development    │
│ │ 4 partitions · 2 subsets          │ │ 1 partition · 0 subsets           │
│ │ production workloads · AWS · prod │ │ development workloads · AWS · dev │
│ │ ██████░░░░  2 healthy · 1 observed│ │ ░░░░░░░░░░  1 attention           │
│ │ 1 attention · 1 unassigned        │ │ demo-observe · Observe only       │
│ │ Payments · Platform        Open → │ │ Open →                            │
│ └───────────────────────────────────┘ └───────────────────────────────────┘
│ ┌───────────────────────────────────┐
│ │ Ungrouped (0)                     │
│ │ no partitions outside a set       │
│ └───────────────────────────────────┘
│                                                                          │
│ RECENTLY CHANGED                                                         │
│ demo-deploy   Payments     Healthy      rollout dep_9a2a  4m ago [open] │
│ demo-build    Payments     Healthy      image :demo       6m ago [open] │
│ demo-import   Platform     Observed     snapshot          1m ago [open] │
│ demo-observe  Department Y ObservedDrift 1 resource       12s ago [open]│
│                                                                          │
│ FIND A PARTITION                                                         │
│ [Search partitions by name, repo, or MonoFS path…]        [Browse all]   │
│ Partition lists are paginated and filtered by your access.              │
└──────────────────────────────────────────────────────────────────────────┘
```

The sidebar is a **set tree**, not a flat partition list:

```
All sets (2)
Department X — AWS Production (4)
  └── Payments (3)
        demo-deploy
        demo-build
        demo-new (unassigned)
        Refunds (0)
  └── Platform (1)
Department Y — AWS Development (1)
  └── demo-observe
Ungrouped (0)
```

Changes from today:

- **Sets are the overview unit.** The page shows one card per top-level set
  with description, labels, counts, rollup, and attention — not thousands of
  partition cards. The flat partition grid is replaced by set cards plus a
  search/paginated “Find a partition” path.
- **Needs attention** is the first thing on the page and includes the set
  context and the next action. Assignment gaps are attention items.
- **Recently changed** answers "what changed?" and includes the set name.
- **Set rows** in the sidebar show a segmented health bar; the set page has the
  numeric rollup.
- Empty state: "Nothing needs attention. 2 sets healthy." with a link to
  recent changes.

### 5.2 Application / partition view

```
┌──────────────────────────────────────────────────────────────────────────┐
│ ← Overview   demo-observe                              [Check now] [⋯]   │
│ ● ObservedDrifted · Observe only · account 7553… · us-east-1             │
│ last observed 12s ago · 1 drifted resource                               │
├──────────────────────────────────────────────────────────────────────────┤
│ Summary · Topology · Rollouts · History                                  │
├──────────────────────────────────────────────────────────────────────────┤
│ DRIFT                                                                    │
│ SSM parameter /strata-demo/observe/value                                 │
│ desired "observe-me" · live "drifted-by-demo"                            │
│ [View in History]                                                        │
│                                                                          │
│ INTENTS                                                                  │
│ strata-demo-observe   Observed   1 asset   last check 12s ago            │
└──────────────────────────────────────────────────────────────────────────┘
```

- The hero shows **status, origin, account/region, last change, and the one
  action that makes sense** (Reconcile for owned partitions; no Apply button
  for observe-only).
- **Summary** is the new default tab and contains the old Overview content
  (intent map, service health, recent events) only for this partition.
- Drift gets a first-class card: resource, desired vs live, and a History link.
  This is the S3 demo's "it did not fix it" moment.
- **Source & targets** is a full-width card on Summary: repository, branch/
  commit, repo path, MonoFS path, payload path, selector, target pusher, and
  per-intent pusher mapping. Missing repo or unregistered pusher turns the card
  into an explicit warning with the next action.
- **Rollouts** and **History** keep their current content; they are scoped here.

### 5.3 Adopt — one queue

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Adopt                                                                    │
│ 2 stacks are not owned in code.                                          │
├──────────────────────────────────────────────────────────────────────────┤
│ ● DemoImportStack     unmanaged   CREATE_COMPLETE   aws-6772…  [Adopt]  │
│ ● StrataDemoObserve   unmanaged   CREATE_COMPLETE   aws-7553…  [Adopt]  │
│                                                                          │
│ Scan an account                                                          │
│ [profile ▾] [region ▾] [Scan]   last scan 2m ago                         │
└──────────────────────────────────────────────────────────────────────────┘
```

- Unmanaged stacks and scans are the same job, so they share one screen.
- **Adopt** opens the existing associate-repo dialog (repo URL, partition).
- The row explains *what happens next*: "Guardian will open a pull request;
  merge it to bind the stack to the repo."
- The adoption panel shows the full assignment before the PR: repo URL,
  partition, set, target pusher, binding path, selector, and the resulting
  repo/MonoFS paths. The only write is the pull request.
- Remove the separate "Scan & Import" top-level tab.

### 5.4 Connections — agents, accounts, repos

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Connections                                                              │
│ 3 agents · 3 accounts · 4 repos                                          │
├──────────────────────────────────────────────────────────────────────────┤
│ AGENTS                                                                   │
│ aws-7806…  account 7806…  us-east-1  http://172.18.0.1:19090  ● ready   │
│ aws-6772…  account 6772…  us-east-1  http://172.18.0.1:19091  ● ready   │
│ aws-7553…  account 7553…  us-east-1  http://172.18.0.1:19092  ● ready   │
│                                                                          │
│ REPOSITORIES                                                             │
│ strata-demo-deploy   → demo-deploy    last ingest 4m ago   ●            │
│ strata-demo-build    → demo-build     last ingest 4m ago   ●            │
│ strata-demo-import   → demo-import    last ingest 1m ago   ●            │
│ strata-demo-observe  → demo-observe   last ingest 1m ago   ●            │
└──────────────────────────────────────────────────────────────────────────┘
```

- Keep the current tables and add a health dot + last-ingest/error column.
- The row is the unit of action: "open repo", "remove binding", "re-register".
- The page answers "are my connections healthy?" before "what are the URLs?".
- **Connections is the only global view.** Partition pages show only the repos and
  pushers assigned to that partition; “Manage in Connections” opens the filtered
  global view.

### 5.5 Set page — named, nested navigation

```
Breadcrumb: Sets / Department X — AWS Production / Payments

Payments                                      [Add partitions] [Edit set]
Customer payment services for Department X, AWS prod, us-east-1.
department X · cloud AWS · account 7806… · region us-east-1 · owner payments-team

3 partitions · 2 healthy · 1 unassigned       parent: Department X · 1 subset

SUBSETS
  Refunds (0)  ·  empty

PARTITIONS                                    [Assignment: all] [Status: all]
  demo-deploy  Healthy      strata-demo-deploy   /partitions/demo-deploy   Assigned ✓
  demo-build   Healthy      strata-demo-build    /partitions/demo-build    Assigned ✓
  demo-new     Unassigned   —                    /partitions/demo-new      Unassigned  [Bind repo]
  Showing 1–3 of 3
```

- The set page shows **only its own partitions and child sets**.
- Each partition row carries the source repo, MonoFS path, and assignment
  state, so ownership is visible where the grouping happens.
- Set details show parent, labels, visibility, created/updated, and the rule
  that membership is virtual.
- Delete is disabled while a set contains partitions or subsets. Move the
  contents first.

### 5.6 Build — visual partition builder

Build uses the canvas/palette/inspector design in
`docs/ui-partition-builder.md`. The mockup adds the ownership pieces:

- **Assignment block** in the inspector: repository, repo path, MonoFS path,
  target pusher, and set. These are required before Save/Deploy.
- **Draft row** in the set tree while the partition is unsaved; the set count
  does not include drafts.
- **Problems bar** validates the whole draft: missing pusher, missing repo,
  path collision, account/region mismatch, and the mutable image tag warning
  shown in the mockup.
- **Save draft** is explicit; Deploy is a separate action and remains disabled
  while there are blocking problems.
- Raw YAML stays available for power users, but the canvas and inspector are
  the primary surface.

## 6. Status model

One vocabulary across the UI, API labels, and CLI:

| Status | Meaning | Color | Default action |
|---|---|---|---|
| **Healthy** | desired state applied and in sync | green | none |
| **Progressing** | diff/check/apply in flight | blue | watch rollout |
| **Attention** | drift, missing repo, or degraded | amber | inspect drift |
| **Failing** | apply failed, rolled back | red | open rollout/error |
| **Observed** | snapshot only, never applied | neutral | inspect snapshot |
| **ObservedDrifted** | snapshot differs from live | amber | inspect drift |
| **Unmanaged** | live stack, no partition/repo | neutral | adopt |

Rules:

- Never use a color without the word. Color-blind users and screenshots.
- Use `Attention` for drift and `Failing` for apply failures; the current mix of
  "Attention", "Needs action", "Stable", and "ObservedDrifted" is too wide.
- Observed partitions show `Observed` / `ObservedDrifted`, not `Healthy`.
- The origin badge (Native CDK, User CDK, Adopted, Observe only) stays next to
  the status; it explains *why* the action is available or not.

## 7. Interaction details

- **Keyboard:** `/` focuses search; `g o` Overview, `g c` Connections,
  `g a` Adopt, `g b` Build; `Esc` closes the panel. (Optional phase 3.)
- **Refresh:** keep the auto-refresh pill; show "updated 12s ago" everywhere.
- **Actions:** one primary button per screen. Destructive actions (delete
  partition, remove binding) are secondary and confirm with the partition name.
- **Empty states:** every table/card has a sentence and a primary action.
  Example: "No unmanaged stacks. All live stacks are covered by a repo."
- **Errors:** show the error text and the `guardianctl` command that reproduces
  it. This is the SRE escape hatch.
- **No modals for reading.** Drift, rollout details, and history open in the
  page; modals are only for irreversible actions.

## 8. Implementation plan

### Phase 1 — low risk, no backend changes (1–2 days)

- Rename the nav/header consistently: **Overview** (not Applications/Control
  Center), **Adopt**, **Connections**, **Build**.
- Collapse the six-cell stat strip into one health sentence + filters.
- Move "Needs attention" above the fold; add reason + action per row.
- Add "last changed / last observed" timestamps to application cards.
- Unify status labels/colors through one helper in `static-src`.
- Add empty-state copy and CLI hint to Unmanaged Stacks and Pushers & Repos.
- Keep the current tab set behind a feature flag if needed; no route changes.

### Phase 2 — partition-scoped navigation and assignment (3–5 days)

- Hide Topology/Rollouts/History until a partition is selected.
- Merge Unmanaged Stacks + Scan & Import into **Adopt**.
- Merge Pushers & Repos into **Connections**.
- Add the Summary tab, the drift card, and the **Source & targets** card.
- Add the scoped assignment aggregate to the partition API (repos by binding,
  pushers by intent, paths, warnings).
- Adopt: scan + adoption panel with the full assignment summary before the PR.
- Build: assignment block (repo, repo path, MonoFS path, pusher, set) and
  validation problems before Save/Deploy.
- Remove Pushers & Repos from the partition tab bar; link to filtered Connections.
- Keep deep links working (`/#partition/demo-observe/history`).

### Phase 3 — virtual sets (5–8 days)

- Set records and membership index in the Guardian store
  (`/.registry/sets`, `/.registry/set-memberships`).
- Tree API with ACL-filtered counts and lazy children.
- Sidebar set tree, pinned/recent sets, **Ungrouped**.
- Set page: description, labels, subsets, paginated partition table with
  source/MonoFS path/assignment columns.
- Create/edit/move/delete sets (admin-only; delete blocked while non-empty).
- “Add partitions” multi-select picker with name/repo/path search.

### Phase 4 — polish (optional)

- Command palette, saved filters, keyboard shortcuts.
- “Copy CLI command” on every action.
- Screenshot-based tests for the primary screens.

## 9. Decisions applied

The mockups follow the recommended defaults:

1. **Four destinations** — Overview, Connections, Adopt, Build.
2. **Attention-first overview** — needs-attention rows appear before the app grid.
3. **Adopt and Connections stay separate** — setup/admin work vs. daily operations.
4. **Status vocabulary** — the seven canonical statuses in §6.
5. **Virtual sets** — nested, many-to-many membership with one optional primary
   set; descriptions and labels are first-class.
6. **Unassigned is attention, not blocking** — manual/observe-only partitions
   remain valid.
7. **Delete non-empty set is blocked** — move or delete contents first.
8. **Set edits are admin-only** — contents are filtered by the partition ACL.

Next step: review `docs/mockups/` before implementation. The mockups are design
artifacts; they do not change the production UI.
