import { escapeHtml, escapeAttr, formatDateTime, fetchJSON, toast } from "./utils";

export interface SetsOptions {
  getOverview: () => any;
  getSelectedPartition: () => string;
  getSelectedSet: () => string;
  onSelectPartition: (name: string) => void;
  onOpenSet: (id: string) => void;
  onOpenConnections: () => void;
  onChanged?: () => void;
  onToast: (message: string, type?: "success" | "error" | "info") => void;
  onError: (error: any) => void;
}

interface RepoBinding {
  partition: string;
  path?: string;
  selector?: string;
  account?: string;
  region?: string;
}

interface Repo {
  name: string;
  pusher: string;
  sourceType?: string;
  bindings?: RepoBinding[];
  partition?: string;
  path?: string;
  source?: string;
  branch?: string;
}

interface SetRecord {
  id: string;
  name: string;
  description?: string;
  parentId?: string;
  labels?: Record<string, string>;
  pinned?: boolean;
  partitions?: string[];
  partitionCount?: number;
  childCount?: number;
  createdAt?: string;
  updatedAt?: string;
}

interface SetHealth {
  healthy: number;
  progressing: number;
  observed: number;
  attention: number;
  failing: number;
}

let opts: SetsOptions;
let sets: SetRecord[] = [];
let repos: Repo[] = [];
let ungrouped = 0;
let loaded = false;
let loading = false;
let setModal: HTMLElement | null = null;

export function initSetsPanel(options: SetsOptions): void {
  opts = options;
  document.getElementById("newSetButton")?.addEventListener("click", () => openSetEditor());
  document.getElementById("createPartitionButton")?.addEventListener("click", () => openSetEditor());
  document.getElementById("manageSetsButton")?.addEventListener("click", () => openManageSets());
  document.getElementById("partitionSearch")?.addEventListener("input", renderSidebar);
  document.getElementById("setCards")?.addEventListener("click", (event) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>("[data-open-set]");
    if (target) opts.onOpenSet(target.dataset.openSet ?? "");
  });
  document.getElementById("setPage")?.addEventListener("click", (event) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>("[data-set-action],[data-set-partition],[data-open-set]");
    if (!target) return;
    if (target.dataset.openSet !== undefined) {
      opts.onOpenSet(target.dataset.openSet);
      return;
    }
    if (target.dataset.setPartition) {
      opts.onSelectPartition(target.dataset.setPartition);
      return;
    }
    const action = target.dataset.setAction;
    if (action === "add") openPartitionPicker(opts.getSelectedSet());
    if (action === "new") openSetEditor();
    if (action === "edit") openSetEditor(currentSet());
    if (action === "subset") openSetEditor(undefined, opts.getSelectedSet());
    if (action === "delete") deleteSet(currentSet()).catch(opts.onError);
    if (action === "pin") togglePin(currentSet()).catch(opts.onError);
    if (action === "connections") opts.onOpenConnections();
  });
  refreshSets().catch(opts.onError);
}

export function selectedSetName(): string {
  const id = opts?.getSelectedSet() ?? "";
  if (!id) return "Sets";
  if (id === "ungrouped") return "Ungrouped";
  return sets.find((set) => set.id === id)?.name ?? "Set";
}

export function renderSets(): void {
  renderSidebar();
  renderOverviewCards();
  if (document.getElementById("setPanel")?.classList.contains("active")) renderSetPage();
}

export async function refreshSets(): Promise<void> {
  if (loading) return;
  loading = true;
  try {
    const [setResponse, repoResponse] = await Promise.all([
      fetchJSON("/api/sets"),
      fetchJSON("/api/repos").catch(() => ({ repos: [] })),
    ]);
    sets = Array.isArray(setResponse?.sets) ? setResponse.sets : [];
    ungrouped = Number(setResponse?.ungrouped ?? 0);
    repos = Array.isArray(repoResponse?.repos) ? repoResponse.repos : [];
    loaded = true;
    renderSets();
    opts.onChanged?.();
  } finally {
    loading = false;
  }
}

