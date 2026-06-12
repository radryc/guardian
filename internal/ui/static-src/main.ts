import "./main.css";
import { AppState, createState } from "./state";
import {
  fetchJSON,
  setSync,
  setText,
  escapeHtml,
  escapeAttr,
  formatDateTime,
  fromDateTimeLocalValue,
  humanize,
  clone,
  nextName,
  toast,
  toDateTimeLocalValue,
  truncate,
} from "./utils";
import { renderTopology, renderTopologyLegend } from "./topology";

// ── Bootstrap ────────────────────────────────────────
const state: AppState = createState();
const REFRESH_BASE_MS = 20_000;
const REFRESH_FAST_MS = 4_000;
const REFRESH_HIDDEN_MS = 60_000;
const FAST_REFRESH_BURST_MS = 60_000;

document.addEventListener("DOMContentLoaded", () => {
  hydrateStateFromLocation();
  wireEvents();
  bootstrap().catch(handleError);
});

async function bootstrap(): Promise<void> {
  activatePanel(state.activePanel);
  await refreshOverview();
  scheduleNextOverviewRefresh();
}

function scheduleNextOverviewRefresh(): void {
  if (state.refreshTimer !== undefined) {
    window.clearTimeout(state.refreshTimer);
  }
  state.refreshTimer = window.setTimeout(async () => {
    try {
      await refreshOverview(state.activePanel !== "historyPanel");
    } catch {
      // Keep polling after transient fetch errors.
    } finally {
      scheduleNextOverviewRefresh();
    }
  }, nextOverviewRefreshDelayMs());
}

function nextOverviewRefreshDelayMs(): number {
  if (document.hidden) {
    return REFRESH_HIDDEN_MS;
  }
  if (Date.now() < state.fastRefreshUntil || hasActivePartitionTransitions()) {
    return REFRESH_FAST_MS;
  }
  return REFRESH_BASE_MS;
}

function hasActivePartitionTransitions(): boolean {
  const detail = state.detail;
  if (!detail) {
    return false;
  }
  if (String(detail?.health?.status ?? "").toLowerCase() === "pending") {
    return true;
  }
  const intents = Array.isArray(detail?.intents) ? detail.intents : [];
  for (const intent of intents) {
    switch (String(intent?.status ?? "")) {
      case "Checking":
      case "Diffing":
      case "Applying":
      case "Destroying":
      case "Ready":
      case "Blocked":
        return true;
      default:
        break;
    }
  }
  const services = Array.isArray(detail?.health?.services) ? detail.health.services : [];
  return services.some((svc: any) => svc?.taskActive === true);
}

function armFastRefreshBurst(durationMs = FAST_REFRESH_BURST_MS): void {
  const until = Date.now() + durationMs;
  if (until > state.fastRefreshUntil) {
    state.fastRefreshUntil = until;
  }
}

// ── Overview ─────────────────────────────────────────
async function refreshOverview(loadSelected = true): Promise<void> {
  setSync("Refreshing…");
  state.overview = await fetchJSON("/api/overview");
  renderSummary();
  renderPartitionBrowser();

  const current = state.selectedPartition;
  const names = (state.overview?.partitions ?? []).map((p: any) => p.name as string);

  if (!current && names.length > 0) {
    await selectPartition(names[0], false);
  } else if (current && names.includes(current) && loadSelected) {
    await selectPartition(current, false);
  } else if (!names.length) {
    state.selectedPartition = "";
    state.detail = null;
    state.history = null;
    state.rollouts = null;
    state.expandedRolloutKeys = {};
    renderPartitionDetail();
    renderHistory();
    renderRollouts();
    renderTopologyPanel();
    syncLocationState();
  }
  setSync("Updated just now");
}

async function selectPartition(name: string, announce = true): Promise<void> {
  if (!name) return;
  armFastRefreshBurst();
  const samePartition = state.selectedPartition === name;
  state.selectedPartition = name;
  state.activityDrawer = { intentName: "", data: null, loading: false, error: "" };
  if (!samePartition) {
    state.expandedAssetKey = "";
    state.expandedRolloutKeys = {};
    state.diagnosticDetails = {};
    state.history = null;
    state.historyLoading = false;
    state.historyError = "";
    state.rollouts = null;
    state.rolloutsLoading = false;
    state.rolloutsError = "";
  }
  syncLocationState();
  renderPartitionBrowser();
  state.detail = await fetchJSON(`/api/partitions/${encodeURIComponent(name)}`);
  renderPartitionDetail();
  renderHistory();
  renderRollouts();
  renderTopologyPanel();
  renderPageChrome();
  if (state.activePanel === "historyPanel") {
    ensureHistoryLoaded().catch(handleError);
  }
  if (state.activePanel === "rolloutsPanel") {
    ensureRolloutsLoaded(samePartition).catch(handleError);
  }
}

// ── Panel activation ─────────────────────────────────
function activatePanel(panelId: string): void {
  state.activePanel = panelId;
  syncLocationState();

  document.querySelectorAll(".panel").forEach((panel) => {
    const isActive = panel.id === panelId;
    panel.classList.toggle("active", isActive);
    panel.classList.toggle("hidden", !isActive);
  });
  document.querySelectorAll("[data-tab-target]").forEach((btn) => {
    btn.classList.toggle("active", (btn as HTMLElement).dataset.tabTarget === panelId);
  });

  renderPartitionBrowser();
  renderPartitionDetail();
  renderHistory();
  renderRollouts();
  renderPageChrome();

  if (panelId === "historyPanel" && state.selectedPartition) {
    ensureHistoryLoaded().catch(handleError);
  }
  if (panelId === "rolloutsPanel" && state.selectedPartition) {
    ensureRolloutsLoaded().catch(handleError);
  }
}

// ── URL state ─────────────────────────────────────────
function hydrateStateFromLocation(): void {
  const params = new URLSearchParams(window.location.search);
  const partition = params.get("partition");
  if (partition) state.selectedPartition = partition.trim();
  const panel = params.get("panel");
  if (["overviewPanel", "topologyPanel", "rolloutsPanel", "historyPanel"].includes(panel ?? "")) {
    state.activePanel = panel!;
  }
  const rawLimit = Number.parseInt(params.get("historyLimit") ?? "", 10);
  if (Number.isFinite(rawLimit) && rawLimit > 0) state.historyOptions.limit = rawLimit;
  const since = params.get("historySince");
  if (since) state.historyOptions.since = since;
  const until = params.get("historyUntil");
  if (until) state.historyOptions.until = until;
  syncHistoryControlsFromState();
}

function syncLocationState(): void {
  const params = new URLSearchParams(window.location.search);
  if (state.selectedPartition) params.set("partition", state.selectedPartition);
  else params.delete("partition");
  if (state.activePanel && state.activePanel !== "overviewPanel") params.set("panel", state.activePanel);
  else params.delete("panel");
  if (state.historyOptions.limit !== 10) params.set("historyLimit", String(state.historyOptions.limit));
  else params.delete("historyLimit");
  if (state.historyOptions.since) params.set("historySince", state.historyOptions.since);
  else params.delete("historySince");
  if (state.historyOptions.until) params.set("historyUntil", state.historyOptions.until);
  else params.delete("historyUntil");
  const query = params.toString();
  window.history.replaceState(null, "", `${window.location.pathname}${query ? `?${query}` : ""}`);
}

async function fetchPartitionHistory(name: string): Promise<any> {
  const params = new URLSearchParams();
  params.set("limit", String(state.historyOptions.limit));
  if (state.historyOptions.since) params.set("since", state.historyOptions.since);
  if (state.historyOptions.until) params.set("until", state.historyOptions.until);
  return fetchJSON(`/api/partitions/${encodeURIComponent(name)}/history?${params.toString()}`);
}

async function fetchPartitionRollouts(name: string): Promise<any> {
  return fetchJSON(`/api/partitions/${encodeURIComponent(name)}/rollouts`);
}

function syncHistoryControlsFromState(): void {
  const limitInput = document.getElementById("historyLimit") as HTMLInputElement | null;
  if (limitInput) limitInput.value = String(state.historyOptions.limit);
  const sinceInput = document.getElementById("historySince") as HTMLInputElement | null;
  if (sinceInput) sinceInput.value = toDateTimeLocalValue(state.historyOptions.since);
  const untilInput = document.getElementById("historyUntil") as HTMLInputElement | null;
  if (untilInput) untilInput.value = toDateTimeLocalValue(state.historyOptions.until);
}

function readHistoryControlsIntoState(): void {
  const limitInput = document.getElementById("historyLimit") as HTMLInputElement | null;
  const parsedLimit = Number.parseInt(limitInput?.value ?? "", 10);
  state.historyOptions.limit = Number.isFinite(parsedLimit) && parsedLimit > 0 ? parsedLimit : 10;
  const sinceInput = document.getElementById("historySince") as HTMLInputElement | null;
  state.historyOptions.since = fromDateTimeLocalValue(sinceInput?.value ?? "");
  const untilInput = document.getElementById("historyUntil") as HTMLInputElement | null;
  state.historyOptions.until = fromDateTimeLocalValue(untilInput?.value ?? "");
}

async function applyHistoryFilters(): Promise<void> {
  readHistoryControlsIntoState();
  syncLocationState();
  if (!state.selectedPartition) {
    renderHistory();
    return;
  }
  await ensureHistoryLoaded(true);
}

async function resetHistoryFilters(): Promise<void> {
  state.historyOptions = { limit: 10, since: "", until: "" };
  syncHistoryControlsFromState();
  await applyHistoryFilters();
}

async function ensureHistoryLoaded(force = false): Promise<void> {
  if (!state.selectedPartition) {
    state.history = null;
    state.historyLoading = false;
    state.historyError = "";
    renderHistory();
    renderPageChrome();
    return;
  }
  if (state.historyLoading) {
    return;
  }
  if (!force && state.history) {
    renderHistory();
    renderPageChrome();
    return;
  }

  state.historyLoading = true;
  state.historyError = "";
  renderHistory();
  renderPageChrome();
  try {
    state.history = await fetchPartitionHistory(state.selectedPartition);
  } catch (error: any) {
    state.history = null;
    state.historyError = error?.message ?? "Failed to load history.";
    throw error;
  } finally {
    state.historyLoading = false;
    renderHistory();
    renderPageChrome();
  }
}

