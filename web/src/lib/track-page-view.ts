const DEBOUNCE_MS = 2000;

export function trackPageView(action: string, resource?: string) {
  if (typeof window === "undefined") {
    return;
  }
  const key = `eh-pv:${action}:${window.location.pathname}:${resource || ""}`;
  const now = Date.now();
  try {
    const last = Number(sessionStorage.getItem(key) || 0);
    if (now - last < DEBOUNCE_MS) {
      return;
    }
    sessionStorage.setItem(key, String(now));
  } catch {
    // ignore quota / private mode
  }
  const body = JSON.stringify({ action, resource: resource || "" });
  const url = "/api/stat/view";
  try {
    if (typeof navigator.sendBeacon === "function") {
      navigator.sendBeacon(url, new Blob([body], { type: "application/json" }));
      return;
    }
  } catch {
    // fall through
  }
  void fetch(url, {
    method: "POST",
    body,
    headers: { "Content-Type": "application/json" },
    keepalive: true,
  }).catch(() => {});
}
