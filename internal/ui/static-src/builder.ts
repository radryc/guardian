import { fetchJSON, escapeHtml, escapeAttr, toast } from "./utils";

// ── Types ────────────────────────────────────────────
interface CatalogField {
  path: string;
  title: string;
  control: string;
  placeholder?: string;
  description?: string;
  options?: string[];
  refTypes?: string[];
}

interface CatalogTemplate {
  type: string;
  title: string;
  description: string;
  icon: string;
  category: string;
  template: Record<string, any>;
  fields: CatalogField[];
}

interface AssetSpec {
  type: string;
  name: string;
  version?: string;
  dependsOn?: string[];
  hints?: any[];
  payload?: Record<string, string>;
  properties: Record<string, any>;
  skip?: boolean;
}

interface Placement {
  cluster?: string;
  namespace?: string;
  region?: string;
  account?: string;
}

interface IntentManifest {
  apiVersion?: string;
  kind?: string;
  metadata: { name: string };
  spec: {
    intentType?: string;
    joins?: string[];
    targetPusher?: string;
    target?: Placement;
    locked?: boolean;
    hints?: any[];
    assets: AssetSpec[];
  };
}

interface PartitionManifest {
  apiVersion?: string;
  kind?: string;
  metadata: { name: string };
  spec: {
    deletionPolicy?: string;
    reconciliation?: { mode?: string; interval?: string; jitterPercent?: number };
    defaults?: { targetPusher?: string; target?: Placement };
  };
}

interface IntentDraft {
  uid: string;
  name: string;
  isNew: boolean;
  dirty: boolean;
  expectedVersionID: string;
  intentType: string;
  targetPusher: string;
  target: Placement;
  joins: string[];
  locked: boolean;
  hints: any[];
  assets: AssetSpec[];
  positions: Record<string, { x: number; y: number }>;
}

interface Draft {
  editingPartition: string;
  isNew: boolean;
  partitionVersionID: string;
  partition: PartitionManifest;
  intents: IntentDraft[];
  selection: { kind: "partition" | "intent" | "asset"; intentUid?: string; assetName?: string };
  dirty: boolean;
}

interface ConnectorDef {
  key: string;
  path: string;
  label: string;
  refTypes: string[];
  many: boolean;
  special?: "volumeMounts" | "configMounts" | "targets" | "traefikTarget" | "envRef";
  envKey?: string;
  envOutput?: string;
}

interface PusherInfo {
  name: string;
  kind: string;
  account?: string;
  region?: string;
  description?: string;
}

interface IntentTemplate {
  id: string;
  name: string;
  description: string;
  savedAt: number;
  intentType: string;
  targetPusher: string;
  target: Placement;
  locked: boolean;
  hints: any[];
  assets: AssetSpec[];
  sourceIntent?: string;
}

interface Issue {
  severity: "error" | "warning";
  message: string;
  intentUid?: string;
  intent?: string;
  asset?: string;
  source: "client" | "server";
}

interface BuilderOptions {
  getSelectedPartition: () => string;
  onOpenPartition: (name: string) => void;
  onToast: (message: string, type?: "success" | "error" | "info") => void;
  onError: (error: any) => void;
}

export interface BuilderHandle {
  render: () => void;
  open: (partitionName?: string) => void;
}

interface ModalField {
  name: string;
  label: string;
  type: "select" | "text" | "checkbox";
  options?: { value: string; label: string }[];
  value?: string | boolean;
  description?: string;
}

interface ModalOptions {
  title: string;
  description?: string;
  bodyHtml?: string;
  fields?: ModalField[];
  actions: { key: string; label: string; primary?: boolean; danger?: boolean }[];
}

// ── Constants ────────────────────────────────────────
const NODE_W = 262;
const LANE_W = 320;
const LANE_GAP = 22;
const COL_GAP = 64;
const MAX_COLUMN_ROWS = 3;
const NODE_GAP = 28;
const HEADER_H = 46;
const PORT_ROW_H = 18;
const SOCKET_ROW_H = 26;
const NODE_BOTTOM = 12;
const NAME_RE = /^[a-z][a-z0-9-]*$/;
const STORAGE_PREFIX = "guardian.builder.v1.";
const TEMPLATE_KEY = "guardian.builder.templates.v1";

const PUSHER_SUPPORT: Record<string, string[]> = {
  aws: ["CDKStack", "Secret", "Volume", "Config", "Compute", "LoadBalancer", "Database", "SQLDatabase", "ObjectStore", "Network", "Observability"],
  kubernetes: ["Volume", "Config", "Secret", "Compute", "ImageBuild", "TraefikRoute", "LoadBalancer", "ObjectStore", "Database", "SQLDatabase", "Observability", "K8sResource", "Network"],
  docker: ["Volume", "Config", "Network", "Secret", "Compute", "ImageBuild", "TraefikRoute", "LoadBalancer", "ObjectStore", "Database", "SQLDatabase", "Observability"],
  local: ["Compute", "Database", "SQLDatabase"],
  other: ["Compute", "Database", "SQLDatabase", "Volume", "Config", "Network", "Secret", "LoadBalancer", "TraefikRoute", "ObjectStore", "Observability", "ImageBuild", "CDKStack", "K8sResource"],
};

const KIND_LABELS: Record<string, string> = {
  aws: "AWS",
  kubernetes: "Kubernetes",
  docker: "Docker",
  local: "Local",
  other: "Pusher",
};

const ASSET_HINTS: Record<string, string> = {
  Compute: "Set an image, then connect a Network, Volume, or Config.",
  LoadBalancer: "Connect the services it should route to.",
  TraefikRoute: "Connect the compute service to expose.",
  Database: "Connect a volume and a network.",
  SQLDatabase: "Connect a volume and a network.",
  ObjectStore: "Connect a volume or set an endpoint.",
  Observability: "Connect target compute assets; the AWS pusher injects the OTEL sidecar.",
  Volume: "Set a size, then connect a service.",
  Config: "Add inline content or data files.",
  Network: "Connect services that share this network.",
  Secret: "Set a secret reference or inline value.",
  ImageBuild: "Set the repository and source image.",
  CDKStack: "Set the AWS payload path.",
  K8sResource: "Set the manifest payload path.",
};

const ESSENTIAL_FIELDS: Record<string, string[]> = {
  Compute: ["image", "replicas", "ports", "env", "networks"],
  ImageBuild: ["repository", "sourceImage", "buildContext", "dockerfile"],
  CDKStack: ["context", "env"],
  Database: ["engine", "version", "database", "user", "port", "volume", "networks"],
  SQLDatabase: ["engine", "version", "database", "user", "port", "volume", "networks"],
  Volume: ["size", "accessMode", "class"],
  Config: ["format", "content", "data"],
  Network: ["driver", "scope", "internal"],
  Secret: ["secretRef", "value"],
  LoadBalancer: ["targets", "listeners", "serviceType", "config", "networks"],
  TraefikRoute: ["hostname", "target", "portName"],
  ObjectStore: ["engine", "endpoint", "buckets", "versioning", "volume", "config", "networks"],
  Observability: ["provider", "targets", "endpoint", "receivers", "exporters", "config", "volume", "networks"],
  K8sResource: ["namespace"],
};

// ── Module state ─────────────────────────────────────
let opts: BuilderOptions;
let draft: Draft | null = null;
let catalog: CatalogTemplate[] = [];
let catalogByName = new Map<string, CatalogTemplate>();
let registryPushers: PusherInfo[] = [];
let catalogLoaded = false;
let catalogLoading = false;
let connectorCache = new Map<string, ConnectorDef[]>();
let serverIssues: Issue[] = [];
let validationTimer: number | undefined;
let persistTimer: number | undefined;
let canvasRefreshTimer: number | undefined;
let undoStack: string[] = [];
let redoStack: string[] = [];
let storedDraftAvailable = false;
let connectState: { intentUid: string; assetName: string; connector: ConnectorDef } | null = null;
let armedConnect: { intentUid: string; assetName: string; connector: ConnectorDef } | null = null;
let rawJsonEdits = new Map<string, string>();
let switchPromptedFor = "";
let intentTemplates: IntentTemplate[] = [];
let initialized = false;

// ── Public API ───────────────────────────────────────
export function initBuilderPanel(options: BuilderOptions): BuilderHandle {
  opts = options;
  if (!initialized) {
    initialized = true;
    window.addEventListener("beforeunload", (event) => {
      if (draft?.dirty) {
        event.preventDefault();
        event.returnValue = "";
      }
    });
    wireBuilderPanel();
    loadTemplates();
    ensureCatalog().catch(options.onError);
  }
  return {
    render: () => renderBuilder(),
    open: (partitionName?: string) => openBuilder(partitionName),
  };
}

async function ensureCatalog(): Promise<void> {
  if (catalogLoaded || catalogLoading) return;
  catalogLoading = true;
  try {
    const [cat, pushers] = await Promise.all([
      fetchJSON("/api/catalog"),
      fetchJSON("/api/pushers").catch(() => ({ pushers: [] })),
    ]);
    catalog = cat?.assetTypes ?? [];
    catalogByName = new Map(catalog.map((t) => [t.type, t]));
    const fromCatalog: PusherInfo[] = (cat?.pushers ?? []).map((name: string) => ({ name, kind: pusherKind(name) }));
    const fromRegistry: PusherInfo[] = (pushers?.pushers ?? []).map((p: any) => ({
      name: p.name,
      kind: pusherKind(p.name),
      account: p.account,
      region: p.region,
      description: p.description,
    }));
    const merged = new Map<string, PusherInfo>();
    for (const p of [...fromCatalog, ...fromRegistry]) merged.set(p.name, { ...merged.get(p.name), ...p });
    registryPushers = [...merged.values()].sort((a, b) => a.name.localeCompare(b.name));
  } finally {
    catalogLoaded = true;
    catalogLoading = false;
  }
}

function openBuilder(partitionName?: string): void {
  if (!partitionName) {
    if (draft && draft.isNew) {
      renderBuilder();
      return;
    }
    if (draft?.dirty) {
      confirmAction(
        "Discard unsaved changes?",
        `You have unsaved changes for "${draft.editingPartition}". Starting a new partition will discard them.`,
        "Discard and start new",
        true,
      ).then((ok) => {
        if (ok) startNewDraft();
      }).catch(opts.onError);
      return;
    }
    startNewDraft();
    return;
  }
  if (draft && draft.editingPartition === partitionName) {
    renderBuilder();
    return;
  }
  if (draft?.dirty) {
    confirmAction("Discard unsaved changes?", `You have unsaved changes for "${draft.editingPartition || "new partition"}". Loading "${partitionName}" will discard them.`, "Discard and load", true)
      .then((ok) => {
        if (ok) loadPartition(partitionName).catch(opts.onError);
      })
      .catch(opts.onError);
    return;
  }
  loadPartition(partitionName).catch(opts.onError);
}

function startNewDraft(): void {
  draft = newDraft();
  undoStack = [];
  redoStack = [];
  checkStoredDraft("");
  renderBuilder();
}

function renderBuilder(): void {
  const panel = document.getElementById("builderPanel");
  if (!panel) return;
  const selected = opts.getSelectedPartition();
  if (!draft) {
    if (selected) {
      loadPartition(selected).catch(opts.onError);
      return;
    }
    renderEmptyState();
    return;
  }
  if (selected && draft.editingPartition && selected !== draft.editingPartition && !draft.isNew) {
    if (!draft.dirty) {
      loadPartition(selected).catch(opts.onError);
      return;
    }
    if (switchPromptedFor !== selected) {
      switchPromptedFor = selected;
      confirmAction(
        "Unsaved changes",
        `You are editing "${draft.editingPartition}" with unsaved changes. Switch to "${selected}"?`,
        "Switch",
        true,
      ).then((ok) => {
        if (ok) {
          draft = null;
          loadPartition(selected).catch(opts.onError);
        }
      }).catch(opts.onError);
    }
  }
  if (!catalogLoaded) {
    renderLoadingState();
    ensureCatalog().then(() => renderBuilder()).catch(opts.onError);
    return;
  }
  if (canvasRefreshTimer !== undefined) {
    window.clearTimeout(canvasRefreshTimer);
    canvasRefreshTimer = undefined;
  }
  renderTopbar();
  renderPalette();
  renderCanvas();
  renderInspector();
  renderProblems();
}

async function loadPartition(name: string): Promise<void> {
  await ensureCatalog();
  const detail = await fetchJSON(`/api/partitions/${encodeURIComponent(name)}`);
  draft = draftFromDetail(name, detail);
  undoStack = [];
  redoStack = [];
  serverIssues = [];
  rawJsonEdits.clear();
  checkStoredDraft(name);
  renderBuilder();
}

// ── Draft lifecycle ──────────────────────────────────
function newDraft(): Draft {
  return {
    editingPartition: "",
    isNew: true,
    partitionVersionID: "",
    partition: {
      apiVersion: "guardian/v1alpha1",
      kind: "Partition",
      metadata: { name: "" },
      spec: {
        deletionPolicy: "orphan",
        reconciliation: { mode: "auto", interval: "15m" },
      },
    },
    intents: [],
    selection: { kind: "partition" },
    dirty: false,
  };
}

function draftFromDetail(name: string, detail: any): Draft {
  const partition = JSON.parse(JSON.stringify(detail?.partition?.manifest ?? {}));
  partition.apiVersion = partition.apiVersion || "guardian/v1alpha1";
  partition.kind = partition.kind || "Partition";
  partition.metadata = partition.metadata ?? { name };
  partition.metadata.name = partition.metadata.name || name;
  partition.spec = partition.spec ?? {};
  partition.spec.reconciliation = partition.spec.reconciliation ?? { mode: "auto", interval: "15m" };
  const intents: IntentDraft[] = (detail?.intents ?? []).map((doc: any) => {
    const manifest = JSON.parse(JSON.stringify(doc?.manifest ?? {}));
    const spec = manifest.spec ?? {};
    const assets: AssetSpec[] = spec.assets ?? [];
    const d: IntentDraft = {
      uid: manifest.metadata?.name ?? doc?.name ?? "intent",
      name: manifest.metadata?.name ?? doc?.name ?? "intent",
      isNew: false,
      dirty: false,
      expectedVersionID: doc?.manifestVersionID ?? "",
      intentType: spec.intentType || "standard",
      targetPusher: spec.targetPusher || "",
      target: spec.target ?? {},
      joins: [...(spec.joins ?? [])],
      locked: !!spec.locked,
      hints: JSON.parse(JSON.stringify(spec.hints ?? [])),
      assets,
      positions: {},
    };
    autoLayoutIntent(d);
    return d;
  });
  return {
    editingPartition: name,
    isNew: false,
    partitionVersionID: detail?.partition?.manifestVersionID ?? "",
    partition,
    intents,
    selection: intents.length ? { kind: "intent", intentUid: intents[0].uid } : { kind: "partition" },
    dirty: false,
  };
}

function intentFromDraft(intent: IntentDraft): IntentManifest {
  return {
    apiVersion: "guardian/v1alpha1",
    kind: "Intent",
    metadata: { name: intent.name },
    spec: {
      intentType: intent.intentType || "standard",
      joins: intent.joins.length ? [...intent.joins] : undefined,
      targetPusher: intent.targetPusher,
      target: compactPlacement(intent.target),
      locked: intent.locked,
      hints: intent.hints?.length ? intent.hints : undefined,
      assets: intent.assets.map((asset) => compactAsset(asset)),
    },
  };
}

function compactAsset(asset: AssetSpec): AssetSpec {
  const out: AssetSpec = {
    type: asset.type,
    name: asset.name,
    properties: asset.properties ?? {},
  };
  if (asset.version) out.version = asset.version;
  if (asset.dependsOn?.length) out.dependsOn = [...asset.dependsOn];
  if (asset.hints?.length) out.hints = asset.hints;
  if (asset.payload && Object.keys(asset.payload).length) out.payload = asset.payload;
  if (asset.skip) out.skip = true;
  return out;
}

function compactPlacement(placement: Placement): Placement | undefined {
  const out: Placement = {};
  for (const key of ["cluster", "namespace", "region", "account"] as const) {
    const value = placement?.[key];
    if (value && String(value).trim()) out[key] = String(value).trim();
  }
  return Object.keys(out).length ? out : undefined;
}