function renderSidebar(): void {
  const container = document.getElementById("partitionList");
  if (!container) return;
  if (!loaded) {
    container.className = "set-tree text-sm text-[#566778]";
    container.textContent = "Loading sets…";
    return;
  }
  const search = (document.getElementById("partitionSearch") as HTMLInputElement)?.value.trim().toLowerCase() ?? "";
  const selected = opts.getSelectedSet();
  const selectedPartition = opts.getSelectedPartition();
  const children = new Map<string, SetRecord[]>();
  for (const set of sets) {
    const key = set.parentId ?? "";
    if (!children.has(key)) children.set(key, []);
    children.get(key)!.push(set);
  }
  const matches = (set: SetRecord) => !search || `${set.name} ${set.description ?? ""} ${Object.values(set.labels ?? {}).join(" ")} ${(set.partitions ?? []).join(" ")}`.toLowerCase().includes(search);
  const visibleSets = sets.filter(matches);
  const visibleIDs = new Set(visibleSets.map((set) => set.id));
  const rows: string[] = [];

  const allActive = !selected;
  rows.push(`<button class="set-tree-row ${allActive ? "active" : ""}" data-open-set="">
    <span class="set-tree-chevron">▾</span><span class="set-tree-name">All sets</span><span class="set-tree-count">${sets.length}</span>
    ${rollupBar(healthForAll())}
  </button>`);

  const renderSet = (set: SetRecord, depth: number) => {
    if (!visibleIDs.has(set.id) && !children.get(set.id)?.some((child) => visibleIDs.has(child.id))) return;
    const kids = (children.get(set.id) ?? []).sort((a, b) => a.name.localeCompare(b.name));
    const active = selected === set.id;
    const containsSelected = (set.partitions ?? []).includes(selectedPartition);
    rows.push(`<div class="set-tree-row ${active ? "active" : ""}" style="padding-left:${8 + depth * 12}px" data-open-set="${escapeAttr(set.id)}" role="button" tabindex="0">
      <span class="set-tree-chevron">${kids.length ? "▾" : "•"}</span>
      <span class="set-tree-name" title="${escapeAttr(set.name)}">${escapeHtml(set.name)}</span>
      <span class="set-tree-count">${descendantPartitions(set).length}</span>
      ${rollupBar(healthForSet(set))}
    </div>`);
    for (const child of kids) renderSet(child, depth + 1);
    if (active || containsSelected || search) {
      const allVisible = (set.partitions ?? []).filter((partition) => !search || partition.toLowerCase().includes(search) || set.name.toLowerCase().includes(search));
      const visible = allVisible.slice(0, 40);
      for (const partition of visible) {
        const summary = overviewPartition(partition);
        const partitionActive = partition === selectedPartition;
        rows.push(`<div class="set-tree-row set-tree-partition ${partitionActive ? "active" : ""}" style="padding-left:${20 + depth * 12}px" data-set-partition="${escapeAttr(partition)}" role="button" tabindex="0">
          <span class="status-dot status-dot-${escapeAttr(partitionDot(summary))}"></span>
          <span class="set-tree-name">${escapeHtml(partition)}</span>
        </div>`);
      }
      if (allVisible.length > visible.length) {
        rows.push(`<div class="set-tree-row set-tree-more" style="padding-left:${20 + depth * 12}px" data-open-set="${escapeAttr(set.id)}">+ ${allVisible.length - visible.length} more · view set</div>`);
      }
    }
  };
  for (const set of (children.get("") ?? []).sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned) || a.name.localeCompare(b.name))) {
    renderSet(set, 0);
  }
  rows.push(`<button class="set-tree-row ${selected === "ungrouped" ? "active" : ""}" data-open-set="ungrouped">
    <span class="set-tree-chevron">•</span><span class="set-tree-name">Ungrouped</span><span class="set-tree-count">${ungrouped}</span>
  </button>`);
  if (search && !visibleSets.length && ungrouped === 0) {
    rows.push(`<div class="set-tree-empty">No sets or partitions match.</div>`);
  }
  container.className = "set-tree";
  container.innerHTML = rows.join("");

  const wire = (item: HTMLElement) => {
    item.addEventListener("click", (event) => {
      const target = event.currentTarget as HTMLElement;
      if (target.dataset.setPartition) {
        opts.onSelectPartition(target.dataset.setPartition);
      } else if (target.dataset.openSet !== undefined) {
        opts.onOpenSet(target.dataset.openSet);
      }
    });
    item.addEventListener("keydown", (event) => {
      if ((event as KeyboardEvent).key === "Enter" || (event as KeyboardEvent).key === " ") {
        event.preventDefault();
        (event.currentTarget as HTMLElement).click();
      }
    });
  };
  container.querySelectorAll<HTMLElement>("[data-open-set],[data-set-partition]").forEach(wire);
}

