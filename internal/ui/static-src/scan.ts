import { fetchJSON, escapeHtml, escapeAttr, formatDateTime, toast } from "./utils";

interface ScanListEntry {
  scanID: string;
  pusher: string;
  account?: string;
  status: string;
  regions?: string[];
  createdAt?: string;
  finishedAt?: string;
  summary?: Record<string, number>;
}

interface ScanListResponse {
  pushers: string[];
  scans: ScanListEntry[];
}

interface ScanBundleResponse {
  scanID: string;
  bundle: {
    partition: any;
    intents: { manifest: any }[];
  };
  draft: any;
}

const SCAN_POLL_MS = 3_000;
const INVENTORY_DISPLAY_CAP = 200;

let onError: (error: any) => void = () => undefined;
let pushers: string[] = [];
let scans: ScanListEntry[] = [];
let selectedScanID = "";
let scanDetail: any = null;
let bundleResponse: ScanBundleResponse | null = null;
let selectedIntents: Record<string, boolean> = {};
let pollTimer: number | undefined;
let panelActive = false;
let busy = false;

export function initScanPanel(errorHandler: (error: any) => void): void {
  onError = errorHandler;
  document.getElementById("scanStartButton")?.addEventListener("click", () => startScan().catch(onError));
  document.getElementById("scanGenerateButton")?.addEventListener("click", () => generateBundle().catch(onError));
  document.getElementById("scanList")?.addEventListener("click", (event) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>("[data-scan-select]");
    if (!target) return;
    selectScan(target.dataset.scanSelect ?? "").catch(onError);
  });
  document.getElementById("scanBundleResult")?.addEventListener("change", (event) => {
    const target = event.target as HTMLElement;
    if (!(target instanceof HTMLInputElement)) return;
    const name = target.dataset.intentToggle;
    if (!name) return;
    selectedIntents[name] = target.checked;
    renderBundleResult();
  });
  document.getElementById("scanBundleResult")?.addEventListener("click", (event) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>("[data-scan-save]");
    if (!target) return;
    const reconcile = target.dataset.scanSave === "reconcile";
    saveBundle(reconcile).catch(onError);
  });
}

export function renderScanPanel(): void {
  panelActive = !document.getElementById("scanPanel")?.classList.contains("hidden");
  if (!panelActive) {
    stopPolling();
    return;
  }
  if (pushers.length === 0 && scans.length === 0) {
    loadScans().catch(onError);
  } else {
    renderScans();
  }
  schedulePolling();
}

function stopPolling(): void {
  if (pollTimer !== undefined) {
    window.clearTimeout(pollTimer);
    pollTimer = undefined;
  }
}

function schedulePolling(): void {
  if (pollTimer !== undefined) return;
  const tick = async () => {
    pollTimer = undefined;
    if (!panelActive) return;
    try {
      await loadScans();
      if (selectedScanID && needsDetailRefresh()) {
        await refreshSelectedDetail();
      }
    } catch {
      // keep polling on transient errors
    }
    if (panelActive && hasPendingScans()) {
      pollTimer = window.setTimeout(tick, SCAN_POLL_MS);
    }
  };
  if (hasPendingScans()) {
    pollTimer = window.setTimeout(tick, SCAN_POLL_MS);
  }
}

function hasPendingScans(): boolean {
  return scans.some((scan) => scan.status === "Queued" || scan.status === "Running");
}

function needsDetailRefresh(): boolean {
  return !scanDetail || scanDetail.status === "Queued" || scanDetail.status === "Running";
}

async function loadScans(): Promise<void> {
  const response: ScanListResponse = await fetchJSON("/api/scans");
  pushers = (response.pushers ?? []).filter((pusher) => pusher.includes("aws"));
  scans = response.scans ?? [];
  renderPusherSelect();
  renderScans();
}

function renderPusherSelect(): void {
  const select = document.getElementById("scanPusher") as HTMLSelectElement | null;
  if (!select) return;
  const current = select.value;
  select.innerHTML = pushers.length === 0
    ? `<option value="">No AWS pushers configured</option>`
    : pushers.map((pusher) => `<option value="${escapeAttr(pusher)}">${escapeHtml(pusher)}</option>`).join("");
  if (current && pushers.includes(current)) select.value = current;
}

function statusBadge(status: string): string {
  const normalized = String(status ?? "").toLowerCase();
  if (normalized === "succeeded") return "badge badge-healthy";
  if (normalized === "failed") return "badge badge-failing";
  if (normalized === "running") return "badge badge-pending";
  return "badge badge-neutral";
}