function buildSaveRequest(): any {
  const loadedNames = draft!.intents.filter((i) => !i.isNew).map((i) => i.name);
  const currentNames = new Set(draft!.intents.map((i) => i.name));
  const removed = loadedNames.filter((n) => !currentNames.has(n));
  const sendAll = removed.length > 0;
  const intents = draft!.intents
    .filter((i) => sendAll || i.isNew || i.dirty)
    .map((i) => ({ manifest: intentFromDraft(i), expectedVersionID: i.expectedVersionID || undefined }));
  return {
    partition: draft!.partition,
    partitionExpectedVersionID: draft!.partitionVersionID || undefined,
    intents,
    removeMissingIntents: sendAll,
  };
}

function markDirty(intent?: IntentDraft): void {
  if (!draft) return;
  draft.dirty = true;
  if (intent) intent.dirty = true;
  updateTopbarButtons();
  persistDraft();
  scheduleServerValidation();
  if (canvasRefreshTimer !== undefined) window.clearTimeout(canvasRefreshTimer);
  canvasRefreshTimer = window.setTimeout(() => {
    canvasRefreshTimer = undefined;
    renderCanvas();
    renderProblems();
  }, 500);
}

function persistDraft(): void {
  if (!draft) return;
  if (persistTimer !== undefined) window.clearTimeout(persistTimer);
  persistTimer = window.setTimeout(() => {
    try {
      const key = STORAGE_PREFIX + (draft!.editingPartition || "__new__");
      localStorage.setItem(key, JSON.stringify({ draft, savedAt: Date.now() }));
    } catch { /* storage unavailable */ }
  }, 400);
}

function checkStoredDraft(name: string): void {
  try {
    const raw = localStorage.getItem(STORAGE_PREFIX + (name || "__new__"));
    storedDraftAvailable = !!raw;
  } catch {
    storedDraftAvailable = false;
  }
}

function restoreStoredDraft(): void {
  if (!draft) return;
  try {
    const raw = localStorage.getItem(STORAGE_PREFIX + (draft.editingPartition || "__new__"));
    if (!raw) return;
    const parsed = JSON.parse(raw);
    if (!parsed?.draft) return;
    draft = parsed.draft;
    storedDraftAvailable = false;
    serverIssues = [];
    renderBuilder();
    opts.onToast("Draft restored", "success");
  } catch (error) {
    opts.onError(error);
  }
}

function discardDraft(): void {
  if (!draft) return;
  const name = draft.editingPartition;
  confirmAction("Discard changes?", "This removes your unsaved edits and reloads the saved manifests.", "Discard", true)
    .then((ok) => {
      if (!ok) return;
      try { localStorage.removeItem(STORAGE_PREFIX + (name || "__new__")); } catch { /* ignore */ }
      storedDraftAvailable = false;
      undoStack = [];
      redoStack = [];
      serverIssues = [];
      if (name) {
        loadPartition(name).catch(opts.onError);
      } else {
        draft = newDraft();
        renderBuilder();
      }
    })
    .catch(opts.onError);
}

// ── Undo / redo ──────────────────────────────────────
function pushUndo(): void {
  if (!draft) return;
  undoStack.push(JSON.stringify(draft));
  if (undoStack.length > 60) undoStack.shift();
  redoStack = [];
}

function undo(): void {
  if (!draft || !undoStack.length) return;
  redoStack.push(JSON.stringify(draft));
  draft = JSON.parse(undoStack.pop()!);
  renderBuilder();
}

function redo(): void {
  if (!draft || !redoStack.length) return;
  undoStack.push(JSON.stringify(draft));
  draft = JSON.parse(redoStack.pop()!);
  renderBuilder();
}

// ── Layout ───────────────────────────────────────────
// Assets flow left-to-right: dependency depth picks the column (references
// count too, not just dependsOn), and any column with more than MAX_COLUMN_ROWS
// nodes wraps into a new column to the right. Intents themselves are stacked
// vertically (one row each).
function autoLayoutIntent(intent: IntentDraft): void {
  const ordered = topoOrder(intent.assets);
  const byName = new Map(intent.assets.map((asset) => [asset.name, asset]));
  const names = new Set(intent.assets.map((asset) => asset.name));
  const depsOf = (name: string): string[] => {
    const asset = byName.get(name);
    if (!asset) return [];
    const deps = new Set<string>();
    for (const dep of asset.dependsOn ?? []) {
      if (names.has(dep)) deps.add(dep);
    }
    for (const connector of connectorsFor(asset.type)) {
      for (const ref of connectorRefs(asset, connector)) {
        if (names.has(ref) && ref !== name) deps.add(ref);
      }
    }
    return [...deps];
  };
  const depths = new Map<string, number>();
  const visiting = new Set<string>();
  const depthOf = (name: string): number => {
    const cached = depths.get(name);
    if (cached !== undefined) return cached;
    if (visiting.has(name)) return 0;
    visiting.add(name);
    let value = 0;
    for (const dep of depsOf(name)) {
      value = Math.max(value, depthOf(dep) + 1);
    }
    visiting.delete(name);
    depths.set(name, value);
    return value;
  };
  const grouped = new Map<number, string[]>();
  for (const name of ordered) {
    const depth = depthOf(name);
    if (!grouped.has(depth)) grouped.set(depth, []);
    grouped.get(depth)!.push(name);
  }
  let column = 0;
  for (const depth of [...grouped.keys()].sort((a, b) => a - b)) {
    const group = grouped.get(depth)!;
    for (let i = 0; i < group.length; i += MAX_COLUMN_ROWS) {
      let y = 70;
      for (const name of group.slice(i, i + MAX_COLUMN_ROWS)) {
        intent.positions[name] = { x: 20 + column * (NODE_W + COL_GAP), y };
        y += nodeHeight(byName.get(name)!) + NODE_GAP;
      }
      column++;
    }
  }
}

function topoOrder(assets: AssetSpec[]): string[] {
  const names = new Set(assets.map((a) => a.name));
  const deps = new Map(assets.map((a) => [a.name, (a.dependsOn ?? []).filter((d) => names.has(d))]));
  const result: string[] = [];
  const visiting = new Set<string>();
  const visited = new Set<string>();
  const visit = (name: string) => {
    if (visited.has(name) || visiting.has(name)) return;
    visiting.add(name);
    for (const dep of deps.get(name) ?? []) visit(dep);
    visiting.delete(name);
    visited.add(name);
    result.push(name);
  };
  assets.forEach((a) => visit(a.name));
  return result;
}

function nextFreePosition(intent: IntentDraft): { x: number; y: number } {
  const placed = intent.assets
    .map((asset) => ({ asset, position: intent.positions[asset.name] }))
    .filter((entry) => !!entry.position);
  if (!placed.length) return { x: 20, y: 70 };
  const countByColumn = new Map<number, number>();
  for (const entry of placed) {
    countByColumn.set(entry.position!.x, (countByColumn.get(entry.position!.x) ?? 0) + 1);
  }
  for (const x of [...countByColumn.keys()].sort((a, b) => a - b)) {
    if ((countByColumn.get(x) ?? 0) >= MAX_COLUMN_ROWS) continue;
    const bottom = Math.max(...placed.filter((entry) => entry.position!.x === x).map((entry) => entry.position!.y + nodeHeight(entry.asset)));
    return { x, y: bottom + NODE_GAP };
  }
  const right = Math.max(...placed.map((entry) => entry.position!.x + NODE_W));
  return { x: right + COL_GAP, y: 70 };
}

function laneWidth(intent: IntentDraft): number {
  let width = LANE_W;
  for (const asset of intent.assets) {
    const position = intent.positions[asset.name];
    if (!position) continue;
    width = Math.max(width, position.x + NODE_W + 24);
  }
  return Math.round(width);
}

function autoLayoutAll(): void {
  pushUndo();
  draft?.intents.forEach((intent) => autoLayoutIntent(intent));
  persistDraft();
  renderBuilder();
}

// ── Connectors ───────────────────────────────────────
function connectorsFor(type: string): ConnectorDef[] {
  const cached = connectorCache.get(type);
  if (cached) return cached;
  const template = catalogByName.get(type);
  const connectors: ConnectorDef[] = [];
  for (const field of template?.fields ?? []) {
    if (field.control === "asset-ref" || field.control === "asset-refs") {
      const connector: ConnectorDef = {
        key: field.path,
        path: field.path,
        label: field.title,
        refTypes: field.refTypes ?? [],
        many: field.control === "asset-refs",
      };
      if (type === "LoadBalancer" && field.path === "targets") connector.special = "targets";
      if (type === "TraefikRoute" && field.path === "target") connector.special = "traefikTarget";
      connectors.push(connector);
    }
  }
  if (type === "Compute") {
    connectors.push({ key: "volumeMounts", path: "volumeMounts", label: "Volume", refTypes: ["Volume"], many: true, special: "volumeMounts" });
    connectors.push({ key: "configMounts", path: "configMounts", label: "Config", refTypes: ["Config"], many: true, special: "configMounts" });
    connectors.push({ key: "databaseRefs", path: "env", label: "Database", refTypes: ["Database", "SQLDatabase"], many: true, special: "envRef", envKey: "DATABASE_URL", envOutput: "url" });
    connectors.push({ key: "objectStoreRefs", path: "env", label: "Object storage", refTypes: ["ObjectStore"], many: true, special: "envRef", envKey: "OBJECT_STORE_ENDPOINT", envOutput: "endpoint" });
  }
  connectorCache.set(type, connectors);
  return connectors;
}

function connectorRefs(asset: AssetSpec, connector: ConnectorDef): string[] {
  const properties = asset.properties ?? {};
  if (connector.special === "volumeMounts" || connector.special === "configMounts") {
    const field = connector.special;
    return ensureArray(properties[field]).map((m: any) => String(m?.[field === "volumeMounts" ? "volume" : "config"] ?? "")).filter(Boolean);
  }
  if (connector.special === "envRef") {
    const allowedFields = connector.refTypes.includes("ObjectStore") ? ["endpoint", "bucket"] : [connector.envOutput || "url"];
    const refs = new Set<string>();
    for (const value of Object.values(properties.env ?? {})) {
      const match = OUTPUT_REF_RE.exec(String(value ?? ""));
      if (!match) continue;
      const [assetName, field] = match[2].split(".");
      if (assetName && allowedFields.includes(field)) refs.add(assetName);
    }
    return [...refs];
  }
  const value = properties[connector.path];
  if (connector.many) return ensureArray(value).map((v) => String(v)).filter(Boolean);
  return value ? [String(value)] : [];
}

const OUTPUT_REF_RE = /\$\{intent\.([a-zA-Z0-9-]+)\.outputs\.([a-zA-Z0-9_.-]+)\}/;

function outputRefAssetName(value: string): string {
  const match = OUTPUT_REF_RE.exec(value);
  if (!match) return "";
  return match[2].split(".")[0] ?? "";
}

function rewriteOutputIntentRefs(value: any, fromIntent: string, toIntent: string): any {
  if (typeof value === "string") {
    const needle = `\${intent.${fromIntent}.outputs.`;
    return value.split(needle).join(`\${intent.${toIntent}.outputs.`);
  }
  if (Array.isArray(value)) return value.map((item) => rewriteOutputIntentRefs(item, fromIntent, toIntent));
  if (value && typeof value === "object") {
    const out: Record<string, any> = {};
    for (const [key, item] of Object.entries(value)) out[key] = rewriteOutputIntentRefs(item, fromIntent, toIntent);
    return out;
  }
  return value;
}

function refTypesLabel(connector: ConnectorDef): string {
  return connector.refTypes.length ? connector.refTypes.join(" or ") : "asset";
}

function connectorAccepts(connector: ConnectorDef, type: string): boolean {
  return !connector.refTypes.length || connector.refTypes.includes(type);
}

function connectAsset(intent: IntentDraft, consumer: AssetSpec, connector: ConnectorDef, provider: AssetSpec, options?: { path?: string; readOnly?: boolean; portName?: string }): void {
  const properties = consumer.properties ?? (consumer.properties = {});
  if (connector.special === "volumeMounts") {
    const mounts = ensureArray(properties.volumeMounts);
    if (!mounts.some((m: any) => m?.volume === provider.name)) {
      mounts.push({ volume: provider.name, path: options?.path || `/mnt/${provider.name}`, readOnly: !!options?.readOnly });
    }
    properties.volumeMounts = mounts;
  } else if (connector.special === "configMounts") {
    const mounts = ensureArray(properties.configMounts);
    if (!mounts.some((m: any) => m?.config === provider.name)) {
      mounts.push({ config: provider.name, path: options?.path || `/etc/${provider.name}`, readOnly: !!options?.readOnly });
    }
    properties.configMounts = mounts;
  } else if (connector.special === "envRef") {
    const outputKey = provider.type === "ObjectStore" && pusherKind(intent.targetPusher) === "aws" ? "bucket" : (connector.envOutput || "url");
    let envKey = provider.type === "ObjectStore" && pusherKind(intent.targetPusher) === "aws" ? "S3_BUCKET" : (connector.envKey || "VALUE");
    const placeholder = `\${intent.${intent.name}.outputs.${provider.name}.${outputKey}}`;
    const env: Record<string, any> = { ...(properties.env ?? {}) };
    const already = Object.values(env).some((value) => outputRefAssetName(String(value ?? "")) === provider.name);
    if (!already) {
      if (env[envKey] !== undefined) {
        envKey = `${provider.name.toUpperCase().replace(/[^A-Z0-9]+/g, "_")}_${envKey}`;
      }
      env[envKey] = placeholder;
      properties.env = env;
    }
  } else if (connector.many) {
    const values = ensureArray(properties[connector.path]).map((v) => String(v));
    if (!values.includes(provider.name)) values.push(provider.name);
    properties[connector.path] = values;
  } else {
    properties[connector.path] = provider.name;
  }
  if (connector.special === "traefikTarget" && options?.portName) {
    properties.portName = options.portName;
  }
  if (connector.special === "targets") {
    const listeners = ensureArray(properties.listeners);
    const providerPorts = ensureArray(provider.properties?.ports);
    const chosen = options?.portName ? providerPorts.find((p: any) => String(p?.name) === options.portName) : providerPorts[0];
    const port = Number(chosen?.port ?? chosen?.containerPort ?? 0);
    if (port > 0 && !listeners.some((l: any) => Number(l?.port) === port)) {
      listeners.push({ name: chosen?.name || "http", port, protocol: chosen?.protocol || "http", description: `Route to ${provider.name}` });
      properties.listeners = listeners;
    }
  }
  addDependsOn(consumer, provider.name);
}

function disconnectAsset(consumer: AssetSpec, connector: ConnectorDef, providerName: string): void {
  const properties = consumer.properties ?? {};
  if (connector.special === "volumeMounts") {
    properties.volumeMounts = ensureArray(properties.volumeMounts).filter((m: any) => m?.volume !== providerName);
  } else if (connector.special === "configMounts") {
    properties.configMounts = ensureArray(properties.configMounts).filter((m: any) => m?.config !== providerName);
  } else if (connector.special === "envRef") {
    const env: Record<string, any> = { ...(properties.env ?? {}) };
    for (const [key, value] of Object.entries(env)) {
      if (outputRefAssetName(String(value ?? "")) === providerName) delete env[key];
    }
    if (Object.keys(env).length) properties.env = env;
    else delete properties.env;
  } else if (connector.many) {
    const values = ensureArray(properties[connector.path]).map((v) => String(v)).filter((v) => v !== providerName);
    if (values.length) properties[connector.path] = values;
    else delete properties[connector.path];
  } else if (String(properties[connector.path] ?? "") === providerName) {
    delete properties[connector.path];
    if (connector.special === "traefikTarget") delete properties.portName;
  }
  removeUnusedDependsOn(consumer);
}

function addDependsOn(asset: AssetSpec, providerName: string): void {
  const deps = ensureArray(asset.dependsOn).map((d) => String(d));
  if (!deps.includes(providerName)) deps.push(providerName);
  asset.dependsOn = deps;
}