function renderOverviewCards(): void {
  const container = document.getElementById("setCards");
  if (!container) return;
  if (!loaded) {
    container.innerHTML = `<div class="empty-state">Loading sets…</div>`;
    return;
  }
  const top = sets.filter((set) => !set.parentId).sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned) || a.name.localeCompare(b.name));
  const cards = top.map((set) => setCard(set));
  if (ungrouped > 0) {
    cards.push(`<button class="set-card" data-open-set="ungrouped">
      <div class="set-card-head"><span class="name">Ungrouped</span><span class="right">Open →</span></div>
      <div class="set-card-desc">Partitions that are not in any named set.</div>
      <div class="set-card-stats"><strong>${ungrouped} partitions</strong><span>·</span><span>ungrouped</span></div>
      <div class="set-card-foot"><span class="badge">Ungrouped</span><span class="action">View →</span></div>
    </button>`);
  }
  container.innerHTML = cards.length ? cards.join("") : `<div class="empty-state">No sets yet. Create one to group partitions.</div>`;
}

function setCard(set: SetRecord): string {
  const rollupNames = descendantPartitions(set);
  const health = healthForNames(rollupNames);
  const total = rollupNames.length;
  const children = sets.filter((candidate) => candidate.parentId === set.id);
  const labels = Object.entries(set.labels ?? {}).slice(0, 4).map(([key, value]) => `<span class="chip"><span class="k">${escapeHtml(key)}</span> ${escapeHtml(value)}</span>`).join("");
  return `<button class="set-card" data-open-set="${escapeAttr(set.id)}">
    <div class="set-card-head"><span class="name">${escapeHtml(set.name)}</span><span class="right">Open →</span></div>
    <div class="set-card-desc">${escapeHtml(set.description || "No description yet.")}</div>
    ${labels ? `<div class="set-card-labels">${labels}</div>` : ""}
    <div class="set-card-stats"><strong>${total} partition${total === 1 ? "" : "s"}</strong><span>·</span><span>${children.length} subset${children.length === 1 ? "" : "s"}</span></div>
    ${rollupBar(health, true)}
    <div class="set-card-legend">${healthLegend(health)}</div>
    <div class="set-card-foot"><span class="badge">${children.length ? "Nested set" : "Set"}</span><span class="action">View set →</span></div>
  </button>`;
}