async function ensureRolloutsLoaded(force = false): Promise<void> {
  if (!state.selectedPartition) {
    state.rollouts = null;
    state.rolloutsLoading = false;
    state.rolloutsError = "";
    renderRollouts();
    renderPageChrome();
    return;
  }
  if (state.rolloutsLoading) {
    return;
  }
  if (!force && state.rollouts) {
    renderRollouts();
    renderPageChrome();
    return;
  }

  state.rolloutsLoading = true;
  state.rolloutsError = "";
  renderRollouts();
  renderPageChrome();
  try {
    state.rollouts = await fetchPartitionRollouts(state.selectedPartition);
  } catch (error: any) {
    state.rollouts = null;
    state.rolloutsError = error?.message ?? "Failed to load rollouts.";
    throw error;
  } finally {
    state.rolloutsLoading = false;
    renderRollouts();
    renderPageChrome();
  }
}

// ── Render: summary strip ─────────────────────────────
function renderSummary(): void {
  const s = state.overview?.summary ?? {};
  setText("summaryPartitions", s.partitions ?? 0);
  setText("summaryIntents", s.intents ?? 0);
  setText("summaryAssets", s.assets ?? 0);
  setText("summaryStable", s.healthyAssets ?? s.servicesHealthy ?? 0);
  setText("summaryAttention", s.attentionAssets ?? s.servicesAttention ?? 0);
  setText("summaryFailed", s.failingAssets ?? s.failedIntents ?? 0);
}

// ── Render: partition browser (sidebar vs app-grid) ───
function renderPartitionBrowser(): void {
  const showOverviewChooser = state.activePanel === "overviewPanel" && !state.selectedPartition;

  renderPartitionList();
  if (showOverviewChooser) {
    renderAppGrid();
  } else {
    clearAppGrid();
  }
}

function clearPartitionList(): void {
  const el = document.getElementById("partitionList");
  if (el) { el.className = "grid gap-1"; el.innerHTML = ""; }
}

function clearAppGrid(): void {
  const el = document.getElementById("appGrid");
  if (el) { el.className = "grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5"; el.innerHTML = ""; }
}

function renderPartitionList(): void {
  const container = document.getElementById("partitionList");
  if (!container) return;
  if (!state.overview) {
    container.className = "grid gap-1 loading-state text-sm text-[#566778]";
    container.textContent = "Loading partitions…";
    return;
  }
  const search = (document.getElementById("partitionSearch") as HTMLInputElement)?.value.trim().toLowerCase() ?? "";
  const partitions = (state.overview?.partitions ?? []).filter((p: any) => {
    if (!search) return true;
    return `${p.name} ${Object.keys(p.labels ?? {}).join(" ")} ${Object.values(p.labels ?? {}).join(" ")}`.toLowerCase().includes(search);
  });
  if (!partitions.length) {
    container.className = "grid gap-1 empty-state text-sm text-[#566778]";
    container.textContent = "No partitions available.";
    return;
  }
  container.className = "grid gap-1";
  container.innerHTML = partitions.map((item: any) => {
    const active = item.name === state.selectedPartition;
    const diagnostic = joinDiagnosticLines([
      item.errors?.join("\n"),
      item.lastDisplayStatus ? `Last known status: ${item.lastDisplayStatus}` : "",
    ]);
    return `
      <button class="partition-list-item ${active ? "active" : ""}" data-partition="${escapeAttr(item.name)}">
        <div class="partition-list-title">
          <strong>${escapeHtml(item.name)}</strong>
          ${renderBadge(item.health, item.displayStatus, `${item.name} status`, diagnostic, `partition:${item.name}`)}
        </div>
        <div class="partition-list-meta">
          <span>${item.intentCount ?? 0} intents</span>
          <span>${item.assetCount ?? 0} assets</span>
          <span>${item.healthyAssets ?? item.servicesHealthy ?? 0} stable</span>
        </div>
      </button>
    `;
  }).join("");
  container.querySelectorAll<HTMLElement>("[data-partition]").forEach((btn) => {
    btn.addEventListener("click", () => selectPartition(btn.dataset.partition!).catch(handleError));
  });
}

function renderAppGrid(): void {
  const container = document.getElementById("appGrid");
  if (!container) return;
  const search = ((document.getElementById("appGridSearch") as HTMLInputElement)?.value ?? "").trim().toLowerCase();
  const partitions = (state.overview?.partitions ?? []).filter((p: any) => {
    if (!search) return true;
    return `${p.name} ${Object.values(p.labels ?? {}).join(" ")}`.toLowerCase().includes(search);
  });
  if (!partitions.length) {
    container.className = "grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5 empty-state text-sm text-[#566778]";
    container.textContent = state.overview ? "No partitions match the filter." : "Loading partitions…";
    return;
  }
  container.className = "grid grid-cols-[repeat(auto-fill,minmax(230px,1fr))] gap-2.5";
  container.innerHTML = partitions.map((item: any) => {
    const active = item.name === state.selectedPartition;
    const labels = tileLabelEntries(item.labels ?? {});
    return `
      <button class="app-tile ${active ? "active" : ""}" data-partition="${escapeAttr(item.name)}" data-health="${escapeAttr(item.health ?? "neutral")}">
        <div class="app-tile-body">
          <div class="app-tile-name">${escapeHtml(item.name)}</div>
          ${labels.length ? `<div class="app-tile-labels">${labels.map((l: string) => `<span class="app-tile-label">${escapeHtml(l)}</span>`).join("")}</div>` : ""}
          <div class="app-tile-status-row">
            <span class="status-row">
              <span class="status-dot status-dot-${escapeAttr(item.health ?? "neutral")}"></span>
              <span>${escapeHtml(item.displayStatus ?? humanize(item.health ?? "neutral"))}</span>
            </span>
          </div>
          <div class="app-tile-meta">
            <span class="app-tile-meta-item">${item.intentCount ?? 0} intents</span>
            <span class="app-tile-meta-item">${item.assetCount ?? 0} assets</span>
            <span class="app-tile-meta-item">${item.healthyAssets ?? item.servicesHealthy ?? 0} healthy</span>
          </div>
        </div>
      </button>
    `;
  }).join("");
  container.querySelectorAll<HTMLElement>("[data-partition]").forEach((tile) => {
    tile.addEventListener("click", () => selectPartition(tile.dataset.partition!).catch(handleError));
  });
}

// ── Render: partition detail ──────────────────────────
function renderPartitionDetail(): void {
  const detail = state.detail;
  const hero = document.getElementById("heroContent");
  if (!hero) return;

  if (state.activePanel !== "overviewPanel") {
    renderPageChrome();
    return;
  }

  if (!detail) {
    hero.className = "loading-state text-sm text-[#566778]";
    hero.textContent = "Select a partition to inspect its current shape.";
    (["intentCards","attentionAssetsList","serviceHealthCards","recentEventsList"] as const).forEach((id) => {
      const el = document.getElementById(id);
      if (el) { el.className = "loading-state text-sm text-[#566778]"; el.textContent = "Choose a partition."; }
    });
    renderPageChrome();
    return;
  }

  const health = detail.health ?? {};
  const partitionLabels = { ...(detail.partition?.manifest?.metadata?.labels ?? {}), ...(detail.partition?.manifest?.spec?.labels ?? {}) };
  hero.className = "";
  hero.innerHTML = `
    <div class="hero-grid">
      <div class="hero-main">
        <div class="pill-row mb-2">
          ${renderBadge(health.status, health.displayStatus)}
          ${partitionLabels.role ? `<span class="pill">${escapeHtml(partitionLabels.role)}</span>` : ""}
          ${partitionLabels.component ? `<span class="pill">${escapeHtml(partitionLabels.component)}</span>` : ""}
          ${partitionLabels.stack ? `<span class="pill">${escapeHtml(partitionLabels.stack)}</span>` : ""}
          <span class="pill">${escapeHtml(detail.partition.manifest.spec?.deletionPolicy ?? "orphan")} deletion</span>
          <span class="pill">${escapeHtml(detail.partition.manifest.spec?.reconciliation?.mode ?? "manual")} reconcile</span>
          ${detail.compilerError ? `<span class="badge badge-failing">Compiler warning</span>` : ""}
        </div>
        <h2>${escapeHtml(detail.partition.manifest.metadata.name)}</h2>
        <p>${escapeHtml(health.summary ?? "Partition summary unavailable.")}</p>
        ${health.status === "pending" ? renderProgressingInfo(detail) : ""}
        <div class="pill-row mt-2">
          ${partitionLabels.endpoint ? `<span class="pill">${escapeHtml(partitionLabels.endpoint)}</span>` : ""}
          ${partitionLabels.topology ? `<span class="pill">${escapeHtml(partitionLabels.topology)}</span>` : ""}
          ${partitionLabels.managedBy ? `<span class="pill">${escapeHtml(partitionLabels.managedBy)}</span>` : ""}
          ${(detail.partition.state?.errors ?? []).map((e: string) => `<span class="pill">${escapeHtml(e)}</span>`).join("")}
          ${detail.compilerError ? `<span class="pill">${escapeHtml(detail.compilerError)}</span>` : ""}
        </div>
      </div>
      ${heroMetric("Healthy", health.healthy ?? 0)}
      ${heroMetric("Attention", (health.attention ?? 0) + (health.pending ?? 0))}
      ${heroMetric("Failing", health.failing ?? 0)}
    </div>
  `;

  renderAttentionAssets();
  renderIntentCards();
  renderServiceHealth();
  renderRecentEvents();
  renderPageChrome();
}

function heroMetric(label: string, value: number): string {
  return `
    <div class="stat-card rounded-lg border border-white/[0.09]">
      <div class="stat-label">${escapeHtml(label)}</div>
      <div class="stat-value">${value}</div>
    </div>
  `;
}

