export const DEFAULT_NOTICE_TITLE = "站点公告";
export const MAX_NOTICE_TITLE_LEN = 64;
export const MAX_NOTICE_CONTENT_LEN = 2048;
export const MAX_NOTICE_VERSION_LEN = 128;

export interface NoticeConfig {
  enabled: boolean;
  title: string;
  content: string;
  version: string;
}

export function createDefaultNoticeConfig(): NoticeConfig {
  return {
    enabled: false,
    title: DEFAULT_NOTICE_TITLE,
    content: "",
    version: "", // 默认留空，面向所有版本生效
  };
}

export function normalizeNoticeConfig(raw: Partial<NoticeConfig> | undefined): NoticeConfig {
  const fallback = createDefaultNoticeConfig();
  if (!raw) return fallback;

  const title = String(raw.title ?? "").trim() || DEFAULT_NOTICE_TITLE;
  const content = String(raw.content ?? "").trim();
  const version = String(raw.version ?? "").trim();

  return {
    enabled: Boolean(raw.enabled),
    title: title.slice(0, MAX_NOTICE_TITLE_LEN),
    content: content.slice(0, MAX_NOTICE_CONTENT_LEN),
    version: version.slice(0, MAX_NOTICE_VERSION_LEN),
  };
}
