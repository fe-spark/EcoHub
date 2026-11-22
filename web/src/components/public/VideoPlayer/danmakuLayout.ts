/**
 * 弹幕按播放器尺寸排版。
 *
 * 插件的字号是固定像素（默认 25px），这里以常规桌面小屏（BASE_WIDTH=960）为 1x 基准动态缩放字号：
 * 手机/窄屏按比例缩小，全屏只略放大（1080p 约 1.2 倍，再大也不超过这个上限）；
 * 显示区域沿用插件原生的 [上边距, 下边距]（如 [10, "50%"]），全屏与小屏均从播放器顶部起算，
 * 存在上下黑边时可充分利用黑边空间，减少对正片画面的遮挡。
 */
import { WIDE_FROM } from "./playerLayout";

export type Size = number | `${number}%`;

/** 基准播放器宽度（常规桌面小屏约 960×540 时使用 1x 原始字号，窄屏按比例缩小、全屏最多 1.2 倍） */
export const BASE_WIDTH = 960;
const MIN_SCALE = 0.5;
const MAX_SCALE = 1.2;
const UP_SCALE_FACTOR = 0.2;
const MIN_FONT_SIZE = 12;
const MAX_FONT_SIZE = 120;
export const DEFAULT_FONT_SIZE = 25;
export const DEFAULT_MARGIN: [Size, Size] = [10, "50%"];
/**
 * 播放器窄于这个宽度（playerLayout.ts 的 compact、medium 两档）时，顶部/底部固定弹幕按滚动弹幕显示，
 * 和滚动弹幕一起避让：半屏只有五到八行，固定弹幕会压住滚动弹幕。
 */
export const SCROLL_ONLY_WIDTH = WIDE_FROM;

export function danmakuScale(playerWidth: number, playerHeight = 0): number {
  // 按 16:9 等效宽度取宽高的较小约束，避免超宽屏/竖屏单边过大导致字号失真
  const effectiveWidth = playerHeight > 0 ? Math.min(playerWidth, playerHeight * (16 / 9)) : playerWidth;
  if (!(effectiveWidth > 0)) return 1;
  const ratio = effectiveWidth / BASE_WIDTH;
  const raw = ratio > 1 ? 1 + (ratio - 1) * UP_SCALE_FACTOR : ratio;
  return Math.min(MAX_SCALE, Math.max(MIN_SCALE, raw));
}

/** 窄屏字号上限：约每 22px 宽 1px 字，避免用户调大字号后在小屏撑满屏幕 */
const SMALL_PLAYER_CHARS_PER_PX = 22;

/**
 * 用户设定的字号（按 1x 基准播放器计）换算成当前播放器上的像素。
 * 百分比字号按当前播放器高度换算。
 */
export function effectiveFontSize(base: Size, playerWidth: number, playerHeight: number): number {
  const px = typeof base === "number" ? base : (Number.parseFloat(base) / 100) * playerHeight;
  let size = px * danmakuScale(playerWidth, playerHeight);
  if (playerWidth > 0 && playerWidth < BASE_WIDTH) size = Math.min(size, playerWidth / SMALL_PLAYER_CHARS_PER_PX);
  return Math.min(MAX_FONT_SIZE, Math.max(MIN_FONT_SIZE, Math.round(size)));
}

/** 当前播放器上的像素字号反算回 1x 基准播放器字号，用于保存 */
export function baseFontSize(px: number, playerWidth: number, playerHeight = 0): number {
  return Math.min(MAX_FONT_SIZE, Math.max(MIN_FONT_SIZE, Math.round(px / danmakuScale(playerWidth, playerHeight))));
}

/** 用户设定：字号按大播放器计，显示区域相对播放器 */
export interface DanmakuBase {
  fontSize: Size;
  margin: [Size, Size];
}

export interface DanmakuLayoutOptions {
  fontSize: number;
  margin: [Size, Size];
}

export function computeDanmakuLayout(
  base: DanmakuBase,
  playerWidth: number,
  playerHeight: number,
): DanmakuLayoutOptions {
  return {
    fontSize: effectiveFontSize(base.fontSize, playerWidth, playerHeight),
    margin: base.margin,
  };
}

interface DanmakuPluginLike {
  option: Record<string, unknown> & { fontSize?: unknown };
  config(option: Record<string, unknown>): unknown;
  reset?(): unknown;
}

interface ArtLike {
  template: { $player?: HTMLElement };
  plugins: unknown;
  on(name: string, fn: (...args: any[]) => void): unknown;
}

const plugin = (art: ArtLike) =>
  (art.plugins as { artplayerPluginDanmuku?: DanmakuPluginLike } | undefined)?.artplayerPluginDanmuku;

/**
 * 播放器尺寸变化（窗口缩放、旋转、全屏）时重新换算字号。
 * 换算出来的值只给插件用，不当成用户设置保存。
 * onUserChange 收到用户在设置面板里真正改动的字段（字号已换回大播放器基准）。
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
    const next = computeDanmakuLayout(base, w, h);
    // 尺寸变了：屏幕上正在飞的弹幕还按旧宽度、旧行位排着，换算后会和新弹幕叠在一起，清掉重新发
    const sizeKey = `${w}x${h}`;
    const resized = lastSize !== "" && lastSize !== sizeKey;
    lastSize = sizeKey;
    if (resized) p.reset?.();
    if (p.option.fontSize === next.fontSize) return;
    applying = true;
    try {
      p.config({ fontSize: next.fontSize });
    } finally {
      applying = false;
    }
  };
  art.on("artplayerPluginDanmuku:config", (option: unknown) => {
    const next = pick(option);
    const changed = diff(last, next);
    last = next;
    if (applying) return;
    if (typeof changed.fontSize === "number") {
      const { w, h } = size();
      base.fontSize = baseFontSize(changed.fontSize, w, h);
      changed.fontSize = base.fontSize;
    }
    if (Object.keys(changed).length) onUserChange(changed);
  });
  for (const name of ["ready", "resize", "fullscreen", "fullscreenWeb"]) art.on(name, apply);
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