// ── Render: attention assets ──────────────────────────
function renderAttentionAssets(): void {
  const container = document.getElementById("attentionAssetsList");
  if (!container) return;
  const flagged = collectFlaggedAssets();
  const pending = collectPendingAssets();
  if (!flagged.length && !pending.length) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = "No assets need attention right now.";
    return;
  }
  container.className = "attention-asset-list";
  const flaggedHtml = flagged.map(({ intent, asset }: any) => {
    const displaySummary = assetSummaryForDisplay(asset);
    return `
    <article class="attention-asset-card attention-asset-card-${escapeAttr(asset.health)}">
      <div class="attention-asset-card-header">
        <div>
          <h3>${escapeHtml(asset.name)}</h3>
          <div class="muted">${escapeHtml(intent.name)} · ${escapeHtml(assetTitle(asset.type))}</div>
        </div>
        ${renderBadge(asset.health, asset.displayStatus, `${intent.name} / ${asset.name}`, asset.summary, `asset:${state.selectedPartition}:${intent.name}:${asset.name}`)}
      </div>
      <p class="muted mt-1">${escapeHtml(displaySummary)}</p>
      <div class="pill-row mt-2">
        <span class="pill">${escapeHtml(intent.targetSummary ?? "Unassigned")}</span>
        ${(asset.quickFacts ?? []).slice(0, 3).map((f: any) => `<span class="${f.label === "Release" ? "pill pill-release" : "pill"}">${escapeHtml(`${f.label}: ${f.value}`)}</span>`).join("")}
      </div>
    </article>
  `;
  }).join("");
  const pendingHtml = pending.length ? `
    <div class="progressing-asset-list mt-2">
      <div class="progressing-assets-header">Progressing — awaiting first reconcile (${pending.length})</div>
      ${pending.map(({ intent, asset }: any) => {
        const displaySummary = assetSummaryForDisplay(asset);
        return `
        <div class="progressing-asset-item">
          <div>
            <div>${escapeHtml(asset.name)}</div>
            <div class="muted">${escapeHtml(intent.name)} · ${escapeHtml(assetTitle(asset.type))}${displaySummary ? ` · ${escapeHtml(displaySummary)}` : ""}</div>
          </div>
          ${renderBadge(asset.health, asset.displayStatus, `${intent.name} / ${asset.name}`, asset.summary, `asset:${state.selectedPartition}:${intent.name}:${asset.name}`)}
        </div>
      `;
      }).join("")}
    </div>
  ` : "";
  container.innerHTML = flaggedHtml + pendingHtml;
}

function collectFlaggedAssets(): any[] {
  return (state.detail?.intents ?? [])
    .flatMap((intent: any) => (intent.assets ?? []).map((asset: any) => ({ intent, asset })))
    .filter(({ asset }: any) => asset?.health === "failing" || asset?.health === "attention")
    .sort((a: any, b: any) => {
      const sev = attentionSeverityRank(a.asset.health) - attentionSeverityRank(b.asset.health);
      if (sev !== 0) return sev;
      return a.intent.name !== b.intent.name ? a.intent.name.localeCompare(b.intent.name) : a.asset.name.localeCompare(b.asset.name);
    });
}

function collectPendingAssets(): any[] {
  return (state.detail?.intents ?? [])
    .flatMap((intent: any) => (intent.assets ?? []).map((asset: any) => ({ intent, asset })))
    .filter(({ asset }: any) => asset?.health === "pending")
    .sort((a: any, b: any) => a.intent.name !== b.intent.name ? a.intent.name.localeCompare(b.intent.name) : a.asset.name.localeCompare(b.asset.name));
}

function assetSummaryForDisplay(asset: any): string {
  const summary = String(asset?.summary ?? "").trim();
  const observed = String(asset?.observedHealth?.summary ?? "").trim();
  const status = String(asset?.status ?? "");
  if ((status === "Drifted" || status === "DriftedLocked") && observed) {
    if (summary.includes(observed)) {
      return summary;
    }
    return summary ? `${summary}: ${observed}` : observed;
  }
  return summary;
}

function attentionSeverityRank(status: string): number {
  if (status === "failing") return 0;
  if (status === "attention") return 1;
  return 2;
}

// ── Render: intent cards ──────────────────────────────
function renderIntentCards(): void {
  const container = document.getElementById("intentCards");
  if (!container) return;
  const intents = state.detail?.intents ?? [];
  if (!intents.length) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = "No intents defined for this partition yet.";
    return;
  }
  container.className = "intent-stack";
  container.innerHTML = intents.map((intent: any) => {
    const catSummary = summarizeIntentCategories(intent.assets ?? []);
    const activityOpen = state.activityDrawer.intentName === intent.name;
    const assetGroups = groupAssetsByCategory(intent.assets ?? []);
    const assetsHtml = assetGroups.map((group: any) => `
      <section class="intent-asset-group">
        <div class="intent-asset-group-title">
          <span class="intent-lane-group-dot" style="background:${categoryAccent(group.category)}"></span>
          <span>${escapeHtml(group.category)} · ${group.assets.length}</span>
        </div>
        <div class="asset-grid">
          ${group.assets.map((asset: any) => {
            const displaySummary = assetSummaryForDisplay(asset);
            const assetKey = makeAssetKey(intent.name, asset.name);
            const expanded = state.expandedAssetKey === assetKey;
            const cat = assetCategory(asset.type);
            const detailId = `asset-detail-${makeAssetDomKey(assetKey)}`;
            const quickFacts = [...(asset.quickFacts ?? [])]
              .sort((a: any, b: any) => (a.label === "Release" ? -1 : b.label === "Release" ? 1 : 0))
              .map((f: any) => `<span class="fact${factClass(f.label)}" title="${escapeAttr(factTitle(f.label))}">${escapeHtml(f.label)}: ${escapeHtml(f.value)}</span>`)
              .join("");
            const propertyFacts = expanded ? renderAssetPropertyFacts(asset, { limit: Number.MAX_SAFE_INTEGER, truncateAt: 160 }) : "";
            const outputFacts = expanded ? renderOutputFacts(asset.outputs ?? {}, [], { limit: Number.MAX_SAFE_INTEGER, truncateAt: 160 }) : "";
            const referenceFacts = expanded
              ? (asset.references ?? []).map((r: string) => `<span class="fact">${escapeHtml(r)}</span>`).join("")
              : "";
            const dependencyFacts = expanded && (asset.dependsOn ?? []).length
              ? (asset.dependsOn ?? []).map((dep: string) => `<span class="fact">${escapeHtml(dep)}</span>`).join("")
              : "";
            return `
              <article
                class="asset-chip asset-chip-${escapeAttr(asset.health ?? "neutral")}${expanded ? " asset-chip-expanded" : ""}"
                data-asset-toggle="${escapeAttr(assetKey)}"
                data-asset-card="${escapeAttr(makeAssetDomKey(assetKey))}"
                role="button"
                tabindex="0"
                aria-expanded="${expanded ? "true" : "false"}"
                aria-controls="${escapeAttr(detailId)}"
              >
                <div class="asset-chip-top">
                  <div>
                    <div class="asset-chip-title">${escapeHtml(asset.name)}</div>
                    <div class="asset-chip-type-row">
                      <span class="asset-chip-type">${escapeHtml(assetTitle(asset.type))}</span>
                      <span class="asset-chip-category">${escapeHtml(cat)}</span>
                    </div>
                  </div>
                  ${renderBadge(asset.health, asset.displayStatus, `${intent.name} / ${asset.name}`, asset.summary, `asset:${state.selectedPartition}:${intent.name}:${asset.name}`)}
                </div>
                ${displaySummary ? `<div class="muted mt-1">${escapeHtml(displaySummary)}</div>` : ""}
                ${quickFacts ? `<div class="fact-row">${quickFacts}</div>` : ""}
                <div class="asset-chip-toggle-row">
                  <span class="asset-chip-toggle-copy">${expanded ? "Hide full asset details" : "Show image, mounts, outputs, and manifest details"}</span>
                  <span class="asset-chip-toggle-indicator" aria-hidden="true">${expanded ? "−" : "+"}</span>
                </div>
                ${expanded ? `
                  <div class="asset-chip-details" id="${escapeAttr(detailId)}">
                    ${dependencyFacts ? `<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Depends on</div><div class="fact-row">${dependencyFacts}</div></div>` : ""}
                    ${propertyFacts ? `<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Manifest details</div><div class="fact-row">${propertyFacts}</div></div>` : ""}
                    ${outputFacts ? `<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Outputs</div><div class="fact-row">${outputFacts}</div></div>` : ""}
                    ${referenceFacts ? `<div class="asset-chip-detail-block"><div class="asset-chip-detail-heading">Output refs</div><div class="fact-row">${referenceFacts}</div></div>` : ""}
                  </div>
                ` : ""}
              </article>
            `;
          }).join("")}
        </div>
      </section>
    `).join("");

    return `
      <article class="intent-card">
        <div class="intent-card-header">
          <div>
            <h3>${escapeHtml(intent.name)}</h3>
            <div class="muted">${escapeHtml(intent.summary ?? "")}</div>
            <div class="pill-row mt-2">
              ${renderBadge(intent.health, intent.displayStatus, `${intent.name} intent`, intent.summary, `intent:${state.selectedPartition}:${intent.name}`)}
              <span class="pill">${escapeHtml(intent.targetSummary ?? "Unassigned")}</span>
              ${(intent.joined ?? []).map((j: string) => `<span class="pill">joins ${escapeHtml(j)}</span>`).join("")}
              ${catSummary.map((g: any) => `<span class="pill">${escapeHtml(`${g.category} ${g.count}`)}</span>`).join("")}
              ${intent.locked ? `<span class="pill">locked</span>` : ""}
              <button class="activity-btn ${activityOpen ? "active" : ""}" type="button" data-activity-intent="${escapeAttr(intent.name)}">&#9685;</button>
            </div>
          </div>
        </div>
        ${activityOpen ? renderActivityDrawer() : ""}
        ${assetsHtml}
      </article>
    `;
  }).join("");

  container.querySelectorAll<HTMLElement>("[data-activity-intent]").forEach((btn) => {
    btn.addEventListener("click", () => toggleActivityDrawer(btn.dataset.activityIntent!).catch(handleError));
  });
  container.querySelectorAll<HTMLElement>("[data-asset-toggle]").forEach((card) => {
    card.addEventListener("click", () => toggleExpandedAsset(card.dataset.assetToggle ?? ""));
    card.addEventListener("keydown", (event) => {
      if (event.key !== "Enter" && event.key !== " ") {
        return;
      }
      event.preventDefault();
      toggleExpandedAsset(card.dataset.assetToggle ?? "");
    });
  });
}