function removeUnusedDependsOn(asset: AssetSpec): void {
  const connectors = connectorsFor(asset.type);
  const referenced = new Set<string>();
  for (const connector of connectors) {
    for (const ref of connectorRefs(asset, connector)) referenced.add(ref);
  }
  const deps = ensureArray(asset.dependsOn).map((d) => String(d)).filter((d) => referenced.has(d));
  if (deps.length) asset.dependsOn = deps;
  else delete asset.dependsOn;
}

function providerPorts(asset: AssetSpec): { name: string; port: number }[] {
  return ensureArray(asset.properties?.ports)
    .map((p: any) => ({ name: String(p?.name ?? ""), port: Number(p?.port ?? p?.containerPort ?? 0) }))
    .filter((p) => p.port > 0);
}

// ── Rendering: structure ─────────────────────────────
function renderEmptyState(): void {
  setTopbarTitle("Select a partition");
  const canvas = document.getElementById("builderCanvasContent");
  if (canvas) {
    canvas.innerHTML = `
      <div class="builder-empty">
        <h3>Build a partition visually</h3>
        <p>Pick a partition from the sidebar to edit it, or start a new one. Drag assets from the palette onto intent lanes and connect them.</p>
        <button class="btn-primary" id="builderEmptyNew">New partition</button>
      </div>`;
    document.getElementById("builderEmptyNew")?.addEventListener("click", () => openBuilder());
  }
  renderPalette();
  const inspector = document.getElementById("builderInspector");
  if (inspector) inspector.innerHTML = `<div class="builder-inspector-empty">Select a partition or create a new one.</div>`;
  renderProblems();
  updateTopbarButtons();
}

function renderLoadingState(): void {
  const canvas = document.getElementById("builderCanvasContent");
  if (canvas) canvas.innerHTML = `<div class="builder-empty"><p>Loading catalog…</p></div>`;
}

function setTopbarTitle(text: string): void {
  const el = document.getElementById("builderPartitionLabel");
  if (el) el.textContent = text;
}

function updateTopbarButtons(): void {
  const dirtyBadge = document.getElementById("builderDirtyBadge");
  if (dirtyBadge) dirtyBadge.classList.toggle("hidden", !draft?.dirty);
  const restore = document.getElementById("builderRestoreDraft");
  if (restore) restore.classList.toggle("hidden", !storedDraftAvailable);
  const save = document.getElementById("builderSave") as HTMLButtonElement | null;
  if (save) save.disabled = !draft;
  const discard = document.getElementById("builderDiscard") as HTMLButtonElement | null;
  if (discard) discard.disabled = !draft;
  const status = document.getElementById("builderStatusText");
  if (status) {
    if (draft?.isNew) status.textContent = "New partition";
    else if (draft) status.textContent = draft.dirty ? "Editing · unsaved changes" : "Editing saved manifests";
    else status.textContent = "";
  }
}

function renderTopbar(): void {
  setTopbarTitle(draft?.isNew ? (draft.partition.metadata.name || "New partition") : (draft?.editingPartition || "Partition"));
  updateTopbarButtons();
  ensureBuilderTutorialButton();
  maybeShowBuilderTutorial();
}

// ── Builder tutorial (temporary; delete this block to remove it) ─────
const BUILDER_TUTORIAL_KEY = "guardian.builder.tutorial.hidden";
let builderTutorialShown = false;

function ensureBuilderTutorialButton(): void {
  const actions = document.querySelector(".builder-topbar-actions");
  if (!actions || document.getElementById("builderTutorialHelp")) return;
  const button = document.createElement("button");
  button.id = "builderTutorialHelp";
  button.type = "button";
  button.className = "btn-ghost";
  button.title = "Builder tutorial";
  button.setAttribute("aria-label", "Builder tutorial");
  button.textContent = "?";
  button.addEventListener("click", showBuilderTutorial);
  actions.prepend(button);
}

function maybeShowBuilderTutorial(): void {
  if (builderTutorialShown || !draft) return;
  builderTutorialShown = true;
  try {
    if (localStorage.getItem(BUILDER_TUTORIAL_KEY) === "1") return;
  } catch { /* storage unavailable */ }
  showBuilderTutorial();
}

