/**
 * 弹幕显示设置的本地持久化。
 * 插件设置面板里改的开关、透明度、字号、速度、显示区域、弹幕类型、防重叠，
 * 存到 localStorage，下次创建播放器（含换集）时作为插件初始参数传入。
 */
type Size = number | `${number}%`;

export interface DanmakuSettings {
  visible?: boolean;
  opacity?: number;
  fontSize?: Size;
  speed?: number;
  margin?: [Size, Size];
  modes?: (0 | 1 | 2)[];
  antiOverlap?: boolean;
}

const STORAGE_KEY = "ecohub:danmaku-settings";

const isSize = (v: unknown): v is Size =>
  (typeof v === "number" && Number.isFinite(v)) || (typeof v === "string" && /^\d+(\.\d+)?%$/.test(v));

// 只保留类型正确的字段，插件会对参数做类型校验，脏数据会让插件初始化直接报错
function sanitize(raw: unknown): DanmakuSettings {
  if (!raw || typeof raw !== "object") return {};
  const src = raw as Record<string, unknown>;
  const out: DanmakuSettings = {};
  if (typeof src.visible === "boolean") out.visible = src.visible;
  if (typeof src.antiOverlap === "boolean") out.antiOverlap = src.antiOverlap;
  if (typeof src.opacity === "number" && src.opacity >= 0 && src.opacity <= 1) out.opacity = src.opacity;
  if (typeof src.speed === "number" && src.speed >= 1 && src.speed <= 10) out.speed = src.speed;
  if (isSize(src.fontSize)) out.fontSize = src.fontSize;
  if (Array.isArray(src.margin) && src.margin.length === 2 && src.margin.every(isSize)) {
    out.margin = [src.margin[0], src.margin[1]];
  }
  if (Array.isArray(src.modes) && src.modes.every((m) => m === 0 || m === 1 || m === 2)) {
    out.modes = src.modes as (0 | 1 | 2)[];
  }
  return out;
}

export function loadDanmakuSettings(): DanmakuSettings {
  try {
    return sanitize(JSON.parse(window.localStorage.getItem(STORAGE_KEY) || "null"));
  } catch {
    return {};
  }
}

export function saveDanmakuSettings(patch: unknown): void {
  try {
    const next = { ...loadDanmakuSettings(), ...sanitize(patch) };
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // localStorage 不可用（隐私模式等）时不持久化
  }
}
