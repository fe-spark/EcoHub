/**
 * 弹幕按播放器尺寸排版。
 *
 * 插件的字号是固定像素（默认 25px），显示区域按整个播放器高度的百分比算。
 * 手机上播放页把播放器撑高（≤480px 宽时最小 360px 高），16:9 的画面上下留黑边：
 * 25px 字、按 360px 算的「半屏」会铺满画面并压到上方黑边。
 * 这里按播放器宽度缩放字号，按画面实际所在的区域换算显示区域，不看浏览器标识。
 */
import { WIDE_FROM } from "./playerLayout";

export type Size = number | `${number}%`;

/** 播放器宽度达到这个值时用原始字号，更窄时按比例缩小 */
export const FULL_SIZE_WIDTH = 575;
/** 缩放下限：343px 宽约 0.6，25px 缩到 15px */
const MIN_SCALE = 0.5;
const MIN_FONT_SIZE = 12;
export const DEFAULT_FONT_SIZE = 25;
export const DEFAULT_MARGIN: [Size, Size] = [10, "50%"];
/**
 * 播放器窄于这个宽度（playerLayout.ts 的 compact、medium 两档）时，顶部/底部固定弹幕按滚动弹幕显示，
 * 和滚动弹幕一起避让：半屏只有五到八行，固定弹幕会压住滚动弹幕。
 */
export const SCROLL_ONLY_WIDTH = WIDE_FROM;

export function danmakuScale(playerWidth: number): number {
  if (!(playerWidth > 0)) return 1;
  return Math.min(1, Math.max(MIN_SCALE, playerWidth / FULL_SIZE_WIDTH));
}

/** 小播放器上字号上限：约每 22px 宽 1px 字，343px 宽约 16px，不管保存的字号多大 */
const SMALL_PLAYER_CHARS_PER_PX = 22;

/**
 * 用户设定的字号（按大播放器计）换算成当前播放器上的像素。
 * 百分比字号按画面高度算（不是整个播放器高度，窄屏播放器上下有黑边）。
 */
export function effectiveFontSize(base: Size, playerWidth: number, contentHeight: number): number {
  const px = typeof base === "number" ? base : (Number.parseFloat(base) / 100) * contentHeight;
  let size = px * danmakuScale(playerWidth);
  if (playerWidth > 0 && playerWidth < FULL_SIZE_WIDTH) size = Math.min(size, playerWidth / SMALL_PLAYER_CHARS_PER_PX);
  return Math.max(MIN_FONT_SIZE, Math.round(size));
}

/** 当前播放器上的像素字号反算回大播放器上的字号，用于保存 */
export function baseFontSize(px: number, playerWidth: number): number {
  return Math.round(px / danmakuScale(playerWidth));
}

/** 画面在播放器里的上下位置（object-fit: contain）。不知道视频尺寸时按 16:9 算 */
export function videoContentBox(
  playerWidth: number,
  playerHeight: number,
  videoWidth?: number,
  videoHeight?: number,
): { top: number; height: number } {
  const ratio = videoWidth && videoHeight ? videoHeight / videoWidth : 9 / 16;
  const height = Math.min(playerHeight, Math.round(playerWidth * ratio));
  return { top: Math.max(0, Math.round((playerHeight - height) / 2)), height };
}

const resolve = (v: Size, total: number) => (typeof v === "number" ? v : (Number.parseFloat(v) / 100) * total);

/**
 * 用户选的显示区域（如 [10, "50%"]，含义是相对画面）换算成插件要的、相对整个播放器的像素边距。
 * 画面上下有黑边时，弹幕只出现在画面范围内。
 */
export function effectiveMargin(
  base: [Size, Size],
  playerHeight: number,
  box: { top: number; height: number },
): [number, number] {
  const bottomBand = Math.max(0, playerHeight - box.top - box.height);
  return [Math.round(box.top + resolve(base[0], box.height)), Math.round(bottomBand + resolve(base[1], box.height))];
}

/** 插件「显示区域」滑块的四档（与插件默认相同），含义改为相对画面 */
export const MARGIN_STEPS: { name: string; value: [Size, Size] }[] = [
  { name: "1/4", value: [10, "75%"] },
  { name: "半屏", value: [10, "50%"] },
  { name: "3/4", value: [10, "25%"] },
  { name: "满屏", value: [10, 10] },
];

/** 用户设定：字号按大播放器计，显示区域相对画面 */
export interface DanmakuBase {
  fontSize: Size;
  margin: [Size, Size];
}

export interface DanmakuLayoutOptions {
  fontSize: number;
  margin: [number, number];
  MARGIN: { steps: { name: string; value: [number, number] }[] };
}

export function computeDanmakuLayout(
  base: DanmakuBase,
  playerWidth: number,
  playerHeight: number,
  videoWidth?: number,
  videoHeight?: number,
): DanmakuLayoutOptions {
  const box = videoContentBox(playerWidth, playerHeight, videoWidth, videoHeight);
  return {
    fontSize: effectiveFontSize(base.fontSize, playerWidth, box.height),
    margin: effectiveMargin(base.margin, playerHeight, box),
    // 滑块按值找当前档位，档位值也换算成像素，否则显示「未知」
    MARGIN: { steps: MARGIN_STEPS.map((s) => ({ name: s.name, value: effectiveMargin(s.value, playerHeight, box) })) },
  };
}