function makeAssetKey(intentName: string, assetName: string): string {
  return `${intentName}::${assetName}`;
}

function makeAssetDomKey(assetKey: string): string {
  return assetKey.replace(/[^a-zA-Z0-9_-]+/g, "-");
}

function toggleExpandedAsset(assetKey: string): void {
  if (!assetKey) {
    return;
  }
  state.expandedAssetKey = state.expandedAssetKey === assetKey ? "" : assetKey;
  renderIntentCards();
}

function focusAssetInIntentMap(assetKey: string): void {
  if (!assetKey) {
    return;
  }
  state.expandedAssetKey = assetKey;
  renderIntentCards();
  const domKey = makeAssetDomKey(assetKey);
  requestAnimationFrame(() => {
    document.querySelector<HTMLElement>(`[data-asset-card="${domKey}"]`)?.scrollIntoView({
      behavior: "smooth",
      block: "center",
      inline: "nearest",
    });
  });
}

async function toggleActivityDrawer(intentName: string): Promise<void> {
  if (state.activityDrawer.intentName === intentName) {
    state.activityDrawer = { intentName: "", data: null, loading: false, error: "" };
    renderIntentCards();
    return;
  }
  state.activityDrawer = { intentName, data: null, loading: true, error: "" };
  renderIntentCards();
  try {
    const partition = state.selectedPartition;
    const data = await fetchJSON(`/api/partitions/${encodeURIComponent(partition)}/intents/${encodeURIComponent(intentName)}/activity`);
    state.activityDrawer = { intentName, data, loading: false, error: "" };
  } catch (err: any) {
    state.activityDrawer = { intentName, data: null, loading: false, error: err.message ?? "Failed to load activity" };
  }
  renderIntentCards();
}

function renderActivityDrawer(): string {
  const { data, loading, error } = state.activityDrawer;
  if (loading) return `<div class="activity-drawer"><div class="activity-loading">Loading activity…</div></div>`;
  if (error) return `<div class="activity-drawer"><div class="activity-error">${escapeHtml(error)}</div></div>`;
  if (!data) return `<div class="activity-drawer"><div class="activity-loading">No activity data.</div></div>`;

  const ts = data.timestamps ?? {};
  const tsRows = [
    { label: "Queued", value: ts.lastQueuedAt },
    { label: "Check", value: ts.lastCheckAt },
    { label: "Diff", value: ts.lastDiffAt },
    { label: "Apply", value: ts.lastApplyAt },
  ].filter((t) => t.value && t.value !== "0001-01-01T00:00:00Z");
  const logs = data.logs ?? [];
  const drift = data.drift;

  return `
    <div class="activity-drawer">
      <div class="activity-header">
        <span class="activity-header-title">Activity log</span>
        ${data.lastOp ? `<span class="activity-op-badge">last op: ${escapeHtml(data.lastOp)}</span>` : ""}
        ${data.lastTaskID ? `<span class="activity-task-id">${escapeHtml(data.lastTaskID.slice(0, 16))}…</span>` : ""}
      </div>
      ${tsRows.length ? `
        <div class="activity-timestamps">
          ${tsRows.map((t) => `<span class="activity-ts-item"><span class="activity-ts-label">${escapeHtml(t.label)}</span> ${formatDateTime(t.value)}</span>`).join("")}
        </div>` : ""}
      ${data.lastError ? `<div class="activity-error-row"><span class="activity-error-label">Error:</span> ${escapeHtml(data.lastError)}</div>` : ""}
      ${drift ? `<div class="activity-drift">
        <span class="activity-drift-label">Drift:</span> ${escapeHtml(drift.summary ?? drift.status ?? "")}
        ${(drift.changedAssets ?? []).length ? `<span class="activity-drift-assets">${drift.changedAssets.map((a: string) => escapeHtml(a)).join(", ")}</span>` : ""}
      </div>` : ""}
      ${logs.length ? `
        <div class="activity-logs-label">Logs (${logs.length})</div>
        <div class="activity-logs">${logs.map((log: any) => {
          const lvl = (log.level ?? "info").toLowerCase();
          const assetPfx = log.asset ? `[${escapeHtml(log.asset)}] ` : "";
          const tsStr = log.timestamp ? formatDateTime(log.timestamp) + " " : "";
          return `<div class="activity-log-entry ${escapeAttr(lvl)}">${tsStr}<span class="activity-log-level">${escapeHtml(log.level ?? "info")}</span> ${assetPfx}${escapeHtml(log.message ?? "")}</div>`;
        }).join("")}</div>`
      : `<div class="activity-no-logs">No logs from last task result.</div>`}
    </div>
  `;
}

// ── Render: service health ────────────────────────────
function renderServiceHealth(): void {
  const container = document.getElementById("serviceHealthCards");
  if (!container) return;
  const services = state.detail?.health?.services ?? [];
  if (!services.length) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = "No service-like assets to score yet.";
    return;
  }
  const healthyCount = services.filter((svc: any) => svc.status === "healthy").length;
  const attentionCount = services.filter((svc: any) => svc.status === "attention").length;
  const failingCount = services.filter((svc: any) => svc.status === "failing").length;
  const activeCount = services.filter((svc: any) => svc.taskActive).length;
  const timedOutCount = services.filter((svc: any) => svc.taskTimedOut).length;
  container.className = "service-stack";
  container.innerHTML = `
    <div class="service-health-summary">
      <span class="pill">${services.length} services</span>
      ${healthyCount ? `<span class="pill">stable ${healthyCount}</span>` : ""}
      ${attentionCount ? `<span class="pill">attention ${attentionCount}</span>` : ""}
      ${failingCount ? `<span class="pill">failing ${failingCount}</span>` : ""}
      ${activeCount ? `<span class="pill">reconciling ${activeCount}</span>` : ""}
      ${timedOutCount ? `<span class="pill">timed out ${timedOutCount}</span>` : ""}
    </div>
    ${services.map((svc: any) => `
    <article class="service-card service-card-${escapeAttr(svc.status ?? "neutral")}">
      <div class="service-card-header">
        <div>
          <h3>${escapeHtml(svc.asset)}</h3>
          <div class="muted">${escapeHtml(svc.intent)} · ${escapeHtml(assetTitle(svc.type))}</div>
        </div>
        ${renderBadge(svc.status, svc.displayStatus, `${svc.intent} / ${svc.asset}`, svc.summary, `service:${state.selectedPartition}:${svc.intent}:${svc.asset}`)}
      </div>
      <p class="service-card-note">${escapeHtml(serviceHealthNote(svc))}</p>
      <div class="service-health-meta">
        ${renderServiceHealthMeta(svc)}
      </div>
      <div class="service-card-actions">
        <button class="btn-secondary service-card-action" type="button" data-service-focus="${escapeAttr(makeAssetKey(svc.intent, svc.asset))}">Open details</button>
      </div>
    </article>
  `).join("")}
  `;
  container.querySelectorAll<HTMLElement>("[data-service-focus]").forEach((button) => {
    button.addEventListener("click", () => focusAssetInIntentMap(button.dataset.serviceFocus ?? ""));
  });
}

function serviceHealthNote(service: any): string {
  if (service.taskTimedOut) {
    return "Last reconcile task timed out. Open details in the intent map for config and outputs.";
  }
  if (service.taskActive) {
    return "Reconcile is currently running for this service.";
  }
  switch (service.status) {
    case "healthy":
      return "Operational summary only. Configuration and ports stay in the intent map.";
    case "pending":
      return "Waiting for the first successful reconcile.";
    case "attention":
      return String(service.summary ?? "Needs attention.");
    case "failing":
      return String(service.summary ?? "Service is failing.");
    default:
      return String(service.summary ?? "Service status unavailable.");
  }
}

function renderServiceHealthMeta(service: any): string {
  const items: string[] = [];
  if (service.taskActive) {
    items.push("reconcile running");
  }
  if (service.taskTimedOut) {
    items.push("last task timed out");
  }
  const updatedAt = formatDateTime(service.lastUpdatedAt);
  if (updatedAt !== "—") {
    items.push(`updated ${updatedAt}`);
  }
  return items.map((item) => `<span class="service-health-meta-item">${escapeHtml(item)}</span>`).join("");
}

// ── Render: recent events ─────────────────────────────
function renderRecentEvents(): void {
  const container = document.getElementById("recentEventsList");
  if (!container) return;
  const events = state.detail?.recentEvents ?? [];
  if (!events.length) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = "Recent event history loads only in the History tab.";
    return;
  }
  container.className = "timeline-stack";
  container.innerHTML = groupEventsByType(events).map(renderEventCard).join("");
}

