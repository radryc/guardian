// Presentation-only display-profile helpers for the Guardian UI.
// Canonical keys stay available to the UI; the switcher only changes labels
// and the JSON/API presentation unless the backend returns mapped-only data.

const PROFILE_KEY = "strata.displayProfile";
const KEYS_KEY = "strata.displayKeys";

export function currentDisplayProfile(): string {
  try {
    return localStorage.getItem(PROFILE_KEY) ?? "";
  } catch {
    return "";
  }
}

export function displayHeaders(): Record<string, string> {
  const profile = currentDisplayProfile();
  if (!profile || profile === "canonical") return {};
  return {
    "X-Strata-Display-Profile": profile,
    "X-Strata-Display-Keep-Canonical": "1",
  };
}

function keyMap(): Record<string, string> {
  try {
    const raw = localStorage.getItem(KEYS_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

export function displayKey(canonical: string): string {
  return keyMap()[canonical] ?? canonical;
}

export async function setDisplayProfile(profile: string): Promise<void> {
  try {
    if (!profile || profile === "canonical") {
      localStorage.removeItem(PROFILE_KEY);
      localStorage.removeItem(KEYS_KEY);
      return;
    }
    localStorage.setItem(PROFILE_KEY, profile);
    const res = await fetch(`/api/display-profiles/${encodeURIComponent(profile)}`, {
      headers: { Accept: "application/json" },
    });
    if (!res.ok) return;
    const found = await res.json();
    if (found?.keys) localStorage.setItem(KEYS_KEY, JSON.stringify(found.keys));
  } catch {
    /* presentation-only: ignore storage/network failures */
  }
}

export function applyDisplayLabels(): void {
  document.querySelectorAll<HTMLElement>("[data-display-key]").forEach((element) => {
    const canonical = element.dataset.displayKey;
    if (!canonical) return;
    element.textContent = displayKey(canonical);
  });
}

export async function initDisplaySwitcher(): Promise<void> {
  const select = document.getElementById("displayProfileSelect") as HTMLSelectElement | null;
  if (!select) return;
  const canonical = document.createElement("option");
  canonical.value = "canonical";
  canonical.textContent = "Canonical labels";
  select.appendChild(canonical);
  try {
    const res = await fetch("/api/display-profiles", { headers: { Accept: "application/json" } });
    if (res.ok) {
      const body = await res.json();
      for (const profile of body.profiles ?? []) {
        const option = document.createElement("option");
        option.value = profile.id;
        option.textContent = profile.name || profile.id;
        select.appendChild(option);
      }
    }
  } catch {
    /* keep canonical-only */
  }
  if (!currentDisplayProfile()) {
    try {
      const res = await fetch("/api/display-profiles/effective", { headers: { Accept: "application/json" } });
      if (res.ok) {
        const body = await res.json();
        if (body.profile) await setDisplayProfile(body.profile);
      }
    } catch {
      /* canonical */
    }
  }
  const active = currentDisplayProfile() || "canonical";
  select.value = active;
  select.addEventListener("change", () => {
    setDisplayProfile(select.value).finally(() => window.location.reload());
  });
  applyDisplayLabels();
}
