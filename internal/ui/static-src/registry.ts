import { fetchJSON, escapeHtml, escapeAttr, formatDateTime, toast } from "./utils";

interface Pusher {
  name: string;
  account?: string;
  region?: string;
  observedApiUrl: string;
  tokenEnv?: string;
  description?: string;
  updatedAt?: string;
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
  partition?: string;
  bindings?: RepoBinding[];
  path?: string;
  source?: string;
  sourceType?: string;
  branch?: string;
  assemblySubdir?: string;
  lastSyncedAt?: string;
  lastSyncError?: string;
}

export interface TargetPartitionOptions {
  targetPartition?: string;
  resetTarget?: boolean;
}

interface BindingDraft {
  selector: string;
  path: string;
}

let onError: (error: any) => void = () => undefined;
let pushers: Pusher[] = [];
let repos: Repo[] = [];
let partitions: string[] = [];
let loaded = false;
let busy = false;
let targetPrefill = "";
const formBindings = new Map<string, BindingDraft>();
const assignBindings = new Map<string, BindingDraft>();
let assignRepoName = "";

export function initRegistryPanel(errorHandler: (error: any) => void): void {
  onError = errorHandler;
  document.getElementById("pusherSaveButton")?.addEventListener("click", () => savePusher().catch(onError));
  document.getElementById("repoSaveButton")?.addEventListener("click", () => saveRepo().catch(onError));
  document.getElementById("repoSourceType")?.addEventListener("change", updateSourceFields);
  document.getElementById("repoAssignSave")?.addEventListener("click", () => saveAssignments().catch(onError));
  document.getElementById("repoAssignCancel")?.addEventListener("click", closeAssign);
  wireBindingPicker("repoPartitionPicker", formBindings);
  wireBindingPicker("repoAssignPicker", assignBindings);
  document.getElementById("pusherList")?.addEventListener("click", (event) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>("[data-pusher-delete]");
    if (!target) return;
    deletePusher(target.dataset.pusherDelete ?? "").catch(onError);
  });
  document.getElementById("repoList")?.addEventListener("click", (event) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>("[data-repo-action]");
    if (!target) return;
    const name = target.dataset.repoName ?? "";
    if (target.dataset.repoAction === "delete") deleteRepo(name).catch(onError);
    if (target.dataset.repoAction === "ingest") ingestRepo(name).catch(onError);
    if (target.dataset.repoAction === "assign") startAssign(name);
  });
}

function wireBindingPicker(id: string, bindings: Map<string, BindingDraft>): void {
  const container = document.getElementById(id);
  if (!container) return;
  container.addEventListener("change", (event) => {
    const target = event.target as HTMLElement;
    if (!(target instanceof HTMLInputElement)) return;
    const name = target.dataset.bindingCheck;
    if (!name) return;
    if (target.checked) bindings.set(name, bindings.get(name) ?? { selector: "", path: "" });
    else bindings.delete(name);
    renderBindingPicker(id, bindings);
  });
  container.addEventListener("input", (event) => {
    const target = event.target as HTMLElement;
    if (!(target instanceof HTMLInputElement)) return;
    const name = target.dataset.bindingSelector ?? target.dataset.bindingPath;
    if (!name) return;
    const draft = bindings.get(name) ?? { selector: "", path: "" };
    if (target.dataset.bindingSelector) draft.selector = target.value;
    if (target.dataset.bindingPath) draft.path = target.value;
    bindings.set(name, draft);
  });
}

export function renderRegistryPanel(options: TargetPartitionOptions = {}): void {
  applyTargetPartition(options);
  const panel = document.getElementById("registryPanel");
  if (!panel || panel.classList.contains("hidden")) return;
  if (!loaded) {
    loadRegistry().catch(onError);
  } else {
    renderPushers();
    renderRepos();
    renderRepoPusherOptions();
    renderAssignPusherOptions();
    renderBindingPicker("repoPartitionPicker", formBindings);
    renderBindingPicker("repoAssignPicker", assignBindings);
    updateSourceFields();
  }
}

function applyTargetPartition(options: TargetPartitionOptions): void {
  const target = options.targetPartition?.trim() ?? "";
  const untouched = formBindings.size === 0 || (formBindings.size === 1 && targetPrefill !== "" && formBindings.has(targetPrefill));
  if (options.resetTarget) {
    if (untouched) {
      formBindings.clear();
      targetPrefill = "";
    }
    return;
  }
  if (target && untouched) {
    formBindings.clear();
    formBindings.set(target, { selector: "", path: "" });
    targetPrefill = target;
  }
}