// ── Render: history panel ─────────────────────────────
function renderHistory(): void {
  const dContainer = document.getElementById("deploymentTimeline");
  const eContainer = document.getElementById("eventTimeline");
  if (!dContainer || !eContainer) return;
  if (!state.selectedPartition) {
    dContainer.className = "empty-state text-sm text-[#566778]";
    dContainer.textContent = "Select a partition to inspect deployment history.";
    eContainer.className = "empty-state text-sm text-[#566778]";
    eContainer.textContent = "Select a partition to inspect event history.";
    return;
  }
  if (state.historyLoading) {
    dContainer.className = "loading-state text-sm text-[#566778]";
    dContainer.textContent = "Loading deployment history…";
    eContainer.className = "loading-state text-sm text-[#566778]";
    eContainer.textContent = "Loading event history…";
    return;
  }
  if (state.historyError) {
    dContainer.className = "empty-state text-sm text-[#566778]";
    dContainer.textContent = state.historyError;
    eContainer.className = "empty-state text-sm text-[#566778]";
    eContainer.textContent = state.historyError;
    return;
  }
  const history = state.history;
  if (!history) {
    dContainer.className = "empty-state text-sm text-[#566778]";
    dContainer.textContent = "Open the History tab to load deployment history.";
    eContainer.className = "empty-state text-sm text-[#566778]";
    eContainer.textContent = "Open the History tab to load event history.";
    return;
  }
  const filter = ((document.getElementById("historyFilter") as HTMLInputElement)?.value ?? "").trim().toLowerCase();
  const deployments = (history.deployments ?? []).filter((d: any) => {
    if (!filter) return true;
    return `${d.intent} ${d.deploymentRevision} ${(d.assets ?? []).map((a: any) => `${a.asset} ${a.version ?? ""}`).join(" ")}`.toLowerCase().includes(filter);
  });
  const events = (history.events ?? []).filter((e: any) => {
    if (!filter) return true;
    return `${e.intent ?? ""} ${e.title ?? ""} ${e.message ?? ""}`.toLowerCase().includes(filter);
  });

  dContainer.className = deployments.length ? "timeline-stack" : "empty-state text-sm text-[#566778]";
  dContainer.innerHTML = deployments.length ? deployments.map(renderDeploymentCard).join("") : "No deployment entries match the current filter.";

  const groupToggle = document.getElementById("historyGroupToggle") as HTMLInputElement | null;
  const shouldGroup = !groupToggle || groupToggle.checked;
  const groupedEvents = shouldGroup ? groupEventsByType(events) : events;
  eContainer.className = groupedEvents.length ? "timeline-stack" : "empty-state text-sm text-[#566778]";
  eContainer.innerHTML = groupedEvents.length ? groupedEvents.map(renderEventCard).join("") : "No events match the current filter.";
}

function renderRollouts(): void {
  const container = document.getElementById("rolloutsTimeline");
  if (!container) return;
  if (!state.selectedPartition) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = "Select a partition to inspect rollout history.";
    return;
  }
  if (state.rolloutsLoading) {
    container.className = "loading-state text-sm text-[#566778]";
    container.textContent = "Loading rollouts…";
    return;
  }
  if (state.rolloutsError) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = state.rolloutsError;
    return;
  }
  const rollouts = state.rollouts?.rollouts ?? [];
  container.className = rollouts.length ? "timeline-stack" : "empty-state text-sm text-[#566778]";
  container.innerHTML = rollouts.length
    ? rollouts.map(renderRolloutCard).join("")
    : "No archived rollouts were found for this partition yet.";
  container.querySelectorAll<HTMLElement>("[data-rollout-toggle]").forEach((button) => {
    button.addEventListener("click", (event) => {
      event.preventDefault();
      const rolloutKey = button.dataset.rolloutToggle ?? "";
      if (!rolloutKey) return;
      toggleRolloutExpanded(rolloutKey);
    });
  });
}

function renderRolloutCard(item: any): string {
  const assets = item.assets ?? [];
  const assetCount = assets.length;
  const rolloutKey = makeRolloutKey(item);
  const expanded = !!state.expandedRolloutKeys[rolloutKey];
  const statusBadge = item.current
    ? renderBadge("healthy", "Current")
    : item.newIntent
      ? renderBadge("pending", "New intent")
      : renderBadge("healthy", "Rollout");
  return `
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <h3>${escapeHtml(item.intent)}</h3>
          <div class="muted">${escapeHtml(item.summary || item.deploymentRevision)}</div>
        </div>
        <div class="timeline-head-actions">
          ${assetCount ? `
            <button
              class="rollout-toggle${expanded ? " active" : ""}"
              type="button"
              data-rollout-toggle="${escapeAttr(rolloutKey)}"
              aria-expanded="${expanded ? "true" : "false"}"
              aria-label="${expanded ? "Hide asset details" : "Show asset details"}"
            >
              <span class="rollout-toggle-indicator" aria-hidden="true">${expanded ? "−" : "+"}</span>
              <span>${expanded ? "Hide assets" : `Assets ${assetCount}`}</span>
            </button>
          ` : ""}
          ${statusBadge}
        </div>
      </div>
      <div class="timeline-meta">
        <span>${formatDateTime(item.createdAt)}</span>
        ${item.target ? `<span>${escapeHtml(item.target)}</span>` : ""}
        <span>${escapeHtml(item.deploymentRevision)}</span>
        ${(item.taskIDs ?? []).map((taskId: string) => `<span>${escapeHtml(taskId)}</span>`).join("")}
      </div>
      ${assetCount ? `<div class="timeline-assets-summary muted">${expanded ? `${assetCount} asset${assetCount === 1 ? "" : "s"} shown` : `${assetCount} asset${assetCount === 1 ? "" : "s"} hidden`}</div>` : ""}
      ${expanded ? `
        <div class="timeline-assets">
          ${assets.map((asset: any) => `
            <div class="timeline-asset">
              <div class="flex justify-between items-start gap-2">
                <div>
                  <strong class="text-[13px] text-[#E5ECF4]">${escapeHtml(asset.name)}</strong>
                  <div class="muted">${escapeHtml(asset.type || "Asset")}</div>
                </div>
                ${renderBadge(rolloutChangeStatus(asset.change), humanize(asset.change || "updated"))}
              </div>
              <div class="fact-row mt-1">
                <span class="fact fact-release">Release: ${escapeHtml(asset.version || item.deploymentRevision)}</span>
                ${asset.type ? `<span class="fact">Type: ${escapeHtml(asset.type)}</span>` : ""}
              </div>
            </div>
          `).join("")}
        </div>
      ` : ""}
    </article>
  `;
}

function toggleRolloutExpanded(rolloutKey: string): void {
  if (!rolloutKey) return;
  state.expandedRolloutKeys = {
    ...state.expandedRolloutKeys,
    [rolloutKey]: !state.expandedRolloutKeys[rolloutKey],
  };
  renderRollouts();
}

function makeRolloutKey(item: any): string {
  return `${item.intent ?? ""}::${item.deploymentRevision ?? ""}`;
}

function rolloutChangeStatus(change: string | undefined): string {
  switch ((change ?? "").toLowerCase()) {
    case "added":
      return "pending";
    case "removed":
      return "attention";
    default:
      return "healthy";
  }
}

function groupEventsByType(events: any[]): any[] {
  const groups = new Map<string, { latest: any; count: number }>();
  for (const event of events) {
    const key = `${event.intent ?? ""}::${event.title}`;
    const existing = groups.get(key);
    if (!existing || new Date(event.timestamp) > new Date(existing.latest.timestamp)) {
      groups.set(key, { latest: event, count: (existing?.count ?? 0) + 1 });
    } else {
      existing.count++;
    }
  }
  return Array.from(groups.values())
    .sort((a, b) => new Date(b.latest.timestamp).getTime() - new Date(a.latest.timestamp).getTime())
    .map((g) => ({ ...g.latest, groupCount: g.count }));
}

function renderEventCard(item: any): string {
  const countPill = item.groupCount > 1 ? `<span class="event-count-pill" title="${item.groupCount} occurrences">${item.groupCount}×</span>` : "";
  const titleNorm = (item.title ?? "").toLowerCase().replace(/[^a-z0-9]/g, "");
  const msgNorm = (item.message ?? "").toLowerCase().replace(/[^a-z0-9]/g, "");
  const showMsg = item.message && msgNorm !== titleNorm;
  return `
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <span class="event-type-eyebrow">Event type</span>
          <h3>${escapeHtml(item.title ?? "Event")} ${countPill}</h3>
          ${showMsg ? `<div class="muted">${escapeHtml(item.message)}</div>` : ""}
        </div>
        ${renderBadge(item.status, item.displayStatus, item.title ?? "Event", item.message ?? "")}
      </div>
      <div class="timeline-meta">
        <span>${formatDateTime(item.timestamp)}</span>
        ${item.intent ? `<span>${escapeHtml(item.intent)}</span>` : ""}
        ${item.taskID ? `<span>${escapeHtml(item.taskID)}</span>` : ""}
        ${item.deploymentRevision ? `<span>${escapeHtml(item.deploymentRevision)}</span>` : ""}
      </div>
    </article>
  `;
}

function renderDeploymentCard(item: any): string {
  return `
    <article class="timeline-card">
      <div class="timeline-head">
        <div>
          <h3>${escapeHtml(item.intent)}</h3>
          <div class="muted">${escapeHtml(item.deploymentRevision)}</div>
        </div>
        <span class="badge badge-healthy">Pushed</span>
      </div>
      <div class="timeline-meta">
        <span>${formatDateTime(item.createdAt)}</span>
        <span>${escapeHtml(item.target ?? "Unassigned")}</span>
        ${(item.taskIDs ?? []).map((t: string) => `<span>${escapeHtml(t)}</span>`).join("")}
      </div>
      <div class="timeline-assets">
        ${(item.assets ?? []).map((a: any) => `
          <div class="timeline-asset">
            <div class="flex justify-between items-start gap-2">
              <div>
                <strong class="text-[13px] text-[#E5ECF4]">${escapeHtml(a.asset)}</strong>
                <div class="muted">${escapeHtml(a.summary ?? "")}</div>
              </div>
              ${renderBadge(a.status, a.displayStatus)}
            </div>
            <div class="fact-row mt-1">
              ${a.version ? `<span class="fact fact-release">Release: ${escapeHtml(a.version)}</span>` : ""}
              ${Object.entries(a.outputs ?? {}).map(([k, v]) => `<span class="fact">${escapeHtml(k)}=${escapeHtml(String(v))}</span>`).join("")}
            </div>
            <div class="timeline-asset-logs">
              ${(a.logs ?? []).map((l: any) => `<div class="timeline-log">${escapeHtml(l.level ?? "info")} · ${escapeHtml(l.message ?? "")}</div>`).join("")}
            </div>
          </div>
        `).join("")}
      </div>
    </article>
  `;
}

