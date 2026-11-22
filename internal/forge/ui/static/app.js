const token = new URLSearchParams(location.search).get("token") || "";

const UI_BUILD = "2026-11-22-quota-debug-1";
console.info(`[forge] UI build ${UI_BUILD}`);

const state = {
  tab: "overview",
  overview: null,
  partitions: [],
  selected: "",
  detail: null,
  catalog: [],
  quotaProfiles: [],
  status: [],
  submitResult: null,
  assetType: "",
  busy: false,
};

const $ = (id) => document.getElementById(id);

const THEME_KEY = "forge.theme";

const THEME_ICONS = {
  system: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8M12 17v4"/></svg>',
  light:
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>',
  dark: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>',
};

function storedTheme() {
  try {
    return localStorage.getItem(THEME_KEY) || "system";
  } catch {
    return "system";
  }
}

function systemPrefersLight() {
  return Boolean(window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches);
}

function applyTheme(choice) {
  const selected = ["system", "light", "dark"].includes(choice) ? choice : "system";
  const dark = selected === "dark" || (selected === "system" && !systemPrefersLight());
  document.documentElement.dataset.theme = dark ? "dark" : "light";

  const button = $("themeButton");
  if (button) {
    button.innerHTML = THEME_ICONS[selected];
    button.title = `Theme: ${selected}`;
    button.setAttribute("aria-label", `Theme: ${selected}`);
  }
  document.querySelectorAll("[data-theme-choice]").forEach((item) => {
    const active = item.dataset.themeChoice === selected;
    item.classList.toggle("active", active);
    item.setAttribute("aria-checked", active ? "true" : "false");
  });
}

function initTheme() {
  applyTheme(storedTheme());
  const button = $("themeButton");
  const menu = $("themeMenu");
  const control = $("themeControl");
  const closeMenu = () => {
    menu?.classList.add("hidden");
    button?.setAttribute("aria-expanded", "false");
  };
  const openMenu = () => {
    menu?.classList.remove("hidden");
    button?.setAttribute("aria-expanded", "true");
  };

  button?.addEventListener("click", (event) => {
    event.stopPropagation();
    if (menu?.classList.contains("hidden")) openMenu();
    else closeMenu();
  });
  menu?.querySelectorAll("[data-theme-choice]").forEach((item) => {
    item.addEventListener("click", (event) => {
      event.stopPropagation();
      const choice = item.dataset.themeChoice;
      try {
        localStorage.setItem(THEME_KEY, choice);
      } catch {}
      applyTheme(choice);
      closeMenu();
    });
  });
  document.addEventListener("click", (event) => {
    if (control && !control.contains(event.target)) closeMenu();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") closeMenu();
  });

  if (window.matchMedia) {
    const query = window.matchMedia("(prefers-color-scheme: light)");
    const followSystem = () => {
      if (storedTheme() === "system") applyTheme("system");
    };
    if (query.addEventListener) query.addEventListener("change", followSystem);
    else if (query.addListener) query.addListener(followSystem);
  }
}

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
  const response = await fetch(path, { ...options, headers, cache: "no-store" });
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
    access: ["Access", "partition-scoped access policy"],
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
  else if (state.tab === "access") content.innerHTML = viewAccess();
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

function valuesText(value) {
  if (!value || !Object.keys(value).length) return "";
  return JSON.stringify(value, null, 2);
}

function quotaDebugText(profile, enforcement) {
  return JSON.stringify(
    {
      ui: UI_BUILD,
      profiles: (state.quotaProfiles || []).map((item) => item.name),
      selected: profile,
      enforcement,
    },
    null,
    2,
  );
}

function quotaChoices(selected) {
  return (state.quotaProfiles || [])
    .map(
      (profile) =>
        `<label class="choice"><input type="radio" name="profile" value="${escapeHtml(profile.name)}" data-quota-profile ${profile.name === selected ? "checked" : ""} required onchange="forgeQuotaChanged(this)" /> <span>${escapeHtml(profile.name)}${profile.builtin ? "" : " (custom)"}</span></label>`,
    )
    .join("");
}