function renderSetPage(): void {
  const container = document.getElementById("setPage");
  if (!container) return;
  const id = opts.getSelectedSet();
  if (!id) {
    container.innerHTML = `<div class="card empty-state">Select a set from the sidebar.</div>`;
    return;
  }
  const set = id === "ungrouped" ? ungroupedSet() : sets.find((candidate) => candidate.id === id);
  if (!set) {
    container.innerHTML = `<div class="card empty-state">Set not found.</div>`;
    return;
  }
  const partitions = (set.partitions ?? []).map((name) => ({ name, summary: overviewPartition(name), repo: repoForPartition(name) }));
  const rollupNames = set.id === "ungrouped" ? (set.partitions ?? []) : descendantPartitions(set);
  const health = healthForNames(rollupNames);
  const platformSet = (set.labels?.scope ?? "").toLowerCase() === "core";
  const children = sets.filter((candidate) => candidate.parentId === set.id);
  const parent = set.parentId ? sets.find((candidate) => candidate.id === set.parentId) : undefined;
  const labels = Object.entries(set.labels ?? {}).map(([key, value]) => `<span class="chip"><span class="k">${escapeHtml(key)}</span> ${escapeHtml(value)}</span>`).join("");
  const visible = partitions.slice(0, 50);
  const rows = visible.map(({ name, summary, repo }) => {
    const binding = repo?.bindings?.find((candidate) => candidate.partition === name) ?? (repo?.partition === name ? { partition: name, path: repo.path } : undefined);
    const native = repo?.sourceType === "native";
    const path = native ? "no repository required" : binding?.path || `/partitions/${name}/payloads/aws`;
    return `<tr>
      <td><div class="app-name">${escapeHtml(name)}</div><div class="repo-sub">${native ? "native source" : repo ? "assigned repo" : "no repository"}</div></td>
      <td><span class="status-dot status-dot-${escapeAttr(partitionDot(summary))}"></span> <span class="status-text">${escapeHtml(summary?.displayStatus ?? summary?.health ?? "Unknown")}</span></td>
      <td class="source-cell">
        ${native ? `<span class="mono">Native platform</span>` : repo ? `<span class="mono">${escapeHtml(repo.name)}</span>` : `<span style="color:var(--ui-amber)">No repository</span>`}
        <div class="path-cell">${escapeHtml(path)}</div>
      </td>
      <td>${repo ? `<span class="assign-ok">Assigned ✓</span>` : platformSet ? `<span class="assign-none">Native</span>` : `<span class="assign-warn">Unassigned</span>`}</td>
      <td><button class="btn-secondary" style="padding:2px 8px;font-size:10px;" data-set-partition="${escapeAttr(name)}">Open →</button></td>
    </tr>`;
  }).join("");
  const childCards = children.map((child) => `<button class="subset-card" data-open-set="${escapeAttr(child.id)}">
    <div class="name">${escapeHtml(child.name)}</div>
    <div class="meta">${child.partitionCount ?? child.partitions?.length ?? 0} partitions</div>
    <div class="foot"><span class="badge">${child.partitions?.length ? "Nested set" : "empty"}</span><span style="margin-left:auto;color:var(--ui-accent);font-size:10px;">Open →</span></div>
  </button>`).join("");
  const rollupItems = rollupNames.map((name) => ({ name, repo: repoForPartition(name) }));
  const assigned = rollupItems.filter((item) => item.repo).length;
  const unassigned = rollupItems.length - assigned;
  container.innerHTML = `
    <div class="set-page">
      <section class="card set-hero">
        <div class="set-hero-top">
          <div>
            <p class="set-desc">${escapeHtml(set.description || "No description yet.")}</p>
            <div class="chip-row">
              ${parent ? `<span class="chip"><span class="k">parent</span> ${escapeHtml(parent.name)}</span>` : ""}
              ${labels}
            </div>
          </div>
          <div class="right">
            ${set.id === "ungrouped" ? `
              <button class="btn-primary" data-set-action="new">Create a set</button>
            ` : `
              <button class="btn-primary" data-set-action="add">Add partitions</button>
              <button class="btn-secondary" data-set-action="subset">New subset</button>
              <button class="btn-secondary" data-set-action="edit">Edit set</button>
              <button class="btn-ghost" data-set-action="pin">${set.pinned ? "★ Pinned" : "☆ Pin"}</button>
            `}
          </div>
        </div>
        <div class="set-stats">
          <strong>${rollupNames.length} partition${rollupNames.length === 1 ? "" : "s"}</strong><span class="sep">·</span>
          <span><span class="status-dot status-dot-healthy"></span> ${health.healthy} healthy</span>
          <span><span class="status-dot status-dot-pending"></span> ${health.progressing} progressing</span>
          <span><span class="status-dot status-dot-attention"></span> ${health.attention} attention</span>
          <span class="right">${children.length} subsets · ${platformSet ? "native platform" : `${unassigned} unassigned`}</span>
        </div>
      </section>
      <div class="set-page-layout">
        <div class="set-page-main">
          ${children.length ? `<section class="card"><div class="card-header"><h2 class="card-title">Subsets</h2></div><div class="subset-grid">${childCards}</div></section>` : ""}
          <section class="card">
            <div class="card-header"><div class="flex items-center gap-2"><h2 class="card-title">Partitions</h2><span class="pill">${partitions.length}</span></div><p class="muted">Only partitions in this set are listed here.</p></div>
            ${visible.length ? `<div class="set-table-wrap"><table class="table"><thead><tr><th>Application</th><th>Status</th><th>Source / MonoFS path</th><th>Assignment</th><th></th></tr></thead><tbody>${rows}</tbody></table></div>` : `<div class="empty-state">${children.length ? "No direct partitions. Open a subset to see its partitions." : set.id === "ungrouped" ? "No ungrouped partitions." : `This set has no partitions yet. <button class="btn-secondary" data-set-action="add">Add partitions</button>`}</div>`}
            ${partitions.length > visible.length ? `<div class="set-table-more">Showing 1–${visible.length} of ${partitions.length}. Use the set picker to search.</div>` : ""}
          </section>
        </div>
        <div class="set-page-side">
          <section class="card">
            <div class="card-header"><h2 class="card-title">Health rollup</h2><p class="muted">Includes this set and child sets.</p></div>
            <div class="health-rollup-bar">${rollupBar(health, true)}</div>
            <div class="rollup-legend">
              ${legendRow("Healthy", health.healthy, "healthy")}
              ${legendRow("Progressing", health.progressing, "pending")}
              ${legendRow("Observed", health.observed, "neutral")}
              ${legendRow("Attention", health.attention, "attention")}
              ${legendRow("Failing", health.failing, "failing")}
            </div>
            <div class="set-divider"></div>
            <div class="kv-list">
              ${platformSet ? `<div class="kv-row"><span class="kv-label">Assignment</span><span class="kv-value">native platform · no repo binding</span></div>` : `
              <div class="kv-row"><span class="kv-label">Assigned</span><span class="kv-value">${assigned} of ${rollupNames.length}</span></div>
              <div class="kv-row"><span class="kv-label">Unassigned</span><span class="kv-value" style="color:${unassigned ? "var(--ui-amber)" : "inherit"}">${unassigned}</span></div>`}
              <div class="kv-row"><span class="kv-label">Sources</span><span class="kv-value">${[...new Set(rollupItems.map((p) => p.repo?.name).filter(Boolean))].join(", ") || "—"}</span></div>
              <div class="kv-row"><span class="kv-label">Pushers</span><span class="kv-value">${[...new Set(rollupItems.map((p) => p.repo?.pusher).filter(Boolean))].join(", ") || "—"}</span></div>
            </div>
            <button class="btn-secondary" style="margin-top:9px;" data-set-action="connections">Manage in Connections →</button>
          </section>
          <section class="card">
            <div class="card-header"><h2 class="card-title">Set details</h2><span class="pill">virtual</span></div>
            <div class="kv-list">
              <div class="kv-row"><span class="kv-label">Parent</span><span class="kv-value">${parent ? escapeHtml(parent.name) : "—"}</span></div>
              <div class="kv-row"><span class="kv-label">Visibility</span><span class="kv-value">all users · contents filtered by ACL</span></div>
              <div class="kv-row"><span class="kv-label">Members</span><span class="kv-value">${rollupNames.length} partitions · ${children.length} subsets</span></div>
              <div class="kv-row"><span class="kv-label">Ownership</span><span class="kv-value">navigation only</span></div>
              <div class="kv-row"><span class="kv-label">Created</span><span class="kv-value">${set.createdAt ? formatDateTime(set.createdAt) : "—"}</span></div>
              <div class="kv-row"><span class="kv-label">Updated</span><span class="kv-value">${set.updatedAt ? formatDateTime(set.updatedAt) : "—"}</span></div>
            </div>
            <div class="desc" style="margin-top:8px;color:var(--ui-faint);font-size:10.5px;">Delete is disabled while the set contains partitions or subsets.</div>
            ${set.id === "ungrouped" ? "" : `<div style="display:flex;gap:5px;margin-top:8px;">
              <button class="btn-secondary" data-set-action="edit">Move / edit</button>
              <button class="btn-ghost" data-set-action="delete" ${partitions.length || children.length ? "disabled" : ""}>Delete</button>
            </div>`}
          </section>
        </div>
      </div>
    </div>`;
}