// ── Render: topology panel ────────────────────────────
function renderTopologyPanel(): void {
  const canvas = document.getElementById("topologyCanvas");
  if (!canvas) return;
  const topology = state.detail?.topology;
  renderTopologyLegend(document.getElementById("topologyLegend")!);
  renderTopology({
    canvas,
    topology,
    zoom: state.topology.zoom,
    savedPositions: state.topology.nodePositions,
    selectedNodeId: state.topology.selectedNodeId,
    filters: {
      contains: (document.getElementById("showContainEdges") as HTMLInputElement)?.checked ?? true,
      join: (document.getElementById("showJoinEdges") as HTMLInputElement)?.checked ?? true,
      dependsOn: (document.getElementById("showAssetEdges") as HTMLInputElement)?.checked ?? true,
      outputRef: (document.getElementById("showOutputEdges") as HTMLInputElement)?.checked ?? true,
    },
    onSelectNode: (nodeId: string, positions: Record<string, { x: number; y: number }>) => {
      state.topology.selectedNodeId = nodeId;
      state.topology.nodePositions = positions;
      renderTopologyDetailsPanel();
      renderTopologyPanel(); // re-render for selection highlight
    },
    onDragNode: (nodeId: string, positions: Record<string, { x: number; y: number }>) => {
      state.topology.nodePositions = positions;
    },
  });
  renderTopologyDetailsPanel();
}

function renderTopologyDetailsPanel(): void {
  const container = document.getElementById("topologyDetails");
  if (!container) return;
  const topology = state.detail?.topology;
  if (!topology?.nodes?.length) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = "Select a node to inspect its status, metadata, and linked details.";
    return;
  }
  const nodeMap = new Map<string, any>(topology.nodes.map((n: any) => [n.id, n]));
  const selectedNode = nodeMap.get(state.topology.selectedNodeId);
  if (!selectedNode) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = "Select a node to inspect its status, metadata, and linked details.";
    return;
  }
  const filters = {
    contains: (document.getElementById("showContainEdges") as HTMLInputElement)?.checked ?? true,
    join: (document.getElementById("showJoinEdges") as HTMLInputElement)?.checked ?? true,
    dependsOn: (document.getElementById("showAssetEdges") as HTMLInputElement)?.checked ?? true,
    outputRef: (document.getElementById("showOutputEdges") as HTMLInputElement)?.checked ?? true,
  };
  const edges = (topology.edges ?? []).filter((e: any) => (filters as any)[e.kind] !== false);
  const relatedEdges = edges.filter((e: any) => e.from === selectedNode.id || e.to === selectedNode.id);
  const assetDoc = selectedNode.kind === "asset" ? findAssetInDetail(selectedNode.intent, selectedNode.asset ?? selectedNode.label) : null;
  const intentDoc = selectedNode.kind === "intent" ? findIntentInDetail(selectedNode.intent ?? selectedNode.label) : null;
  const propertyFacts = assetDoc ? renderAssetPropertyFacts(assetDoc) : "";
  const outputFacts = renderOutputFacts(assetDoc?.outputs ?? intentDoc?.outputs ?? {}, assetDoc ? [] : (intentDoc?.outputHints ?? []));
  const refFacts = (assetDoc?.references ?? []).map((r: string) => `<span class="fact">${escapeHtml(r)}</span>`).join("");

  const relatedRows = relatedEdges.map((edge: any) => {
    const peerId = edge.from === selectedNode.id ? edge.to : edge.from;
    const peer = nodeMap.get(peerId);
    if (!peer) return "";
    const dir = edge.from === selectedNode.id ? "out" : "in";
    return `
      <div class="topology-detail-row">
        <span class="topology-detail-direction">${dir}</span>
        <span class="topology-detail-name">${escapeHtml(peer.label)}</span>
        <span class="topology-detail-kind">${escapeHtml(humanize(edge.kind))}</span>
      </div>
    `;
  }).join("");

  container.className = "topology-detail-card";
  container.innerHTML = `
    <div style="--node-accent:${topologyNodeAccent(selectedNode)}">
      <div class="topology-detail-header">
        <div class="topology-detail-icon">${escapeHtml(topologyNodeIcon(selectedNode))}</div>
        <div>
          <h3>${escapeHtml(selectedNode.label)}</h3>
          <p>${escapeHtml(topologyNodeSubtitle(selectedNode))}</p>
        </div>
      </div>
      <div class="pill-row mb-2">
        ${renderBadge(selectedNode.health ?? selectedNode.status, selectedNode.displayStatus ?? humanize(selectedNode.kind), selectedNode.label, selectedNode.description, `topology:${state.selectedPartition}:${selectedNode.id}`)}
        <span class="pill">${escapeHtml(humanize(selectedNode.kind))}</span>
        ${selectedNode.assetType ? `<span class="pill">${escapeHtml(assetTitle(selectedNode.assetType))}</span>` : ""}
      </div>
      <div class="topology-detail-copy">${escapeHtml(selectedNode.description ?? "")}</div>
      ${Object.keys(selectedNode.meta ?? {}).length ? `
        <div class="topology-detail-meta mt-2">
          ${Object.entries(selectedNode.meta ?? {}).map(([k, v]) => `<span class="fact">${escapeHtml(`${humanize(k)}: ${v}`)}</span>`).join("")}
        </div>
      ` : ""}
      ${propertyFacts ? `<div class="topology-detail-block"><div class="topology-detail-heading">Properties</div><div class="fact-row">${propertyFacts}</div></div>` : ""}
      ${outputFacts ? `<div class="topology-detail-block"><div class="topology-detail-heading">Outputs</div><div class="fact-row">${outputFacts}</div></div>` : ""}
      ${refFacts ? `<div class="topology-detail-block"><div class="topology-detail-heading">Output refs</div><div class="fact-row">${refFacts}</div></div>` : ""}
      <div class="topology-detail-block">
        <div class="topology-detail-heading">Linked edges</div>
        ${relatedRows ? `<div class="topology-detail-list">${relatedRows}</div>` : `<div class="muted">No linked edges after current filters.</div>`}
      </div>
    </div>
  `;
}

// ── Render: page chrome ───────────────────────────────
const PANEL_COPY: Record<string, { eyebrow: string; title: string }> = {
  overviewPanel: { eyebrow: "Control center", title: "Control Center" },
  topologyPanel: { eyebrow: "Deployment graph", title: "Topology" },
  rolloutsPanel: { eyebrow: "Release timeline", title: "Rollouts" },
  historyPanel:  { eyebrow: "Push timeline",    title: "History" },
};

function renderPageChrome(): void {
  const activePanel = state.activePanel;
  const detail = state.detail;
  const copy = PANEL_COPY[activePanel] ?? PANEL_COPY.overviewPanel;
  const overviewOnly = activePanel === "overviewPanel";
  const hasSelection = Boolean(state.selectedPartition);
  const showOverviewChooser = overviewOnly && !hasSelection;

  // Section visibility — use style.display to beat Tailwind's display utilities
  const set = (id: string, visible: boolean) => {
    const el = document.getElementById(id);
    if (el) el.style.display = visible ? "" : "none";
  };
  set("appGridSection", showOverviewChooser);
  set("summaryGrid", showOverviewChooser);
  set("selectedPartitionHero", overviewOnly && hasSelection);
  set("sidebarPartitionSection", true);

  setText("pageEyebrow", copy.eyebrow);
  setText("pageTitle", copy.title);

  let subtitle = "Monitor partitions, inspect topology, and review history.";
  let pills = "";

  if (activePanel === "overviewPanel" && detail) {
    subtitle = `${detail.partition.manifest.spec?.deletionPolicy ?? "orphan"} policy · ${detail.partition.manifest.spec?.reconciliation?.mode ?? "manual"} reconcile · ${detail.intents.length} intents`;
    pills = `${renderBadge(detail.health?.status, detail.health?.displayStatus ?? "Selected", `${detail.partition.manifest.metadata.name} health`, detail.health?.summary, `partition-health:${detail.partition.manifest.metadata.name}`)} <span class="pill">${detail.topology?.nodes?.length ?? 0} nodes</span>`;
  }
  if (activePanel === "topologyPanel") {
    subtitle = detail ? `Topology for ${detail.partition.manifest.metadata.name}.` : "Select a partition to inspect its graph.";
    pills = detail ? `<span class="pill">${escapeHtml(detail.partition.manifest.metadata.name)}</span><span class="pill">${detail.topology?.nodes?.length ?? 0} nodes</span>` : "";
  }
  if (activePanel === "rolloutsPanel") {
    subtitle = detail ? `Review archived rollout changes for ${detail.partition.manifest.metadata.name}.` : "Select a partition to inspect rollout history.";
    pills = detail ? `<span class="pill">${escapeHtml(detail.partition.manifest.metadata.name)}</span><span class="pill">${state.rollouts?.rollouts?.length ?? 0} rollouts</span>` : "";
  }
  if (activePanel === "historyPanel") {
    subtitle = detail ? `Review deployments and state changes for ${detail.partition.manifest.metadata.name}.` : "Select a partition to inspect history.";
    pills = detail ? `<span class="pill">${escapeHtml(detail.partition.manifest.metadata.name)}</span><span class="pill">${state.history?.deployments?.length ?? 0} deployments</span>` : "";
  }

  setText("pageSubtitle", subtitle);
  const pillsEl = document.getElementById("headerContextPills");
  if (pillsEl) { pillsEl.innerHTML = pills; pillsEl.style.display = pills.trim() ? "" : "none"; }

  const topnavEl = document.getElementById("topnavPartition");
  if (topnavEl) topnavEl.textContent = state.selectedPartition || detail?.partition?.manifest?.metadata?.name || copy.title || "Control Center";
}

// ── Helpers ───────────────────────────────────────────
function renderBadge(status: string | undefined, label?: string, diagnosticTitle?: string, diagnosticDetail?: string, diagnosticKey?: string): string {
  const normalized = String(status ?? "neutral").toLowerCase();
  const display = label ?? humanize(normalized);
  const detail = resolveDiagnosticDetail(diagnosticKey, normalized, diagnosticDetail);
  const clickable = (normalized === "failing" || normalized === "attention") && detail.length > 0;
  if (!clickable) {
    return `<span class="badge badge-${escapeAttr(normalized)}">${escapeHtml(display)}</span>`;
  }
  return `<button type="button" class="badge badge-${escapeAttr(normalized)} badge-clickable" data-diagnostic-title="${escapeAttr((diagnosticTitle ?? display).trim())}" data-diagnostic-detail="${escapeAttr(detail)}" aria-label="Show diagnostic details for ${escapeAttr(display)}">${escapeHtml(display)}</button>`;
}

