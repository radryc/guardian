import { escapeHtml, fetchJSON, toast } from "./utils";

/**
 * Unmanaged stacks panel: lists CloudFormation stacks that are not covered by
 * a repository-backed partition and prepares repo associations (observed
 * snapshot + MR branch).
 */

let onError: (error: any) => void = (error) => console.error(error);
let stacks: any[] = [];
let loading = false;

export function initUnmanagedPanel(errorHandler: (error: any) => void): void {
  onError = errorHandler;
  document.getElementById("unmanagedRefreshButton")?.addEventListener("click", () => refreshUnmanaged().catch(onError));
  document.getElementById("unmanagedList")?.addEventListener("click", (event) => {
    const target = (event.target as HTMLElement).closest<HTMLElement>("[data-unmanaged-associate]");
    if (!target) return;
    associateStack(target.dataset.unmanagedAssociate ?? "").catch(onError);
  });
}

export function renderUnmanagedPanel(_options: { targetPartition?: string; resetTarget?: boolean } = {}): void {
  refreshUnmanaged().catch(onError);
}

async function refreshUnmanaged(): Promise<void> {
  if (loading) return;
  loading = true;
  const container = document.getElementById("unmanagedList");
  if (container) container.innerHTML = `<div class="loading-state text-sm text-[#566778]">Loading stacks…</div>`;
  try {
    const result = await fetchJSON("/api/aws/unmanaged");
    stacks = Array.isArray(result?.stacks) ? result.stacks : [];
    renderList(Array.isArray(result?.warnings) ? result.warnings : []);
  } finally {
    loading = false;
  }
}

function renderList(warnings: string[]): void {
  const container = document.getElementById("unmanagedList");
  if (!container) return;
  const warningHtml = warnings.length
    ? `<div class="text-[12px] text-amber-300 mb-2">${warnings.map(escapeHtml).join("<br>")}</div>`
    : "";
  if (!stacks.length) {
    container.innerHTML = warningHtml + `<div class="text-[12px] text-[#566778]">No unmanaged stacks detected. Every live stack is covered by a repository-backed partition.</div>`;
    return;
  }
  const rows = stacks.map((stack) => {
    const stateBadge = stack.state === "missing-repo"
      ? `<span class="badge badge-attention">missing repo</span>`
      : `<span class="badge">unmanaged</span>`;
    const partition = stack.partition ? escapeHtml(stack.partition) : "—";
    const drift = stack.driftStatus ? escapeHtml(stack.driftStatus) : "—";
    return `<tr>
      <td>${stateBadge}</td>
      <td class="font-mono text-[12px]">${escapeHtml(stack.stackName ?? "")}</td>
      <td class="text-[12px]">${escapeHtml(stack.region ?? "")}</td>
      <td class="text-[12px]">${escapeHtml(stack.pusher ?? "")}</td>
      <td class="text-[12px]">${escapeHtml(stack.stackStatus ?? "")}</td>
      <td class="text-[12px]">${drift}</td>
      <td class="text-[12px]">${partition}</td>
      <td><button class="btn-secondary" data-unmanaged-associate="${escapeHtml(stack.stackName ?? "")}">Associate repo</button></td>
    </tr>`;
  }).join("");
  container.innerHTML = warningHtml + `<table class="data-table w-full text-left">
    <thead><tr>
      <th>State</th><th>Stack</th><th>Region</th><th>Pusher</th><th>Status</th><th>Drift</th><th>Partition</th><th></th>
    </tr></thead>
    <tbody>${rows}</tbody>
  </table>`;
}

async function associateStack(stackName: string): Promise<void> {
  const stack = stacks.find((candidate) => candidate.stackName === stackName);
  if (!stack) return;
  const repoUrl = window.prompt(`Repository URL to associate with ${stackName}:`);
  if (!repoUrl) return;
  const body: Record<string, string> = {
    repoUrl,
    pusher: stack.pusher ?? "",
    account: stack.account ?? "",
    region: stack.region ?? "",
  };
  if (stack.partition) body.partition = stack.partition;
  const result = await fetchJSON(`/api/aws/unmanaged/${encodeURIComponent(stackName)}/associate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (result?.publishError) {
    toast(`Repo registered, but the MonoFS publish failed: ${result.publishError}`, "error");
    await refreshUnmanaged();
    return;
  }
  const publish = result?.publish;
  if (publish?.repository?.pullRequestUrl) {
    const created = publish.repository.pullRequestCreated ? "created" : "ready to open";
    toast(`MR/PR ${created}: ${publish.repository.pullRequestUrl} (${publish.repository.targetBranch ?? result?.branch ?? ""})`, "success");
  } else if (publish?.repository?.targetBranch) {
    toast(`Published review branch ${publish.repository.targetBranch}`, "success");
  } else {
    const fileCount = Object.keys(result?.files ?? {}).length;
    toast(`Prepared ${result?.branch ?? "adoption branch"} (${fileCount} file(s)). Publish the branch through MonoFS to open the MR.`, "success");
  }
  await refreshUnmanaged();
}