function repoBindings(repo: Repo): RepoBinding[] {
  if (repo.bindings && repo.bindings.length) return repo.bindings;
  if (repo.partition) return [{ partition: repo.partition, path: repo.path }];
  return [];
}

function collectBindings(bindings: Map<string, BindingDraft>): RepoBinding[] {
  return [...bindings.entries()].map(([partition, draft]) => {
    const binding: RepoBinding = { partition };
    if (draft.selector.trim()) binding.selector = draft.selector.trim();
    if (draft.path.trim()) binding.path = draft.path.trim();
    return binding;
  });
}

function renderBindingPicker(id: string, bindings: Map<string, BindingDraft>): void {
  const container = document.getElementById(id);
  if (!container) return;
  const names = [...new Set([...partitions, ...bindings.keys()])].sort((a, b) => a.localeCompare(b));
  if (names.length === 0) {
    container.innerHTML = `<p class="text-[12px] text-[#566778]">No partitions found. Create one first from Applications or Build.</p>`;
    return;
  }
  container.innerHTML = names.map((name) => {
    const checked = bindings.has(name);
    const draft = bindings.get(name) ?? { selector: "", path: "" };
    const classes = checked
      ? "border-[#00ADE4]/50 bg-[#00ADE4]/[0.06]"
      : "border-white/[0.07] bg-[#0D1220]";
    return `
      <div class="px-2.5 py-1.5 rounded border ${classes}">
        <label class="flex items-center gap-2 cursor-pointer">
          <input type="checkbox" data-binding-check="${escapeAttr(name)}" ${checked ? "checked" : ""} class="accent-[#00ADE4]" />
          <span class="text-[12px] text-[#E5ECF4] truncate flex-1">${escapeHtml(name)}</span>
          ${checked ? "" : `<span class="text-[11px] text-[#566778]">not assigned</span>`}
        </label>
        ${checked ? `
        <div class="grid grid-cols-2 gap-2 mt-2 pl-6">
          <input class="search-input" data-binding-selector="${escapeAttr(name)}" value="${escapeAttr(draft.selector)}" placeholder="Stack selector (empty = all)" />
          <input class="search-input" data-binding-path="${escapeAttr(name)}" value="${escapeAttr(draft.path)}" placeholder="/partitions/${escapeAttr(name)}/payloads/aws" />
        </div>` : ""}
      </div>`;
  }).join("");
}

async function loadRegistry(): Promise<void> {
  const [pusherResponse, repoResponse, overview] = await Promise.all([
    fetchJSON("/api/pushers"),
    fetchJSON("/api/repos"),
    fetchJSON("/api/overview").catch(() => null),
  ]);
  pushers = pusherResponse.pushers ?? [];
  repos = repoResponse.repos ?? [];
  partitions = (overview?.partitions ?? []).map((p: any) => String(p?.name ?? "")).filter(Boolean);
  loaded = true;
  renderPushers();
  renderRepos();
  renderRepoPusherOptions();
  renderAssignPusherOptions();
  renderBindingPicker("repoPartitionPicker", formBindings);
  renderBindingPicker("repoAssignPicker", assignBindings);
  updateSourceFields();
}

function renderPushers(): void {
  const container = document.getElementById("pusherList");
  if (!container) return;
  if (pushers.length === 0) {
    container.innerHTML = `<p class="text-[13px] text-[#566778]">No pushers registered yet.</p>`;
    return;
  }
  container.innerHTML = pushers
    .map(
      (pusher) => `
      <div class="flex items-center justify-between gap-3 border border-[#1d2b3f] rounded px-3 py-2">
        <div class="min-w-0">
          <div class="text-sm text-[#dbe7f7] font-semibold">${escapeHtml(pusher.name)}</div>
          <div class="text-[12px] text-[#7f93ad] truncate">${escapeHtml(pusher.observedApiUrl)}${pusher.account ? ` · account ${escapeHtml(pusher.account)}` : ""}${pusher.region ? ` · ${escapeHtml(pusher.region)}` : ""}</div>
        </div>
        <button class="btn-ghost text-[12px]" data-pusher-delete="${escapeAttr(pusher.name)}">Remove</button>
      </div>`,
    )
    .join("");
}

