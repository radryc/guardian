# UI redesign — simple by default, deep when needed

Status: draft for review. Scope: the Guardian web UI
(`internal/ui/templates/index.html`, `internal/ui/static-src/*`).

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
│  Adopt                       │  stacks that are not owned in code yet
│  Fleet                       │  agents, accounts, repos
│  Build                       │  create/edit a partition (existing design)
└──────────────────────────────┘

Selecting an application opens a partition view:

  Summary · Topology · Rollouts · History
```

- **Overview** replaces Applications + the current Control Center overview.
- **Adopt** merges Unmanaged Stacks and Scan & Import into one queue.
- **Fleet** merges Pushers & Repos into one connection view.
- **Build** stays as designed in `ui-partition-builder.md`.
- Topology / Rollouts / History become partition-scoped tabs. They no longer
  appear when no partition is selected.

This is a **navigation change only**; every existing panel and endpoint stays.

## 5. Screen designs

### 5.1 Overview — "what needs me?"

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Overview                                            [Search]  [Refresh]  │
│ 4 applications · 3 healthy · 1 needs attention        last reconcile 12s  │
├──────────────────────────────────────────────────────────────────────────┤
│ NEEDS ATTENTION (1)                                                      │
│ ┌──────────────────────────────────────────────────────────────────────┐ │
│ │ ● demo-observe   ObservedDrifted   1 drifted resource  12s ago       │ │
│ │   SSM parameter /strata-demo/observe/value changed outside Guardian  │ │
│ │   [View drift]  [History]                                            │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│                                                                          │
│ RECENTLY CHANGED                                                         │
│ demo-deploy   Healthy      rollout dep_9a2a  applied 4m ago     [open]  │
│ demo-build    Healthy      image :demo       applied 6m ago     [open]  │
│ demo-import   Observed     snapshot          checked 1m ago     [open]  │
│                                                                          │
│ ALL APPLICATIONS (4)                                  [Healthy ▾] [New]  │
│ ┌───────────────┐ ┌───────────────┐ ┌───────────────┐ ┌───────────────┐ │
│ │ demo-deploy   │ │ demo-build    │ │ demo-import   │ │ demo-observe  │ │
│ │ ● Healthy     │ │ ● Healthy     │ │ ● Observed    │ │ ● Attention   │ │
│ │ 4 assets      │ │ 3 assets      │ │ observed only │ │ 1 drift       │ │
│ │ Native CDK    │ │ Native CDK    │ │ Adopted       │ │ Observe only  │ │
│ └───────────────┘ └───────────────┘ └───────────────┘ └───────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘
```

Changes from today:

- One health sentence replaces the six-cell stat strip. The numbers are still
  available in the sentence and in filters; they no longer dominate the page.
- **Needs attention** is the first thing on the page and includes the *reason*
  and the next action, not just a status.
- **Recently changed** answers "what changed?" without opening History.
- **All applications** is a filtered card grid; the existing intent map and
  service-health cards move into the partition view (they are detail, not
  overview).
- Empty state: "Nothing needs attention. 4 applications healthy." with a link
  to recent changes.

### 5.2 Application / partition view

```
┌──────────────────────────────────────────────────────────────────────────┐
│ ← Overview   demo-observe                              [Reconcile] [⋯]   │
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
- Remove the separate "Scan & Import" top-level tab.

### 5.4 Fleet — agents, accounts, repos

```
┌──────────────────────────────────────────────────────────────────────────┐
│ Fleet                                                                    │
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
- The page answers "is my fleet connected?" before "what are the URLs?".

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

- **Keyboard:** `/` focuses search; `g o` Overview, `g a` Adopt, `g f` Fleet,
  `g b` Build; `Esc` closes the panel. (Optional phase 3.)
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
  Center), **Adopt**, **Fleet**, **Build**.
- Collapse the six-cell stat strip into one health sentence + filters.
- Move "Needs attention" above the fold; add reason + action per row.
- Add "last changed / last observed" timestamps to application cards.
- Unify status labels/colors through one helper in `static-src`.
- Add empty-state copy and CLI hint to Unmanaged Stacks and Pushers & Repos.
- Keep the current tab set behind a feature flag if needed; no route changes.

### Phase 2 — partition-scoped navigation (3–5 days)

- Hide Topology/Rollouts/History until a partition is selected.
- Merge Unmanaged Stacks + Scan & Import into **Adopt**.
- Merge Pushers & Repos into **Fleet**.
- Add the Summary tab and the drift card.
- Keep deep links working (`/#partition/demo-observe/history`).

### Phase 3 — polish (optional)

- Command palette, saved filters, keyboard shortcuts.
- "Copy CLI command" on every action.
- Screenshot-based tests for the three primary screens.

## 9. Open questions for review

1. Do we keep the current eight-tab navigation for this iteration, or adopt the
   four-destination IA in §4? The latter is the bigger simplification but
   changes muscle memory.
2. Is the primary landing question "what is broken?" (attention-first, as
   designed) or "show me all applications" (card grid first)?
3. Should **Adopt** and **Fleet** stay separate top-level screens? They serve
   different personas (platform admin vs on-call), but both are "setup" work.
4. Which statuses are canonical for the API? I propose the seven in §6; the
   current API also emits `Stable`, `Needs action`, and `Progressing`.
5. Do you want an HTML mockup of the Overview and Application screens next, or
   should I implement Phase 1 directly?