function showBuilderTutorial(): void {
  if (document.getElementById("builderTutorialOverlay")) return;
  const overlay = document.createElement("div");
  overlay.id = "builderTutorialOverlay";
  overlay.className = "builder-modal-overlay";
  overlay.innerHTML = `
    <div class="builder-modal" role="dialog" aria-modal="true" aria-labelledby="builderTutorialTitle">
      <h3 id="builderTutorialTitle">Builder tutorial</h3>
      <p>From empty canvas to a saved partition in four steps.</p>
      <ol class="builder-tutorial-steps">
        <li><b>Add an intent</b> and set its target pusher and placement. The palette follows the pusher, and each type says who supports it.</li>
        <li><b>Drag or click assets</b> from the palette into the lane. Unsupported types are dimmed with the reason and the pusher kinds that do support them.</li>
        <li><b>Connect</b> consumers to providers: drag from a socket on a card, or open the inspector and use “+ Connect”. Volumes, Configs and Networks are providers.</li>
        <li><b>Fix the Problems strip</b> at the bottom, then <b>Save</b>. Open the inspector's Raw YAML when you need full control.</li>
      </ol>
      <p class="builder-muted">Reopen this from the <b>?</b> button in the toolbar any time.</p>
      <div class="builder-modal-actions">
        <button class="btn-ghost" id="builderTutorialHide">Don't show again</button>
        <button class="btn-primary" id="builderTutorialClose">Got it</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  const close = () => overlay.remove();
  document.getElementById("builderTutorialClose")?.addEventListener("click", close);
  document.getElementById("builderTutorialHide")?.addEventListener("click", () => {
    try { localStorage.setItem(BUILDER_TUTORIAL_KEY, "1"); } catch { /* ignore */ }
    close();
  });
  overlay.addEventListener("click", (event) => { if (event.target === overlay) close(); });
  document.addEventListener("keydown", (event) => { if (event.key === "Escape") close(); }, { once: true });
}

function paletteSubgroup(template: CatalogTemplate): string {
  if (template.category === "Network" && (template.type === "LoadBalancer" || template.type === "TraefikRoute")) {
    return "Load balancing";
  }
  return "";
}

function paletteItemHtml(item: CatalogTemplate, support: string[] | null, activeKind: string): string {
  const unsupported = support !== null && !support.includes(item.type);
  const title = unsupported ? unsupportedPaletteReason(item.type, activeKind) : item.description;
  return `
    <button class="builder-palette-item${unsupported ? " unsupported" : ""}" ${unsupported ? `aria-disabled="true"` : `draggable="true"`} data-asset-type="${escapeAttr(item.type)}" title="${escapeHtml(title)}">
      <span class="builder-palette-icon">${escapeHtml(item.icon || "•")}</span>
      <span class="builder-palette-copy"><b>${escapeHtml(item.title)}</b><small>${escapeHtml(title)}</small></span>
      ${unsupported ? `<span class="builder-palette-badge">unsupported</span>` : ""}
    </button>`;
}

function templatePaletteItem(template: IntentTemplate): string {
  const detail = `${template.assets.length} asset${template.assets.length === 1 ? "" : "s"}${template.description ? ` · ${template.description}` : ""}`;
  return `
    <div class="builder-template-item" data-template-id="${escapeAttr(template.id)}" title="${escapeAttr(template.description || `Insert "${template.name}" as a new intent`)}">
      <span class="builder-palette-icon">📋</span>
      <span class="builder-palette-copy"><b>${escapeHtml(template.name)}</b><small>${escapeHtml(detail)}</small></span>
      <button class="builder-template-delete" data-delete-template="${escapeAttr(template.id)}" title="Delete template">×</button>
    </div>`;
}

function renderPalette(): void {
  const list = document.getElementById("builderPaletteList");
  if (!list) return;
  const search = (document.getElementById("builderPaletteSearch") as HTMLInputElement | null)?.value?.trim().toLowerCase() ?? "";
  const filtered = catalog.filter((t) => !search || t.title.toLowerCase().includes(search) || t.type.toLowerCase().includes(search) || (t.category ?? "").toLowerCase().includes(search));
  const grouped = new Map<string, Map<string, CatalogTemplate[]>>();
  for (const template of filtered) {
    const category = template.category || "Other";
    const subgroup = paletteSubgroup(template);
    if (!grouped.has(category)) grouped.set(category, new Map());
    const bySubgroup = grouped.get(category)!;
    if (!bySubgroup.has(subgroup)) bySubgroup.set(subgroup, []);
    bySubgroup.get(subgroup)!.push(template);
  }
  const activeIntent = selectedIntent() ?? draft?.intents[0];
  const activeKind = activeIntent ? pusherKind(activeIntent.targetPusher) : "";
  const support = activeKind ? (PUSHER_SUPPORT[activeKind] ?? PUSHER_SUPPORT.other) : null;
  const parts: string[] = [];
  const templates = search
    ? intentTemplates.filter((t) => t.name.toLowerCase().includes(search) || t.description.toLowerCase().includes(search))
    : intentTemplates;
  if (templates.length) {
    parts.push(`<div class="builder-palette-group"><div class="builder-palette-group-title">Templates</div>${templates.map(templatePaletteItem).join("")}</div>`);
  }
  for (const [category, bySubgroup] of grouped) {
    parts.push(`<div class="builder-palette-group"><div class="builder-palette-group-title">${escapeHtml(category)}</div>`);
    for (const item of bySubgroup.get("") ?? []) {
      parts.push(paletteItemHtml(item, support, activeKind));
    }
    for (const [subgroup, items] of bySubgroup) {
      if (!subgroup) continue;
      parts.push(`<div class="builder-palette-subgroup"><div class="builder-palette-subgroup-title">${escapeHtml(subgroup)}</div>`);
      for (const item of items) parts.push(paletteItemHtml(item, support, activeKind));
      parts.push(`</div>`);
    }
    parts.push(`</div>`);
  }
  if (!parts.length) parts.push(`<div class="builder-palette-empty">No asset types match.</div>`);
  list.innerHTML = parts.join("");
  list.querySelectorAll<HTMLElement>(".builder-palette-item").forEach((el) => {
    if (el.classList.contains("unsupported")) {
      el.addEventListener("click", () => {
        const intent = selectedIntent() ?? draft?.intents[0];
        if (!intent) return;
        opts.onToast(unsupportedPaletteReason(el.dataset.assetType!, pusherKind(intent.targetPusher)), "info");
      });
      return;
    }
    el.addEventListener("dragstart", (event) => {
      event.dataTransfer?.setData("text/plain", el.dataset.assetType!);
      event.dataTransfer?.setData("application/x-guardian-asset", el.dataset.assetType!);
      if (event.dataTransfer) event.dataTransfer.effectAllowed = "copy";
    });
    el.addEventListener("click", () => {
      const intent = selectedIntent() ?? draft?.intents[0];
      if (!intent) {
        opts.onToast("Add an intent first, then drop assets onto it.", "info");
        return;
      }
      addAsset(intent.uid, el.dataset.assetType!);
    });
  });
  list.querySelectorAll<HTMLElement>("[data-template-id]").forEach((el) => {
    el.addEventListener("click", (event) => {
      if ((event.target as HTMLElement).closest("[data-delete-template]")) return;
      instantiateTemplate(el.dataset.templateId!);
    });
  });
  list.querySelectorAll<HTMLElement>("[data-delete-template]").forEach((el) => {
    el.addEventListener("click", (event) => {
      event.stopPropagation();
      deleteTemplate(el.dataset.deleteTemplate!);
    });
  });
}

// unsupportedPaletteReason explains which pusher kinds can run an asset type
// so a disabled palette item is actionable instead of mysterious.
function unsupportedPaletteReason(assetType: string, kind: string): string {
  const label = KIND_LABELS[kind] ?? kind ?? "this";
  const supportedBy = Object.keys(PUSHER_SUPPORT)
    .filter((k) => k !== "other" && PUSHER_SUPPORT[k].includes(assetType))
    .map((k) => KIND_LABELS[k] ?? k);
  if (supportedBy.length) {
    return `${assetType} is not supported by ${label} pushers yet. Available with: ${supportedBy.join(", ")}.`;
  }
  return `${assetType} is not supported by ${label} pushers yet.`;
}

// ── Rendering: canvas ────────────────────────────────
function renderCanvas(): void {
  const content = document.getElementById("builderCanvasContent");
  if (!content || !draft) return;
  const scroller = document.getElementById("builderCanvas");
  const scrollLeft = scroller?.scrollLeft ?? 0;
  const scrollTop = scroller?.scrollTop ?? 0;
  const parts: string[] = [];
  draft.intents.forEach((intent) => {
    parts.push(renderLane(intent));
  });
  parts.push(`
    <button class="builder-add-lane" id="builderAddLane">
      <span>+</span>
      <b>New intent</b>
      <small>Group assets that deploy together to one pusher.</small>
    </button>`);
  content.innerHTML = `<svg id="builderEdgeLayer" class="builder-edge-layer" xmlns="http://www.w3.org/2000/svg"></svg>${parts.join("")}`;
  wireLaneEvents();
  drawEdges();
  window.requestAnimationFrame(() => {
    drawEdges();
    if (scroller) {
      scroller.scrollLeft = scrollLeft;
      scroller.scrollTop = scrollTop;
    }
  });
}

function renderLane(intent: IntentDraft): string {
  const kind = pusherKind(intent.targetPusher);
  const pusherLabel = intent.targetPusher || "no pusher";
  const placement = placementSummary(intent);
  const incompatible = incompatibleCount(intent);
  const nodes = intent.assets.map((asset) => renderNode(intent, asset, kind)).join("");
  const selected = draft?.selection.kind === "intent" && draft.selection.intentUid === intent.uid;
  return `
    <section class="builder-lane${selected ? " selected" : ""}" data-intent-uid="${escapeAttr(intent.uid)}" style="width:${laneWidth(intent)}px">
      <header class="builder-lane-header" data-intent-uid="${escapeAttr(intent.uid)}">
        <div class="builder-lane-title-row">
          <input class="builder-lane-name" data-intent-uid="${escapeAttr(intent.uid)}" value="${escapeHtml(intent.name)}" aria-label="Intent name">
          <span class="builder-kind-badge kind-${escapeAttr(kind)}">${escapeHtml(KIND_LABELS[kind] ?? kind)}</span>
        </div>
        <div class="builder-lane-sub">
          <button class="builder-pusher-button" data-intent-uid="${escapeAttr(intent.uid)}" data-action="pick-pusher" title="Assign this intent to a pusher">
            ${escapeHtml(pusherLabel)}${placement ? ` · ${escapeHtml(placement)}` : ""}
          </button>
          <span class="builder-lane-count">${intent.assets.length} asset${intent.assets.length === 1 ? "" : "s"}${incompatible ? ` · ${incompatible} incompatible` : ""}</span>
        </div>
      </header>
      <div class="builder-lane-body" data-intent-uid="${escapeAttr(intent.uid)}" style="min-height:${laneBodyHeight(intent)}px">
        ${nodes}
      </div>
    </section>`;
}

function renderNode(intent: IntentDraft, asset: AssetSpec, laneKind: string): string {
  const template = catalogByName.get(asset.type);
  const connectors = connectorsFor(asset.type);
  const position = intent.positions[asset.name] ?? nextFreePosition(intent);
  const issues = assetIssues(intent, asset);
  const supported = PUSHER_SUPPORT[laneKind]?.includes(asset.type) ?? true;
  const selected = draft?.selection.kind === "asset" && draft.selection.intentUid === intent.uid && draft.selection.assetName === asset.name;
  const height = nodeHeight(asset);
  const ports = asset.type === "Compute" ? ensureArray(asset.properties?.ports).slice(0, 3) : [];
  const socketRows = connectors.map((connector) => {
    const refs = connectorRefs(asset, connector);
    const chips = refs.map((ref) => `
      <span class="builder-socket-chip ${assetSupportsRef(intent, connector, ref) ? "" : "missing"}" data-intent-uid="${escapeAttr(intent.uid)}" data-asset="${escapeAttr(asset.name)}" data-connector="${escapeAttr(connector.key)}" data-ref="${escapeAttr(ref)}" title="Disconnect ${escapeAttr(ref)}">${escapeHtml(ref)} <b>×</b></span>`).join("");
    const empty = refs.length === 0;
    return `
      <div class="builder-socket${empty ? " empty" : ""}" data-intent-uid="${escapeAttr(intent.uid)}" data-asset="${escapeAttr(asset.name)}" data-connector="${escapeAttr(connector.key)}" title="${escapeHtml(`${connector.label}: connect to ${refTypesLabel(connector)}`)}">
        <span class="builder-socket-dot"></span>
        <span class="builder-socket-label">${escapeHtml(connector.label)}</span>
        <span class="builder-socket-refs">${chips || `<span class="builder-socket-placeholder">drag to ${escapeHtml(refTypesLabel(connector).toLowerCase())}</span>`}</span>
      </div>`;
  }).join("");
  const portRows = ports.map((port: any) => `
    <div class="builder-port-row"><span class="builder-port-dot"></span>${escapeHtml(String(port?.name ?? "port"))}<span class="builder-port-number">:${escapeHtml(String(port?.port ?? port?.containerPort ?? "?"))}</span></div>`).join("");
  const hintText = issues[0]?.message ?? ASSET_HINTS[asset.type] ?? template?.description ?? "";
  const hint = `<div class="builder-node-hint${issues.length ? " error" : ""}">${escapeHtml(hintText)}</div>`;
  const warn = issues.length || !supported;
  return `
    <div class="builder-node${selected ? " selected" : ""}${warn ? " has-warning" : ""}" data-intent-uid="${escapeAttr(intent.uid)}" data-asset="${escapeAttr(asset.name)}" style="transform:translate(${position.x}px,${position.y}px);width:${NODE_W}px;height:${height}px">
      <div class="builder-node-header">
        <span class="builder-node-icon">${escapeHtml(template?.icon ?? "•")}</span>
        <span class="builder-node-title">${escapeHtml(asset.name)}</span>
        ${warn ? `<span class="builder-node-warning" title="${escapeAttr(unsupportedMessage(asset, laneKind) || issues[0]?.message || "")}">!</span>` : ""}
        <span class="builder-node-type">${escapeHtml(template?.title ?? asset.type)}</span>
      </div>
      ${portRows ? `<div class="builder-node-ports">${portRows}</div>` : ""}
      <div class="builder-node-sockets">${socketRows}</div>
      ${hint}
    </div>`;
}

function unsupportedMessage(asset: AssetSpec, laneKind: string): string {
  if (PUSHER_SUPPORT[laneKind]?.includes(asset.type)) return "";
  return `${asset.type} is not supported by ${KIND_LABELS[laneKind] ?? laneKind} pushers.`;
}

function laneBodyHeight(intent: IntentDraft): number {
  let max = 260;
  for (const asset of intent.assets) {
    const position = intent.positions[asset.name];
    if (!position) continue;
    max = Math.max(max, position.y + nodeHeight(asset) + 40);
  }
  return max;
}

function nodeHeight(asset: AssetSpec): number {
  const ports = asset.type === "Compute" ? Math.min(3, ensureArray(asset.properties?.ports).length) : 0;
  const sockets = connectorsFor(asset.type).length;
  return HEADER_H + ports * PORT_ROW_H + sockets * SOCKET_ROW_H + 20 + NODE_BOTTOM;
}

function socketOffsetY(asset: AssetSpec, connectorKey: string): number {
  const connectors = connectorsFor(asset.type);
  const index = connectors.findIndex((c) => c.key === connectorKey);
  const ports = asset.type === "Compute" ? Math.min(3, ensureArray(asset.properties?.ports).length) : 0;
  return HEADER_H + 8 + ports * PORT_ROW_H + Math.max(0, index) * SOCKET_ROW_H + SOCKET_ROW_H / 2;
}

// ── Rendering: edges ─────────────────────────────────
function laneOffsets(): Map<string, { left: number; top: number; bodyTop: number; width: number }> {
  const result = new Map<string, { left: number; top: number; bodyTop: number; width: number }>();
  const content = document.getElementById("builderCanvasContent");
  if (!content) return result;
  content.querySelectorAll<HTMLElement>(".builder-lane").forEach((lane) => {
    const body = lane.querySelector<HTMLElement>(".builder-lane-body");
    result.set(lane.dataset.intentUid!, {
      left: lane.offsetLeft,
      top: lane.offsetTop,
      bodyTop: (body?.offsetTop ?? 0),
      width: lane.offsetWidth,
    });
  });
  return result;
}

function drawEdges(): void {
  const svg = document.getElementById("builderEdgeLayer") as unknown as SVGSVGElement | null;
  const content = document.getElementById("builderCanvasContent");
  if (!svg || !content || !draft) return;
  const offsets = laneOffsets();
  const parts: string[] = [];
  for (const intent of draft.intents) {
    const offset = offsets.get(intent.uid);
    if (!offset) continue;
    const nodePos = (name: string) => {
      const position = intent.positions[name];
      if (!position) return null;
      return { x: offset.left + position.x, y: offset.bodyTop + position.y };
    };
    for (const asset of intent.assets) {
      const consumer = nodePos(asset.name);
      if (!consumer) continue;
      for (const connector of connectorsFor(asset.type)) {
        const refs = connectorRefs(asset, connector);
        refs.forEach((ref, refIndex) => {
          const provider = intent.assets.find((a) => a.name === ref);
          const providerPos = nodePos(ref);
          if (!providerPos) return;
          const sy = providerPos.y + HEADER_H / 2;
          const sx = providerPos.x + NODE_W;
          const ey = consumer.y + socketOffsetY(asset, connector.key) + refIndex * 2;
          const ex = consumer.x;
          const delta = Math.max(50, Math.abs(ex - sx) / 2);
          const color = provider && connectorAccepts(connector, provider.type) ? "" : " invalid";
          parts.push(`<path class="builder-edge${color}" d="M ${sx} ${sy} C ${sx + delta} ${sy}, ${ex - delta} ${ey}, ${ex} ${ey}" />`);
        });
      }
    }
  }
  for (const intent of draft.intents) {
    const offset = offsets.get(intent.uid);
    if (!offset) continue;
    for (const join of intent.joins) {
      const source = draft.intents.find((i) => i.name === join);
      if (!source) continue;
      const sourceOffset = offsets.get(source.uid);
      if (!sourceOffset) continue;
      const sx = sourceOffset.left + sourceOffset.width;
      const sy = sourceOffset.top + 26;
      const ex = offset.left;
      const ey = offset.top + 26;
      const delta = Math.max(40, Math.abs(ex - sx) / 2);
      parts.push(`<path class="builder-edge join" d="M ${sx} ${sy} C ${sx + delta} ${sy}, ${ex - delta} ${ey}, ${ex} ${ey}" />`);
    }
  }
  if (connectState) {
    const source = connectSourcePoint(connectState);
    if (source) {
      parts.push(`<path class="builder-edge pending" data-pending="1" d="M ${source.x} ${source.y} L ${connectStatePointer.x} ${connectStatePointer.y}" />`);
    }
  }
  svg.setAttribute("width", String(content.scrollWidth));
  svg.setAttribute("height", String(content.scrollHeight));
  svg.innerHTML = parts.join("");
}

let connectStatePointer = { x: 0, y: 0 };

function connectSourcePoint(state: { intentUid: string; assetName: string; connector: ConnectorDef }): { x: number; y: number } | null {
  const intent = draft?.intents.find((i) => i.uid === state.intentUid);
  if (!intent) return null;
  const asset = intent.assets.find((a) => a.name === state.assetName);
  if (!asset) return null;
  const position = intent.positions[asset.name];
  const offsets = laneOffsets();
  const offset = offsets.get(intent.uid);
  if (!position || !offset) return null;
  return { x: offset.left + position.x + NODE_W, y: offset.bodyTop + position.y + socketOffsetY(asset, state.connector.key) };
}

// ── Canvas interactions ──────────────────────────────
function wireLaneEvents(): void {
  const content = document.getElementById("builderCanvasContent");
  if (!content) return;
  content.querySelectorAll<HTMLInputElement>(".builder-lane-name").forEach((input) => {
    input.addEventListener("change", () => renameIntent(input.dataset.intentUid!, input.value));
    input.addEventListener("keydown", (event) => {
      if ((event as KeyboardEvent).key === "Enter") (input as HTMLInputElement).blur();
    });
  });
  content.querySelectorAll<HTMLElement>('[data-action="pick-pusher"]').forEach((button) => {
    button.addEventListener("click", () => pickPusher(button.dataset.intentUid!));
  });
  content.querySelectorAll<HTMLElement>(".builder-lane").forEach((lane) => {
    lane.addEventListener("click", (event) => {
      const target = event.target as HTMLElement;
      if (target.closest("button, input, select, textarea, .builder-node, .builder-socket")) return;
      selectIntent(lane.dataset.intentUid!);
    });
  });
  content.querySelectorAll<HTMLElement>(".builder-node").forEach((node) => {
    wireNodeDrag(node);
  });
  content.querySelectorAll<HTMLElement>(".builder-socket").forEach((socket) => {
    socket.addEventListener("pointerdown", (event) => {
      const target = event.target as HTMLElement;
      if (target.closest(".builder-socket-chip")) return;
      event.preventDefault();
      event.stopPropagation();
      const intentUid = socket.dataset.intentUid!;
      const assetName = socket.dataset.asset!;
      const connectorKey = socket.dataset.connector!;
      const intent = draft?.intents.find((i) => i.uid === intentUid);
      const asset = intent?.assets.find((a) => a.name === assetName);
      const connector = asset ? connectorsFor(asset.type).find((c) => c.key === connectorKey) : undefined;
      if (!intent || !asset || !connector) return;
      startConnect(intentUid, assetName, connector, event);
    });
  });
  content.querySelectorAll<HTMLElement>(".builder-socket-chip").forEach((chip) => {
    chip.addEventListener("click", (event) => {
      event.stopPropagation();
      disconnectByChip(chip.dataset.intentUid!, chip.dataset.asset!, chip.dataset.connector!, chip.dataset.ref!);
    });
  });
  content.querySelectorAll<HTMLElement>(".builder-lane-body").forEach((body) => {
    body.addEventListener("dragover", (event) => {
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = "copy";
      body.classList.add("drag-over");
    });
    body.addEventListener("dragleave", () => body.classList.remove("drag-over"));
    body.addEventListener("drop", (event) => {
      event.preventDefault();
      body.classList.remove("drag-over");
      const type = event.dataTransfer?.getData("application/x-guardian-asset") || event.dataTransfer?.getData("text/plain");
      if (!type || !catalogByName.has(type)) return;
      const rect = body.getBoundingClientRect();
      const position = {
        x: Math.max(8, event.clientX - rect.left - NODE_W / 2),
        y: Math.max(8, event.clientY - rect.top - HEADER_H / 2),
      };
      addAsset(body.dataset.intentUid!, type, position);
    });
  });
  document.getElementById("builderAddLane")?.addEventListener("click", () => addIntent());
}

function wireNodeDrag(node: HTMLElement): void {
  const intentUid = node.dataset.intentUid!;
  const assetName = node.dataset.asset!;
  let dragging = false;
  let moved = false;
  let startX = 0;
  let startY = 0;
  let originX = 0;
  let originY = 0;
  let hoverLane: string | null = null;

  node.addEventListener("pointerdown", (event) => {
    const target = event.target as HTMLElement;
    if (event.button !== 0 || target.closest("input, button, .builder-socket, .builder-socket-chip")) return;
    dragging = true;
    moved = false;
    startX = event.clientX;
    startY = event.clientY;
    const intent = draft?.intents.find((i) => i.uid === intentUid);
    const position = intent?.positions[assetName] ?? { x: 0, y: 0 };
    originX = position.x;
    originY = position.y;
    node.setPointerCapture(event.pointerId);
    event.stopPropagation();
  });

  node.addEventListener("pointermove", (event) => {
    if (!dragging) return;
    const dx = event.clientX - startX;
    const dy = event.clientY - startY;
    if (!moved && Math.abs(dx) < 3 && Math.abs(dy) < 3) return;
    moved = true;
    const intent = draft?.intents.find((i) => i.uid === intentUid);
    if (!intent) return;
    const next = { x: Math.max(6, originX + dx), y: Math.max(6, originY + dy) };
    intent.positions[assetName] = next;
    node.style.transform = `translate(${next.x}px,${next.y}px)`;
    node.classList.add("dragging");
    drawEdges();
    const lane = laneAtPoint(event.clientX, event.clientY, intentUid);
    if (lane !== hoverLane) {
      hoverLane = lane;
      document.querySelectorAll<HTMLElement>(".builder-lane").forEach((el) => el.classList.toggle("drop-target", el.dataset.intentUid === lane));
    }
    event.stopPropagation();
  });

  node.addEventListener("pointerup", (event) => {
    if (!dragging) return;
    dragging = false;
    node.releasePointerCapture(event.pointerId);
    node.classList.remove("dragging");
    document.querySelectorAll(".builder-lane").forEach((el) => el.classList.remove("drop-target"));
    if (moved) {
      const targetLane = laneAtPoint(event.clientX, event.clientY, intentUid);
      if (targetLane && targetLane !== intentUid) {
        moveAsset(intentUid, targetLane, assetName);
      } else {
        persistDraft();
        selectAsset(intentUid, assetName);
      }
    } else {
      selectAsset(intentUid, assetName);
    }
    event.stopPropagation();
  });

  node.addEventListener("click", (event) => {
    const target = event.target as HTMLElement;
    if (target.closest("input, button, .builder-socket, .builder-socket-chip")) return;
    if (moved) return;
    if (armedConnect) {
      event.stopPropagation();
      completeArmedConnect(intentUid, assetName);
      return;
    }
    selectAsset(intentUid, assetName);
  });
}

function laneAtPoint(clientX: number, clientY: number, excludeUid?: string): string | null {
  const elements = document.elementsFromPoint(clientX, clientY);
  for (const element of elements) {
    const lane = (element as HTMLElement).closest?.(".builder-lane") as HTMLElement | null;
    if (lane && lane.dataset.intentUid !== excludeUid) return lane.dataset.intentUid ?? null;
  }
  return null;
}

function startConnect(intentUid: string, assetName: string, connector: ConnectorDef, event: PointerEvent): void {
  connectState = { intentUid, assetName, connector };
  connectStatePointer = { x: 0, y: 0 };
  const source = connectSourcePoint(connectState);
  if (source) connectStatePointer = { x: source.x, y: source.y };
  document.body.classList.add("builder-connecting");
  highlightTargets(intentUid, assetName, connector);
  drawEdges();
  const move = (moveEvent: PointerEvent) => {
    const content = document.getElementById("builderCanvasContent");
    if (!content) return;
    const rect = content.getBoundingClientRect();
    connectStatePointer = { x: moveEvent.clientX - rect.left, y: moveEvent.clientY - rect.top };
    drawEdges();
  };
  const up = (upEvent: PointerEvent) => {
    window.removeEventListener("pointermove", move);
    window.removeEventListener("pointerup", up);
    finishConnect(upEvent);
  };
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", up);
  event.stopPropagation();
}

function highlightTargets(intentUid: string, assetName: string, connector: ConnectorDef): void {
  document.querySelectorAll<HTMLElement>(".builder-node").forEach((node) => {
    const sameIntent = node.dataset.intentUid === intentUid;
    const intent = draft?.intents.find((i) => i.uid === node.dataset.intentUid);
    const asset = intent?.assets.find((a) => a.name === node.dataset.asset);
    const compatible = sameIntent && node.dataset.asset !== assetName && !!asset && connectorAccepts(connector, asset.type);
    node.classList.toggle("connect-target", compatible);
    node.classList.toggle("connect-invalid", sameIntent && !compatible && node.dataset.asset !== assetName);
  });
}

function clearConnectHighlight(): void {
  document.querySelectorAll(".builder-node").forEach((node) => node.classList.remove("connect-target", "connect-invalid"));
  document.body.classList.remove("builder-connecting");
}

function finishConnect(event: PointerEvent): void {
  const state = connectState;
  connectState = null;
  clearConnectHighlight();
  if (!state) return;
  const element = document.elementFromPoint(event.clientX, event.clientY) as HTMLElement | null;
  const node = element?.closest<HTMLElement>(".builder-node");
  if (!node) {
    renderBuilder();
    return;
  }
  performConnect(state.intentUid, state.assetName, state.connector, node.dataset.intentUid!, node.dataset.asset!);
}

function completeArmedConnect(targetIntentUid: string, targetAssetName: string): void {
  const state = armedConnect;
  if (!state) return;
  armedConnect = null;
  clearConnectHighlight();
  performConnect(state.intentUid, state.assetName, state.connector, targetIntentUid, targetAssetName);
}

async function performConnect(intentUid: string, consumerName: string, connector: ConnectorDef, targetIntentUid: string, providerName: string): Promise<void> {
  if (intentUid !== targetIntentUid) {
    opts.onToast("Assets can only reference others in the same intent.", "error");
    renderBuilder();
    return;
  }
  const intent = draft?.intents.find((i) => i.uid === intentUid);
  const consumer = intent?.assets.find((a) => a.name === consumerName);
  const provider = intent?.assets.find((a) => a.name === providerName);
  if (!intent || !consumer || !provider) return;
  if (consumer === provider) {
    opts.onToast("An asset cannot connect to itself.", "error");
    renderBuilder();
    return;
  }
  if (!connectorAccepts(connector, provider.type)) {
    opts.onToast(`${connector.label} needs a ${refTypesLabel(connector)}.`, "error");
    renderBuilder();
    return;
  }
  const options: { path?: string; readOnly?: boolean; portName?: string } = {};
  if (connector.special === "targets" || connector.special === "traefikTarget") {
    const ports = providerPorts(provider);
    if (ports.length > 1) {
      const result = await showModal({
        title: connector.special === "targets" ? "Route to which port?" : "Expose which port?",
        description: `${provider.name} exposes ${ports.length} ports.`,
        fields: [{ name: "portName", label: "Port", type: "select", options: ports.map((p) => ({ value: p.name, label: `${p.name} :${p.port}` })) }],
        actions: [{ key: "cancel", label: "Cancel" }, { key: "ok", label: "Connect", primary: true }],
      });
      if (!result || result.action !== "ok") {
        renderBuilder();
        return;
      }
      options.portName = String(result.values.portName ?? "");
    } else if (ports.length === 1) {
      options.portName = ports[0].name;
    }
  }
  pushUndo();
  connectAsset(intent, consumer, connector, provider, options);
  markDirty(intent);
  renderBuilder();
  opts.onToast(`Connected ${provider.name} → ${consumer.name}`, "success");
}

function disconnectByChip(intentUid: string, assetName: string, connectorKey: string, ref: string): void {
  const intent = draft?.intents.find((i) => i.uid === intentUid);
  const consumer = intent?.assets.find((a) => a.name === assetName);
  const connector = consumer ? connectorsFor(consumer.type).find((c) => c.key === connectorKey) : undefined;
  if (!intent || !consumer || !connector) return;
  pushUndo();
  disconnectAsset(consumer, connector, ref);
  markDirty(intent);
  renderBuilder();
}

// ── Rendering: inspector ─────────────────────────────
function selectedIntent(): IntentDraft | undefined {
  if (!draft) return undefined;
  if (draft.selection.kind === "intent") return draft.intents.find((i) => i.uid === draft!.selection.intentUid);
  if (draft.selection.kind === "asset") return draft.intents.find((i) => i.uid === draft!.selection.intentUid);
  return undefined;
}

function selectedAsset(): { intent: IntentDraft; asset: AssetSpec } | undefined {
  const intent = selectedIntent();
  if (!intent || draft?.selection.kind !== "asset") return undefined;
  const asset = intent.assets.find((a) => a.name === draft!.selection.assetName);
  return asset ? { intent, asset } : undefined;
}

function renderInspector(): void {
  const inspector = document.getElementById("builderInspector");
  if (!inspector || !draft) return;
  if (draft.selection.kind === "asset") {
    const selected = selectedAsset();
    if (selected) {
      renderAssetInspector(inspector, selected.intent, selected.asset);
      return;
    }
  }
  if (draft.selection.kind === "intent") {
    const intent = selectedIntent();
    if (intent) {
      renderIntentInspector(inspector, intent);
      return;
    }
  }
  renderPartitionInspector(inspector);
}

function renderPartitionInspector(inspector: HTMLElement): void {
  const partition = draft!.partition;
  const isNew = draft!.isNew;
  inspector.innerHTML = `
    <div class="builder-inspector-scroll">
      <div class="builder-inspector-head">
        <span class="builder-inspector-icon">📦</span>
        <div>
          <div class="builder-inspector-title">${isNew ? "New partition" : escapeHtml(partition.metadata.name)}</div>
          <div class="builder-inspector-sub">Partition settings</div>
        </div>
      </div>
      <section class="builder-inspector-section">
        <h4>Identity</h4>
        ${isNew ? `
        <label class="builder-field"><span>Name</span>
          <input id="builderPartitionName" class="search-input" value="${escapeHtml(partition.metadata.name)}" placeholder="my-partition">
          <small>Lowercase letters, digits, and dashes.</small>
        </label>` : ""}
      </section>
      <section class="builder-inspector-section">
        <h4>Lifecycle</h4>
        <label class="builder-field"><span>Deletion policy</span>
          <select id="builderDeletionPolicy" class="search-input">
            ${["orphan", "destroy"].map((v) => `<option value="${v}"${partition.spec.deletionPolicy === v ? " selected" : ""}>${v}</option>`).join("")}
          </select>
        </label>
        <label class="builder-field"><span>Reconciliation</span>
          <select id="builderReconcileMode" class="search-input">
            ${["auto", "manual", "readonly"].map((v) => `<option value="${v}"${partition.spec.reconciliation?.mode === v ? " selected" : ""}>${v}</option>`).join("")}
          </select>
        </label>
        <label class="builder-field"><span>Interval</span>
          <input id="builderReconcileInterval" class="search-input" value="${escapeHtml(partition.spec.reconciliation?.interval ?? "")}" placeholder="15m">
        </label>
      </section>
      <section class="builder-inspector-section">
        <h4>Defaults</h4>
        <label class="builder-field"><span>Default pusher</span>
          <select id="builderDefaultPusher" class="search-input">${pusherOptions(partition.spec.defaults?.targetPusher ?? "")}</select>
        </label>
      </section>
    </div>`;
  const nameInput = document.getElementById("builderPartitionName") as HTMLInputElement | null;
  nameInput?.addEventListener("change", () => {
    draft!.partition.metadata.name = nameInput.value.trim();
    draft!.dirty = true;
    persistDraft();
    renderTopbar();
    scheduleServerValidation();
  });
  bindSimpleField("builderDeletionPolicy", (value) => {
    draft!.partition.spec.deletionPolicy = value;
    draft!.dirty = true;
  });
  bindSimpleField("builderReconcileMode", (value) => {
    draft!.partition.spec.reconciliation = draft!.partition.spec.reconciliation ?? {};
    draft!.partition.spec.reconciliation.mode = value;
    draft!.dirty = true;
  });
  bindSimpleField("builderReconcileInterval", (value) => {
    draft!.partition.spec.reconciliation = draft!.partition.spec.reconciliation ?? {};
    draft!.partition.spec.reconciliation.interval = value;
    draft!.dirty = true;
  });
  bindSimpleField("builderDefaultPusher", (value) => {
    draft!.partition.spec.defaults = draft!.partition.spec.defaults ?? {};
    draft!.partition.spec.defaults.targetPusher = value;
    draft!.dirty = true;
  });
}

function renderIntentInspector(inspector: HTMLElement, intent: IntentDraft): void {
  const kind = pusherKind(intent.targetPusher);
  const incompatible = intentIncompatible(intent);
  inspector.innerHTML = `
    <div class="builder-inspector-scroll">
      <div class="builder-inspector-head">
        <span class="builder-inspector-icon">🗂️</span>
        <div>
          <div class="builder-inspector-title">${escapeHtml(intent.name)}</div>
          <div class="builder-inspector-sub">Intent · ${escapeHtml(KIND_LABELS[kind] ?? kind)}</div>
        </div>
      </div>
      <section class="builder-inspector-section">
        <h4>Placement</h4>
        <label class="builder-field"><span>Target pusher</span>
          <div class="builder-inline">
            <select id="builderIntentPusher" class="search-input">${pusherOptions(intent.targetPusher)}</select>
            <button class="btn-ghost" id="builderMigratePusher">Migrate…</button>
          </div>
        </label>
        ${placementFieldsHtml(intent)}
        ${incompatible.length ? `<div class="builder-inspector-warning">${incompatible.length} asset${incompatible.length === 1 ? "" : "s"} not supported by this pusher: ${escapeHtml(incompatible.map((a) => a.name).join(", "))}</div>` : ""}
      </section>
      <section class="builder-inspector-section">
        <h4>Behaviour</h4>
        <label class="builder-field"><span>Type</span>
          <select id="builderIntentType" class="search-input">
            <option value="standard"${intent.intentType !== "observed" ? " selected" : ""}>standard</option>
            <option value="observed"${intent.intentType === "observed" ? " selected" : ""}>observed (AWS, report-only)</option>
          </select>
        </label>
        <label class="builder-field checkbox"><input type="checkbox" id="builderIntentLocked"${intent.locked ? " checked" : ""}> <span>Locked (block automatic changes)</span></label>
      </section>
      <section class="builder-inspector-section">
        <h4>Apply after</h4>
        <div class="builder-chip-row">
          ${draft!.intents.filter((i) => i.uid !== intent.uid).map((other) => `
            <label class="builder-chip checkbox"><input type="checkbox" data-join="${escapeAttr(other.name)}"${intent.joins.includes(other.name) ? " checked" : ""}> ${escapeHtml(other.name)}</label>`).join("") || `<span class="builder-muted">No other intents yet.</span>`}
        </div>
      </section>
      <section class="builder-inspector-section">
        <h4>Reuse</h4>
        <button class="btn-ghost" id="builderSaveTemplate">Save as template</button>
        <p class="builder-muted">Templates appear in the palette and can seed a new intent in any partition.</p>
      </section>
      <section class="builder-inspector-section">
        <button class="btn-ghost builder-danger" id="builderDeleteIntent">Delete intent</button>
      </section>
    </div>`;
  const pusherSelect = document.getElementById("builderIntentPusher") as HTMLSelectElement | null;
  pusherSelect?.addEventListener("change", () => migrateIntent(intent, pusherSelect.value));
  document.getElementById("builderMigratePusher")?.addEventListener("click", () => pickPusher(intent.uid));
  bindPlacementFields(intent);
  bindSimpleField("builderIntentType", (value) => {
    intent.intentType = value;
    markDirty(intent);
    renderBuilder();
  });
  const locked = document.getElementById("builderIntentLocked") as HTMLInputElement | null;
  locked?.addEventListener("change", () => {
    intent.locked = locked.checked;
    markDirty(intent);
  });
  inspector.querySelectorAll<HTMLInputElement>("[data-join]").forEach((checkbox) => {
    checkbox.addEventListener("change", () => {
      const name = checkbox.dataset.join!;
      const set = new Set(intent.joins);
      if (checkbox.checked) set.add(name);
      else set.delete(name);
      intent.joins = [...set];
      markDirty(intent);
      renderBuilder();
    });
  });
  document.getElementById("builderDeleteIntent")?.addEventListener("click", () => deleteIntent(intent.uid));
  document.getElementById("builderSaveTemplate")?.addEventListener("click", () => saveIntentAsTemplate(intent));
}

function renderAssetInspector(inspector: HTMLElement, intent: IntentDraft, asset: AssetSpec): void {
  const template = catalogByName.get(asset.type);
  const connectors = connectorsFor(asset.type);
  const fields = template?.fields ?? [];
  const essentials = new Set(ESSENTIAL_FIELDS[asset.type] ?? []);
  const essentialFields = fields.filter((f) => essentials.has(f.path));
  const advancedFields = fields.filter((f) => !essentials.has(f.path));
  const issues = assetIssues(intent, asset);
  const supported = PUSHER_SUPPORT[pusherKind(intent.targetPusher)]?.includes(asset.type) ?? true;
  const payloadKeys = payloadKeysFor(asset.type);
  inspector.innerHTML = `
    <div class="builder-inspector-scroll">
      <div class="builder-inspector-head">
        <span class="builder-inspector-icon">${escapeHtml(template?.icon ?? "•")}</span>
        <div>
          <div class="builder-inspector-title">${escapeHtml(asset.name)}</div>
          <div class="builder-inspector-sub">${escapeHtml(template?.title ?? asset.type)}</div>
        </div>
      </div>
      ${!supported ? `<div class="builder-inspector-warning">${escapeHtml(unsupportedMessage(asset, pusherKind(intent.targetPusher)))}</div>` : ""}
      ${issues.length ? `<div class="builder-inspector-issues">${issues.map((i) => `<div class="builder-inspector-issue ${i.severity}">${escapeHtml(i.message)}</div>`).join("")}</div>` : ""}
      <section class="builder-inspector-section">
        <h4>Identity</h4>
        <label class="builder-field"><span>Name</span>
          <input id="builderAssetName" class="search-input" value="${escapeHtml(asset.name)}">
          <small>Referenced by name inside this intent.</small>
        </label>
      </section>
      <section class="builder-inspector-section">
        <h4>Connections</h4>
        ${connectors.length ? connectors.map((connector) => {
          const refs = connectorRefs(asset, connector);
          return `
            <div class="builder-connection">
              <div class="builder-connection-head">
                <span>${escapeHtml(connector.label)}</span>
                <button class="btn-ghost builder-mini" data-connect="${escapeAttr(connector.key)}">+ Connect</button>
              </div>
              <div class="builder-chip-row">
                ${refs.map((ref) => `<span class="builder-chip">${escapeHtml(ref)} <button data-disconnect="${escapeAttr(connector.key)}" data-ref="${escapeAttr(ref)}">×</button></span>`).join("") || `<span class="builder-muted">Not connected</span>`}
              </div>
            </div>`;
        }).join("") : `<div class="builder-muted">This asset type has no inbound connections.</div>`}
      </section>
      <section class="builder-inspector-section">
        <h4>Configuration</h4>
        ${essentialFields.map((field) => renderCatalogField(asset, field)).join("")}
        ${advancedFields.length ? `<details class="builder-advanced"><summary>Advanced</summary>${advancedFields.map((field) => renderCatalogField(asset, field)).join("")}</details>` : ""}
      </section>
      ${payloadKeys.length ? `
      <section class="builder-inspector-section">
        <h4>Payload</h4>
        ${payloadKeys.map((key) => `
          <label class="builder-field"><span>${escapeHtml(key)} path</span>
            <input class="search-input" data-payload="${escapeAttr(key)}" value="${escapeHtml(asset.payload?.[key] ?? "")}" placeholder="/partitions/.../payloads/...">
          </label>`).join("")}
      </section>` : ""}
      <section class="builder-inspector-section">
        <h4>Apply order</h4>
        <div class="builder-chip-row">
          ${ensureArray(asset.dependsOn).map((dep) => `<span class="builder-chip">after ${escapeHtml(String(dep))} <button data-remove-dep="${escapeAttr(String(dep))}">×</button></span>`).join("") || `<span class="builder-muted">Auto-managed by connections</span>`}
        </div>
      </section>
      <section class="builder-inspector-section">
        <label class="builder-field checkbox"><input type="checkbox" id="builderAssetSkip"${asset.skip ? " checked" : ""}> <span>Skip this asset</span></label>
      </section>
      <section class="builder-inspector-section builder-inspector-actions">
        <button class="btn-ghost" id="builderDuplicateAsset">Duplicate</button>
        <button class="btn-ghost builder-danger" id="builderDeleteAsset">Delete</button>
      </section>
    </div>`;
  const nameInput = document.getElementById("builderAssetName") as HTMLInputElement | null;
  nameInput?.addEventListener("change", () => renameAsset(intent, asset, nameInput.value));
  nameInput?.addEventListener("keydown", (event) => {
    if ((event as KeyboardEvent).key === "Enter") (nameInput as HTMLInputElement).blur();
  });
  inspector.querySelectorAll<HTMLElement>("[data-connect]").forEach((button) => {
    button.addEventListener("click", () => armConnect(intent.uid, asset.name, button.dataset.connect!));
  });
  inspector.querySelectorAll<HTMLElement>("[data-disconnect]").forEach((button) => {
    button.addEventListener("click", () => disconnectByChip(intent.uid, asset.name, button.dataset.disconnect!, button.dataset.ref!));
  });
  inspector.querySelectorAll<HTMLElement>("[data-remove-dep]").forEach((button) => {
    button.addEventListener("click", () => {
      pushUndo();
      asset.dependsOn = ensureArray(asset.dependsOn).map(String).filter((d) => d !== button.dataset.removeDep);
      markDirty(intent);
      renderBuilder();
    });
  });
  bindCatalogFields(intent, asset, fields);
  bindPayloadFields(intent, asset);
  const skip = document.getElementById("builderAssetSkip") as HTMLInputElement | null;
  skip?.addEventListener("change", () => {
    asset.skip = skip.checked;
    markDirty(intent);
  });
  document.getElementById("builderDuplicateAsset")?.addEventListener("click", () => duplicateAsset(intent.uid, asset.name));
  document.getElementById("builderDeleteAsset")?.addEventListener("click", () => deleteAsset(intent.uid, asset.name));
}

function placementFieldsHtml(intent: IntentDraft): string {
  const kind = pusherKind(intent.targetPusher);
  const fields: { key: keyof Placement; label: string; placeholder: string }[] = [];
  if (kind === "aws") {
    fields.push({ key: "region", label: "Region", placeholder: "eu-west-1" }, { key: "account", label: "Account", placeholder: "123456789012" });
  } else if (kind === "kubernetes" || kind === "docker") {
    fields.push({ key: "cluster", label: "Cluster", placeholder: "prod" }, { key: "namespace", label: "Namespace", placeholder: "default" });
  } else {
    fields.push({ key: "cluster", label: "Cluster", placeholder: "local" }, { key: "namespace", label: "Namespace", placeholder: "" }, { key: "region", label: "Region", placeholder: "" }, { key: "account", label: "Account", placeholder: "" });
  }
  return fields.map((field) => `
    <label class="builder-field"><span>${escapeHtml(field.label)}</span>
      <input class="search-input" data-placement="${field.key}" value="${escapeHtml(intent.target[field.key] ?? "")}" placeholder="${escapeHtml(field.placeholder)}">
    </label>`).join("");
}

function bindPlacementFields(intent: IntentDraft): void {
  document.querySelectorAll<HTMLInputElement>("[data-placement]").forEach((input) => {
    input.addEventListener("change", () => {
      const key = input.dataset.placement as keyof Placement;
      const value = input.value.trim();
      if (value) intent.target[key] = value;
      else delete intent.target[key];
      markDirty(intent);
    });
  });
}

function placementSummary(intent: IntentDraft): string {
  const kind = pusherKind(intent.targetPusher);
  if (kind === "aws") return [intent.target.account, intent.target.region].filter(Boolean).join(" / ");
  const parts = [intent.target.cluster, intent.target.namespace].filter(Boolean);
  return parts.join(" / ");
}

function payloadKeysFor(type: string): string[] {
  if (type === "CDKStack") return ["aws"];
  if (type === "K8sResource") return ["k8s"];
  return [];
}

function renderCatalogField(asset: AssetSpec, field: CatalogField): string {
  const value = getProp(asset.properties, field.path);
  const control = field.control || "text";
  const label = `<span>${escapeHtml(field.title)}</span>`;
  const hint = field.description ? `<small>${escapeHtml(field.description)}</small>` : "";
  if (control === "asset-ref" || control === "asset-refs") {
    const refs = ensureArray(value).map(String).filter(Boolean);
    return `
      <div class="builder-field">
        ${label}
        <div class="builder-chip-row">
          ${refs.map((ref) => `<span class="builder-chip">${escapeHtml(ref)}</span>`).join("") || `<span class="builder-muted">Not connected</span>`}
        </div>
        ${hint}
      </div>`;
  }
  if (control === "boolean") {
    return `<label class="builder-field checkbox"><input type="checkbox" data-field-path="${escapeAttr(field.path)}"${value === true ? " checked" : ""}> ${label}${hint}</label>`;
  }
  if (control === "select") {
    const current = value === undefined || value === null ? "" : String(value);
    return `
      <label class="builder-field">${label}
        <select class="search-input" data-field-path="${escapeAttr(field.path)}">
          ${(field.options ?? []).map((option) => `<option value="${escapeAttr(option)}"${current === option ? " selected" : ""}>${escapeHtml(option)}</option>`).join("")}
        </select>${hint}
      </label>`;
  }
  if (control === "json" || control === "list") {
    const rawKey = `${asset.name}:${field.path}`;
    let display: string;
    if (control === "json") {
      display = rawJsonEdits.get(rawKey) ?? (value === undefined ? "" : JSON.stringify(value, null, 2));
    } else {
      display = ensureArray(value).map(String).join(", ");
    }
    const controlTag = control === "json"
      ? `<textarea class="builder-json" rows="4" data-json-path="${escapeAttr(field.path)}" data-raw-key="${escapeAttr(rawKey)}" spellcheck="false">${escapeHtml(display)}</textarea>`
      : `<input class="search-input" data-list-path="${escapeAttr(field.path)}" value="${escapeHtml(display)}">`;
    return `<label class="builder-field">${label}${controlTag}${hint}</label>`;
  }
  if (control === "number") {
    return `<label class="builder-field">${label}<input class="search-input" type="number" data-field-path="${escapeAttr(field.path)}" value="${value === undefined || value === null ? "" : escapeHtml(String(value))}">${hint}</label>`;
  }
  if (control === "textarea") {
    return `<label class="builder-field">${label}<textarea class="builder-json" rows="3" data-field-path="${escapeAttr(field.path)}">${escapeHtml(value === undefined ? "" : String(value))}</textarea>${hint}</label>`;
  }
  return `<label class="builder-field">${label}<input class="search-input" data-field-path="${escapeAttr(field.path)}" value="${value === undefined || value === null ? "" : escapeHtml(String(value))}" placeholder="${escapeHtml(field.placeholder ?? "")}">${hint}</label>`;
}

function bindCatalogFields(intent: IntentDraft, asset: AssetSpec, fields: CatalogField[]): void {
  document.querySelectorAll<HTMLElement>("[data-field-path]").forEach((el) => {
    const path = el.dataset.fieldPath!;
    const field = fields.find((f) => f.path === path);
    const eventName = el instanceof HTMLSelectElement || (el as HTMLInputElement).type === "checkbox" || field?.control === "textarea" ? "change" : "input";
    el.addEventListener(eventName, () => {
      const props = asset.properties ?? (asset.properties = {});
      const input = el as HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement;
      if (input instanceof HTMLInputElement && input.type === "checkbox") {
        setProp(props, path, input.checked);
      } else if (field?.control === "number") {
        const parsed = Number(input.value);
        setProp(props, path, input.value === "" ? undefined : Number.isFinite(parsed) ? parsed : input.value);
      } else {
        setProp(props, path, input.value);
      }
      markDirty(intent);
    });
  });
  document.querySelectorAll<HTMLTextAreaElement>("[data-json-path]").forEach((textarea) => {
    const path = textarea.dataset.jsonPath!;
    textarea.addEventListener("input", () => rawJsonEdits.set(`${asset.name}:${path}`, textarea.value));
    textarea.addEventListener("blur", () => {
      const raw = textarea.value.trim();
      if (!raw) {
        rawJsonEdits.delete(`${asset.name}:${path}`);
        pushUndo();
        delete asset.properties?.[path];
        markDirty(intent);
        renderBuilder();
        return;
      }
      try {
        const parsed = JSON.parse(raw);
        rawJsonEdits.delete(`${asset.name}:${path}`);
        pushUndo();
        asset.properties = asset.properties ?? {};
        asset.properties[path] = parsed;
        markDirty(intent);
        renderBuilder();
      } catch {
        opts.onToast(`Invalid JSON for ${path}`, "error");
      }
    });
  });
  document.querySelectorAll<HTMLInputElement>("[data-list-path]").forEach((input) => {
    const path = input.dataset.listPath!;
    input.addEventListener("change", () => {
      const values = input.value.split(",").map((v) => v.trim()).filter(Boolean);
      pushUndo();
      asset.properties = asset.properties ?? {};
      if (values.length) asset.properties[path] = values;
      else delete asset.properties[path];
      markDirty(intent);
      renderBuilder();
    });
  });
}

function bindPayloadFields(intent: IntentDraft, asset: AssetSpec): void {
  document.querySelectorAll<HTMLInputElement>("[data-payload]").forEach((input) => {
    input.addEventListener("change", () => {
      const key = input.dataset.payload!;
      const value = input.value.trim();
      asset.payload = asset.payload ?? {};
      if (value) asset.payload[key] = value;
      else delete asset.payload[key];
      if (!Object.keys(asset.payload).length) delete asset.payload;
      markDirty(intent);
    });
  });
}

// ── Mutations ────────────────────────────────────────
function selectAsset(intentUid: string, assetName: string, rerender = true): void {
  if (!draft) return;
  draft.selection = { kind: "asset", intentUid, assetName };
  if (rerender) renderBuilder();
}

function selectIntent(intentUid: string, rerender = true): void {
  if (!draft) return;
  if (draft.selection.kind === "intent" && draft.selection.intentUid === intentUid) return;
  draft.selection = { kind: "intent", intentUid };
  if (rerender) renderBuilder();
}

function addAsset(intentUid: string, type: string, position?: { x: number; y: number }): void {
  const intent = draft?.intents.find((i) => i.uid === intentUid);
  const template = catalogByName.get(type);
  if (!intent || !template || !draft) return;
  pushUndo();
  const name = uniqueName(slugify(template.title), intent.assets.map((a) => a.name));
  const asset: AssetSpec = {
    type,
    name,
    properties: JSON.parse(JSON.stringify(template.template ?? {})),
  };
  intent.assets.push(asset);
  intent.positions[name] = position ?? nextFreePosition(intent);
  draft.selection = { kind: "asset", intentUid, assetName: name };
  markDirty(intent);
  renderBuilder();
  const hint = ASSET_HINTS[type];
  if (hint) opts.onToast(hint, "info");
}

function duplicateAsset(intentUid: string, assetName: string): void {
  const intent = draft?.intents.find((i) => i.uid === intentUid);
  const asset = intent?.assets.find((a) => a.name === assetName);
  if (!intent || !asset || !draft) return;
  pushUndo();
  const copy: AssetSpec = JSON.parse(JSON.stringify(asset));
  copy.name = uniqueName(`${asset.name}-copy`, intent.assets.map((a) => a.name));
  const position = intent.positions[assetName] ?? nextFreePosition(intent);
  intent.assets.push(copy);
  intent.positions[copy.name] = { x: Math.max(6, position.x + 20), y: position.y + 30 };
  draft.selection = { kind: "asset", intentUid, assetName: copy.name };
  markDirty(intent);
  renderBuilder();
}

function deleteAsset(intentUid: string, assetName: string): void {
  const intent = draft?.intents.find((i) => i.uid === intentUid);
  const asset = intent?.assets.find((a) => a.name === assetName);
  if (!intent || !asset || !draft) return;
  const consumers = intent.assets.filter((candidate) =>
    connectorsFor(candidate.type).some((connector) => connectorRefs(candidate, connector).includes(assetName)));
  const message = consumers.length
    ? `${assetName} is used by ${consumers.map((c) => c.name).join(", ")}. Deleting it will remove those connections.`
    : `Delete ${assetName}?`;
  confirmAction("Delete asset", message, "Delete", true).then((ok) => {
    if (!ok) return;
    pushUndo();
    for (const consumer of consumers) {
      for (const connector of connectorsFor(consumer.type)) disconnectAsset(consumer, connector, assetName);
    }
    intent.assets = intent.assets.filter((a) => a.name !== assetName);
    delete intent.positions[assetName];
    for (const other of intent.assets) {
      if (other.dependsOn) {
        other.dependsOn = other.dependsOn.filter((d) => d !== assetName);
        if (!other.dependsOn.length) delete other.dependsOn;
      }
    }
    draft!.selection = { kind: "intent", intentUid };
    markDirty(intent);
    renderBuilder();
  }).catch(opts.onError);
}

function renameAsset(intent: IntentDraft, asset: AssetSpec, rawName: string): void {
  const next = rawName.trim();
  if (next === asset.name) return;
  if (!NAME_RE.test(next)) {
    opts.onToast("Names must start with a letter and use lowercase letters, digits, and dashes.", "error");
    renderBuilder();
    return;
  }
  if (intent.assets.some((a) => a.name === next)) {
    opts.onToast(`Asset "${next}" already exists in this intent.`, "error");
    renderBuilder();
    return;
  }
  pushUndo();
  const previous = asset.name;
  asset.name = next;
  for (const other of intent.assets) {
    for (const connector of connectorsFor(other.type)) {
      const properties = other.properties ?? {};
      if (connector.special === "volumeMounts") {
        for (const mount of ensureArray(properties.volumeMounts)) if (mount?.volume === previous) mount.volume = next;
      } else if (connector.special === "configMounts") {
        for (const mount of ensureArray(properties.configMounts)) if (mount?.config === previous) mount.config = next;
      } else if (connector.many) {
        const values = ensureArray(properties[connector.path]).map((v: any) => String(v) === previous ? next : v);
        if (values.length) properties[connector.path] = values;
      } else if (String(properties[connector.path] ?? "") === previous) {
        properties[connector.path] = next;
      }
    }
    if (other.dependsOn) {
      other.dependsOn = other.dependsOn.map((d) => d === previous ? next : d);
    }
  }
  if (intent.hints?.length) {
    const prefix = `assets.${previous}.`;
    intent.hints = intent.hints.map((hint: any) => hint?.path?.startsWith(prefix) ? { ...hint, path: `assets.${next}.` + hint.path.slice(prefix.length) } : hint);
  }
  const position = intent.positions[previous];
  delete intent.positions[previous];
  intent.positions[next] = position ?? nextFreePosition(intent);
  if (draft) draft.selection = { kind: "asset", intentUid: intent.uid, assetName: next };
  markDirty(intent);
  renderBuilder();
  opts.onToast(`Renamed ${previous} → ${next}`, "success");
}

function moveAsset(fromUid: string, toUid: string, assetName: string): void {
  const from = draft?.intents.find((i) => i.uid === fromUid);
  const to = draft?.intents.find((i) => i.uid === toUid);
  const asset = from?.assets.find((a) => a.name === assetName);
  if (!from || !to || !asset || !draft) return;
  if (to.assets.some((a) => a.name === asset.name)) {
    opts.onToast(`"${asset.name}" already exists in ${to.name}. Rename it first.`, "error");
    renderBuilder();
    return;
  }
  pushUndo();
  const movedName = asset.name;
  from.assets = from.assets.filter((a) => a.name !== movedName);
  delete from.positions[movedName];
  const removed: string[] = [];
  for (const candidate of from.assets) {
    for (const connector of connectorsFor(candidate.type)) {
      if (connectorRefs(candidate, connector).includes(movedName)) {
        disconnectAsset(candidate, connector, movedName);
        removed.push(candidate.name);
      }
    }
  }
  for (const connector of connectorsFor(asset.type)) {
    for (const ref of connectorRefs(asset, connector)) {
      if (!to.assets.some((a) => a.name === ref)) {
        disconnectAsset(asset, connector, ref);
        removed.push(ref);
      }
    }
  }
  to.assets.push(asset);
  to.positions[movedName] = nextFreePosition(to);
  if (draft) draft.selection = { kind: "asset", intentUid: to.uid, assetName: movedName };
  markDirty(from);
  markDirty(to);
  renderBuilder();
  const kind = pusherKind(to.targetPusher);
  const supported = PUSHER_SUPPORT[kind]?.includes(asset.type) ?? true;
  if (!supported) {
    opts.onToast(`Moved ${movedName} to ${to.name}, but ${asset.type} is not supported by ${KIND_LABELS[kind] ?? kind} pushers.`, "error");
  } else if (removed.length) {
    opts.onToast(`Moved ${movedName}; removed ${removed.length} cross-intent connection${removed.length === 1 ? "" : "s"}.`, "info");
  } else {
    opts.onToast(`Moved ${movedName} to ${to.name}`, "success");
  }
}

function addIntent(): void {
  if (!draft) return;
  pushUndo();
  const name = uniqueName("intent", draft.intents.map((i) => i.name));
  const defaultPusher = draft.partition.spec.defaults?.targetPusher || draft.intents[0]?.targetPusher || registryPushers[0]?.name || "";
  const intent: IntentDraft = {
    uid: name,
    name,
    isNew: true,
    dirty: true,
    expectedVersionID: "",
    intentType: "standard",
    targetPusher: defaultPusher,
    target: defaultPlacementFor(defaultPusher),
    joins: [],
    locked: false,
    hints: [],
    assets: [],
    positions: {},
  };
  draft.intents.push(intent);
  draft.selection = { kind: "intent", intentUid: intent.uid };
  markDirty(intent);
  renderBuilder();
}

// ── Intent templates ─────────────────────────────────
function loadTemplates(): void {
  try {
    const raw = localStorage.getItem(TEMPLATE_KEY);
    if (!raw) {
      intentTemplates = [];
      return;
    }
    const parsed = JSON.parse(raw);
    const list = Array.isArray(parsed) ? parsed : parsed?.templates;
    intentTemplates = Array.isArray(list) ? list.filter((t: any) => t?.id && t?.name && Array.isArray(t?.assets)) : [];
  } catch {
    intentTemplates = [];
  }
}

function persistTemplates(): void {
  try {
    localStorage.setItem(TEMPLATE_KEY, JSON.stringify({ templates: intentTemplates }));
  } catch { /* storage unavailable */ }
}

function templateId(): string {
  return `tpl-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
}