function ungroupedSet(): SetRecord {
  const assigned = new Set<string>();
  for (const set of sets) for (const partition of set.partitions ?? []) assigned.add(partition);
  const names = (opts.getOverview()?.partitions ?? []).map((p: any) => String(p.name)).filter((name: string) => !assigned.has(name));
  return { id: "ungrouped", name: "Ungrouped", description: "Partitions that are not in any named set.", partitions: names, partitionCount: names.length };
}

function currentSet(): SetRecord | undefined {
  const id = opts.getSelectedSet();
  return id === "ungrouped" ? ungroupedSet() : sets.find((set) => set.id === id);
}

function overviewPartition(name: string): any {
  return (opts.getOverview()?.partitions ?? []).find((partition: any) => partition.name === name);
}

// A locked intent is a strong signal: show a white dot regardless of the
// partition's health color, so operators can spot protected partitions at a
// glance in the set tree.
function partitionDot(summary: any): string {
  return summary?.locked ? "locked" : (summary?.health ?? "neutral");
}

function repoForPartition(name: string): Repo | undefined {
  return repos.find((repo) => (repo.bindings ?? []).some((binding) => binding.partition === name) || repo.partition === name);
}

function descendantPartitions(set: SetRecord): string[] {
  const out = new Set<string>(set.partitions ?? []);
  for (const child of sets.filter((candidate) => candidate.parentId === set.id)) {
    for (const name of descendantPartitions(child)) out.add(name);
  }
  return [...out].sort();
}

