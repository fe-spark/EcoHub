/**
 * 弹幕显示设置的本地持久化。
 * 插件设置面板里改的开关、透明度、字号、速度、显示区域、弹幕类型、防重叠，
 * 存到 localStorage，下次创建播放器（含换集）时作为插件初始参数传入。
 *
 * 只存用户真正改过的项（和改之前比有变化的字段）。插件每次改设置都会把整份参数发出来，
 * 以前整份存下，插件默认的 25px 字号也被当成「用户选的」带到每次播放。
 *
 * 字号按设备分开存：手机播放器只有约 340×190，桌面的 25px 放到手机上会占掉大半画面。
 * 手机读 mobileFontSize，桌面读 fontSize；手机上调字号只写 mobileFontSize。
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

interface StoredDanmakuSettings extends DanmakuSettings {
  mobileFontSize?: Size;
}

const STORAGE_KEY = "ecohub:danmaku-settings";

/** 手机默认字号：按播放器高度的 8%（343×193 约 15px，横屏全屏约 30px），随播放器大小变化 */
export const MOBILE_DEFAULT_FONT_SIZE: Size = "8%";

const isSize = (v: unknown): v is Size =>
  (typeof v === "number" && Number.isFinite(v)) || (typeof v === "string" && /^\d+(\.\d+)?%$/.test(v));

// 只保留类型正确的字段，插件会对参数做类型校验，脏数据会让插件初始化直接报错
function sanitize(raw: unknown): StoredDanmakuSettings {
  if (!raw || typeof raw !== "object") return {};
  const src = raw as Record<string, unknown>;
  const out: StoredDanmakuSettings = {};
  if (typeof src.visible === "boolean") out.visible = src.visible;
  if (typeof src.antiOverlap === "boolean") out.antiOverlap = src.antiOverlap;
  if (typeof src.opacity === "number" && src.opacity >= 0 && src.opacity <= 1) out.opacity = src.opacity;
  if (typeof src.speed === "number" && src.speed >= 1 && src.speed <= 10) out.speed = src.speed;
  if (isSize(src.fontSize)) out.fontSize = src.fontSize;
  if (isSize(src.mobileFontSize)) out.mobileFontSize = src.mobileFontSize;
  if (Array.isArray(src.margin) && src.margin.length === 2 && src.margin.every(isSize)) {
    out.margin = [src.margin[0], src.margin[1]];
  }
  if (Array.isArray(src.modes) && src.modes.every((m) => m === 0 || m === 1 || m === 2)) {
    out.modes = src.modes as (0 | 1 | 2)[];
  }
  return out;
}

function readStored(): StoredDanmakuSettings {
  try {
    return sanitize(JSON.parse(window.localStorage.getItem(STORAGE_KEY) || "null"));
  } catch {
    return {};
  }
}

/** 创建播放器时的初始参数。手机不沿用桌面字号，只用手机上自己调过的 */
export function loadDanmakuSettings(isMobile: boolean): DanmakuSettings {
  const { mobileFontSize, fontSize, ...rest } = readStored();
  const size = isMobile ? mobileFontSize : fontSize;
  return size === undefined ? rest : { ...rest, fontSize: size };
}

/** 从插件参数里取出要持久化的字段（不含 mobileFontSize） */
export function pickDanmakuSettings(option: unknown): DanmakuSettings {
  const { mobileFontSize: _ignored, ...rest } = sanitize(option);
  void _ignored;
  return rest;
}

/** next 相对 prev 有变化的字段 */
export function changedDanmakuSettings(prev: DanmakuSettings, next: DanmakuSettings): DanmakuSettings {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(next) as (keyof DanmakuSettings)[]) {
    if (JSON.stringify(prev[key]) !== JSON.stringify(next[key])) out[key] = next[key];
  }
  return out as DanmakuSettings;
}

export function saveDanmakuSettings(patch: DanmakuSettings, isMobile: boolean): void {
  try {
    const clean = pickDanmakuSettings(patch);
    if (Object.keys(clean).length === 0) return;
    const { fontSize, ...rest } = clean;
    const next: StoredDanmakuSettings = { ...readStored(), ...rest };
    if (fontSize !== undefined) {
      if (isMobile) next.mobileFontSize = fontSize;
      else next.fontSize = fontSize;
    }
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // localStorage 不可用（隐私模式等）时不持久化
  }
}