function renderScans(): void {
  const container = document.getElementById("scanList");
  if (!container) return;
  if (scans.length === 0) {
    container.className = "empty-state text-sm text-[#566778]";
    container.textContent = "No scans yet. Start one above.";
    return;
  }
  container.className = "grid gap-1.5";
  container.innerHTML = scans.map((scan) => {
    const summary = scan.summary ?? {};
    const regions = (scan.regions ?? []).join(", ") || "—";
    const created = scan.createdAt ? formatDateTime(scan.createdAt) : "—";
    const counts = [
      `${summary.bucketCount ?? 0} buckets`,
      `${summary.serviceCount ?? 0} services`,
      `${summary.loadBalancerCount ?? 0} LBs`,
      `${summary.stackCount ?? 0} stacks`,
      `${summary.inventoryCount ?? 0} inventory`,
    ].join(" · ");
    const selected = scan.scanID === selectedScanID;
    return `
      <div class="flex items-center gap-3 px-3.5 py-2.5 rounded-lg border ${selected ? "border-[#00ADE4]/50 bg-[#00ADE4]/[0.06]" : "border-white/[0.07] bg-[#0D1220]"}">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-[13px] font-semibold text-[#E5ECF4] truncate">${escapeHtml(scan.pusher)}</span>
            <span class="${statusBadge(scan.status)}">${escapeHtml(scan.status ?? "")}</span>
            <span class="text-[11px] text-[#566778]">${escapeHtml(created)}</span>
          </div>
          <div class="text-[12px] text-[#9BB0CF] mt-0.5 truncate">${escapeHtml(regions)} · ${escapeHtml(counts)}</div>
        </div>
        <button class="btn-ghost shrink-0" data-scan-select="${escapeAttr(scan.scanID)}">${scan.status === "Succeeded" ? "Inspect" : "View"}</button>
      </div>`;
  }).join("");
}