function healthForSet(set: SetRecord): SetHealth {
  return healthForNames(descendantPartitions(set));
}

function healthForAll(): SetHealth {
  return healthForNames((opts.getOverview()?.partitions ?? []).map((p: any) => p.name));
}

function healthForNames(names: string[]): SetHealth {
  const out: SetHealth = { healthy: 0, progressing: 0, observed: 0, attention: 0, failing: 0 };
  for (const name of names) {
    const summary = overviewPartition(name);
    const health = String(summary?.health ?? "").toLowerCase();
    if (health === "healthy") out.healthy++;
    else if (health === "failing") out.failing++;
    else if (health === "attention") out.attention++;
    else if (health === "progressing" || health === "pending") out.progressing++;
    else out.observed++;
  }
  return out;
}

function rollupBar(health: SetHealth, large = false): string {
  const total = health.healthy + health.progressing + health.observed + health.attention + health.failing;
  if (!total) return `<span class="rollup-bar ${large ? "large" : ""} empty"></span>`;
  const seg = (kind: string, count: number) => count ? `<span class="seg ${kind}" style="flex:${count}"></span>` : "";
  return `<span class="rollup-bar ${large ? "large" : ""}" title="${health.healthy} healthy · ${health.progressing} progressing · ${health.observed} observed · ${health.attention} attention · ${health.failing} failing">
    ${seg("healthy", health.healthy)}${seg("progressing", health.progressing)}${seg("neutral", health.observed)}${seg("attention", health.attention)}${seg("failing", health.failing)}
  </span>`;
}

function healthLegend(health: SetHealth): string {
  return [
    health.healthy ? `<span class="item"><span class="status-dot status-dot-healthy"></span> ${health.healthy} healthy</span>` : "",
    health.progressing ? `<span class="item"><span class="status-dot status-dot-pending"></span> ${health.progressing} progressing</span>` : "",
    health.observed ? `<span class="item"><span class="status-dot status-dot-neutral"></span> ${health.observed} observed</span>` : "",
    health.attention ? `<span class="item"><span class="status-dot status-dot-attention"></span> ${health.attention} attention</span>` : "",
    health.failing ? `<span class="item"><span class="status-dot status-dot-failing"></span> ${health.failing} failing</span>` : "",
  ].filter(Boolean).join("");
}

function legendRow(label: string, value: number, kind: string): string {
  return `<div class="rollup-legend-row"><span class="status-dot status-dot-${kind}"></span><span class="label">${escapeHtml(label)}</span><span class="value">${value}</span></div>`;
}