function resolveDiagnosticDetail(cacheKey: string | undefined, status: string, detail: string | undefined): string {
  const key = String(cacheKey ?? "").trim();
  const nextDetail = String(detail ?? "").trim();
  if (!key) {
    return nextDetail;
  }
  if (status === "failing" || status === "attention") {
    if (nextDetail) {
      state.diagnosticDetails[key] = nextDetail;
      return nextDetail;
    }
    return state.diagnosticDetails[key] ?? "";
  }
  delete state.diagnosticDetails[key];
  return nextDetail;
}

function joinDiagnosticLines(lines: Array<string | undefined | null>): string {
  return lines
    .map((line) => String(line ?? "").trim())
    .filter((line) => line.length > 0)
    .join("\n");
}

function ensureDiagnosticsModal(): HTMLElement {
  const existing = document.getElementById("diagnosticsModal");
  if (existing) return existing;

  const overlay = document.createElement("div");
  overlay.id = "diagnosticsModal";
  overlay.className = "diagnostics-modal hidden";
  overlay.innerHTML = `
    <div class="diagnostics-modal-card" role="dialog" aria-modal="true" aria-labelledby="diagnosticsModalTitle">
      <div class="diagnostics-modal-header">
        <h3 id="diagnosticsModalTitle">Status details</h3>
        <button type="button" class="diagnostics-close" data-diagnostics-close="true" aria-label="Close diagnostics">×</button>
      </div>
      <pre id="diagnosticsModalBody" class="diagnostics-modal-body"></pre>
    </div>
  `;
  overlay.addEventListener("click", (event) => {
    const target = event.target as HTMLElement;
    if (target === overlay || target.closest("[data-diagnostics-close='true']")) {
      closeDiagnosticsModal();
    }
  });
  document.body.appendChild(overlay);
  return overlay;
}

function openDiagnosticsModal(title: string, detail: string): void {
  const overlay = ensureDiagnosticsModal();
  const titleEl = overlay.querySelector<HTMLElement>("#diagnosticsModalTitle");
  const bodyEl = overlay.querySelector<HTMLElement>("#diagnosticsModalBody");
  if (titleEl) {
    titleEl.textContent = title.trim() || "Status details";
  }
  if (bodyEl) {
    bodyEl.textContent = detail.trim();
  }
  overlay.classList.remove("hidden");
  document.body.classList.add("diagnostics-open");
}

function closeDiagnosticsModal(): void {
  const overlay = document.getElementById("diagnosticsModal");
  if (!overlay) return;
  overlay.classList.add("hidden");
  document.body.classList.remove("diagnostics-open");
}

function renderProgressingInfo(detail: any): string {
  const reconcileMode = detail.partition?.manifest?.spec?.reconciliation?.mode ?? "manual";
  const managedBy = detail.partition?.manifest?.spec?.labels?.managedBy ?? "";
  const intents = detail.intents ?? [];
  const hasPusher = intents.some((i: any) => i.targetSummary && i.targetSummary !== "Unassigned");
  const pendingCount = detail.health?.pending ?? 0;
  const lines: string[] = [];
  lines.push(`${pendingCount} asset${pendingCount !== 1 ? "s are" : " is"} in <strong>Planned</strong> state — no reconcile has run yet.`);
  if (managedBy === "external-compose") lines.push("Resources in this partition are managed externally by Docker Compose.");
  if (reconcileMode === "manual") {
    if (hasPusher) lines.push("Click <strong>Reconcile now</strong> in the sidebar to run the first reconcile and deploy assets.");
    else lines.push("No pusher is assigned. Assets will stay in Planned state until a pusher is configured.");
  } else {
    lines.push("The reconciler will process these assets automatically in the next cycle.");
  }
  return `<div class="info-callout mt-2"><span class="info-callout-icon">?</span><div><strong>Why is this partition Progressing?</strong><p>${lines.join(" ")}</p></div></div>`;
}

function tileLabelEntries(labels: Record<string, string>): string[] {
  const ordered = ["component", "role", "stack", "topology"];
  const values: string[] = [];
  ordered.forEach((k) => { if (labels[k]) values.push(labels[k]); });
  return [...new Set(values)];
}

function findIntentInDetail(intentName: string): any {
  return (state.detail?.intents ?? []).find((i: any) => i.name === intentName) ?? null;
}

function findAssetInDetail(intentName: string, assetName: string): any {
  const intent = findIntentInDetail(intentName);
  return intent?.assets?.find((a: any) => a.name === assetName) ?? null;
}

// ── Topology node helpers ─────────────────────────────
const ACCESSIBLE_NODE_COLORS = {
  partition: "#F0E442",
  intent: "#CC79A7",
  runtime: "#0072B2",
  config: "#009E73",
  storage: "#56B4E9",
  traffic: "#D55E00",
  muted: "#8B949E",
};

function topologyNodeAccent(node: any): string {
  if (node.kind === "partition") return ACCESSIBLE_NODE_COLORS.partition;
  if (node.kind === "intent") return ACCESSIBLE_NODE_COLORS.intent;
  return assetAccent(node.assetType ?? node.kind);
}
function topologyNodeIcon(node: any): string {
  if (node.kind === "partition") return "◫";
  if (node.kind === "intent") return "⊞";
  return assetIcon(node.assetType ?? node.kind);
}
function topologyNodeSubtitle(node: any): string {
  if (node.kind === "partition") return `${node.meta?.reconciliation ?? "manual"} reconcile · ${node.meta?.deletionPolicy ?? "orphan"} delete`;
  if (node.kind === "intent") return node.meta?.target ?? node.displayStatus ?? "Intent";
  return `${assetTitle(node.assetType ?? node.kind)} · ${node.displayStatus ?? "Asset"}`;
}

// ── Asset helpers ─────────────────────────────────────
const LIBRARY_SECTION_ORDER = ["Compute","Network","Config","Storage","Operations"];
const ASSET_ACCENTS: Record<string, string> = {
  Compute: ACCESSIBLE_NODE_COLORS.runtime,
  Volume: ACCESSIBLE_NODE_COLORS.storage,
  Config: ACCESSIBLE_NODE_COLORS.config,
  ObjectStore: ACCESSIBLE_NODE_COLORS.storage,
  Database: ACCESSIBLE_NODE_COLORS.traffic,
  SQLDatabase: ACCESSIBLE_NODE_COLORS.traffic,
  LoadBalancer: ACCESSIBLE_NODE_COLORS.traffic,
  Observability: ACCESSIBLE_NODE_COLORS.config,
};

// ── Fact label helpers ────────────────────────────────
const FACT_HINTS: Record<string, string> = {
  Image:     "Container image reference (registry/name:tag@digest)",
  Scale:     "Desired replica count",
  Env:       "Environment variables injected at runtime",
  Config:    "ConfigMap or file mounts",
  Storage:   "Persistent volume mounts",
  Ports:     "Exposed service ports",
  Port:      "Service listener port",
  Health:    "Health check probe is configured",
  CPU:       "CPU resource limit/request",
  Memory:    "Memory resource limit/request",
  Engine:    "Storage engine or database type",
  Version:   "Engine version",
  Database:  "Database name",
  Mode:      "Deployment or storage mode",
  Endpoint:  "Connection endpoint address",
  Size:      "Volume storage capacity",
  Access:    "Volume access mode (e.g. ReadWriteOnce)",
  Format:    "Config file format (yaml / json / env)",
  Files:     "Config file definitions",
  Inline:    "Config data is defined inline in the manifest",
  Targets:   "Number of load balancer backend targets",
  Listeners: "Number of load balancer listeners",
  Buckets:   "Object storage bucket names",
  Provider:  "Observability provider",
  Receivers: "Telemetry input protocols",
  Exporters: "Telemetry export destinations",
  Outputs:   "Output keys exposed to dependent intents",
};
const FACT_COLOR: Record<string, string> = {
  Scale:    "fact-scale",
  Ports:    "fact-port",
  Port:     "fact-port",
  CPU:      "fact-resource",
  Memory:   "fact-resource",
  Env:      "fact-env",
  Storage:  "fact-storage",
  Size:     "fact-storage",
  Engine:   "fact-engine",
  Version:  "fact-engine",
  Outputs:  "fact-outputs",
};
function factClass(label: string): string {
  if (label === "Release") return "fact-release";
  return FACT_COLOR[label] ? ` ${FACT_COLOR[label]}` : "";
}
function factTitle(label: string): string {
  return FACT_HINTS[label] ?? label;
}

function assetTemplate(assetType: string): any | null {
  return (state.catalog?.assetTypes ?? []).find((t: any) => t.type === assetType) ?? null;
}

function normalizeAssetHintPath(path: string): string {
  return path.replace(/\[\d+\]/g, "[]");
}

function hintForPath(hints: any[] | undefined, path: string): any | null {
  const normalized = normalizeAssetHintPath(path);
  const topLevel = normalized.replace(/\[\]/g, "").split(".")[0];
  return (hints ?? []).find((hint: any) => hint.path === normalized || hint.path === topLevel)
    ?? null;
}

function assetHintForPath(assetDoc: any, path: string): any | null {
  const template = assetTemplate(assetDoc?.type ?? "");
  return hintForPath(assetDoc?.hints, path)
    ?? hintForPath(template?.hints, path)
    ?? hintForPath(template?.fields, path)
    ?? null;
}

function flattenAssetProperties(value: any, path = ""): Array<{ path: string; value: string }> {
  if (value == null || value === "") return [];
  if (Array.isArray(value)) {
    if (!value.length) return path ? [{ path, value: "[]" }] : [];
    return value.flatMap((item, index) => flattenAssetProperties(item, `${path}[${index}]`));
  }
  if (typeof value === "object") {
    const entries = Object.entries(value);
    if (!entries.length) return path ? [{ path, value: "{}" }] : [];
    return entries.flatMap(([key, child]) => flattenAssetProperties(child, path ? `${path}.${key}` : key));
  }
  return path ? [{ path, value: String(value) }] : [];
}