interface DanmakuPluginLike {
  option: Record<string, unknown> & { margin?: unknown; fontSize?: unknown; MARGIN?: unknown };
  config(option: Record<string, unknown>): unknown;
  reset?(): unknown;
}

interface ArtLike {
  template: { $player?: HTMLElement };
  video?: HTMLVideoElement;
  plugins: unknown;
  on(name: string, fn: (...args: any[]) => void): unknown;
}

const plugin = (art: ArtLike) =>
  (art.plugins as { artplayerPluginDanmuku?: DanmakuPluginLike } | undefined)?.artplayerPluginDanmuku;

/**
 * 播放器尺寸变化（窗口缩放、旋转、全屏、视频尺寸就绪）时重新换算字号和显示区域。
 * 换算出来的值只给插件用，不当成用户设置保存。
 * onUserChange 收到用户在设置面板里真正改动的字段（字号、显示区域已换回用户设定的含义）。
 */
export function bindDanmakuLayout(
  art: ArtLike,
  base: DanmakuBase,
  onUserChange: (changed: Record<string, unknown>) => void,
  pick: (option: unknown) => Record<string, unknown>,
  diff: (prev: Record<string, unknown>, next: Record<string, unknown>) => Record<string, unknown>,
): () => void {
  let applying = false;
  let last = pick(plugin(art)?.option);
  let lastSize = "";
  const size = () => {
    const $p = art.template.$player;
    return { w: $p?.clientWidth || 0, h: $p?.clientHeight || 0 };
  };
  const apply = () => {
    const p = plugin(art);
    const { w, h } = size();
    if (!p || !w || !h) return;
    const next = computeDanmakuLayout(base, w, h, art.video?.videoWidth, art.video?.videoHeight);
    // 尺寸变了：屏幕上正在飞的弹幕还按旧宽度、旧行位排着，换算后会和新弹幕叠在一起，清掉重新发
    const sizeKey = `${w}x${h}`;
    const resized = lastSize !== "" && lastSize !== sizeKey;
    lastSize = sizeKey;
    if (resized) p.reset?.();
    const patch: Record<string, unknown> = {};
    if (p.option.fontSize !== next.fontSize) patch.fontSize = next.fontSize;
    if (JSON.stringify(p.option.margin) !== JSON.stringify(next.margin)) patch.margin = next.margin;
    if (JSON.stringify(p.option.MARGIN) !== JSON.stringify(next.MARGIN)) patch.MARGIN = next.MARGIN;
    if (Object.keys(patch).length === 0) return;
    applying = true;
    try {
      p.config(patch);
    } finally {
      applying = false;
    }
  };
  art.on("artplayerPluginDanmuku:config", (option: unknown) => {
    const next = pick(option);
    const changed = diff(last, next);
    last = next;
    if (applying) return;
    let relayout = false;
    if (typeof changed.fontSize === "number") {
      base.fontSize = baseFontSize(changed.fontSize, size().w);
      changed.fontSize = base.fontSize;
      relayout = true;
    }
    if (changed.margin) {
      const steps = (plugin(art)?.option.MARGIN as DanmakuLayoutOptions["MARGIN"] | undefined)?.steps ?? [];
      const idx = steps.findIndex((s) => JSON.stringify(s.value) === JSON.stringify(changed.margin));
      if (idx >= 0) {
        base.margin = MARGIN_STEPS[idx].value;
        changed.margin = base.margin;
      } else {
        delete changed.margin;
      }
      relayout = true;
    }
    if (Object.keys(changed).length) onUserChange(changed);
    if (relayout) queueMicrotask(apply);
  });
  for (const name of ["ready", "resize", "fullscreen", "fullscreenWeb", "video:loadedmetadata"]) art.on(name, apply);
  // 只改容器尺寸（侧栏开合、页面排版变化）时窗口不触发 resize，另外盯住播放器本身
  if (typeof ResizeObserver !== "undefined" && art.template.$player) {
    let raf = 0;
    const ro = new ResizeObserver(() => {
      if (raf) return;
      raf = requestAnimationFrame(() => {
        raf = 0;
        apply();
      });
    });
    ro.observe(art.template.$player);
    art.on("destroy", () => ro.disconnect());
  }
  return apply;
}

/**
 * 插件只在同类弹幕之间避让：顶部/底部固定弹幕会压在滚动弹幕上。
 * 播放器窄于 SCROLL_ONLY_WIDTH 时只有几行，显示前把固定弹幕改成滚动弹幕，统一避让。
 */
export function smallPlayerScrollOnly(getPlayer: () => HTMLElement | undefined) {
  return (danmu: { mode?: number }) => {
    const w = getPlayer()?.clientWidth || 0;
    if (w > 0 && w < SCROLL_ONLY_WIDTH && danmu.mode !== 0) danmu.mode = 0;
    return true;
  };
}
