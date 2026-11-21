const token = new URLSearchParams(location.search).get("token") || "";

const state = {
  tab: "overview",
  overview: null,
  partitions: [],
  selected: "",
  detail: null,
  catalog: [],
  status: [],
  submitResult: null,
  busy: false,
};

const $ = (id) => document.getElementById(id);

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function label(key) {
  return state.overview?.display?.[key] || key;
}

function labelWithCanonical(key) {
  const value = label(key);
  return value === key ? key : `${value} (${key})`;
}

async function api(path, options = {}) {
  const headers = { "X-Forge-Token": token, ...(options.headers || {}) };
  if (options.body) headers["Content-Type"] = "application/json";
  const response = await fetch(path, { ...options, headers });
  let data = {};
  try {
    data = await response.json();
  } catch {
    data = {};
  }
  if (!response.ok) {
    throw new Error(data.error || `${response.status} ${response.statusText}`);
  }
  return data;
}

function toast(message, kind = "info") {
  const el = $("toast");
  el.textContent = message;
  el.className = `toast ${kind}`;
  clearTimeout(toast._timer);
  toast._timer = setTimeout(() => el.classList.add("hidden"), 5000);
}

function diagnosticRows(list) {
  if (!list?.length) return `<p class="muted">No diagnostics.</p>`;
  return `<table class="table">
    <thead><tr><th>Severity</th><th>Code</th><th>Partition</th><th>File</th><th>Message</th></tr></thead>
    <tbody>${list
      .map(
        (d) => `<tr class="sev-${escapeHtml(d.severity)}">
          <td>${escapeHtml(d.severity)}</td>
          <td><code>${escapeHtml(d.code)}</code></td>
          <td>${escapeHtml(d.partition || "")}</td>
          <td class="mono small">${escapeHtml(d.file || "")}</td>
          <td>${escapeHtml(d.message)}${d.hint ? `<div class="hint">${escapeHtml(d.hint)}</div>` : ""}</td>
        </tr>`,
      )
      .join("")}</tbody>
  </table>`;
}

function renderPartitionList() {
  const el = $("partitionList");
  const newButton = `<button class="ghost" data-action="new-partition">+ New partition</button>`;
  if (!state.partitions.length) {
    el.innerHTML = `<p class="muted small">No partitions found.</p>${newButton}`;
    return;
  }
  el.innerHTML =
    state.partitions
      .map(
        (name) =>
          `<button class="partition ${name === state.selected ? "selected" : ""}" data-action="select" data-name="${escapeHtml(name)}">
            ${escapeHtml(name)}
          </button>`,
      )
      .join("") + newButton;
}

function renderTopActions() {
  const el = $("topActions");
  if (state.tab === "validate") {
    el.innerHTML = `<button data-action="validate">Run validation</button>`;
  } else if (state.tab === "status") {
    el.innerHTML = `<button data-action="refresh-status">Refresh</button>`;
  } else if (state.tab === "alerts") {
    el.innerHTML = `<button data-action="suggest-alerts">Suggest alerts</button>`;
  } else {
    el.innerHTML = `<button class="ghost" data-action="refresh">Refresh</button>`;
  }
}

function render() {
  document.querySelectorAll(".nav button").forEach((button) => {
    button.classList.toggle("active", button.dataset.tab === state.tab);
  });
  renderPartitionList();
  renderTopActions();
  const badge = $("displayBadge");
  const keys = state.overview?.display || {};
  if (Object.keys(keys).length) {
    badge.textContent = `Display: ${Object.entries(keys).map(([k, v]) => `${v}→${k}`).join(", ")}`;
    badge.classList.remove("hidden");
  }

  const titles = {
    overview: ["Overview", "Repository readiness"],
    partition: ["Partition", state.selected ? `partitions/${state.selected}` : "select a partition"],
    assets: ["Assets", "add and connect typed assets"],
    alerts: ["Alerts", "starter rule groups using canonical labels"],
    validate: ["Validate", "offline checks before a proposal"],
    submit: ["Submit", "opens a reviewed PR/MR through MonoFS"],
    status: ["Status", "local proposal cache"],
  };
  $("title").textContent = titles[state.tab][0];
  $("subtitle").textContent = titles[state.tab][1];

  const content = $("content");
  if (state.tab === "overview") content.innerHTML = viewOverview();
  else if (state.tab === "partition") content.innerHTML = viewPartition();
  else if (state.tab === "assets") content.innerHTML = viewAssets();
  else if (state.tab === "alerts") content.innerHTML = viewAlerts();
  else if (state.tab === "validate") content.innerHTML = viewValidate();
  else if (state.tab === "submit") content.innerHTML = viewSubmit();
  else if (state.tab === "status") content.innerHTML = viewStatus();
}