function renderRepos(): void {
  const container = document.getElementById("repoList");
  if (!container) return;
  if (repos.length === 0) {
    container.innerHTML = `<p class="text-[13px] text-[#566778]">No repositories registered yet.</p>`;
    return;
  }
  container.innerHTML = repos
    .map((repo) => {
      const bindings = repoBindings(repo);
      const chips = bindings.length
        ? bindings.map((binding) => `<span class="pill">${escapeHtml(binding.partition)}${binding.selector ? ` <span class="text-[#566778]">· ${escapeHtml(binding.selector)}</span>` : ""}</span>`).join(" ")
        : `<span class="pill">no partitions</span>`;
      const sourceType = repo.sourceType === "monofs" ? "MonoFS" : "Git";
      const syncLine = repo.lastSyncError
        ? `<div class="text-[12px] text-[#f87171] truncate">${escapeHtml(repo.lastSyncError)}</div>`
        : repo.lastSyncedAt
          ? `<div class="text-[12px] text-[#566778]">last ingest ${escapeHtml(formatDateTime(repo.lastSyncedAt))}</div>`
          : "";
      return `
      <div class="border border-[#1d2b3f] rounded px-3 py-2">
        <div class="flex items-center justify-between gap-3">
          <div class="min-w-0">
            <div class="text-sm text-[#dbe7f7] font-semibold">${escapeHtml(repo.name)}</div>
            <div class="flex flex-wrap gap-1 mt-1">${chips}</div>
            <div class="text-[12px] text-[#7f93ad] truncate mt-1">${sourceType} · pusher ${escapeHtml(repo.pusher || "unassigned")} · ${escapeHtml(repo.source || repo.path || "no source")}</div>
            ${syncLine}
          </div>
          <div class="flex items-center gap-2 shrink-0">
            <button class="btn-secondary text-[12px] px-2 py-0.5" data-repo-action="assign" data-repo-name="${escapeAttr(repo.name)}">Assign</button>
            <button class="btn-secondary text-[12px] px-2 py-0.5" data-repo-action="ingest" data-repo-name="${escapeAttr(repo.name)}"${repo.pusher ? "" : ` disabled title="Assign a pusher first"`}>Ingest</button>
            <button class="btn-ghost text-[12px]" data-repo-action="delete" data-repo-name="${escapeAttr(repo.name)}">Remove</button>
          </div>
        </div>
      </div>`;
    })
    .join("");
}

function renderRepoPusherOptions(): void {
  const select = document.getElementById("repoPusher") as HTMLSelectElement | null;
  if (!select) return;
  const current = select.value;
  select.innerHTML = `<option value="">— none (bind later) —</option>` +
    pushers.map((pusher) => `<option value="${escapeAttr(pusher.name)}">${escapeHtml(pusher.name)}</option>`).join("");
  if (current) select.value = current;
}

function renderAssignPusherOptions(current?: string): void {
  const select = document.getElementById("repoAssignPusher") as HTMLSelectElement | null;
  if (!select) return;
  const selected = current ?? select.value;
  select.innerHTML = `<option value="">— none (bind later) —</option>` +
    pushers.map((pusher) => `<option value="${escapeAttr(pusher.name)}">${escapeHtml(pusher.name)}</option>`).join("");
  if (selected && pushers.some((pusher) => pusher.name === selected)) {
    select.value = selected;
  }
}

function updateSourceFields(): void {
  const type = (document.getElementById("repoSourceType") as HTMLSelectElement | null)?.value ?? "git";
  const label = document.getElementById("repoSourceLabel");
  const source = document.getElementById("repoSource") as HTMLInputElement | null;
  const branch = document.getElementById("repoBranchField");
  if (label) label.textContent = type === "monofs" ? "MonoFS repository path" : "Git URL or local checkout";
  if (source) source.placeholder = type === "monofs" ? "/.repos/github.com/org/strata" : "https://github.com/org/strata.git";
  if (branch) branch.hidden = type === "monofs";
}

