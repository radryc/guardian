import { escapeHtml, escapeAttr, truncate } from "./utils";

export interface TopologyOptions {
  canvas: HTMLElement;
  topology: any;
  zoom: number;
  savedPositions: Record<string, { x: number; y: number }>;
  selectedNodeId: string;
  filters: Record<string, boolean>;
  showInfrastructure: boolean;
  onSelectNode: (nodeId: string, positions: Record<string, { x: number; y: number }>) => void;
  onDragNode:   (nodeId: string, positions: Record<string, { x: number; y: number }>) => void;
}

// ── Visual constants ──────────────────────────────────
const NODE_COLORS = {
  partition: "#F0E442",
  intent:    "#CC79A7",
  runtime:   "#0072B2",
  config:    "#009E73",
  storage:   "#56B4E9",
  traffic:   "#D55E00",
  muted:     "#8B949E",
};

const STATUS_FILL: Record<string, string> = {
  healthy:          "#00C369",
  attention:        "#FCB519",
  failing:          "#EE5F54",
  pending:          "#00ADE4",
  drifted:          "#FCB519",
  "drifted-locked": "#F5A623",
  neutral:          "#566778",
};

// ── Node sizing (matches old app.ts) ─────────────────
function nw(node: any): number {
  if (node.kind === "partition") return 200;
  if (node.kind === "intent")   return 230;
  return 220;
}
function nh(node: any): number {
  return node.kind === "partition" ? 76 : 72;
}
function resourceFamily(node: any): string {
  const cfn = String(node.meta?.cfnType ?? "");
  if (cfn.startsWith("AWS::ECS::")) return "ecs";
  if (cfn.startsWith("AWS::ElasticLoadBalancingV2::")) return "elb";
  if (cfn.startsWith("AWS::Lambda::")) return "lambda";
  if (cfn.startsWith("AWS::SQS::") || cfn.startsWith("AWS::SNS::") || cfn.startsWith("AWS::Events::")) return "messaging";
  if (cfn.startsWith("AWS::DynamoDB::") || cfn.startsWith("AWS::S3::")) return "storage";
  if (cfn.startsWith("AWS::Logs::")) return "logs";
  if (cfn.startsWith("AWS::EC2::") || cfn.startsWith("AWS::IAM::")) return "infra";
  return "";
}
function nAccent(node: any): string {
  if (node.kind === "partition") return NODE_COLORS.partition;
  if (node.kind === "intent")   return NODE_COLORS.intent;
  const family = resourceFamily(node);
  if (family === "ecs" || family === "lambda") return NODE_COLORS.runtime;
  if (family === "elb" || family === "messaging") return NODE_COLORS.traffic;
  if (family === "storage") return NODE_COLORS.storage;
  if (family === "logs") return NODE_COLORS.config;
  if (family === "infra") return NODE_COLORS.muted;
  const m: Record<string, string> = {
    Compute: NODE_COLORS.runtime,  Volume: NODE_COLORS.storage,
    Config:  NODE_COLORS.config,   ObjectStore: NODE_COLORS.storage,
    Database: NODE_COLORS.traffic, SQLDatabase: NODE_COLORS.traffic,
    LoadBalancer: NODE_COLORS.traffic, Observability: NODE_COLORS.config,
  };
  return m[node.assetType ?? ""] ?? NODE_COLORS.muted;
}
function nIcon(node: any): string {
  if (node.kind === "partition") return "◫";
  if (node.kind === "intent")   return "⊞";
  const family = resourceFamily(node);
  if (family === "ecs") return "▣";
  if (family === "elb") return "⇄";
  if (family === "lambda") return "λ";
  if (family === "messaging") return "✉";
  if (family === "storage") return "⛁";
  if (family === "logs") return "≡";
  if (family === "infra") return "⬡";
  const m: Record<string, string> = {
    Compute: "⧖", Volume: "⊠", Config: "≡", ObjectStore: "⬜",
    Database: "⫿", SQLDatabase: "⫿", LoadBalancer: "⊷", Observability: "◎",
  };
  return m[node.assetType ?? ""] ?? "⬡";
}
function nSubtitle(node: any): string {
  if (node.kind === "partition")
    return `${node.meta?.reconciliation ?? "manual"} reconcile · ${node.meta?.deletionPolicy ?? "orphan"} delete`;
  if (node.kind === "intent")
    return node.meta?.target ?? node.displayStatus ?? "Intent";
  const cfn = String(node.meta?.cfnType ?? "");
  if (cfn) {
    const short = cfn.replace(/^AWS::/, "").replace(/::/g, " / ");
    const physical = String(node.meta?.physicalId ?? "");
    return physical ? `${short} · ${truncate(physical, 38)}` : short;
  }
  return `${node.assetType ?? "Asset"} · ${node.displayStatus ?? "Asset"}`;
}
function nStatusFill(node: any): string {
  return STATUS_FILL[node.health ?? node.status ?? "neutral"] ?? STATUS_FILL.neutral;
}