async function startScan(): Promise<void> {
  const pusher = (document.getElementById("scanPusher") as HTMLSelectElement | null)?.value?.trim() ?? "";
  if (!pusher) {
    toast("Select an AWS pusher first.", "error");
    return;
  }
  const regions = (document.getElementById("scanRegions") as HTMLInputElement | null)?.value?.trim() ?? "";
  const includeTypes = (document.getElementById("scanIncludeTypes") as HTMLInputElement | null)?.value?.trim() ?? "";
  const excludeTypes = (document.getElementById("scanExcludeTypes") as HTMLInputElement | null)?.value?.trim() ?? "";
  const inventory = (document.getElementById("scanInventory") as HTMLInputElement | null)?.checked ?? false;
  const body: Record<string, any> = { pusher, inventory };
  if (regions) body.regions = regions.split(",").map((region) => region.trim()).filter(Boolean);
  if (includeTypes) body.includeResourceTypes = includeTypes.split(",").map((type) => type.trim()).filter(Boolean);
  if (excludeTypes) body.excludeResourceTypes = excludeTypes.split(",").map((type) => type.trim()).filter(Boolean);
  const created = await fetchJSON("/api/scans", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  selectedScanID = created.scanID ?? "";
  scanDetail = null;
  bundleResponse = null;
  toast("Scan queued. The AWS pusher will pick it up shortly.", "success");
  await loadScans();
  schedulePolling();
}

async function selectScan(scanID: string): Promise<void> {
  if (!scanID) return;
  selectedScanID = scanID;
  bundleResponse = null;
  selectedIntents = {};
  await refreshSelectedDetail();
  renderScans();
}

async function refreshSelectedDetail(): Promise<void> {
  if (!selectedScanID) return;
  scanDetail = await fetchJSON(`/api/scans/${encodeURIComponent(selectedScanID)}`);
  renderScanDetail();
}

function summaryPill(label: string, value: number | undefined): string {
  return `<span class="pill">${escapeHtml(label)} <strong>${value ?? 0}</strong></span>`;
}

function renderScanDetail(): void {
  const wrap = document.getElementById("scanDetailWrap");
  if (!wrap || !scanDetail) return;
  const finished = scanDetail.status === "Succeeded" || scanDetail.status === "Failed";
  wrap.classList.toggle("hidden", !finished);
  if (!finished) {
    const title = document.getElementById("scanDetailTitle");
    if (title) title.textContent = `Scan ${selectedScanID} is ${scanDetail.status ?? "pending"}`;
    return;
  }
  const title = document.getElementById("scanDetailTitle");
  if (title) title.textContent = `Scan ${selectedScanID}`;
  const subtitle = document.getElementById("scanDetailSubtitle");
  if (subtitle) {
    const account = scanDetail.account ? `account ${scanDetail.account}` : "";
    const regions = Array.isArray(scanDetail.regions) ? scanDetail.regions.join(", ") : "";
    subtitle.textContent = [account, regions].filter(Boolean).join(" · ");
  }
  const summary = scanDetail.summary ?? {};
  const summaryEl = document.getElementById("scanSummary");
  if (summaryEl) {
    summaryEl.innerHTML = [
      summaryPill("Regions", summary.regionCount),
      summaryPill("Buckets", summary.bucketCount),
      summaryPill("EFS", summary.fileSystemCount),
      summaryPill("Params", summary.parameterCount),
      summaryPill("Secrets", summary.secretCount),
      summaryPill("Services", summary.serviceCount),
      summaryPill("LBs", summary.loadBalancerCount),
      summaryPill("Stacks", summary.stackCount),
      summaryPill("Inventory", summary.inventoryCount),
      summaryPill("Managed", summary.managedCount),
      summaryPill("Errors", summary.errorCount),
    ].join("");
  }
  renderManagedList();
  renderUnmappedList();
  renderInventoryList();
  renderErrorsList();
}

function renderManagedList(): void {
  const container = document.getElementById("scanManagedList");
  if (!container) return;
  const entries = collectManagedEntries(scanDetail);
  if (entries.length === 0) {
    container.innerHTML = "";
    return;
  }
  container.innerHTML = `
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Already managed (${entries.length})</div>
    <div class="grid gap-1">
      ${entries.slice(0, 50).map((entry: any) => `
        <div class="flex items-center gap-2 text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-white/[0.07] bg-[#0D1220]">
          <span class="badge badge-neutral">${escapeHtml(entry.kind ?? "")}</span>
          <span class="text-[#E5ECF4] truncate">${escapeHtml(entry.identifier ?? "")}</span>
          <span class="text-[#566778] truncate ml-auto">${escapeHtml(entry.partition ? `${entry.partition}/${entry.intent}/${entry.asset ?? ""}` : (entry.existingIntent ? "referenced by existing intent" : ""))}</span>
        </div>`).join("")}
      ${entries.length > 50 ? `<div class="text-[12px] text-[#566778] px-3">+${entries.length - 50} more</div>` : ""}
    </div>`;
}

function collectManagedEntries(detail: any): any[] {
  const entries: any[] = [];
  const push = (kind: string, resources: any[] | undefined, identifierOf: (resource: any) => string) => {
    for (const resource of resources ?? []) {
      if (resource?.managed?.managed) {
        entries.push({
          kind,
          identifier: identifierOf(resource),
          partition: resource.managed.partition,
          intent: resource.managed.intent,
          asset: resource.managed.asset,
        });
      }
    }
  };
  push("bucket", detail?.buckets, (r) => r.name);
  push("fileSystem", detail?.fileSystems, (r) => r.id);
  push("parameter", detail?.parameters, (r) => r.name);
  push("secret", detail?.secrets, (r) => r.name);
  push("service", detail?.services, (r) => `${r.clusterName ?? ""}/${r.name ?? ""}`);
  push("loadBalancer", detail?.loadBalancers, (r) => r.name);
  push("stack", detail?.stacks, (r) => r.name);
  return entries;
}

function renderUnmappedList(): void {
  const container = document.getElementById("scanUnmappedList");
  if (!container) return;
  const stackReasons = collectForeignStacks(scanDetail);
  if (stackReasons.length === 0) {
    container.innerHTML = "";
    return;
  }
  container.innerHTML = `
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Foreign CloudFormation stacks (${stackReasons.length})</div>
    <div class="grid gap-1">
      ${stackReasons.slice(0, 25).map((stack: any) => `
        <div class="flex items-center gap-2 text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-white/[0.07] bg-[#0D1220]">
          <span class="badge badge-attention">stack</span>
          <span class="text-[#E5ECF4] truncate">${escapeHtml(stack.name ?? "")}</span>
          <span class="text-[#566778] truncate ml-auto">${escapeHtml(stack.region ?? "")} · ${escapeHtml(stack.status ?? "")}</span>
        </div>`).join("")}
      ${stackReasons.length > 25 ? `<div class="text-[12px] text-[#566778] px-3">+${stackReasons.length - 25} more</div>` : ""}
    </div>`;
}

function collectForeignStacks(detail: any): any[] {
  return (detail?.stacks ?? []).filter((stack: any) => !stack?.managed?.managed);
}

function renderInventoryList(): void {
  const container = document.getElementById("scanInventoryList");
  if (!container) return;
  const inventory = scanDetail?.inventory ?? {};
  const regions = Object.keys(inventory).sort();
  if (regions.length === 0) {
    container.innerHTML = "";
    return;
  }
  container.innerHTML = `
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Inventory (not importable)</div>
    <div class="grid gap-1.5">
      ${regions.map((region) => {
        const byType = inventory[region] ?? {};
        const typeNames = Object.keys(byType).sort();
        if (typeNames.length === 0) return "";
        return `
          <details class="rounded border border-white/[0.07] bg-[#0D1220]">
            <summary class="px-3 py-2 text-[12px] text-[#E5ECF4] cursor-pointer select-none">${escapeHtml(region)} <span class="text-[#566778]">(${typeNames.length} types)</span></summary>
            <div class="px-3 pb-2.5 grid gap-1">
              ${typeNames.map((typeName) => {
                const resources: any[] = byType[typeName] ?? [];
                if (resources.length === 0) return "";
                const shown = resources.slice(0, INVENTORY_DISPLAY_CAP);
                return `
                  <details class="rounded border border-white/[0.06] bg-[#151B2B]">
                    <summary class="px-2.5 py-1.5 text-[12px] text-[#9BB0CF] cursor-pointer select-none">${escapeHtml(typeName)} <span class="text-[#566778]">(${resources.length})</span></summary>
                    <div class="px-2.5 pb-2 grid gap-0.5">
                      ${shown.map((resource) => `<div class="text-[11px] text-[#9BB0CF] truncate">${escapeHtml(resource.identifier ?? "")}</div>`).join("")}
                      ${resources.length > shown.length ? `<div class="text-[11px] text-[#566778]">+${resources.length - shown.length} more</div>` : ""}
                    </div>
                  </details>`;
              }).join("")}
            </div>
          </details>`;
      }).join("")}
    </div>`;
}

function renderErrorsList(): void {
  const container = document.getElementById("scanErrorsList");
  if (!container) return;
  const errors: any[] = scanDetail?.errors ?? [];
  if (errors.length === 0) {
    container.innerHTML = "";
    return;
  }
  container.innerHTML = `
    <div class="text-[11px] font-bold uppercase tracking-[0.09em] text-[#566778] mb-1.5">Scan errors (${errors.length})</div>
    <div class="grid gap-1 max-h-52 overflow-y-auto">
      ${errors.slice(0, 100).map((entry) => `
        <div class="text-[12px] text-[#9BB0CF] px-3 py-1.5 rounded border border-[#E5484D]/25 bg-[#E5484D]/[0.06]">
          ${escapeHtml([entry.region, entry.resourceType].filter(Boolean).join(" · "))} — ${escapeHtml(entry.message ?? "")}
        </div>`).join("")}
      ${errors.length > 100 ? `<div class="text-[12px] text-[#566778] px-3">+${errors.length - 100} more</div>` : ""}
    </div>`;
}

async function generateBundle(): Promise<void> {
  if (!selectedScanID) {
    toast("Select a finished scan first.", "error");
    return;
  }
  const partition = (document.getElementById("scanPartitionName") as HTMLInputElement | null)?.value?.trim() ?? "";
  if (!partition) {
    toast("Enter a target partition name.", "error");
    return;
  }
  const grouping = (document.getElementById("scanGrouping") as HTMLSelectElement | null)?.value ?? "stack";
  const params = new URLSearchParams({ partition, grouping });
  const response: ScanBundleResponse = await fetchJSON(
    `/api/scans/${encodeURIComponent(selectedScanID)}/bundle?${params.toString()}`,
  );
  bundleResponse = response;
  selectedIntents = {};
  for (const intent of response.bundle?.intents ?? []) {
    selectedIntents[intent.manifest?.metadata?.name ?? ""] = true;
  }
  renderBundleResult();
}

function renderBundleResult(): void {
  const container = document.getElementById("scanBundleResult");
  if (!container) return;
  if (!bundleResponse) {
    container.innerHTML = "";
    return;
  }
  const draft = bundleResponse.draft ?? {};
  const warnings: string[] = draft.warnings ?? [];
  const managed: any[] = draft.managed ?? [];
  const intents = bundleResponse.bundle?.intents ?? [];
  if (intents.length === 0) {
    container.innerHTML = `
      <div class="empty-state text-sm text-[#566778]">
        Nothing new to import${managed.length > 0 ? ` — ${managed.length} scanned resources are already managed or imported.` : "."}
      </div>`;
    return;
  }
  const selectedCount = intents.filter((entry) => selectedIntents[entry.manifest?.metadata?.name ?? ""]).length;
  container.innerHTML = `
    ${warnings.length > 0 ? `
      <div class="grid gap-1 mb-3">
        ${warnings.map((warning) => `<div class="text-[12px] text-[#FCB519] px-3 py-1.5 rounded border border-[#FCB519]/25 bg-[#FCB519]/[0.06]">${escapeHtml(warning)}</div>`).join("")}
      </div>` : ""}
    <div class="grid gap-1.5 mb-3">
      ${intents.map((entry) => {
        const manifest = entry.manifest ?? {};
        const name = manifest.metadata?.name ?? "";
        const region = manifest.spec?.target?.region ?? "";
        const assets: any[] = manifest.spec?.assets ?? [];
        const checked = selectedIntents[name] ?? false;
        return `
          <label class="flex items-start gap-2.5 px-3.5 py-2.5 rounded-lg border ${checked ? "border-[#00ADE4]/50 bg-[#00ADE4]/[0.06]" : "border-white/[0.07] bg-[#0D1220]"} cursor-pointer">
            <input type="checkbox" ${checked ? "checked" : ""} data-intent-toggle="${escapeAttr(name)}" class="accent-[#00ADE4] mt-0.5" />
            <span class="min-w-0 flex-1">
              <span class="flex items-center gap-2 flex-wrap">
                <span class="text-[13px] font-semibold text-[#E5ECF4]">${escapeHtml(name)}</span>
                <span class="badge badge-neutral">${escapeHtml(region)}</span>
              </span>
              <span class="flex gap-1 flex-wrap mt-1">
                ${assets.map((asset) => `<span class="pill">${escapeHtml(asset.type ?? "")} · ${escapeHtml(asset.name ?? "")}</span>`).join("")}
              </span>
            </span>
          </label>`;
      }).join("")}
    </div>
    <details class="rounded border border-white/[0.07] bg-[#0D1220] mb-3">
      <summary class="px-3 py-2 text-[12px] text-[#E5ECF4] cursor-pointer select-none">Manifest preview (selected: ${selectedCount}/${intents.length})</summary>
      <pre class="px-3 pb-3 text-[11px] text-[#9BB0CF] overflow-x-auto whitespace-pre">${escapeHtml(previewJSON(intents))}</pre>
    </details>
    <div class="flex gap-2 flex-wrap">
      <button class="btn-primary" data-scan-save="reconcile" ${selectedCount === 0 ? "disabled" : ""}>Save &amp; reconcile</button>
      <button class="btn-secondary" data-scan-save="save" ${selectedCount === 0 ? "disabled" : ""}>Save only</button>
    </div>`;
}

function previewJSON(intents: { manifest: any }[]): string {
  const selected = intents.filter((entry) => selectedIntents[entry.manifest?.metadata?.name ?? ""]);
  return JSON.stringify(selected.map((entry) => entry.manifest), null, 2);
}

async function saveBundle(reconcile: boolean): Promise<void> {
  if (!bundleResponse || busy) return;
  const partition = (document.getElementById("scanPartitionName") as HTMLInputElement | null)?.value?.trim() ?? "";
  if (!partition) {
    toast("Enter a target partition name.", "error");
    return;
  }
  const intents = (bundleResponse.bundle?.intents ?? []).filter((entry) => selectedIntents[entry.manifest?.metadata?.name ?? ""]);
  if (intents.length === 0) {
    toast("Select at least one intent.", "error");
    return;
  }
  busy = true;
  try {
    await fetchJSON(`/api/partitions/${encodeURIComponent(partition)}/bundle`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        partition: bundleResponse.bundle.partition,
        intents,
        removeMissingIntents: false,
      }),
    });
    if (reconcile) {
      await fetchJSON(`/api/partitions/${encodeURIComponent(partition)}/reconcile`, { method: "POST" });
    }
    toast(`Bundle saved to partition ${partition}.`, "success");
    window.location.search = `?partition=${encodeURIComponent(partition)}`;
  } finally {
    busy = false;
  }
}