function renderAssetPropertyFacts(assetDoc: any, options: { limit?: number; truncateAt?: number } = {}): string {
  const limit = options.limit ?? 16;
  const truncateAt = options.truncateAt ?? 48;
  const template = assetTemplate(assetDoc?.type ?? "");
  const fieldOrder = new Map<string, number>();
  (template?.fields ?? []).forEach((field: any, index: number) => {
    fieldOrder.set(field.path, index);
  });
  const entries = flattenAssetProperties(assetDoc?.properties ?? {})
    .sort((left, right) => {
      const leftPath = normalizeAssetHintPath(left.path);
      const rightPath = normalizeAssetHintPath(right.path);
      const leftTop = leftPath.replace(/\[\]/g, "").split(".")[0];
      const rightTop = rightPath.replace(/\[\]/g, "").split(".")[0];
      const leftOrder = fieldOrder.get(leftPath) ?? fieldOrder.get(leftTop) ?? Number.MAX_SAFE_INTEGER;
      const rightOrder = fieldOrder.get(rightPath) ?? fieldOrder.get(rightTop) ?? Number.MAX_SAFE_INTEGER;
      if (leftOrder !== rightOrder) return leftOrder - rightOrder;
      return left.path.localeCompare(right.path);
    });
  if (!entries.length) return "";
  const visible = entries.slice(0, limit);
  const facts = visible.map((entry) => {
    const hint = assetHintForPath(assetDoc, entry.path);
    const title = [entry.path, hint?.title, hint?.description].filter(Boolean).join(" - ");
    return `<span class="fact" title="${escapeAttr(title)}">${escapeHtml(`${entry.path}: ${truncate(entry.value, truncateAt)}`)}</span>`;
  }).join("");
  if (entries.length === visible.length) return facts;
  const hidden = entries.length - visible.length;
  return `${facts}<span class="fact" title="${hidden} more properties available">+${hidden} more</span>`;
}

function renderOutputFacts(outputs: Record<string, any>, hints: any[] = [], options: { limit?: number; truncateAt?: number } = {}): string {
  const entries = Object.entries(outputs ?? {});
  const limit = options.limit ?? entries.length;
  const truncateAt = options.truncateAt ?? Number.MAX_SAFE_INTEGER;
  const facts = entries.slice(0, limit).map(([key, value]) => {
    const hint = hintForPath(hints, `outputs.${key}`);
    const title = [`outputs.${key}`, hint?.title, hint?.description].filter(Boolean).join(" - ");
    return `<span class="fact" title="${escapeAttr(title)}">${escapeHtml(`${key}: ${truncate(String(value), truncateAt)}`)}</span>`;
  }).join("");
  if (entries.length <= limit) return facts;
  const hidden = entries.length - limit;
  return `${facts}<span class="fact" title="${hidden} more outputs available">+${hidden} more</span>`;
}

function assetCategory(assetType: string): string {
  return assetTemplate(assetType)?.category ?? "Other";
}
function assetTitle(assetType: string): string {
  return assetTemplate(assetType)?.title ?? humanize(assetType);
}
function assetIcon(assetType: string): string {
  return assetTemplate(assetType)?.icon ?? "⬡";
}
function assetAccent(assetType: string): string {
  return ASSET_ACCENTS[assetType] ?? ACCESSIBLE_NODE_COLORS.muted;
}
function categoryAccent(category: string): string {
  const map: Record<string, string> = {
    Compute: ACCESSIBLE_NODE_COLORS.runtime,
    Network: ACCESSIBLE_NODE_COLORS.traffic,
    Config: ACCESSIBLE_NODE_COLORS.config,
    Storage: ACCESSIBLE_NODE_COLORS.storage,
    Operations: ACCESSIBLE_NODE_COLORS.config,
    Other: ACCESSIBLE_NODE_COLORS.muted,
  };
  return map[category] ?? map.Other;
}

function summarizeIntentCategories(assets: any[]): { category: string; count: number }[] {
  const counts = new Map<string, number>();
  assets.forEach((a) => { const c = assetCategory(a.type); counts.set(c, (counts.get(c) ?? 0) + 1); });
  return [...counts.keys()].sort((a, b) => {
    const la = LIBRARY_SECTION_ORDER.indexOf(a), lb = LIBRARY_SECTION_ORDER.indexOf(b);
    if (la === -1 && lb === -1) return a.localeCompare(b);
    if (la === -1) return 1; if (lb === -1) return -1;
    return la - lb;
  }).map((c) => ({ category: c, count: counts.get(c)! }));
}

function groupAssetsByCategory(assets: any[]): { category: string; assets: any[] }[] {
  const grouped = new Map<string, any[]>();
  assets.forEach((a) => {
    const c = assetCategory(a.type);
    if (!grouped.has(c)) grouped.set(c, []);
    grouped.get(c)!.push(a);
  });
  return [...grouped.keys()].sort((a, b) => {
    const la = LIBRARY_SECTION_ORDER.indexOf(a), lb = LIBRARY_SECTION_ORDER.indexOf(b);
    if (la === -1 && lb === -1) return a.localeCompare(b);
    if (la === -1) return 1; if (lb === -1) return -1;
    return la - lb;
  }).map((c) => ({ category: c, assets: grouped.get(c)!.sort((a, b) => a.name.localeCompare(b.name)) }));
}

// ── Wire events ───────────────────────────────────────
function wireEvents(): void {
  document.querySelectorAll<HTMLElement>("[data-tab-target]").forEach((btn) => {
    btn.addEventListener("click", () => activatePanel(btn.dataset.tabTarget!));
  });
  document.getElementById("partitionSearch")?.addEventListener("input", renderPartitionList);
  document.getElementById("refreshButton")?.addEventListener("click", () => refreshOverview(true).catch(handleError));
  document.getElementById("reconcileButton")?.addEventListener("click", reconcileSelected);
  document.getElementById("createPartitionButton")?.addEventListener("click", () => toast("Create partition via guardianctl partition push", "success"));
  document.getElementById("overviewCreatePartitionButton")?.addEventListener("click", () => toast("Create partition via guardianctl partition push", "success"));
  document.getElementById("appGridSearch")?.addEventListener("input", renderAppGrid);
  document.getElementById("historyFilter")?.addEventListener("input", renderHistory);
  document.getElementById("historyGroupToggle")?.addEventListener("change", renderHistory);
  document.getElementById("historyApply")?.addEventListener("click", () => applyHistoryFilters().catch(handleError));
  document.getElementById("historyReset")?.addEventListener("click", () => resetHistoryFilters().catch(handleError));
  ["showContainEdges","showJoinEdges","showAssetEdges","showOutputEdges"].forEach((id) => {
    document.getElementById(id)?.addEventListener("change", renderTopologyPanel);
  });
  document.getElementById("topologyZoomOut")?.addEventListener("click", () => changeTopologyZoom(-0.1));
  document.getElementById("topologyZoomIn")?.addEventListener("click", () => changeTopologyZoom(0.1));
  document.getElementById("topologyResetLayout")?.addEventListener("click", () => {
    state.topology.nodePositions = {};
    renderTopologyPanel();
  });
  document.addEventListener("click", (event) => {
    const target = event.target as HTMLElement;
    const badge = target.closest<HTMLElement>("[data-diagnostic-detail]");
    if (!badge) return;
    event.preventDefault();
    openDiagnosticsModal(
      badge.dataset.diagnosticTitle ?? "Status details",
      badge.dataset.diagnosticDetail ?? "No diagnostic details were provided.",
    );
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      closeDiagnosticsModal();
    }
  });
  ensureDiagnosticsModal();
}

async function reconcileSelected(): Promise<void> {
  const partitionName = state.selectedPartition;
  if (!partitionName) { toast("Select a partition first.", "error"); return; }
  await fetchJSON(`/api/partitions/${encodeURIComponent(partitionName)}/reconcile`, { method: "POST" });
  armFastRefreshBurst();
  toast("Reconciliation requested.", "success");
  await refreshOverview(false);
  await selectPartition(partitionName, false);
}

function changeTopologyZoom(delta: number): void {
  const canvas = document.getElementById("topologyCanvas");
  const prev = clamp(state.topology.zoom, 0.4, 2.5);
  const next = clamp(Math.round((prev + delta) * 100) / 100, 0.4, 2.5);
  if (prev === next) return;
  const cx = canvas ? canvas.scrollLeft + canvas.clientWidth / 2 : 0;
  const cy = canvas ? canvas.scrollTop + canvas.clientHeight / 2 : 0;
  state.topology.zoom = next;
  renderTopologyPanel();
  if (canvas) {
    const ratio = next / prev;
    canvas.scrollLeft = Math.max(0, cx * ratio - canvas.clientWidth / 2);
    canvas.scrollTop = Math.max(0, cy * ratio - canvas.clientHeight / 2);
  }
  updateTopologyZoomControls();
}

function updateTopologyZoomControls(): void {
  const zoom = state.topology.zoom;
  const zoomOutBtn = document.getElementById("topologyZoomOut") as HTMLButtonElement | null;
  const zoomInBtn = document.getElementById("topologyZoomIn") as HTMLButtonElement | null;
  const zoomVal = document.getElementById("topologyZoomValue");
  if (zoomVal) zoomVal.textContent = `${Math.round(zoom * 100)}%`;
  if (zoomOutBtn) zoomOutBtn.disabled = zoom <= 0.4;
  if (zoomInBtn) zoomInBtn.disabled = zoom >= 2.5;
}

function clamp(v: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, v));
}

function handleError(error: any): void {
  toast(error?.message ?? "Unexpected error", "error");
}

// Load catalog on init
async function loadCatalog(): Promise<void> {
  try { state.catalog = await fetchJSON("/api/catalog"); } catch { /* optional */ }
}
loadCatalog();