function quotaPreviewText(name) {
  const profile = (state.quotaProfiles || []).find((item) => item.name === name);
  if (!profile) return "Select a predefined profile to preview its limits.";
  return JSON.stringify(profile.quota, null, 2);
}

function forgeQuotaChanged(control) {
  const form = control && control.closest ? control.closest("form") : null;
  const checked = form ? form.querySelector("[data-quota-profile]:checked") : null;
  const profile = checked ? checked.value : "";
  const enforcement = form ? form.querySelector("[data-quota-enforcement]")?.value || "" : "";
  const preview = form ? form.querySelector("[data-quota-preview]") : null;
  const status = form ? form.querySelector("[data-quota-status]") : null;
  const debug = form ? form.querySelector("[data-quota-debug]") : null;
  if (preview) preview.textContent = quotaPreviewText(profile);
  if (status) {
    status.textContent = profile ? `${profile} selected — click Save quota to apply.` : "No profile selected.";
  }
  if (debug) {
    debug.textContent = quotaDebugText(profile, enforcement);
  }
  console.info("[forge] quota selection", { profile, enforcement, profiles: (state.quotaProfiles || []).map((item) => item.name) });
}
window.forgeQuotaChanged = forgeQuotaChanged;

function viewPartition() {
  if (!state.detail) return `<p class="muted">Select a partition.</p>`;
  const intents = state.detail.intents || [];
  const alerts = state.detail.alerts || [];
  const currentQuota = state.detail.quota || {};
  const profile = currentQuota.profile || "";
  const enforcement = currentQuota.enforcement || "observe";
  const hasInline = Boolean(currentQuota.ingest || currentQuota.query || currentQuota.rules || currentQuota.storage);
  const profileKnown = !profile || (state.quotaProfiles || []).some((item) => item.name === profile);
  return `<div class="grid two">
      <div class="panel">
        <h2>Partition config</h2>
        <pre class="yaml">${escapeHtml(state.detail.config)}</pre>
      </div>
      <form class="panel form" data-form="quota">
        <h2>Resource quota</h2>
        <p class="muted small">Choose a predefined profile. Built-in size classes come from the
        platform; custom profiles come from <code>quota_profiles</code> in the repository access policy.</p>
        ${state.quotaProfiles.length ? "" : `<p class="bad small">No quota profiles loaded. Restart <code>forge ui</code> with the rebuilt binary, then
        <button type="button" class="ghost" data-action="reload-profiles">reload profiles</button>.</p>`}
        ${hasInline ? `<p class="bad small">This partition currently uses inline quota limits. Saving a profile replaces them.</p>` : ""}
        ${profile && !profileKnown ? `<p class="bad small">Current profile <code>${escapeHtml(profile)}</code> is not available here. Choose a predefined profile to replace it.</p>` : ""}
        <div class="profile-choices">
          <span class="field-label">Profile</span>
          ${quotaChoices(profile)}
        </div>
        <label>Enforcement<select name="enforcement" data-quota-enforcement onchange="forgeQuotaChanged(this)">
          <option value="observe" ${enforcement === "observe" ? "selected" : ""}>observe</option>
          <option value="enforce" ${enforcement === "enforce" ? "selected" : ""}>enforce</option>
        </select></label>
        <pre class="yaml" data-quota-preview>${escapeHtml(quotaPreviewText(profile))}</pre>
        <p class="muted small" data-quota-status>${profile ? `Current profile: ${escapeHtml(profile)}` : "No profile selected."}</p>
        <details class="debug">
          <summary>Debug</summary>
          <pre data-quota-debug>${escapeHtml(quotaDebugText(profile, enforcement))}</pre>
        </details>
        <div class="row">
          <button type="submit">Save quota</button>
          <button type="button" class="ghost" data-action="clear-quota" ${state.detail.quota ? "" : "disabled"}>Clear</button>
        </div>
      </form>
      <form class="panel form" data-form="values">
        <h2>Partition values</h2>
        <p class="muted small">Define <code>${"${value.NAME}"}</code> placeholders for this partition.
        Intent values override these; asset values override both.</p>
        <label>Values JSON<textarea name="values" rows="8" spellcheck="false" placeholder='{"imageTag":"v1"}'>${escapeHtml(valuesText(state.detail.values))}</textarea></label>
        <div class="row">
          <button type="submit">Save values</button>
          <button type="button" class="ghost" data-action="clear-partition-values" ${state.detail.values ? "" : "disabled"}>Clear</button>
        </div>
      </form>
    </div>
    ${intents
      .map(
        (intent) => `<div class="panel">
          <h2>Intent ${escapeHtml(intent.name)}</h2>
          <pre class="yaml">${escapeHtml(intent.yaml)}</pre>
          <form class="form" data-form="values" data-intent="${escapeHtml(intent.name)}">
            <label>Intent values JSON<textarea name="values" rows="5" spellcheck="false" placeholder='{"replicas":3}'>${escapeHtml(valuesText(intent.values))}</textarea></label>
            <div class="row">
              <button type="submit">Save intent values</button>
              <button type="button" class="ghost" data-action="clear-intent-values" data-intent="${escapeHtml(intent.name)}" ${intent.values ? "" : "disabled"}>Clear</button>
            </div>
          </form>
        </div>`,
      )
      .join("")}
    ${alerts.length
      ? `<div class="panel"><h2>Alerts</h2>${alerts
          .map((alert) => `<h3>${escapeHtml(alert.name)}</h3><pre class="yaml">${escapeHtml(alert.yaml)}</pre>`)
          .join("")}</div>`
      : ""}`;
}

