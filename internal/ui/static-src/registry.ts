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

interface Repo {
  name: string;
  pusher: string;
  partition: string;
  source?: string;
  branch?: string;
  assemblySubdir?: string;
  path?: string;
  lastSyncedAt?: string;
  lastSyncError?: string;
}

let onError: (error: any) => void = () => undefined;
let pushers: Pusher[] = [];
let repos: Repo[] = [];
let loaded = false;
let busy = false;

export function initRegistryPanel(errorHandler: (error: any) => void): void {
  onError = errorHandler;
  document.getElementById("pusherSaveButton")?.addEventListener("click", () => savePusher().catch(onError));
  document.getElementById("repoSaveButton")?.addEventListener("click", () => saveRepo().catch(onError));
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
  });
}

export function renderRegistryPanel(): void {
  const panel = document.getElementById("registryPanel");
  if (!panel || panel.classList.contains("hidden")) return;
  if (!loaded) {
    loadRegistry().catch(onError);
  } else {
    renderPushers();
    renderRepos();
  }
}

async function loadRegistry(): Promise<void> {
  const [pusherResponse, repoResponse] = await Promise.all([
    fetchJSON("/api/pushers"),
    fetchJSON("/api/repos"),
  ]);
  pushers = pusherResponse.pushers ?? [];
  repos = repoResponse.repos ?? [];
  loaded = true;
  renderPushers();
  renderRepos();
  renderRepoPusherOptions();
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
    .map(
      (repo) => `
      <div class="flex items-center justify-between gap-3 border border-[#1d2b3f] rounded px-3 py-2">
        <div class="min-w-0">
          <div class="text-sm text-[#dbe7f7] font-semibold">${escapeHtml(repo.name)} → ${escapeHtml(repo.partition)}</div>
          <div class="text-[12px] text-[#7f93ad] truncate">pusher ${escapeHtml(repo.pusher)} · ${escapeHtml(repo.source || repo.path || "no source")}</div>
          ${repo.lastSyncError ? `<div class="text-[12px] text-[#f87171] truncate">${escapeHtml(repo.lastSyncError)}</div>` : repo.lastSyncedAt ? `<div class="text-[12px] text-[#566778]">last ingest ${escapeHtml(formatDateTime(repo.lastSyncedAt))}</div>` : ""}
        </div>
        <div class="flex items-center gap-2">
          <button class="btn-secondary text-[12px] px-2 py-0.5" data-repo-action="ingest" data-repo-name="${escapeAttr(repo.name)}">Ingest</button>
          <button class="btn-ghost text-[12px]" data-repo-action="delete" data-repo-name="${escapeAttr(repo.name)}">Remove</button>
        </div>
      </div>`,
    )
    .join("");
}

function renderRepoPusherOptions(): void {
  const select = document.getElementById("repoPusher") as HTMLSelectElement | null;
  if (!select) return;
  const current = select.value;
  select.innerHTML = pushers.map((pusher) => `<option value="${escapeAttr(pusher.name)}">${escapeHtml(pusher.name)}</option>`).join("");
  if (current) select.value = current;
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
  const pusher = (document.getElementById("repoPusher") as HTMLSelectElement | null)?.value ?? "";
  const payload = {
    name: value("repoName"),
    pusher,
    partition: value("repoPartition"),
    source: value("repoSource"),
    branch: value("repoBranch"),
    assemblySubdir: value("repoAssemblySubdir") || "cdk.out",
    path: value("repoPath"),
    description: value("repoDescription"),
  };
  if (!payload.pusher) {
    toast("Register a pusher first", "error");
    return;
  }
  if (!payload.partition) {
    toast("Partition is required", "error");
    return;
  }
  busy = true;
  try {
    await fetchJSON("/api/repos", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    toast(`Repo ${payload.name} registered`, "success");
    ["repoName", "repoPartition", "repoSource", "repoBranch", "repoPath", "repoDescription"].forEach((id) => {
      const el = document.getElementById(id) as HTMLInputElement | null;
      if (el) el.value = "";
    });
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
