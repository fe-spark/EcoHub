const STORAGE_KEY = "ecohub.play.preferEnabled";

/** 播放源测速优选开关，默认开启 */
export function readPreferEnabled(): boolean {
  if (typeof window === "undefined") return true;
  const raw = window.localStorage.getItem(STORAGE_KEY);
  if (raw === null) return true;
  return raw === "1" || raw === "true";
}

export function writePreferEnabled(enabled: boolean) {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(STORAGE_KEY, enabled ? "1" : "0");
}
