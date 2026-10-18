# UI redesign — implementation plan

Status: in progress. This plan turns `docs/ui-redesign.md` and
`docs/mockups/` into the shipped UI. Each phase is a separate commit.

## Phase 1 — shell, navigation, themes

**Goal:** the four-destination shell and a working theme picker.

- [ ] Add theme tokens and four dark themes in `static-src/main.css`:
      Midnight (default), Graphite, Ocean, Forest.
- [ ] Add a theme picker to the top nav; persist `guardian.theme` in
      localStorage and apply `data-theme` on `<html>` before paint.
- [ ] Replace the eight top-level nav items with:
      Overview, Connections, Adopt, Build.
- [ ] Keep partition-scoped tabs: Summary, Topology, Rollouts, History.
      Hide the tab row when no partition is selected.
- [ ] Rename the global pages in `renderPageChrome`:
      Connections (registry), Adopt (unmanaged + scan), Build.
- [ ] Keep the existing panels/IDs internally; navigation labels change.
- [ ] Commit.

## Phase 2 — assignment, scoped views, adopt/build

**Goal:** every partition shows where it comes from and what it targets.

- [ ] Add `assignments` to `PartitionDetailResponse`:
      repos (by binding), pushers (by intent), repo/MonoFS/payload paths,
      warnings (no repo, unregistered pusher, account/region mismatch).
- [ ] Add a **Source & targets** card to the selected-partition view:
      repo, branch/commit, repo path, MonoFS path, payload path, selector,
      per-intent pusher mapping, warning states.
- [ ] Add a compact assignment section to the builder inspector:
      repo, paths, target pusher, set; link to Connections for changes.
- [ ] Make Adopt a single page with two sections: unmanaged stacks and scan.
- [ ] Connections remains the only global agents/repos view.
- [ ] Commit.

## Phase 3 — virtual sets

**Goal:** named, nested sets for navigation and grouping.

- [ ] Add `internal/sets` with store-backed CRUD:
      `Set{id,name,description,parentId,labels,pinned,partitions}`,
      cycle checks, delete block for non-empty sets, partition membership.
- [ ] Add `/.registry/sets` and `/.registry/set-primary` paths.
- [ ] Add API:
      `GET /api/sets`, `GET /api/sets/{id}`, `POST /api/sets`,
      `PATCH /api/sets/{id}`, `DELETE /api/sets/{id}`,
      `POST /api/sets/{id}/move`, `POST /api/sets/{id}/partitions`.
- [ ] Replace the flat partition list in the sidebar with a lazy set tree:
      All sets, nested sets, Ungrouped, pinned sets, search.
- [ ] Overview shows top-level set cards with descriptions, labels,
      partition/subset counts, health rollup, and assignment summary.
- [ ] Add a set page with:
      description, labels, parent, subsets, scoped partition table
      (status, repo, MonoFS path, assignment), health rollup, set details.
- [ ] Add create/edit/move/delete set and add/remove partitions UI.
- [ ] Commit.

## Phase 4 — polish and verification

- [ ] Status vocabulary and empty states across the new pages.
- [ ] `npm run build:ui`, `go build`, relevant Go tests.
- [ ] Update docs (`docs/ui-redesign.md`, `docs/mockups/README.md`) with the
      shipped behavior.
- [ ] Commit.

## Acceptance

- Four destinations work; partition tabs only appear with a selection.
- Theme picker changes the UI immediately and survives reload.
- A partition shows its assigned repo(s), MonoFS paths, and pusher(s).
- Nested sets can be created, edited, moved, populated, and deleted when empty.
- Overview does not render every partition.
- `go build ./...` and `npm run build:ui` pass.