async function saveIntentAsTemplate(intent: IntentDraft): Promise<void> {
  if (!intent.assets.length) {
    opts.onToast("Add at least one asset before saving a template.", "error");
    return;
  }
  const result = await showModal({
    title: `Save "${intent.name}" as template`,
    description: "Captures assets, connections, and settings so you can drop it into any partition.",
    fields: [
      { name: "name", label: "Template name", type: "text", value: intent.name },
      { name: "description", label: "Description", type: "text", value: "" },
    ],
    actions: [{ key: "cancel", label: "Cancel" }, { key: "ok", label: "Save template", primary: true }],
  });
  if (!result || result.action !== "ok") return;
  const name = String(result.values.name ?? "").trim() || intent.name;
  const template: IntentTemplate = {
    id: templateId(),
    name,
    description: String(result.values.description ?? "").trim(),
    savedAt: Date.now(),
    intentType: intent.intentType,
    targetPusher: intent.targetPusher,
    target: JSON.parse(JSON.stringify(intent.target ?? {})),
    locked: intent.locked,
    hints: JSON.parse(JSON.stringify(intent.hints ?? [])),
    assets: JSON.parse(JSON.stringify(intent.assets)),
    sourceIntent: intent.name,
  };
  intentTemplates.unshift(template);
  persistTemplates();
  renderBuilder();
  opts.onToast(`Template "${name}" saved`, "success");
}

