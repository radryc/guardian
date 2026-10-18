# UI redesign — implementation plan

Status: implemented; verification in progress. This plan turns `docs/ui-redesign.md` and
`docs/mockups/` into the shipped UI. Each phase is a separate commit.

## Phase 1 — shell, navigation, themes

**Goal:** the four-destination shell and a working theme picker.

- [x] Add theme tokens and five themes in `static-src/main.css`:
      Midnight (default), Graphite, Ocean, Forest, Daylight.
- [x] Add a theme picker to the top nav; persist `guardian.theme` in
      localStorage and apply `data-theme` on `<html>` before paint.
- [x] Replace the eight top-level nav items with:
      Overview, Connections, Adopt, Build.
- [x] Keep partition-scoped tabs: Summary, Topology, Rollouts, History.
      Hide the tab row when no partition is selected.
- [x] Rename the global pages in `renderPageChrome`:
      Connections (registry), Adopt (unmanaged + scan), Build.
- [x] Keep the existing panels/IDs internally; navigation labels change.
- [x] Commit (`faed242`).

## Phase 2 — assignment, scoped views, adopt/build

**Goal:** every partition shows where it comes from and what it targets.

- [x] Add `assignments` to `PartitionDetailResponse`:
      repos (by binding), pushers (by intent), repo/MonoFS/payload paths,
      warnings (no repo, unregistered pusher, account/region mismatch).
- [x] Add a **Source & targets** card to the selected-partition view:
      repo, branch/commit, repo path, MonoFS path, payload path, selector,
      per-intent pusher mapping, warning states.
- [x] Add a compact assignment section to the builder inspector:
      repo, paths, target pusher, set; link to Connections for changes.
- [x] Make Adopt a single page with sub-tabs: unmanaged stacks and scan.
- [x] Connections remains the only global agents/repos view.
- [x] Commit (`3d49f49`).

## Phase 3 — virtual sets

**Goal:** named, nested sets for navigation and grouping.

- [x] Add `internal/sets` with store-backed CRUD:
      `Set{id,name,description,parentId,labels,pinned,partitions}`,
      cycle checks, delete block for non-empty sets, partition membership.
- [x] Add `/.registry/sets` and `/.registry/set-primary` paths.
- [x] Add API:
      `GET /api/sets`, `GET /api/sets/{id}`, `POST /api/sets`,
      `PATCH /api/sets/{id}`, `DELETE /api/sets/{id}`,
      `POST /api/sets/{id}/move`, `POST /api/sets/{id}/partitions`.
- [x] Replace the flat partition list in the sidebar with a bounded set tree:
      All sets, nested sets, Ungrouped, pinned sets, search.
- [x] Overview shows top-level set cards with descriptions, labels,
      partition/subset counts, health rollup, and assignment summary.
- [x] Add a set page with:
      description, labels, parent, subsets, scoped partition table
      (status, repo, MonoFS path, assignment), health rollup, set details.
- [x] Add create/edit/move/delete set and add/remove partitions UI.
- [x] Add `guardianctl sets ensure` and seed **Core** (control plane) and
      **Department X** (all `demo-*` AWS deployments) during demo `up`.
- [x] Add CLI parity: `sets create|list|show|update|move|delete|assign` and
      `partition assignments` for the scoped repo/pusher view.
- [x] Commit (`d75f58b`).

## Phase 4 — polish and verification

- [x] Status vocabulary and empty states across the new pages.
- [x] `npm run build:ui`, `go build`, relevant Go tests.
- [x] Update docs (`docs/ui-redesign.md`, `docs/mockups/README.md`) with the
      shipped behavior.
- [x] Commit (`f668051`); managed-set polish in `15e8830`.

## Acceptance

- Four destinations work; partition tabs only appear with a selection.
- Theme picker changes the UI immediately and survives reload.
- A partition shows its assigned repo(s), MonoFS paths, and pusher(s).
- Nested sets can be created, edited, moved, populated, and deleted when empty.
- Overview does not render every partition.
- `go build ./...` and `npm run build:ui` pass.