function viewOverview() {
  const validation = state.overview?.validation || { errors: 0, warnings: 0, partitionsChecked: 0, diagnostics: [] };
  return `<div class="cards">
      <div class="card"><span>Root</span><strong class="mono small">${escapeHtml(state.overview?.root || "")}</strong></div>
      <div class="card"><span>Partitions</span><strong>${validation.partitionsChecked}</strong></div>
      <div class="card"><span>Errors</span><strong class="${validation.errors ? "bad" : "good"}">${validation.errors}</strong></div>
      <div class="card"><span>Warnings</span><strong class="${validation.warnings ? "warn" : "good"}">${validation.warnings}</strong></div>
    </div>
    <div class="panel">
      <h2>Validation</h2>
      ${diagnosticRows(validation.diagnostics)}
    </div>`;
}

function viewPartition() {
  if (!state.detail) return `<p class="muted">Select a partition.</p>`;
  const intents = state.detail.intents || [];
  const alerts = state.detail.alerts || [];
  return `<div class="panel">
      <h2>Partition config</h2>
      <pre class="yaml">${escapeHtml(state.detail.config)}</pre>
    </div>
    ${intents
      .map(
        (intent) => `<div class="panel">
          <h2>Intent ${escapeHtml(intent.name)}</h2>
          <pre class="yaml">${escapeHtml(intent.yaml)}</pre>
        </div>`,
      )
      .join("")}
    ${alerts.length
      ? `<div class="panel"><h2>Alerts</h2>${alerts
          .map((alert) => `<h3>${escapeHtml(alert.name)}</h3><pre class="yaml">${escapeHtml(alert.yaml)}</pre>`)
          .join("")}</div>`
      : ""}`;
}

function intentOptions() {
  return (state.detail?.intents || [])
    .map((intent) => `<option value="${escapeHtml(intent.name)}">${escapeHtml(intent.name)}</option>`)
    .join("");
}

function assetRows() {
  const rows = [];
  for (const intent of state.detail?.intents || []) {
    for (const asset of intent.assets || []) {
      rows.push({ intent: intent.name, ...asset });
    }
  }
  return rows;
}

function viewAssets() {
  if (!state.detail) return `<p class="muted">Select a partition.</p>`;
  const rows = assetRows();
  const types = (state.catalog || []).map((template) => template.type);
  return `<div class="panel">
      <h2>${escapeHtml(labelWithCanonical("asset"))}s</h2>
      ${
        rows.length
          ? `<table class="table">
        <thead><tr><th>Intent</th><th>Type</th><th>${escapeHtml(labelWithCanonical("asset"))}</th><th>Depends on</th></tr></thead>
        <tbody>${rows
          .map(
            (row) => `<tr><td>${escapeHtml(row.intent)}</td><td>${escapeHtml(row.type)}</td>
              <td>${escapeHtml(row.name)}</td><td class="mono small">${escapeHtml((row.dependsOn || []).join(", "))}</td></tr>`,
          )
          .join("")}</tbody></table>`
          : `<p class="muted">No assets yet.</p>`
      }
    </div>
    <div class="grid two">
      <form class="panel form" data-form="asset">
        <h2>Add asset</h2>
        <label>Intent<select name="intent">${intentOptions()}</select></label>
        <label>Type<select name="type">${types.map((t) => `<option>${escapeHtml(t)}</option>`).join("")}</select></label>
        <label>Name<input name="name" required pattern="[a-z][a-z0-9-]*" placeholder="api" /></label>
        <label>Image (Compute)<input name="image" placeholder="ghcr.io/acme/api:1" /></label>
        <div class="row">
          <label>Port<input name="port" type="number" min="1" max="65535" /></label>
          <label>Replicas<input name="replicas" type="number" min="1" /></label>
        </div>
        <button type="submit">Add asset</button>
      </form>
      <form class="panel form" data-form="probe">
        <h2>Add ${escapeHtml(label("asset"))} probe</h2>
        <label>Intent<select name="intent">${intentOptions()}</select></label>
        <label>Target asset<input name="asset" required placeholder="api" /></label>
        <label>Path<input name="path" placeholder="/healthz" /></label>
        <label>Port<input name="port" type="number" min="1" max="65535" /></label>
        <button type="submit">Add probe</button>
      </form>
      <form class="panel form" data-form="secret">
        <h2>Secret reference</h2>
        <label>Intent<select name="intent">${intentOptions()}</select></label>
        <label>Compute asset<input name="asset" required placeholder="api" /></label>
        <label>Environment variable<input name="env" required placeholder="DATABASE_URL" /></label>
        <label>Secret ref<input name="ref" required placeholder="monofs-secret://checkout/db" /></label>
        <button type="submit">Attach reference</button>
      </form>
      <form class="panel form" data-form="connect">
        <h2>Connect assets</h2>
        <label>Intent<select name="intent">${intentOptions()}</select></label>
        <label>From<input name="from" required placeholder="api" /></label>
        <label>To<input name="to" required placeholder="settings" /></label>
        <button type="submit">Add dependsOn</button>
      </form>
    </div>`;
}