async function savePusher(): Promise<void> {
  if (busy) return;
  const value = (id: string): string => (document.getElementById(id) as HTMLInputElement | null)?.value?.trim() ?? "";
  const payload = {
    name: value("pusherName"),
    account: value("pusherAccount"),
    region: value("pusherRegion"),
    observedApiUrl: value("pusherUrl"),
    tokenEnv: value("pusherTokenEnv"),
    token: value("pusherToken"),
    description: value("pusherDescription"),
  };
  if (!payload.observedApiUrl) {
    toast("Pusher URL is required", "error");
    return;
  }
  busy = true;
  try {
    await fetchJSON("/api/pushers", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    toast(`Pusher ${payload.name || payload.account} registered`, "success");
    ["pusherName", "pusherUrl", "pusherToken", "pusherDescription"].forEach((id) => {
      const el = document.getElementById(id) as HTMLInputElement | null;
      if (el) el.value = "";
    });
    await loadRegistry();
  } finally {
    busy = false;
  }
}

async function saveRepo(): Promise<void> {
  if (busy) return;
  const value = (id: string): string => (document.getElementById(id) as HTMLInputElement | null)?.value?.trim() ?? "";
  const name = value("repoName");
  const sourceType = (document.getElementById("repoSourceType") as HTMLSelectElement | null)?.value ?? "git";
  const pusher = (document.getElementById("repoPusher") as HTMLSelectElement | null)?.value ?? "";
  const bindings = collectBindings(formBindings);
  const source = value("repoSource");
  if (!name) {
    toast("Repository name is required", "error");
    return;
  }
  if (!source) {
    toast("Repository source is required", "error");
    return;
  }
  if (sourceType === "monofs" && !source.startsWith("/") && !source.startsWith("monofs://")) {
    toast("MonoFS sources must start with / or monofs://", "error");
    return;
  }
  const payload = {
    name,
    sourceType,
    pusher,
    bindings,
    source,
    branch: sourceType === "git" ? value("repoBranch") : "",
    assemblySubdir: value("repoAssemblySubdir") || "cdk.out",
    description: value("repoDescription"),
  };
  busy = true;
  try {
    await fetchJSON("/api/repos", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    toast(`Repo ${name} registered`, "success");
    ["repoName", "repoSource", "repoBranch", "repoPath", "repoDescription"].forEach((id) => {
      const el = document.getElementById(id) as HTMLInputElement | null;
      if (el) el.value = "";
    });
    formBindings.clear();
    targetPrefill = "";
    await loadRegistry();
  } finally {
    busy = false;
  }
}

function startAssign(name: string): void {
  const repo = repos.find((entry) => entry.name === name);
  if (!repo) return;
  assignRepoName = name;
  assignBindings.clear();
  renderAssignPusherOptions(repo.pusher ?? "");
  for (const binding of repoBindings(repo)) {
    assignBindings.set(binding.partition, { selector: binding.selector ?? "", path: binding.path ?? "" });
  }
  const panel = document.getElementById("repoAssignPanel");
  panel?.classList.remove("hidden");
  const label = document.getElementById("repoAssignName");
  if (label) label.textContent = name;
  renderBindingPicker("repoAssignPicker", assignBindings);
}

function closeAssign(): void {
  assignRepoName = "";
  assignBindings.clear();
  document.getElementById("repoAssignPanel")?.classList.add("hidden");
}

async function saveAssignments(): Promise<void> {
  if (busy || !assignRepoName) return;
  const bindings = collectBindings(assignBindings);
  const pusher = (document.getElementById("repoAssignPusher") as HTMLSelectElement | null)?.value ?? "";
  busy = true;
  try {
    await fetchJSON(`/api/repos/${encodeURIComponent(assignRepoName)}/bindings`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ bindings, pusher }),
    });
    toast(`Assignments updated for ${assignRepoName}`, "success");
    closeAssign();
    await loadRegistry();
  } finally {
    busy = false;
  }
}

async function deletePusher(name: string): Promise<void> {
  if (!name) return;
  await fetchJSON(`/api/pushers/${encodeURIComponent(name)}`, { method: "DELETE" });
  toast(`Pusher ${name} removed`, "success");
  await loadRegistry();
}

async function deleteRepo(name: string): Promise<void> {
  if (!name) return;
  await fetchJSON(`/api/repos/${encodeURIComponent(name)}`, { method: "DELETE" });
  if (assignRepoName === name) closeAssign();
  toast(`Repo ${name} removed`, "success");
  await loadRegistry();
}

async function ingestRepo(name: string): Promise<void> {
  if (!name || busy) return;
  busy = true;
  try {
    const result = await fetchJSON(`/api/repos/${encodeURIComponent(name)}/ingest`, { method: "POST" });
    toast(`Ingested ${name}: ${(result.intents ?? []).length} intent(s)`, "success");
    await loadRegistry();
  } finally {
    busy = false;
  }
}