// ── Set editor / picker / delete ─────────────────────
function openSetEditor(set?: SetRecord, parentId?: string): void {
  const editing = Boolean(set && set.id !== "ungrouped");
  const parent = set?.parentId ?? parentId ?? "";
  const parentOptions = sets.filter((candidate) => candidate.id !== set?.id).map((candidate) =>
    `<option value="${escapeAttr(candidate.id)}"${candidate.id === parent ? " selected" : ""}>${escapeHtml(candidate.name)}</option>`).join("");
  openModal(`<div class="set-modal-head"><h2>${editing ? "Edit set" : "New set"}</h2><button class="btn-ghost" data-modal-close>×</button></div>
    <div class="set-modal-body">
      <label class="field"><span>Name</span><input id="setModalName" class="input" value="${escapeAttr(set?.name ?? "")}" placeholder="Payments"></label>
      <label class="field"><span>Description</span><textarea id="setModalDescription" class="textarea" rows="3" placeholder="Customer payment services for Department X in AWS production.">${escapeHtml(set?.description ?? "")}</textarea></label>
      <label class="field"><span>Parent set</span><select id="setModalParent" class="input"><option value="">— no parent —</option>${parentOptions}</select></label>
      <label class="field"><span>Labels (key=value per line)</span><textarea id="setModalLabels" class="textarea" rows="3" placeholder="department=Department X&#10;cloud=aws&#10;env=prod">${Object.entries(set?.labels ?? {}).map(([k, v]) => `${k}=${v}`).join("\n")}</textarea></label>
      <label class="set-modal-check"><input id="setModalPinned" type="checkbox" ${set?.pinned ? "checked" : ""}> Pin this set</label>
    </div>
    <div class="set-modal-actions"><button class="btn-ghost" data-modal-close>Cancel</button><button class="btn-primary" id="setModalSave">${editing ? "Save" : "Create set"}</button></div>`);
  document.getElementById("setModalSave")?.addEventListener("click", () => saveSet(set).catch(opts.onError));
}

async function saveSet(existing?: SetRecord): Promise<void> {
  const name = (document.getElementById("setModalName") as HTMLInputElement)?.value.trim() ?? "";
  if (!name) { opts.onToast("Set name is required.", "error"); return; }
  const description = (document.getElementById("setModalDescription") as HTMLTextAreaElement)?.value.trim() ?? "";
  const parentId = (document.getElementById("setModalParent") as HTMLSelectElement)?.value ?? "";
  const pinned = (document.getElementById("setModalPinned") as HTMLInputElement)?.checked ?? false;
  const labels: Record<string, string> = {};
  for (const line of ((document.getElementById("setModalLabels") as HTMLTextAreaElement)?.value ?? "").split("\n")) {
    const idx = line.indexOf("=");
    if (idx > 0) labels[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
  }
  const body = { name, description, parentId, labels, pinned };
  if (existing && existing.id !== "ungrouped") {
    await fetchJSON(`/api/sets/${encodeURIComponent(existing.id)}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    opts.onToast("Set updated.", "success");
  } else {
    await fetchJSON("/api/sets", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    opts.onToast("Set created.", "success");
  }
  closeModal();
  await refreshSets();
}

async function togglePin(set?: SetRecord): Promise<void> {
  if (!set || set.id === "ungrouped") return;
  await fetchJSON(`/api/sets/${encodeURIComponent(set.id)}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ...set, pinned: !set.pinned }) });
  await refreshSets();
}

async function deleteSet(set?: SetRecord): Promise<void> {
  if (!set || set.id === "ungrouped") return;
  if (!window.confirm(`Delete set "${set.name}"?`)) return;
  try {
    await fetchJSON(`/api/sets/${encodeURIComponent(set.id)}`, { method: "DELETE" });
    opts.onToast("Set deleted.", "success");
    closeModal();
    opts.onOpenSet("");
    await refreshSets();
  } catch (error: any) {
    opts.onToast(error?.message?.includes("not empty") ? "Move partitions and subsets first." : error?.message ?? "Could not delete set.", "error");
  }
}