function deleteTemplate(id: string): void {
  const template = intentTemplates.find((t) => t.id === id);
  if (!template) return;
  confirmAction("Delete template", `Delete template "${template.name}"?`, "Delete", true).then((ok) => {
    if (!ok) return;
    intentTemplates = intentTemplates.filter((t) => t.id !== id);
    persistTemplates();
    renderBuilder();
    opts.onToast(`Template "${template.name}" deleted`, "success");
  }).catch(opts.onError);
}

function instantiateTemplate(id: string): void {
  const template = intentTemplates.find((t) => t.id === id);
  if (!template) return;
  if (!draft) startNewDraft();
  if (!draft) return;
  pushUndo();
  const name = uniqueName(slugify(template.name), draft.intents.map((i) => i.name));
  const pusher = template.targetPusher || draft.partition.spec.defaults?.targetPusher || draft.intents[0]?.targetPusher || registryPushers[0]?.name || "";
  const target = Object.keys(template.target ?? {}).length ? JSON.parse(JSON.stringify(template.target)) : defaultPlacementFor(pusher);
  let assets = JSON.parse(JSON.stringify(template.assets));
  if (template.sourceIntent && template.sourceIntent !== name) {
    assets = assets.map((asset: AssetSpec) => asset.properties
      ? { ...asset, properties: rewriteOutputIntentRefs(asset.properties, template.sourceIntent!, name) }
      : asset);
  }
  const intent: IntentDraft = {
    uid: name,
    name,
    isNew: true,
    dirty: true,
    expectedVersionID: "",
    intentType: template.intentType || "standard",
    targetPusher: pusher,
    target,
    joins: [],
    locked: false,
    hints: JSON.parse(JSON.stringify(template.hints ?? [])),
    assets,
    positions: {},
  };
  autoLayoutIntent(intent);
  draft.intents.push(intent);
  draft.selection = { kind: "intent", intentUid: intent.uid };
  markDirty(intent);
  renderBuilder();
  opts.onToast(`Intent "${name}" added from template "${template.name}"`, "success");
}

