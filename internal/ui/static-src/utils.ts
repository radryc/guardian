// ── Fetch helpers ─────────────────────────────────────
export async function fetchJSON(url: string, init?: RequestInit): Promise<any> {
  const res = await fetch(url, {
    headers: { "Accept": "application/json", ...(init?.headers ?? {}) },
    ...init,
  });
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try { const body = await res.json(); msg = body.error ?? body.message ?? msg; } catch { /* ignore */ }
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}

// ── DOM helpers ───────────────────────────────────────
export function setText(id: string, value: string | number): void {
  const el = document.getElementById(id);
  if (el) el.textContent = String(value);
}

export function setSync(message: string): void {
  const el = document.getElementById("syncIndicator");
  if (el) el.textContent = message;
}

// ── String helpers ────────────────────────────────────
export function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#x27;");
}

export function escapeAttr(s: string): string {
  return s.replace(/[^a-zA-Z0-9_-]/g, (c) => `&#${c.charCodeAt(0)};`);
}

export function humanize(s: string): string {
  return s.replace(/[-_]/g, " ").replace(/([a-z])([A-Z])/g, "$1 $2").replace(/\b\w/g, (c) => c.toUpperCase());
}

export function truncate(s: string, max: number): string {
  return s.length <= max ? s : `${s.slice(0, max - 1)}…`;
}

export function formatDateTime(iso: string | undefined): string {
  if (!iso || iso === "0001-01-01T00:00:00Z") return "—";
  const d = new Date(iso);
  const now = Date.now();
  const diff = Math.floor((now - d.getTime()) / 1000);
  if (diff < 60) return `${diff}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

export function toDateTimeLocalValue(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fromDateTimeLocalValue(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) return "";
  const d = new Date(trimmed);
  if (Number.isNaN(d.getTime())) return "";
  return d.toISOString();
}

export function clone<T>(val: T): T {
  return JSON.parse(JSON.stringify(val));
}

export function nextName(existing: string[], prefix: string): string {
  let i = 1;
  while (existing.includes(`${prefix}-${i}`)) i++;
  return `${prefix}-${i}`;
}

// ── Toast notifications ───────────────────────────────
export function toast(message: string, type: "success" | "error" | "info" = "info"): void {
  const container = document.getElementById("toastContainer");
  if (!container) return;
  const el = document.createElement("div");
  el.className = `toast toast-${type}`;
  el.setAttribute("role", "status");
  el.setAttribute("aria-live", "polite");
  el.textContent = message;
  container.appendChild(el);
  requestAnimationFrame(() => el.classList.add("toast-visible"));
  setTimeout(() => {
    el.classList.remove("toast-visible");
    el.addEventListener("transitionend", () => el.remove(), { once: true });
    setTimeout(() => el.remove(), 400);
  }, 3600);
}
