export type TipChannelKey = "wechat" | "alipay" | "custom";

export interface TipChannel {
  key: TipChannelKey;
  label: string;
  qrImage: string;
  link: string;
}

export interface TipConfig {
  enabled: boolean;
  title: string;
  message: string;
  channels: TipChannel[];
}

export const MAX_TIP_CHANNELS = 4;
export const MAX_TIP_TITLE_LEN = 32;
export const MAX_TIP_MESSAGE_LEN = 120;
export const MAX_TIP_LABEL_LEN = 32;
export const MAX_TIP_IMAGE_LEN = 512;
export const MAX_TIP_LINK_LEN = 512;
export const DEFAULT_TIP_TITLE = "赞赏支持";
export const DEFAULT_TIP_MESSAGE = "如果这个站对你有帮助，欢迎请作者喝杯咖啡";

export function createDefaultTipConfig(): TipConfig {
  return {
    enabled: false,
    title: DEFAULT_TIP_TITLE,
    message: DEFAULT_TIP_MESSAGE,
    channels: [
      { key: "wechat", label: "微信", qrImage: "", link: "" },
      { key: "alipay", label: "支付宝", qrImage: "", link: "" },
    ],
  };
}

function isTipChannelKey(value: string): value is TipChannelKey {
  return value === "wechat" || value === "alipay" || value === "custom";
}

function defaultTipLabel(key: TipChannelKey): string {
  if (key === "wechat") return "微信";
  if (key === "alipay") return "支付宝";
  return "自定义";
}

function truncateChars(raw: string, max: number): string {
  const chars = Array.from(raw);
  if (max <= 0 || chars.length <= max) return raw;
  return chars.slice(0, max).join("");
}

function normalizeTipLink(raw: string): string {
  const link = raw.trim();
  if (!link) return "";
  const lower = link.toLowerCase();
  if (lower.startsWith("http://") || lower.startsWith("https://")) {
    return truncateChars(link, MAX_TIP_LINK_LEN);
  }
  if (link.includes(":") || link.startsWith("//")) {
    return "";
  }
  if (link.includes(".") && !link.includes(" ")) {
    return truncateChars(`https://${link.replace(/^\/+/, "")}`, MAX_TIP_LINK_LEN);
  }
  return "";
}

function normalizeTipImage(raw: string): string {
  const src = raw.trim();
  if (!src) return "";
  const lower = src.toLowerCase();
  if (lower.startsWith("http://") || lower.startsWith("https://") || src.startsWith("/")) {
    return truncateChars(src, MAX_TIP_IMAGE_LEN);
  }
  return "";
}

export function normalizeTipConfig(input?: Partial<TipConfig> | null): TipConfig {
  const title = truncateChars(String(input?.title ?? "").trim() || DEFAULT_TIP_TITLE, MAX_TIP_TITLE_LEN);
  const message = truncateChars(
    String(input?.message ?? "").trim() || DEFAULT_TIP_MESSAGE,
    MAX_TIP_MESSAGE_LEN,
  );
  const channels: TipChannel[] = [];

  for (const raw of input?.channels ?? []) {
    const keyRaw = String(raw?.key ?? "").trim();
    const key: TipChannelKey = isTipChannelKey(keyRaw) ? keyRaw : "custom";
    const rawLabel = String(raw?.label ?? "").trim();
    const qrImage = normalizeTipImage(String(raw?.qrImage ?? ""));
    const link = normalizeTipLink(String(raw?.link ?? ""));
    if (!qrImage && !link && key === "custom" && !rawLabel) {
      continue;
    }
    channels.push({
      key,
      label: truncateChars(rawLabel || defaultTipLabel(key), MAX_TIP_LABEL_LEN),
      qrImage,
      link,
    });
    if (channels.length >= MAX_TIP_CHANNELS) {
      break;
    }
  }

  return {
    enabled: Boolean(input?.enabled),
    title,
    message,
    channels: channels.length > 0 ? channels : createDefaultTipConfig().channels,
  };
}

export function serializeTipConfig(tip?: Partial<TipConfig> | null): string {
  return JSON.stringify(normalizeTipConfig(tip));
}

export function visibleTipChannels(tip?: TipConfig | null): TipChannel[] {
  return (tip?.channels ?? []).filter(
    (channel) => Boolean(channel.qrImage?.trim()) || Boolean(channel.link?.trim()),
  );
}

export function hasVisibleTip(tip?: TipConfig | null): boolean {
  return Boolean(tip?.enabled) && visibleTipChannels(tip).length > 0;
}