function viewAlerts() {
  if (!state.detail) return `<p class="muted">Select a partition.</p>`;
  return `<div class="panel">
      <h2>Suggest</h2>
      <p class="muted">Generated with canonical metric labels. Display aliases never reach PromQL.</p>
      <pre class="yaml" id="alertPreview">${escapeHtml(state.alertPreview || "Click “Suggest alerts” to generate a starter group.")}</pre>
      <button data-action="write-alerts" ${state.alertPreview ? "" : "disabled"}>Write alerts/suggested-health.yaml</button>
    </div>`;
}

function viewValidate() {
  const result = state.validation || state.overview?.validation;
  return `<div class="panel">
      <h2>Result</h2>
      ${result ? `<p class="muted">${result.partitionsChecked} partition(s) · ${result.errors} error(s) · ${result.warnings} warning(s)</p>${diagnosticRows(result.diagnostics)}` : `<p class="muted">Run validation to see results.</p>`}
    </div>`;
}

function viewSubmit() {
  const result = state.submitResult;
  return `<form class="panel form" data-form="submit">
      <h2>Proposal</h2>
      <label>Commit message<input name="message" placeholder="checkout: add api intent" /></label>
      <label>Pull request title<input name="title" placeholder="optional" /></label>
      <label>Pull request body<textarea name="body" rows="6" placeholder="Plan, validation summary, context…"></textarea></label>
      <label class="check"><input type="checkbox" name="all" /> submit the whole partition instead of changed files</label>
      <div class="row">
        <button type="submit" data-mode="dry" class="ghost">Dry run</button>
        <button type="submit" data-mode="real">Submit PR/MR</button>
      </div>
      <p class="muted small">Submitting opens a review branch and PR/MR through MonoFS. Merging is what deploys.</p>
    </form>
    ${
      result
        ? `<div class="panel">
        <h2>Result</h2>
        <p class="muted">${escapeHtml(result.partition)} · ${escapeHtml(result.status || "")} ${result.dryRun ? "(dry run)" : ""}</p>
        ${
          result.operations?.length
            ? `<table class="table"><thead><tr><th>Kind</th><th>Path</th></tr></thead><tbody>${result.operations
                .map((op) => `<tr><td>${escapeHtml(op.kind)}</td><td class="mono small">${escapeHtml(op.path)}</td></tr>`)
                .join("")}</tbody></table>`
            : ""
        }
        ${result.pullRequestUrl ? `<p><a href="${escapeHtml(result.pullRequestUrl)}" target="_blank" rel="noreferrer">${escapeHtml(result.pullRequestUrl)}</a></p>` : ""}
        ${result.jobId ? `<p class="mono small">job ${escapeHtml(result.jobId)}</p>` : ""}
      </div>`
        : ""
    }`;
}

function viewStatus() {
  return `<div class="panel">
      <h2>Proposals</h2>
      ${
        state.status.length
          ? `<table class="table"><thead><tr><th>Partition</th><th>Job</th><th>Branch</th><th>PR</th><th>Submitted</th></tr></thead><tbody>${state.status
              .map(
                (item) => `<tr><td>${escapeHtml(item.partition)}</td><td class="mono small">${escapeHtml(item.jobId || "")}</td>
                <td class="mono small">${escapeHtml(item.targetBranch || "")}</td>
                <td>${item.pullRequestUrl ? `<a href="${escapeHtml(item.pullRequestUrl)}" target="_blank" rel="noreferrer">open</a>` : ""}</td>
                <td class="small">${escapeHtml(new Date(item.submittedAt).toLocaleString())}</td></tr>`,
              )
              .join("")}</tbody></table>`
          : `<p class="muted">No submissions recorded under this repository.</p>`
      }
    </div>`;
}

async function loadOverview() {
  state.overview = await api("/api/overview");
  state.partitions = state.overview.partitions || [];
  if (!state.selected && state.partitions.length) {
    state.selected = state.partitions[0];
  }
  if (state.selected) {
    await loadDetail(state.selected);
  }
}

