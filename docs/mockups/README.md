# Guardian UI mockups

Static HTML mockups for the next Guardian UI iteration. Open `overview.html`
directly in a browser; no build step or server is required.

The mockups use a small demo data set for readability. The design for thousands
of partitions is the lazy-loaded set tree, server-side counts, and paginated
set pages described below.

| File | What it shows |
|---|---|
| `overview.html` | Set-first overview: set cards, health rollups, assignment gaps, and virtual-set tree |
| `application.html` | Partition summary with the new **Source & targets** card |
| `sets.html` | A nested set page: description, labels, subsets, scoped partitions |
| `connections.html` | Global inventory of agents and repositories, with filters and unassigned work |
| `mockup.css` | Shared styles; mirrors the existing dark visual language |

The overview deliberately does **not** render every partition. It shows top-level
set cards and a bounded “Find a partition” search; individual partitions are
reached through the set tree or the paginated search. That is the design for
thousands of partitions.

## Design decisions used here

These follow the recommended defaults from the design review:

- **Four destinations:** Overview, Connections, Adopt, Build. Topology, Rollouts
  and History live inside a partition, not in the top-level nav.
- **“Fleet” is now “Connections.”** The page is the global inventory of agents,
  repositories, and bindings; “Fleet” sounded like a health dashboard. The nav
  order is Overview → Connections → Adopt → Build (Connections before Adopt, as
  discussed).
- **Virtual sets** are navigation metadata only. They do not change ownership,
  deployment, or MonoFS paths.
- **Nested sets:** one parent per set, arbitrary depth; cycles are rejected.
- **Many-to-many membership:** a partition can appear in more than one set, with
  one optional **primary set** for navigation.
- **Unassigned is an attention state**, not a hard block: manual and
  observe-only partitions can be valid.
- **Delete a non-empty set is blocked.** Move or delete the contents first.
- **Set editing is admin-only** in this iteration; contents are filtered by the
  existing partition ACL.

## What to review

1. **`application.html`** — the Source & targets card: repository, repo path,
   MonoFS path, payload path, target pusher, and per-intent mapping.
2. **`sets.html`** — the nested tree, description/labels, child set, and the
   partition table with source path and assignment state.
3. **`overview.html`** — top-level set cards, health rollups, unassigned work in
   Needs attention, and the bounded “Find a partition” search instead of a
   ten-thousand-card grid.
4. **`connections.html`** — all agents/repos live here; partition pages show only the
   ones assigned to them.

These are design artifacts, not production UI. The real implementation will
reuse the existing templates and styles.
