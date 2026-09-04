const DEBOUNCE_MS = 2000;
const DEVICE_ID_KEY = "eh_device_id";

/** 获取或持久化生成客户端唯一设备 ID */
export function getOrCreateDeviceId(): string {
  if (typeof window === "undefined") {
    return "";
  }
  try {
    let did = localStorage.getItem(DEVICE_ID_KEY);
    if (!did) {
      if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
        did = `eh_did_${crypto.randomUUID().replace(/-/g, "").slice(0, 16)}`;
      } else {
        did = `eh_did_${Math.random().toString(36).slice(2, 10)}${Date.now().toString(36)}`;
      }
      localStorage.setItem(DEVICE_ID_KEY, did);
    }
    return did;
  } catch {
    return "";
  }
}

export interface TrackPageViewOptions {
  action?: "browse" | "search" | "play" | "classify" | string;
  resource?: string;
  source?: "web" | "android" | "harmony" | "ios" | string;
  path?: string;
  page?: string;
  page_title?: string;
  app_version?: string;
  device_model?: string;
  device_id?: string;
}

export function trackPageView(
  actionOrOptions: string | TrackPageViewOptions,
  resource?: string,
  source: string = "web",
  path?: string,
) {
  if (typeof window === "undefined") {
    return;
  }

  let action = "browse";
  let finalResource = resource || "";
  let finalSource = source || "web";
  let finalPath = path || (window.location.pathname + window.location.search);
  let finalPage = finalPath;
  let pageTitle = typeof document !== "undefined" ? document.title : "";
  let deviceId = getOrCreateDeviceId();

  if (typeof actionOrOptions === "object" && actionOrOptions !== null) {
    action = actionOrOptions.action || "browse";
    finalResource = actionOrOptions.resource || "";
    finalSource = actionOrOptions.source || "web";
    finalPath = actionOrOptions.path || (window.location.pathname + window.location.search);
    finalPage = actionOrOptions.page || finalPath;
    if (actionOrOptions.page_title) {
      pageTitle = actionOrOptions.page_title;
    }
    if (actionOrOptions.device_id) {
      deviceId = actionOrOptions.device_id;
    }
  } else if (typeof actionOrOptions === "string") {
    action = actionOrOptions;
  }

  const key = `eh-pv:${action}:${finalPath}:${finalResource}`;
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
    resource: finalResource,
    source: finalSource,
    path: finalPath,
    page: finalPage,
    page_title: pageTitle,
    device_id: deviceId,
  });

  const url = deviceId
    ? `/api/stat/view?device_id=${encodeURIComponent(deviceId)}`
    : "/api/stat/view";
  try {
    if (typeof navigator.sendBeacon === "function") {
      if (navigator.sendBeacon(url, new Blob([body], { type: "application/json" }))) {
        return;
      }
    }
  } catch {
    // fall through
  }

  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (deviceId) {
    headers["X-Device-Id"] = deviceId;
    headers["Device-Id"] = deviceId;
  }
  void fetch(url, {
    method: "POST",
    body,
    headers,
    keepalive: true,
  }).catch(() => {});
}
