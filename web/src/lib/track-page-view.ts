const DEBOUNCE_MS = 2000;

export function trackPageView(
  action: string,
  resource?: string,
  source: string = "web",
  path?: string,
) {
  if (typeof window === "undefined") {
    return;
  }
  const currentPath = path || (window.location.pathname + window.location.search);
  const key = `eh-pv:${action}:${currentPath}:${resource || ""}`;
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
  const body = JSON.stringify({
    action,
    resource: resource || "",
    source: source || "web",
    path: currentPath,
  });
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
