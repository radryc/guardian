# Guardian UI mockups

Static HTML mockups for the next Guardian UI iteration. Open `overview.html`
directly in a browser; no build step or server is required.

| File | What it shows |
|---|---|
| `overview.html` | Health-first overview, assignment gaps, and the virtual-set tree |
| `application.html` | Partition summary with the new **Source & targets** card |
| `sets.html` | A nested set page: description, labels, subsets, scoped partitions |
| `fleet.html` | Global inventory of agents and repositories, with filters and unassigned work |
| `mockup.css` | Shared styles; mirrors the existing dark visual language |

## Design decisions used here

These follow the recommended defaults from the design review:

- **Four destinations:** Overview, Adopt, Fleet, Build. Topology, Rollouts and
  History live inside a partition, not in the top-level nav.
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
3. **`overview.html`** — unassigned work is surfaced in Needs attention and on
   the application card.
4. **`fleet.html`** — all agents/repos live here; partition pages show only the
   ones assigned to them.

These are design artifacts, not production UI. The real implementation will
reuse the existing templates and styles.