// ── Hierarchical layout (port of app.ts computeTopologyPositions) ──
interface TopologyGroup {
  id: string;
  label: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

function computePositions(
  nodes: any[],
  savedPositions: Record<string, { x: number; y: number }>
): { positions: Record<string, { x: number; y: number }>; groups: TopologyGroup[] } {
  const partition = nodes.find((n) => n.kind === "partition");
  const intents   = nodes
    .filter((n) => n.kind === "intent")
    .sort((a, b) => (Number(a.level) - Number(b.level)) || a.label.localeCompare(b.label));
  const assets    = nodes
    .filter((n) => n.kind === "asset")
    .sort((a, b) =>
      (a.parentID ?? "").localeCompare(b.parentID ?? "") ||
      (Number(a.level) - Number(b.level)) ||
      a.label.localeCompare(b.label)
    );

  const positions: Record<string, { x: number; y: number }> = {};
  const groups: TopologyGroup[] = [];
  const assetByID = new Map<string, any>(assets.map((asset) => [asset.id, asset]));
  const childrenByParent = new Map<string, any[]>();
  for (const asset of assets) {
    const parent = asset.parentID || "";
    if (!childrenByParent.has(parent)) childrenByParent.set(parent, []);
    childrenByParent.get(parent)!.push(asset);
  }

  // Group intents by DAG level
  const intentsByLevel = new Map<number, any[]>();
  intents.forEach((intent) => {
    const level = Number(intent.level ?? 1);
    if (!intentsByLevel.has(level)) intentsByLevel.set(level, []);
    intentsByLevel.get(level)!.push(intent);
  });

  // Place intents
  let cursorY = 70;
  [...intentsByLevel.keys()].sort((a, b) => a - b).forEach((level) => {
    const group = intentsByLevel.get(level)!;
    group.forEach((intent) => {
      positions[intent.id] = { x: 260 + (level - 1) * 320, y: cursorY };
      cursorY += 154;
    });
    cursorY += 24;
  });

  // Partition centered vertically on the intent column
  if (partition) {
    const ips = intents.map((i) => positions[i.id]).filter(Boolean);
    const minY = ips.length ? Math.min(...ips.map((p) => p.y)) : 90;
    const maxY = ips.length ? Math.max(...ips.map((p) => p.y)) : 90;
    positions[partition.id] = { x: 40, y: Math.round((minY + maxY) / 2) };
  }

  // Recursively place an asset and its contained assets. Children sit inside a
  // group box to the right of their parent, so ECS Service/Task and similar
  // containment is visible rather than flattened.
  function layoutSubtree(node: any, x: number, y: number): { right: number; bottom: number } {
    positions[node.id] = { x, y };
    const children = childrenByParent.get(node.id) ?? [];
    if (children.length === 0) {
      return { right: x + nw(node), bottom: y + nh(node) };
    }
    let childY = y;
    let right = x + nw(node);
    for (const child of children) {
      const sub = layoutSubtree(child, x + 280, childY);
      right = Math.max(right, sub.right);
      childY = sub.bottom + 24;
    }
    const groupX = x - 14;
    const groupY = y - 30;
    const groupWidth = right - groupX + 14;
    const groupHeight = childY - groupY - 24 + 14;
    groups.push({
      id: node.id,
      label: node.label,
      x: groupX,
      y: groupY,
      width: groupWidth,
      height: groupHeight,
    });
    return { right: right + 14, bottom: childY - 24 + 14 };
  }

  intents.forEach((intent) => {
    const parentPos = positions[intent.id];
    if (!parentPos) return;
    const topLevel = (childrenByParent.get(intent.id) ?? [])
      .filter((asset) => !assetByID.has(asset.parentID) || asset.parentID === intent.id);
    let y = parentPos.y;
    for (const asset of topLevel) {
      const sub = layoutSubtree(asset, parentPos.x + 300, y);
      y = sub.bottom + 30;
    }
  });

  // Orphans: assets whose parent is missing still need a position.
  let orphanY = 70;
  for (const asset of assets) {
    if (positions[asset.id]) continue;
    const sub = layoutSubtree(asset, 900, orphanY);
    orphanY = sub.bottom + 30;
  }

  // Shift up so no node or group is above y=40
  const minY = Math.min(
    ...Object.values(positions).map((p) => p.y),
    ...groups.map((group) => group.y),
    40
  );
  if (minY < 40) {
    const shift = 40 - minY;
    Object.values(positions).forEach((p) => { p.y += shift; });
    groups.forEach((group) => { group.y += shift; });
  }

  // Apply user-dragged overrides
  Object.entries(savedPositions).forEach(([id, pos]) => {
    if (positions[id]) positions[id] = { x: pos.x, y: pos.y };
  });

  return { positions, groups };
}

// ── Cubic bezier between card nodes ──────────────────
function edgePath(
  from: { x: number; y: number }, to: { x: number; y: number },
  fromNode: any, toNode: any
): string {
  const sx = from.x + nw(fromNode),  sy = from.y + nh(fromNode) / 2;
  const ex = to.x,                   ey = to.y  + nh(toNode)   / 2;
  const delta = ex - sx;
  const dir   = delta >= 0 ? 1 : -1;
  const curve = Math.max(70, Math.abs(delta) / 2);
  return `M ${sx} ${sy} C ${sx + curve * dir} ${sy}, ${ex - curve * dir} ${ey}, ${ex} ${ey}`;
}

// ── Live edge redraw during drag ──────────────────────
function updateEdgePaths(
  svgEl: SVGSVGElement,
  positions: Record<string, { x: number; y: number }>,
  edges: any[],
  nodeMap: Map<string, any>
): void {
  svgEl.querySelectorAll<SVGPathElement>("path.topology-edge").forEach((el, i) => {
    const edge = edges[i];
    if (!edge) return;
    const from = positions[edge.from], to = positions[edge.to];
    const fn = nodeMap.get(edge.from), tn = nodeMap.get(edge.to);
    if (from && to && fn && tn) el.setAttribute("d", edgePath(from, to, fn, tn));
  });
}

// ── Main render ───────────────────────────────────────
export function renderTopology(opts: TopologyOptions): void {
  const { canvas, topology, zoom, savedPositions, selectedNodeId, filters, showInfrastructure, onSelectNode, onDragNode } = opts;

  if (!topology?.nodes?.length) {
    canvas.innerHTML = `<p class="empty-state" style="padding:24px">Select a partition to visualize its topology.</p>`;
    return;
  }

  let nodes: any[]  = topology.nodes;
  let edges: any[]  = topology.edges ?? [];
  if (!showInfrastructure) {
    const visible = new Set(
      nodes
        .filter((node) => node.kind !== "asset" || node.meta?.tier !== "infrastructure")
        .map((node) => node.id)
    );
    nodes = nodes.filter((node) => visible.has(node.id));
    edges = edges.filter((edge) => visible.has(edge.from) && visible.has(edge.to));
  }
  edges = edges.filter((edge: any) => (filters as any)[edge.kind] !== false);
  const nodeMap       = new Map<string, any>(nodes.map((n) => [n.id, n]));
  const { positions, groups } = computePositions(nodes, savedPositions);
  const selectedNode  = nodeMap.get(selectedNodeId) ?? nodes.find((n) => n.kind === "intent") ?? nodes[0];

  const maxX  = Math.max(...Object.values(positions).map((p) => p.x + 260), 400);
  const maxY  = Math.max(...Object.values(positions).map((p) => p.y + 100), 260);
  const width = maxX + 40, height = maxY + 40;

  const parts: string[] = [
    `<div class="topology-svg-frame">`,
    `<svg class="topology-svg" viewBox="0 0 ${width} ${height}" width="${Math.round(width * zoom)}" height="${Math.round(height * zoom)}" xmlns="http://www.w3.org/2000/svg">`,
    `<defs><filter id="ts"><feDropShadow dx="0" dy="6" stdDeviation="10" flood-color="rgba(0,0,0,0.28)"/></filter></defs>`,
  ];

  // Containment groups first, largest to smallest so nested groups sit on top.
  groups
    .slice()
    .sort((a, b) => b.width * b.height - a.width * a.height)
    .forEach((group) => {
      parts.push(`
        <g class="topology-group">
          <rect class="topology-group-box" x="${group.x}" y="${group.y}" width="${group.width}" height="${group.height}" rx="16" />
          <text class="topology-group-label" x="${group.x + 12}" y="${group.y + 18}">${escapeHtml(group.label)}</text>
        </g>
      `);
    });

  // Edges first (underneath nodes)
  edges.forEach((edge) => {
    const from = positions[edge.from], to = positions[edge.to];
    const fn = nodeMap.get(edge.from), tn = nodeMap.get(edge.to);
    if (!from || !to || !fn || !tn) return;
    parts.push(`<path class="topology-edge ${escapeAttr(edge.kind)}" d="${edgePath(from, to, fn, tn)}" />`);
    if (edge.label) {
      const sx = from.x + nw(fn), sy = from.y + nh(fn) / 2;
      const ex = to.x,            ey = to.y  + nh(tn) / 2;
      parts.push(`<text class="topology-edge-label" x="${(sx + ex) / 2}" y="${(sy + ey) / 2 - 10}">${escapeHtml(edge.label)}</text>`);
    }
  });

  // Nodes (card-based, matches old app.ts visual)
  nodes.forEach((node) => {
    const pos = positions[node.id];
    if (!pos) return;
    const accent   = nAccent(node);
    const selected = selectedNode?.id === node.id;
    const w = nw(node), h = nh(node);
    parts.push(`
      <g class="topology-node ${escapeAttr(node.kind)}${selected ? " selected" : ""}" data-node="${escapeAttr(node.id)}" transform="translate(${pos.x},${pos.y})">
        <rect class="topology-node-card" width="${w}" height="${h}" rx="12" filter="url(#ts)" />
        <rect class="topology-node-accent" width="4" height="${h}" rx="4" fill="${accent}" />
        <circle cx="18" cy="18" r="5.5" fill="${nStatusFill(node)}" />
        <text x="32" y="20" class="topology-node-title">${escapeHtml(`${nIcon(node)} ${node.label}`)}</text>
        <text x="32" y="38" class="topology-node-subtitle">${escapeHtml(nSubtitle(node))}</text>
        <text x="14" y="60" class="topology-node-description">${escapeHtml(truncate(node.description ?? "", 68))}</text>
      </g>
    `);
  });

  parts.push("</svg>", "</div>");
  canvas.innerHTML = parts.join("");

  const svgEl = canvas.querySelector<SVGSVGElement>("svg.topology-svg")!;

  // Wire drag + click for each node
  svgEl.querySelectorAll<SVGGElement>("[data-node]").forEach((group) => {
    const nodeId = group.dataset.node!;

    // Convert client coords to SVG user-space (accounts for zoom via screen CTM)
    function toSVG(cx: number, cy: number): { x: number; y: number } {
      const pt = svgEl.createSVGPoint();
      pt.x = cx; pt.y = cy;
      const ctm = svgEl.getScreenCTM();
      if (!ctm) return { x: cx, y: cy };
      const t = pt.matrixTransform(ctm.inverse());
      return { x: t.x, y: t.y };
    }

    let dragging = false, hasMoved = false;
    let startCX = 0, startCY = 0, startNX = 0, startNY = 0;

    group.addEventListener("pointerdown", (e: PointerEvent) => {
      if (e.button !== 0) return;
      dragging = true; hasMoved = false;
      startCX = e.clientX; startCY = e.clientY;
      const sp = positions[nodeId];
      startNX = sp ? sp.x : 0; startNY = sp ? sp.y : 0;
      group.setPointerCapture(e.pointerId);
      e.stopPropagation();
    });

    group.addEventListener("pointermove", (e: PointerEvent) => {
      if (!dragging) return;
      const origin = toSVG(startCX, startCY);
      const cur    = toSVG(e.clientX, e.clientY);
      const nx = startNX + (cur.x - origin.x);
      const ny = startNY + (cur.y - origin.y);
      if (!hasMoved && Math.abs(nx - startNX) < 3 && Math.abs(ny - startNY) < 3) return;
      hasMoved = true;
      positions[nodeId] = { x: nx, y: ny };
      group.setAttribute("transform", `translate(${nx},${ny})`);
      group.classList.add("dragging");
      updateEdgePaths(svgEl, positions, edges, nodeMap);
      onDragNode(nodeId, { ...positions });
      e.stopPropagation();
    });

    group.addEventListener("pointerup", (e: PointerEvent) => {
      if (!dragging) return;
      dragging = false;
      group.releasePointerCapture(e.pointerId);
      group.classList.remove("dragging");
      if (hasMoved) onDragNode(nodeId, { ...positions });
      e.stopPropagation();
    });

    group.addEventListener("click", (e: MouseEvent) => {
      if (hasMoved) { e.stopPropagation(); e.preventDefault(); return; }
      onSelectNode(nodeId, { ...positions });
    });
  });
}

// ── Legend ────────────────────────────────────────────
export function renderTopologyLegend(container: HTMLElement): void {
  if (!container) return;
  container.innerHTML = `
    <div class="topology-legend-group">
      <div class="topology-legend-heading">Nodes</div>
      ${[
        { label: "Partition", color: NODE_COLORS.partition },
        { label: "Intent",    color: NODE_COLORS.intent    },
        { label: "Compute",   color: NODE_COLORS.runtime   },
        { label: "Config",    color: NODE_COLORS.config    },
        { label: "Storage",   color: NODE_COLORS.storage   },
        { label: "Network",   color: NODE_COLORS.traffic   },
      ].map((item) => `
        <div class="topology-legend-item">
          <span class="topology-legend-swatch" style="--legend-color:${item.color}"></span>
          <span>${escapeHtml(item.label)}</span>
        </div>
      `).join("")}
    </div>
    <div class="topology-legend-group">
      <div class="topology-legend-heading">Edges</div>
      ${[
        { cls: "contains",  label: "Containment" },
        { cls: "join",      label: "Intent join"  },
        { cls: "routesTo",  label: "Route"        },
        { cls: "triggers",  label: "Trigger"      },
        { cls: "uses",      label: "Uses"         },
        { cls: "logsTo",    label: "Logs"         },
        { cls: "network",   label: "Network"      },
        { cls: "dependsOn", label: "Asset dep."   },
        { cls: "outputRef", label: "Output ref"   },
      ].map((item) => `
        <div class="topology-legend-item">
          <span class="topology-edge-swatch ${escapeAttr(item.cls)}"></span>
          <span>${escapeHtml(item.label)}</span>
        </div>
      `).join("")}
    </div>
  `;
}