function viewAccess() {
  if (!state.detail) return `<p class="muted">Select a partition.</p>`;
  const suffix = state.detail.accessExists
    ? `Loaded from <code>partitions/${escapeHtml(state.selected)}/access.yaml</code>.`
    : `No <code>access.yaml</code> yet — this starter template is shown. Saving creates the file.`;
  return `<form class="panel form" data-form="access">
      <h2>Partition access policy</h2>
      <p class="muted small">${suffix} Grants may only name <strong>${escapeHtml(state.selected)}</strong>.
      Platform-owned sections (defaults, selectors, quotas, clients, deny rules) are rejected on save.</p>
      <label>access.yaml<textarea name="yaml" rows="20" spellcheck="false">${escapeHtml(state.detail.access || "")}</textarea></label>
      <button type="submit">Save access.yaml</button>
    </form>`;
}

function intentOptions() {
  const intents = state.detail?.intents || [];
  if (!intents.length) {
    return `<option value="" disabled selected>No intents yet — create one first</option>`;
  }
  return intents
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

function assetsForIntent(name) {
  return (state.detail?.intents || []).find((intent) => intent.name === name)?.assets || [];
}

function assetOptions(intentName, selected = "") {
  return assetsForIntent(intentName)
    .map(
      (asset) =>
        `<option value="${escapeHtml(asset.name)}" ${asset.name === selected ? "selected" : ""}>${escapeHtml(asset.name)} (${escapeHtml(asset.type)})</option>`,
    )
    .join("");
}

function catalogTemplate(type) {
  return (state.catalog || []).find((item) => item.type === type);
}

function templateValue(template, path) {
  return path.split(".").reduce((value, key) => (value == null ? undefined : value[key]), template);
}

function listText(value) {
  if (!Array.isArray(value)) return value == null ? "" : String(value);
  return value.map((item) => (typeof item === "object" ? JSON.stringify(item) : String(item))).join(", ");
}

function renderAssetFields(type) {
  const template = catalogTemplate(type);
  const fields = template?.fields || [];
  if (!fields.length) {
    return `<p class="muted small">No catalog fields are available for this type.</p>`;
  }
  return `<div class="grid two">${fields
    .map((field) => {
      const value = templateValue(template.template, field.path);
      const control = field.control || "text";
      const path = escapeHtml(field.path);
      const title = escapeHtml(field.title || field.path);
      const placeholder = escapeHtml(field.placeholder || "");
      if (control === "boolean") {
        return `<label class="check"><input type="checkbox" data-prop="${path}" data-control="boolean" ${value ? "checked" : ""} /> ${title}</label>`;
      }
      let input;
      if (control === "select") {
        const options = (field.options || [])
          .map((option) => {
            const opt = String(option);
            return `<option value="${escapeHtml(opt)}" ${value === opt ? "selected" : ""}>${escapeHtml(opt)}</option>`;
          })
          .join("");
        input = `<select data-prop="${path}" data-control="select"><option value="">(default)</option>${options}</select>`;
      } else if (control === "json") {
        const raw = value === undefined ? "" : JSON.stringify(value, null, 2);
        input = `<textarea rows="4" spellcheck="false" data-prop="${path}" data-control="json" placeholder="JSON">${escapeHtml(raw)}</textarea>`;
      } else if (control === "textarea") {
        input = `<textarea rows="4" spellcheck="false" data-prop="${path}" data-control="text" placeholder="${placeholder}">${escapeHtml(value ?? "")}</textarea>`;
      } else if (control === "number") {
        input = `<input type="number" data-prop="${path}" data-control="number" value="${escapeHtml(value ?? "")}" placeholder="${placeholder}" />`;
      } else if (control === "list" || control === "asset-refs") {
        input = `<input data-prop="${path}" data-control="list" value="${escapeHtml(listText(value))}" placeholder="comma-separated or JSON array" />`;
      } else {
        input = `<input data-prop="${path}" data-control="text" value="${escapeHtml(value ?? "")}" placeholder="${placeholder}" />`;
      }
      return `<label>${title}${input}</label>`;
    })
    .join("")}</div>`;
}

function formProperties(form) {
  const properties = {};
  for (const element of form.querySelectorAll("[data-prop]")) {
    const path = element.dataset.prop;
    const control = element.dataset.control;
    if (control === "boolean") {
      if (element.checked) properties[path] = true;
      continue;
    }
    const raw = element.value;
    if (!raw.trim()) continue;
    if (control === "json") {
      try {
        properties[path] = JSON.parse(raw);
      } catch (error) {
        throw new Error(`${path}: ${error.message}`);
      }
    } else if (control === "number") {
      const value = Number(raw);
      if (Number.isNaN(value)) throw new Error(`${path}: not a number`);
      properties[path] = value;
    } else if (control === "list" || control === "asset-refs") {
      const trimmed = raw.trim();
      let items;
      if (trimmed.startsWith("[")) {
        try {
          items = JSON.parse(trimmed);
        } catch (error) {
          throw new Error(`${path}: ${error.message}`);
        }
      } else {
        items = trimmed.split(",").map((item) => item.trim()).filter(Boolean);
        if (items.every((item) => /^-?\d+$/.test(item))) items = items.map(Number);
      }
      if (Array.isArray(items) && items.length) properties[path] = items;
    } else {
      properties[path] = raw;
    }
  }
  return properties;
}

function viewAssets() {
  if (!state.detail) return `<p class="muted">Select a partition.</p>`;
  const rows = assetRows();
  const types = (state.catalog || []).map((template) => template.type);
  const hasIntents = (state.detail.intents || []).length > 0;
  const selectedType = types.includes(state.assetType) ? state.assetType : types[0] || "Compute";
  const firstIntent = (state.detail.intents || [])[0]?.name || "";
  const firstAssets = assetsForIntent(firstIntent);
  return `${hasIntents
      ? ""
      : `<div class="panel">
      <h2>Create an intent first</h2>
      <p class="muted">Assets attach to an intent. This partition has none yet, so create one below — it writes
      <code>partitions/${escapeHtml(state.selected)}/intents/&lt;name&gt;.yaml</code>.</p>
    </div>`}
    <div class="panel">
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
      <form class="panel form" data-form="intent">
        <h2>Create intent</h2>
        <label>Name<input name="name" required pattern="[a-z][a-z0-9-]*" placeholder="api" /></label>
        <label>Target pusher<input name="pusher" required placeholder="k8s-main" /></label>
        <div class="row">
          <label>Cluster<input name="cluster" placeholder="k8s-main" /></label>
          <label>Namespace<input name="namespace" placeholder="${escapeHtml(state.selected)}" /></label>
        </div>
        <button type="submit">Create intent</button>
      </form>
      <form class="panel form" data-form="asset">
        <h2 data-asset-title>Add ${escapeHtml(selectedType)}</h2>
        <label>Intent<select name="intent">${intentOptions()}</select></label>
        <label>Type<select name="type" data-asset-type>${types
          .map((t) => `<option value="${escapeHtml(t)}" ${t === selectedType ? "selected" : ""}>${escapeHtml(t)}</option>`)
          .join("")}</select></label>
        <label>Name<input name="name" required pattern="[a-z][a-z0-9-]*" placeholder="api" /></label>
        <div data-asset-fields>${renderAssetFields(selectedType)}</div>
        <button type="submit" ${hasIntents ? "" : "disabled"}>Add asset</button>
      </form>
      <form class="panel form" data-form="secret">
        <h2>Secret reference</h2>
        <label>Intent<select name="intent">${intentOptions()}</select></label>
        <label>Compute asset<input name="asset" required placeholder="api" /></label>
        <label>Environment variable<input name="env" required placeholder="DATABASE_URL" /></label>
        <label>Secret ref<input name="ref" required placeholder="monofs-secret://checkout/db" /></label>
        <button type="submit" ${hasIntents ? "" : "disabled"}>Attach reference</button>
      </form>
      <form class="panel form" data-form="values" data-level="asset">
        <h2>Asset values</h2>
        <label>Intent<select name="intent">${intentOptions()}</select></label>
        <label>Asset name<input name="asset" required placeholder="api" /></label>
        <label>Values JSON<textarea name="values" rows="5" spellcheck="false" placeholder='{"replicas":5}'></textarea></label>
        <button type="submit" ${hasIntents ? "" : "disabled"}>Save asset values</button>
      </form>
      <form class="panel form" data-form="connect">
        <h2>Connect assets</h2>
        <p class="muted small">Assets can only depend on assets in the <strong>same intent</strong>.
        Use intent <code>joins</code> for cross-intent ordering; cross-partition dependencies are not supported.</p>
        <label>Intent<select name="intent" data-connect-intent>${intentOptions()}</select></label>
        <label>From<select name="from" data-connect-from>${assetOptions(firstIntent)}</select></label>
        <label>To<select name="to" data-connect-to>${assetOptions(firstIntent, firstAssets[1]?.name)}</select></label>
        <button type="submit" ${hasIntents && firstAssets.length >= 2 ? "" : "disabled"}>Add dependsOn</button>
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
  if (Array.isArray(state.detail.quotaProfiles) && state.detail.quotaProfiles.length) {
    state.quotaProfiles = state.detail.quotaProfiles;
  }
  console.info("[forge] partition detail", {
    partition: name,
    quota: state.detail.quota,
    quotaProfiles: (state.quotaProfiles || []).map((item) => item.name),
    values: state.detail.values,
  });
}

async function loadCatalog() {
  const data = await api("/api/catalog");
  state.catalog = data.assetTypes || [];
  state.quotaProfiles = data.quotaProfiles || [];
  console.info("[forge] catalog loaded", {
    assetTypes: state.catalog.length,
    quotaProfiles: state.quotaProfiles.map((item) => item.name),
  });
}

async function loadStatus() {
  const data = await api("/api/status");
  state.status = data.proposals || [];
}

async function refreshAll() {
  await Promise.all([loadOverview(), loadCatalog(), loadStatus()]);
  render();
}

document.addEventListener("change", (event) => {
  const select = event.target.closest("[data-asset-type]");
  if (select) {
    state.assetType = select.value;
    const form = select.closest("form");
    const fields = form?.querySelector("[data-asset-fields]");
    if (fields) fields.innerHTML = renderAssetFields(select.value);
    const title = form?.querySelector("[data-asset-title]");
    if (title) title.textContent = `Add ${select.value}`;
    return;
  }
  const connectSelect = event.target.closest("[data-connect-intent]");
  if (connectSelect) {
    const form = connectSelect.closest("form");
    const intent = connectSelect.value;
    const assets = assetsForIntent(intent);
    const from = form?.querySelector("[data-connect-from]");
    const to = form?.querySelector("[data-connect-to]");
    if (from) from.innerHTML = assetOptions(intent);
    if (to) to.innerHTML = assetOptions(intent, assets[1]?.name);
    const button = form?.querySelector('button[type="submit"]');
    if (button) button.disabled = assets.length < 2;
    return;
  }
  const quotaInput = event.target.closest("[data-quota-profile], [data-quota-enforcement]");
  if (quotaInput) {
    forgeQuotaChanged(quotaInput);
  }
});

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
      } else if (action === "reload-profiles") {
        await loadCatalog();
        render();
      } else if (action === "clear-partition-values") {
        await api("/api/values", {
          method: "POST",
          body: JSON.stringify({ partition: state.selected, clear: true }),
        });
        toast("partition values cleared", "good");
        await loadDetail(state.selected);
        render();
      } else if (action === "clear-intent-values") {
        await api("/api/values", {
          method: "POST",
          body: JSON.stringify({ partition: state.selected, intent: button.dataset.intent, clear: true }),
        });
        toast("intent values cleared", "good");
        await loadDetail(state.selected);
        render();
      } else if (action === "clear-quota") {
        if (!window.confirm("Remove spec.quota from this partition?")) return;
        await api("/api/quota", {
          method: "POST",
          body: JSON.stringify({ partition: state.selected, clear: true }),
        });
        toast("quota cleared", "good");
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
      console.error("[forge] action failed", error);
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
    if (kind === "intent") {
      const created = await api("/api/intent", {
        method: "POST",
        body: JSON.stringify({
          partition: state.selected,
          name: data.name,
          pusher: data.pusher,
          cluster: data.cluster,
          namespace: data.namespace,
        }),
      });
      toast(`created ${created.path}`, "good");
      await loadDetail(state.selected);
      render();
    } else if (kind === "asset") {
      const properties = formProperties(form);
      await api("/api/asset", {
        method: "POST",
        body: JSON.stringify({
          partition: state.selected,
          intent: data.intent,
          type: data.type,
          name: data.name,
          properties,
        }),
      });
      toast("asset added", "good");
      await loadDetail(state.selected);
      render();
    } else if (kind === "values") {
      const raw = String(data.values || "").trim();
      let vals = {};
      if (raw) {
        try {
          vals = JSON.parse(raw);
        } catch (error) {
          throw new Error(`values: ${error.message}`);
        }
      }
      if (vals === null || typeof vals !== "object" || Array.isArray(vals)) {
        throw new Error("values must be a JSON object");
      }
      const intent = form.dataset.intent || String(data.intent || "");
      const asset = String(data.asset || "");
      await api("/api/values", {
        method: "POST",
        body: JSON.stringify({
          partition: state.selected,
          intent,
          asset,
          values: vals,
          clear: raw === "",
        }),
      });
      toast(raw === "" ? "values cleared" : "values saved", "good");
      await loadDetail(state.selected);
      render();
    } else if (kind === "quota") {
      const profile = String(data.profile || "").trim();
      if (!profile) throw new Error("choose a predefined quota profile");
      const quota = { profile };
      if (data.enforcement) quota.enforcement = data.enforcement;
      console.info("[forge] quota save", quota);
      const saved = await api("/api/quota", {
        method: "POST",
        body: JSON.stringify({ partition: state.selected, quota }),
      });
      console.info("[forge] quota saved", saved);
      toast(`quota saved: ${profile}`, "good");
      await loadDetail(state.selected);
      render();
    } else if (kind === "access") {
      await api("/api/access", {
        method: "POST",
        body: JSON.stringify({ partition: state.selected, yaml: data.yaml }),
      });
      toast("access.yaml saved", "good");
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
    console.error("[forge] request failed", error);
    toast(error.message, "bad");
  }
});

initTheme();
refreshAll().catch((error) => {
  console.error("[forge] initial load failed", error);
  toast(error.message, "bad");
});
window.__forge = { version: UI_BUILD, state };