function renameIntent(uid: string, rawName: string): void {
  const intent = draft?.intents.find((i) => i.uid === uid);
  const next = rawName.trim();
  if (!intent || next === intent.name) return;
  if (!NAME_RE.test(next)) {
    opts.onToast("Intent names must start with a letter and use lowercase letters, digits, and dashes.", "error");
    renderBuilder();
    return;
  }
  if (draft!.intents.some((i) => i.uid !== uid && i.name === next)) {
    opts.onToast(`Intent "${next}" already exists.`, "error");
    renderBuilder();
    return;
  }
  pushUndo();
  const previous = intent.name;
  intent.name = next;
  for (const other of draft!.intents) {
    other.joins = other.joins.map((j) => j === previous ? next : j);
    for (const asset of other.assets) {
      if (asset.properties) asset.properties = rewriteOutputIntentRefs(asset.properties, previous, next);
    }
  }
  markDirty(intent);
  renderBuilder();
}

function deleteIntent(uid: string): void {
  const intent = draft?.intents.find((i) => i.uid === uid);
  if (!intent || !draft) return;
  confirmAction("Delete intent", `Delete "${intent.name}" and its ${intent.assets.length} asset${intent.assets.length === 1 ? "" : "s"}?`, "Delete", true).then((ok) => {
    if (!ok) return;
    pushUndo();
    draft!.intents = draft!.intents.filter((i) => i.uid !== uid);
    for (const other of draft!.intents) other.joins = other.joins.filter((j) => j !== intent.name);
    draft!.selection = draft!.intents.length ? { kind: "intent", intentUid: draft!.intents[0].uid } : { kind: "partition" };
    markDirty(intent);
    renderBuilder();
  }).catch(opts.onError);
}

// ── Pusher assignment & migration ────────────────────
function pusherKind(name: string | undefined): string {
  const lower = String(name ?? "").toLowerCase();
  if (lower.includes("aws")) return "aws";
  if (lower.includes("k8s") || lower.includes("kubernetes")) return "kubernetes";
  if (lower.includes("docker")) return "docker";
  if (lower.includes("local")) return "local";
  return lower ? "other" : "other";
}

function pusherOptions(selected: string): string {
  const names = new Set<string>(registryPushers.map((p) => p.name));
  if (selected) names.add(selected);
  const sorted = [...names].sort((a, b) => a.localeCompare(b));
  return `<option value=""${selected ? "" : " selected"}>— select a pusher —</option>` +
    sorted.map((name) => {
      const info = registryPushers.find((p) => p.name === name);
      const kind = info?.kind ?? pusherKind(name);
      return `<option value="${escapeAttr(name)}"${selected === name ? " selected" : ""}>${escapeHtml(name)} · ${escapeHtml(KIND_LABELS[kind] ?? kind)}</option>`;
    }).join("");
}

function defaultPlacementFor(pusherName: string): Placement {
  const kind = pusherKind(pusherName);
  const info = registryPushers.find((p) => p.name === pusherName);
  if (kind === "aws") return { account: info?.account ?? "", region: info?.region ?? "" };
  if (kind === "kubernetes" || kind === "docker") return { cluster: deriveCluster(pusherName), namespace: "" };
  if (kind === "local") return { cluster: "local" };
  return {};
}

function deriveCluster(pusherName: string): string {
  const match = /^(?:k8s|kubernetes|docker|local)[-_:](.+)$/i.exec(pusherName);
  return match ? match[1] : "";
}

async function pickPusher(intentUid: string): Promise<void> {
  const intent = draft?.intents.find((i) => i.uid === intentUid);
  if (!intent) return;
  const options = registryPushers.map((p) => ({
    value: p.name,
    label: `${p.name} · ${KIND_LABELS[p.kind] ?? p.kind}${p.account ? ` · ${p.account}` : ""}${p.region ? ` · ${p.region}` : ""}${p.description ? ` · ${p.description}` : ""}`,
  }));
  if (!options.length) {
    opts.onToast("No pushers registered. Add one under Pushers & Repos.", "error");
    return;
  }
  const result = await showModal({
    title: `Assign "${intent.name}" to a pusher`,
    description: "Assets move with the intent. Placement defaults are filled from the pusher.",
    fields: [{ name: "pusher", label: "Pusher", type: "select", options, value: intent.targetPusher }],
    actions: [{ key: "cancel", label: "Cancel" }, { key: "ok", label: "Assign", primary: true }],
  });
  if (!result || result.action !== "ok") return;
  migrateIntent(intent, String(result.values.pusher ?? ""));
}

async function migrateIntent(intent: IntentDraft, targetPusher: string): Promise<void> {
  if (!draft || !targetPusher || targetPusher === intent.targetPusher) return;
  const newKind = pusherKind(targetPusher);
  const info = registryPushers.find((p) => p.name === targetPusher);
  const nextTarget: Placement = { ...intent.target };
  if (newKind === "aws") {
    nextTarget.account = nextTarget.account || info?.account || "";
    nextTarget.region = nextTarget.region || info?.region || "";
    delete nextTarget.cluster;
  } else if (newKind === "kubernetes" || newKind === "docker") {
    nextTarget.cluster = nextTarget.cluster || deriveCluster(targetPusher);
    delete nextTarget.region;
    delete nextTarget.account;
  } else if (newKind === "local") {
    nextTarget.cluster = nextTarget.cluster || "local";
    delete nextTarget.region;
    delete nextTarget.account;
  }
  const incompatible = intent.assets.filter((a) => !(PUSHER_SUPPORT[newKind]?.includes(a.type) ?? true));
  let removeIncompatible = false;
  if (incompatible.length) {
    const result = await showModal({
      title: `Move to ${KIND_LABELS[newKind] ?? newKind}?`,
      description: `${incompatible.length} asset${incompatible.length === 1 ? " is" : "s are"} not supported by "${targetPusher}". You can keep them (they will fail at apply) or remove them now.`,
      fields: [{ name: "remove", label: `Remove unsupported assets (${incompatible.map((a) => a.name).join(", ")})`, type: "checkbox", value: false }],
      actions: [{ key: "cancel", label: "Cancel" }, { key: "ok", label: "Move intent", primary: true }],
    });
    if (!result || result.action !== "ok") {
      renderBuilder();
      return;
    }
    removeIncompatible = result.values.remove === true;
  }
  pushUndo();
  if (removeIncompatible) {
    const names = new Set(incompatible.map((a) => a.name));
    intent.assets = intent.assets.filter((a) => !names.has(a.name));
    for (const name of names) delete intent.positions[name];
  }
  intent.targetPusher = targetPusher;
  intent.target = nextTarget;
  intent.dirty = true;
  markDirty(intent);
  renderBuilder();
  opts.onToast(`"${intent.name}" assigned to ${targetPusher}`, "success");
}

// ── Problems & validation ────────────────────────────
function assetSupportsRef(intent: IntentDraft, connector: ConnectorDef, ref: string): boolean {
  const provider = intent.assets.find((a) => a.name === ref);
  return !!provider && connectorAccepts(connector, provider.type);
}

function assetIssues(intent: IntentDraft, asset: AssetSpec): Issue[] {
  const issues: Issue[] = [];
  const properties = asset.properties ?? {};
  const required = (condition: any, message: string) => {
    if (!condition) issues.push({ severity: "error", message, intentUid: intent.uid, asset: asset.name, source: "client" });
  };
  switch (asset.type) {
    case "Compute": {
      required(str(properties.image), "Image is required.");
      const observers = intent.assets.filter((candidate) => candidate.type === "Observability" && ensureArray(candidate.properties?.targets).includes(asset.name));
      if (observers.length > 1) {
        issues.push({ severity: "error", message: `Multiple Observability assets target ${asset.name}; only one sidecar per compute is supported.`, intentUid: intent.uid, asset: asset.name, source: "client" });
      }
      break;
    }
    case "Database":
    case "SQLDatabase":
      required(str(properties.engine), "Engine is required.");
      break;
    case "ObjectStore":
      required(str(properties.engine), "Engine is required.");
      break;
    case "Observability":
      required(str(properties.config) || str(properties.endpoint) || ensureArray(properties.exporters).length > 0, "Set a config, endpoint, or exporter.");
      if (pusherKind(intent.targetPusher) === "aws" && !ensureArray(properties.targets).length) {
        issues.push({ severity: "warning", message: "AWS observability runs as a sidecar; connect at least one Compute target.", intentUid: intent.uid, asset: asset.name, source: "client" });
      }
      break;
    case "TraefikRoute":
      required(str(properties.hostname), "Hostname is required.");
      required(str(properties.target), "Connect a Compute target.");
      break;
    case "Secret": {
      const hasValue = str(properties.value);
      const hasRef = str(properties.secretRef);
      if (hasValue && hasRef) issues.push({ severity: "error", message: "Set either value or secretRef, not both.", intentUid: intent.uid, asset: asset.name, source: "client" });
      else required(hasValue || hasRef, "Set value or secretRef.");
      break;
    }
    case "ImageBuild":
      required(str(properties.repository), "Repository is required.");
      required(str(properties.buildContext) || str(properties.sourceImage), "Set buildContext or sourceImage.");
      if (str(properties.buildContext)) required(str(properties.dockerfile), "Dockerfile is required with buildContext.");
      break;
    case "CDKStack":
      required(str(asset.payload?.aws), "payload.aws path is required.");
      break;
    case "K8sResource":
      required(str(asset.payload?.k8s) || str(asset.payload?.kubernetes), "payload.k8s path is required.");
      break;
    case "Config":
      required(str(properties.content) || Object.keys(properties.data ?? {}).length > 0 || Object.keys(asset.payload ?? {}).length > 0, "Set content, data, or payload.");
      break;
    case "LoadBalancer":
      required(ensureArray(properties.targets).length > 0 || str(properties.existingName), "Connect at least one Compute target.");
      required(ensureArray(properties.listeners).length > 0, "Add at least one listener.");
      break;
  }
  for (const connector of connectorsFor(asset.type)) {
    for (const ref of connectorRefs(asset, connector)) {
      const provider = intent.assets.find((a) => a.name === ref);
      if (!provider) {
        issues.push({ severity: "error", message: `${connector.label} references unknown asset "${ref}".`, intentUid: intent.uid, asset: asset.name, source: "client" });
      } else if (!connectorAccepts(connector, provider.type)) {
        issues.push({ severity: "error", message: `${connector.label} must reference a ${refTypesLabel(connector)}, not ${provider.type}.`, intentUid: intent.uid, asset: asset.name, source: "client" });
      }
    }
  }
  return issues;
}

function intentIncompatible(intent: IntentDraft): AssetSpec[] {
  const kind = pusherKind(intent.targetPusher);
  const support = PUSHER_SUPPORT[kind] ?? PUSHER_SUPPORT.other;
  return intent.assets.filter((a) => !support.includes(a.type));
}

function incompatibleCount(intent: IntentDraft): number {
  return intentIncompatible(intent).length;
}