async function loadDetail(name) {
  state.detail = await api(`/api/partition?name=${encodeURIComponent(name)}`);
}

async function loadCatalog() {
  const data = await api("/api/catalog");
  state.catalog = data.assetTypes || [];
}

async function loadStatus() {
  const data = await api("/api/status");
  state.status = data.proposals || [];
}

async function refreshAll() {
  await Promise.all([loadOverview(), loadCatalog(), loadStatus()]);
  render();
}

document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-action]");
  if (button) {
    const action = button.dataset.action;
    try {
      if (action === "select") {
        state.selected = button.dataset.name;
        state.submitResult = null;
        await loadDetail(state.selected);
        render();
      } else if (action === "new-partition") {
        const name = window.prompt("New partition name (lowercase, a-z0-9-)");
        if (!name) return;
        const data = await api("/api/partition", {
          method: "POST",
          body: JSON.stringify({ name }),
        });
        toast(`created ${data.configPath}`, "good");
        await refreshAll();
        state.selected = data.name;
        await loadDetail(state.selected);
        state.tab = "partition";
        render();
      } else if (action === "validate") {
        state.validation = await api("/api/validate", { method: "POST", body: "{}" });
        state.tab = "validate";
        render();
      } else if (action === "refresh") {
        await refreshAll();
      } else if (action === "refresh-status") {
        await loadStatus();
        render();
      } else if (action === "suggest-alerts") {
        const data = await api("/api/alert/suggest", {
          method: "POST",
          body: JSON.stringify({ partition: state.selected }),
        });
        state.alertPreview = data.yaml;
        render();
      } else if (action === "write-alerts") {
        const data = await api("/api/alert/write", {
          method: "POST",
          body: JSON.stringify({ partition: state.selected, overwrite: true }),
        });
        toast(`wrote ${data.path}`, "good");
        await loadDetail(state.selected);
        render();
      }
    } catch (error) {
      toast(error.message, "bad");
    }
    return;
  }

  const nav = event.target.closest(".nav button");
  if (nav) {
    state.tab = nav.dataset.tab;
    render();
  }
});

document.addEventListener("submit", async (event) => {
  const form = event.target.closest("form[data-form]");
  if (!form) return;
  event.preventDefault();
  const data = Object.fromEntries(new FormData(form).entries());
  const kind = form.dataset.form;
  try {
    if (kind === "asset") {
      await api("/api/asset", {
        method: "POST",
        body: JSON.stringify({
          partition: state.selected,
          intent: data.intent,
          type: data.type,
          name: data.name,
          image: data.image,
          port: Number(data.port || 0),
          replicas: Number(data.replicas || 0),
        }),
      });
      toast("asset added", "good");
      await loadDetail(state.selected);
      render();
    } else if (kind === "probe") {
      await api("/api/probe", {
        method: "POST",
        body: JSON.stringify({
          partition: state.selected,
          intent: data.intent,
          asset: data.asset,
          path: data.path,
          port: Number(data.port || 0),
        }),
      });
      toast("probe added", "good");
      await loadDetail(state.selected);
      render();
    } else if (kind === "secret") {
      await api("/api/secret", {
        method: "POST",
        body: JSON.stringify({
          partition: state.selected,
          intent: data.intent,
          asset: data.asset,
          env: data.env,
          ref: data.ref,
        }),
      });
      toast("secret reference attached (no value stored)", "good");
      await loadDetail(state.selected);
      render();
    } else if (kind === "connect") {
      await api("/api/asset/connect", {
        method: "POST",
        body: JSON.stringify({
          partition: state.selected,
          intent: data.intent,
          from: data.from,
          to: data.to,
        }),
      });
      toast("dependency added", "good");
      await loadDetail(state.selected);
      render();
    } else if (kind === "submit") {
      const mode = event.submitter?.dataset.mode || "dry";
      if (mode === "real" && !window.confirm("Open a real PR/MR through MonoFS?")) {
        return;
      }
      const response = await api("/api/submit", {
        method: "POST",
        body: JSON.stringify({
          partition: state.selected,
          message: data.message,
          title: data.title,
          body: data.body,
          all: data.all === "on",
          dryRun: mode === "dry",
          confirm: mode === "real",
        }),
      });
      state.submitResult = response.result;
      if (response.error) toast(response.error, "bad");
      else if (mode === "real") toast("proposal submitted", "good");
      render();
    }
  } catch (error) {
    toast(error.message, "bad");
  }
});

refreshAll().catch((error) => toast(error.message, "bad"));
