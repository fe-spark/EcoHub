export const DEFAULT_NOTICE_TITLE = "站点公告";
export const MAX_NOTICE_TITLE_LEN = 64;
export const MAX_NOTICE_CONTENT_LEN = 2048;
export const MAX_NOTICE_APP_VERSION_LEN = 128;
/** @deprecated 保持兼容性常量 */
export const MAX_NOTICE_VERSION_LEN = 128;

export interface NoticeConfig {
  enabled: boolean;
  title: string;
  content: string;
  showInWeb: boolean;
  showInApp: boolean;
  appVersion: string;
  version?: string;
}

export function createDefaultNoticeConfig(): NoticeConfig {
  return {
    enabled: false,
    title: DEFAULT_NOTICE_TITLE,
    content: "",
    showInWeb: true,
    showInApp: true,
    appVersion: "",
    version: "",
  };
}

export function normalizeNoticeConfig(raw: Partial<NoticeConfig> | undefined): NoticeConfig {
  const fallback = createDefaultNoticeConfig();
  if (!raw) return fallback;

  const title = String(raw.title ?? "").trim() || DEFAULT_NOTICE_TITLE;
  const content = String(raw.content ?? "").trim();
  const rawAppVer = raw.appVersion !== undefined ? raw.appVersion : raw.version;
  const appVersion = String(rawAppVer ?? "").trim();

  return {
    enabled: Boolean(raw.enabled),
    title: title.slice(0, MAX_NOTICE_TITLE_LEN),
    content: content.slice(0, MAX_NOTICE_CONTENT_LEN),
    showInWeb: raw.showInWeb !== undefined ? Boolean(raw.showInWeb) : true,
    showInApp: raw.showInApp !== undefined ? Boolean(raw.showInApp) : true,
    appVersion: appVersion.slice(0, MAX_NOTICE_APP_VERSION_LEN),
    version: appVersion.slice(0, MAX_NOTICE_APP_VERSION_LEN),
  };
}