function openManageSets(): void {
  const rows = sets.map((set) => {
    const parent = set.parentId ? sets.find((candidate) => candidate.id === set.parentId) : undefined;
    return `<div class="set-picker-row">
      <span class="set-manage-name">${escapeHtml(set.name)}</span>
      <span class="pill">${set.partitionCount ?? set.partitions?.length ?? 0} partitions${parent ? ` · ${escapeHtml(parent.name)}` : ""}</span>
      <button class="btn-secondary" style="padding:2px 7px;font-size:10px;" data-manage-edit="${escapeAttr(set.id)}">Edit</button>
      <button class="btn-ghost" style="padding:2px 7px;font-size:10px;" data-manage-delete="${escapeAttr(set.id)}">Delete</button>
    </div>`;
  }).join("");
  openModal(`<div class="set-modal-head"><h2>Manage sets</h2><button class="btn-ghost" data-modal-close>×</button></div>
    <div class="set-modal-body"><div class="set-picker-list">${rows || `<div class="empty-state">No sets yet.</div>`}</div></div>
    <div class="set-modal-actions"><button class="btn-ghost" data-modal-close>Close</button><button class="btn-primary" id="setManageNew">New set</button></div>`);
  document.getElementById("setManageNew")?.addEventListener("click", () => openSetEditor());
  setModal?.addEventListener("click", (event) => {
    const target = event.target as HTMLElement;
    const edit = target.closest<HTMLElement>("[data-manage-edit]");
    if (edit) {
      openSetEditor(sets.find((set) => set.id === edit.dataset.manageEdit));
      return;
    }
    const del = target.closest<HTMLElement>("[data-manage-delete]");
    if (del) {
      const set = sets.find((candidate) => candidate.id === del.dataset.manageDelete);
      if (set) deleteSet(set).then(() => { closeModal(); openManageSets(); }).catch(opts.onError);
    }
  });
}

function openPartitionPicker(setId: string): void {
  if (!setId || setId === "ungrouped") return;
  const set = sets.find((candidate) => candidate.id === setId);
  if (!set) return;
  const all = (opts.getOverview()?.partitions ?? []).map((p: any) => String(p.name)).sort();
  const selected = new Set(set.partitions ?? []);
  const rows = all.map((name) => `<label class="set-picker-row"><input type="checkbox" value="${escapeAttr(name)}" ${selected.has(name) ? "checked" : ""}> <span>${escapeHtml(name)}</span>${repoForPartition(name) ? `<span class="pill">${escapeHtml(repoForPartition(name)!.name)}</span>` : `<span class="pill">unassigned</span>`}</label>`).join("");
  openModal(`<div class="set-modal-head"><h2>Add partitions to ${escapeHtml(set.name)}</h2><button class="btn-ghost" data-modal-close>×</button></div>
    <div class="set-modal-body">
      <input id="setPickerSearch" class="input" placeholder="Search partitions…">
      <div class="set-picker-list" id="setPickerList">${rows || `<div class="empty-state">No partitions available.</div>`}</div>
    </div>
    <div class="set-modal-actions"><button class="btn-ghost" data-modal-close>Cancel</button><button class="btn-primary" id="setPickerSave">Save</button></div>`);
  document.getElementById("setPickerSearch")?.addEventListener("input", (event) => {
    const query = (event.target as HTMLInputElement).value.toLowerCase();
    document.querySelectorAll<HTMLElement>("#setPickerList .set-picker-row").forEach((row) => {
      row.style.display = row.textContent?.toLowerCase().includes(query) ? "" : "none";
    });
  });
  document.getElementById("setPickerSave")?.addEventListener("click", () => savePartitions(setId, selected).catch(opts.onError));
}

async function savePartitions(setId: string, initiallySelected: Set<string>): Promise<void> {
  const checked = new Set<string>();
  document.querySelectorAll<HTMLInputElement>("#setPickerList input[type=checkbox]").forEach((input) => {
    if (input.checked) checked.add(input.value);
  });
  const add = [...checked].filter((name) => !initiallySelected.has(name));
  const remove = [...initiallySelected].filter((name) => !checked.has(name));
  await fetchJSON(`/api/sets/${encodeURIComponent(setId)}/partitions`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ add, remove }),
  });
  closeModal();
  opts.onToast("Set members updated.", "success");
  await refreshSets();
}

function openModal(html: string): void {
  closeModal();
  const root = document.getElementById("setModalRoot");
  if (!root) return;
  setModal = document.createElement("div");
  setModal.className = "set-modal-backdrop";
  setModal.innerHTML = `<div class="set-modal" role="dialog" aria-modal="true">${html}</div>`;
  root.appendChild(setModal);
  setModal.addEventListener("click", (event) => {
    if (event.target === setModal || (event.target as HTMLElement).closest("[data-modal-close]")) closeModal();
  });
}

function closeModal(): void {
  setModal?.remove();
  setModal = null;
}