function clientIssues(): Issue[] {
  const issues: Issue[] = [];
  if (!draft) return issues;
  const partitionName = draft.partition.metadata.name ?? "";
  if (!partitionName) issues.push({ severity: "error", message: "Partition name is required.", source: "client" });
  else if (!NAME_RE.test(partitionName)) issues.push({ severity: "error", message: "Partition name must start with a letter and use lowercase letters, digits, and dashes.", source: "client" });
  if (!draft.intents.length) issues.push({ severity: "error", message: "Add at least one intent.", source: "client" });
  const intentNames = new Set<string>();
  for (const intent of draft.intents) {
    if (!NAME_RE.test(intent.name)) issues.push({ severity: "error", message: `Intent name "${intent.name}" must start with a letter and use lowercase letters, digits, and dashes.`, source: "client" });
    if (intentNames.has(intent.name)) issues.push({ severity: "error", message: `Duplicate intent "${intent.name}".`, source: "client" });
    intentNames.add(intent.name);
    if (!intent.targetPusher) issues.push({ severity: "error", message: "Assign a target pusher.", intentUid: intent.uid, intent: intent.name, source: "client" });
    const kind = pusherKind(intent.targetPusher);
    if (kind === "aws") {
      if (!str(intent.target.region)) issues.push({ severity: "error", message: "AWS placement needs a region.", intentUid: intent.uid, intent: intent.name, source: "client" });
      if (!str(intent.target.account)) issues.push({ severity: "error", message: "AWS placement needs an account.", intentUid: intent.uid, intent: intent.name, source: "client" });
    } else if (kind === "kubernetes" || kind === "docker") {
      if (!str(intent.target.cluster)) issues.push({ severity: "error", message: `${KIND_LABELS[kind]} placement needs a cluster.`, intentUid: intent.uid, intent: intent.name, source: "client" });
    } else if (!str(intent.target.cluster) && !str(intent.target.namespace) && !str(intent.target.region) && !str(intent.target.account)) {
      issues.push({ severity: "error", message: "Set a target placement.", intentUid: intent.uid, intent: intent.name, source: "client" });
    }
    if (intent.intentType === "observed" && kind !== "aws") {
      issues.push({ severity: "error", message: "Observed intents require an AWS pusher.", intentUid: intent.uid, intent: intent.name, source: "client" });
    }
    for (const join of intent.joins) {
      if (!draft.intents.some((other) => other.name === join)) {
        issues.push({ severity: "error", message: `Waits for unknown intent "${join}".`, intentUid: intent.uid, intent: intent.name, source: "client" });
      }
    }
    const assetNames = new Set<string>();
    for (const asset of intent.assets) {
      if (!NAME_RE.test(asset.name)) issues.push({ severity: "error", message: `Asset name "${asset.name}" is invalid.`, intentUid: intent.uid, intent: intent.name, asset: asset.name, source: "client" });
      if (assetNames.has(asset.name)) issues.push({ severity: "error", message: `Duplicate asset "${asset.name}".`, intentUid: intent.uid, intent: intent.name, asset: asset.name, source: "client" });
      assetNames.add(asset.name);
      issues.push(...assetIssues(intent, asset));
      const kindSupport = PUSHER_SUPPORT[kind] ?? PUSHER_SUPPORT.other;
      if (!kindSupport.includes(asset.type)) {
        issues.push({ severity: "warning", message: `${asset.type} is not supported by ${KIND_LABELS[kind] ?? kind} pushers.`, intentUid: intent.uid, intent: intent.name, asset: asset.name, source: "client" });
      }
    }
  }
  if (hasJoinCycle(draft.intents)) issues.push({ severity: "error", message: "Intent apply order contains a cycle.", source: "client" });
  return issues;
}

function hasJoinCycle(intents: IntentDraft[]): boolean {
  const byName = new Map(intents.map((i) => [i.name, i]));
  const visiting = new Set<string>();
  const visited = new Set<string>();
  const visit = (name: string): boolean => {
    if (visited.has(name)) return false;
    if (visiting.has(name)) return true;
    visiting.add(name);
    const intent = byName.get(name);
    for (const join of intent?.joins ?? []) {
      if (visit(join)) return true;
    }
    visiting.delete(name);
    visited.add(name);
    return false;
  };
  return intents.some((i) => visit(i.name));
}

function allIssues(): Issue[] {
  return [...clientIssues(), ...serverIssues];
}

function scheduleServerValidation(): void {
  if (validationTimer !== undefined) window.clearTimeout(validationTimer);
  validationTimer = window.setTimeout(() => {
    runServerValidation().catch(() => { /* keep client validation */ });
  }, 900);
}

async function runServerValidation(): Promise<void> {
  if (!draft) return;
  const name = draft.partition.metadata.name || draft.editingPartition;
  if (!name || !NAME_RE.test(name)) return;
  const payload = buildSaveRequest();
  const response = await fetchJSON(`/api/partitions/${encodeURIComponent(name)}/validate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  serverIssues = (response?.errors ?? []).map((issue: any) => ({
    severity: "error" as const,
    message: issue.message,
    intent: issue.intent,
    asset: issue.asset,
    source: "server" as const,
  }));
  renderProblems();
}

function renderProblems(): void {
  const container = document.getElementById("builderProblems");
  if (!container || !draft) return;
  const issues = allIssues();
  if (!issues.length) {
    container.classList.add("hidden");
    container.innerHTML = "";
    return;
  }
  const errors = issues.filter((i) => i.severity === "error").length;
  const warnings = issues.length - errors;
  container.classList.remove("hidden");
  container.innerHTML = `
    <div class="builder-problems-head">
      <b>${errors ? `${errors} problem${errors === 1 ? "" : "s"}` : ""}${errors && warnings ? " · " : ""}${warnings ? `${warnings} warning${warnings === 1 ? "" : "s"}` : ""}</b>
      <button class="builder-problems-close" id="builderProblemsClose" aria-label="Hide problems">×</button>
    </div>
    <div class="builder-problems-list">
      ${issues.map((issue, index) => `
        <button class="builder-problem ${issue.severity} ${issue.source}" data-problem-index="${index}">
          <span class="builder-problem-dot"></span>
          <span class="builder-problem-text">${issue.intent ? `<b>${escapeHtml(issue.intent)}</b>${issue.asset ? ` / ${escapeHtml(issue.asset)}` : ""} — ` : ""}${escapeHtml(issue.message)}</span>
        </button>`).join("")}
    </div>`;
  document.getElementById("builderProblemsClose")?.addEventListener("click", () => container.classList.add("hidden"));
  container.querySelectorAll<HTMLElement>("[data-problem-index]").forEach((button) => {
    button.addEventListener("click", () => focusIssue(issues[Number(button.dataset.problemIndex)]));
  });
}

function focusIssue(issue: Issue): void {
  if (!draft) return;
  if (issue.intent) {
    const intent = draft.intents.find((i) => i.name === issue.intent);
    if (intent) {
      if (issue.asset) draft.selection = { kind: "asset", intentUid: intent.uid, assetName: issue.asset };
      else draft.selection = { kind: "intent", intentUid: intent.uid };
    }
  } else {
    draft.selection = { kind: "partition" };
  }
  renderBuilder();
  if (issue.asset) {
    const node = document.querySelector<HTMLElement>(`.builder-node[data-asset="${cssEscape(issue.asset)}"]`);
    node?.classList.add("attention");
    setTimeout(() => node?.classList.remove("attention"), 1600);
  }
}

// ── Save ─────────────────────────────────────────────
async function save(): Promise<void> {
  if (!draft) return;
  const clientErrors = clientIssues().filter((i) => i.severity === "error");
  if (clientErrors.length) {
    opts.onToast(`Fix ${clientErrors.length} problem${clientErrors.length === 1 ? "" : "s"} before saving.`, "error");
    focusIssue(clientErrors[0]);
    return;
  }
  const name = draft.partition.metadata.name.trim();
  if (!name) {
    opts.onToast("Partition name is required.", "error");
    return;
  }
  const saveButton = document.getElementById("builderSave") as HTMLButtonElement | null;
  if (saveButton) saveButton.disabled = true;
  try {
    const payload = buildSaveRequest();
    const response = await fetchJSON(`/api/partitions/${encodeURIComponent(name)}/bundle`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    draft.partitionVersionID = response?.partitionVersionID ?? draft.partitionVersionID;
    const versionIDs: Record<string, string> = response?.intentVersionIDs ?? {};
    for (const intent of draft.intents) {
      if (versionIDs[intent.name]) intent.expectedVersionID = versionIDs[intent.name];
      intent.dirty = false;
      intent.isNew = false;
    }
    draft.isNew = false;
    draft.dirty = false;
    draft.editingPartition = name;
    const removed: string[] = response?.removedIntents ?? [];
    try { localStorage.removeItem(STORAGE_PREFIX + name); localStorage.removeItem(STORAGE_PREFIX + "__new__"); } catch { /* ignore */ }
    storedDraftAvailable = false;
    serverIssues = [];
    opts.onOpenPartition(name);
    renderBuilder();
    opts.onToast(`Saved ${draft.intents.length} intent${draft.intents.length === 1 ? "" : "s"}${removed.length ? `, removed ${removed.length}` : ""}`, "success");
  } catch (error: any) {
    opts.onToast(error?.message ?? "Save failed", "error");
    serverIssues = [{ severity: "error", message: error?.message ?? "Save failed", source: "server" }];
    renderProblems();
  } finally {
    if (saveButton) saveButton.disabled = false;
  }
}

// ── Modals ───────────────────────────────────────────
function showModal(options: ModalOptions): Promise<{ action: string; values: Record<string, string | boolean> } | null> {
  return new Promise((resolve) => {
    const overlay = document.createElement("div");
    overlay.className = "builder-modal-overlay";
    const fieldsHtml = (options.fields ?? []).map((field) => {
      if (field.type === "select") {
        return `
          <label class="builder-field"><span>${escapeHtml(field.label)}</span>
            <select class="search-input" data-modal-field="${escapeAttr(field.name)}">
              ${(field.options ?? []).map((option) => `<option value="${escapeAttr(option.value)}"${field.value === option.value ? " selected" : ""}>${escapeHtml(option.label)}</option>`).join("")}
            </select>
            ${field.description ? `<small>${escapeHtml(field.description)}</small>` : ""}
          </label>`;
      }
      if (field.type === "checkbox") {
        return `<label class="builder-field checkbox"><input type="checkbox" data-modal-field="${escapeAttr(field.name)}"${field.value === true || field.value === "true" ? " checked" : ""}> <span>${escapeHtml(field.label)}</span></label>`;
      }
      return `<label class="builder-field"><span>${escapeHtml(field.label)}</span><input class="search-input" data-modal-field="${escapeAttr(field.name)}" value="${escapeHtml(String(field.value ?? ""))}"></label>`;
    }).join("");
    overlay.innerHTML = `
      <div class="builder-modal" role="dialog" aria-modal="true">
        <h3>${escapeHtml(options.title)}</h3>
        ${options.description ? `<p>${escapeHtml(options.description)}</p>` : ""}
        ${options.bodyHtml ?? ""}
        ${fieldsHtml}
        <div class="builder-modal-actions">
          ${options.actions.map((action) => `<button class="btn-${action.primary ? "primary" : action.danger ? "danger" : "ghost"}" data-modal-action="${escapeAttr(action.key)}">${escapeHtml(action.label)}</button>`).join("")}
        </div>
      </div>`;
    const close = (result: { action: string; values: Record<string, string | boolean> } | null) => {
      overlay.remove();
      document.removeEventListener("keydown", onKey);
      resolve(result);
    };
    const collect = () => {
      const values: Record<string, string | boolean> = {};
      overlay.querySelectorAll<HTMLElement>("[data-modal-field]").forEach((el) => {
        const name = el.dataset.modalField!;
        if (el instanceof HTMLInputElement && el.type === "checkbox") values[name] = el.checked;
        else values[name] = (el as HTMLInputElement | HTMLSelectElement).value;
      });
      return values;
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close(null);
    };
    overlay.querySelectorAll<HTMLElement>("[data-modal-action]").forEach((button) => {
      button.addEventListener("click", () => close({ action: button.dataset.modalAction!, values: collect() }));
    });
    overlay.addEventListener("click", (event) => {
      if (event.target === overlay) close(null);
    });
    document.addEventListener("keydown", onKey);
    document.body.appendChild(overlay);
    const firstField = overlay.querySelector<HTMLElement>("[data-modal-field]");
    firstField?.focus();
  });
}

async function confirmAction(title: string, description: string, confirmLabel: string, danger: boolean): Promise<boolean> {
  const result = await showModal({
    title,
    description,
    actions: [{ key: "cancel", label: "Cancel" }, { key: "ok", label: confirmLabel, primary: !danger, danger }],
  });
  return result?.action === "ok";
}

// ── Helpers ──────────────────────────────────────────
function ensureArray(value: any): any[] {
  if (Array.isArray(value)) return value;
  if (value === undefined || value === null || value === "") return [];
  return [value];
}

function getProp(obj: any, path: string): any {
  return path.split(".").reduce((acc, key) => (acc == null ? undefined : acc[key]), obj);
}

function setProp(obj: any, path: string, value: any): void {
  const parts = path.split(".");
  let cursor = obj;
  for (let i = 0; i < parts.length - 1; i++) {
    if (typeof cursor[parts[i]] !== "object" || cursor[parts[i]] === null) cursor[parts[i]] = {};
    cursor = cursor[parts[i]];
  }
  if (value === undefined) delete cursor[parts[parts.length - 1]];
  else cursor[parts[parts.length - 1]] = value;
}

function str(value: any): string {
  return value === undefined || value === null ? "" : String(value).trim();
}

function slugify(value: string): string {
  const slug = value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  return slug.replace(/^[^a-z]+/, "") || "asset";
}

function uniqueName(base: string, existing: string[]): string {
  let name = base;
  let index = 2;
  while (existing.includes(name)) {
    name = `${base}-${index++}`;
  }
  return name;
}

function cssEscape(value: string): string {
  return value.replace(/["\\]/g, "\\$&");
}

function bindSimpleField(id: string, apply: (value: string) => void): void {
  const el = document.getElementById(id) as HTMLInputElement | HTMLSelectElement | null;
  if (!el) return;
  el.addEventListener("change", () => {
    apply(el.value);
    persistDraft();
    renderTopbar();
    scheduleServerValidation();
  });
}

// ── Global wiring ────────────────────────────────────
export function wireBuilderPanel(): void {
  document.getElementById("builderSave")?.addEventListener("click", () => save().catch(opts.onError));
  document.getElementById("builderAutoLayout")?.addEventListener("click", () => autoLayoutAll());
  document.getElementById("builderDiscard")?.addEventListener("click", () => discardDraft());
  document.getElementById("builderRestoreDraft")?.addEventListener("click", () => restoreStoredDraft());
  document.getElementById("builderPaletteSearch")?.addEventListener("input", () => renderPalette());
  document.getElementById("builderPartitionLabel")?.addEventListener("click", () => {
    if (!draft) return;
    draft.selection = { kind: "partition" };
    renderBuilder();
  });
  const panel = document.getElementById("builderPanel");
  panel?.addEventListener("keydown", (event) => {
    const keyboard = event as KeyboardEvent;
    const meta = keyboard.metaKey || keyboard.ctrlKey;
    if (meta && keyboard.key.toLowerCase() === "z") {
      keyboard.preventDefault();
      if (keyboard.shiftKey) redo();
      else undo();
    } else if (meta && keyboard.key.toLowerCase() === "s") {
      keyboard.preventDefault();
      save().catch(opts.onError);
    } else if (keyboard.key === "Escape") {
      if (armedConnect || connectState) {
        armedConnect = null;
        connectState = null;
        clearConnectHighlight();
        renderBuilder();
      }
    }
  });
}

function armConnect(intentUid: string, assetName: string, connectorKey: string): void {
  const intent = draft?.intents.find((i) => i.uid === intentUid);
  const asset = intent?.assets.find((a) => a.name === assetName);
  const connector = asset ? connectorsFor(asset.type).find((c) => c.key === connectorKey) : undefined;
  if (!intent || !asset || !connector) return;
  armedConnect = { intentUid, assetName, connector };
  opts.onToast(`Click a ${refTypesLabel(connector)} to connect (Esc to cancel)`, "info");
  highlightTargets(intentUid, assetName, connector);
}
