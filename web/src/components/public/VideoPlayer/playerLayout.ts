/**
 * 播放器按自身显示宽度（不是浏览器标识、也不是窗口宽度）分三档排版。
 *
 * - compact  < 560px：手机竖屏（360–430px）和很窄的窗口。控制条放下播放、音量、时间、弹幕开关/设置、
 *   设置、全屏后，输入框只剩不到 110px（480px 宽时约 30px，发送按钮还会伸出播放器）：
 *   收起弹幕输入框和网页全屏按钮，加载状态改为弹幕开关上的小圆点（悬停看说明），
 *   失败时再用一次简短提示。
 * - medium   560–767px：手机横屏、平板竖屏、窄窗口。收起网页全屏按钮、按钮收窄，输入框约 110–300px，
 *   随控制条收窄；画面半屏只有五到八行，顶部/底部固定弹幕按滚动显示，统一避让。
 * - wide     ≥ 768px：桌面完整布局。
 *
 * 768 与播放页本身切换移动端排版的断点一致；560 是按钮收窄后输入框还能留出约 110px 的最窄宽度。
 * 尺寸变化（拖窗口、旋转、全屏、网页全屏、侧栏开合）时实时重算。
 */
export type PlayerWidthClass = "compact" | "medium" | "wide";

export const COMPACT_BELOW = 560;
export const WIDE_FROM = 768;

export function playerWidthClass(width: number): PlayerWidthClass {
  if (width > 0 && width < COMPACT_BELOW) return "compact";
  if (width > 0 && width < WIDE_FROM) return "medium";
  return "wide";
}

interface ArtLike {
  template: { $player?: HTMLElement };
  on(name: string, fn: (...args: any[]) => void): unknown;
}

/**
 * 把档位写到播放器根节点的 data-ecohub-width 上（样式按它切换），档位变了时回调。
 * 返回取消监听的函数。
 */
export function bindPlayerWidthClass(
  art: ArtLike,
  onChange?: (cls: PlayerWidthClass, prev: PlayerWidthClass | null) => void,
): () => void {
  let current: PlayerWidthClass | null = null;
  const update = () => {
    const $p = art.template.$player;
    if (!$p) return;
    // 播放器高度跟着视频比例变（超宽视频最低只有 180px），弹幕设置面板按它限制最大高度
    $p.style.setProperty("--ecohub-player-height", `${$p.clientHeight}px`);
    const next = playerWidthClass($p.clientWidth);
    if (next === current) return;
    const prev = current;
    current = next;
    $p.dataset.ecohubWidth = next;
    onChange?.(next, prev);
  };
  for (const name of ["ready", "resize", "fullscreen", "fullscreenWeb"]) art.on(name, update);
  let ro: ResizeObserver | null = null;
  if (typeof ResizeObserver !== "undefined" && art.template.$player) {
    ro = new ResizeObserver(update);
    ro.observe(art.template.$player);
    art.on("destroy", () => ro?.disconnect());
  }
  update();
  return () => ro?.disconnect();
}

export function currentPlayerWidthClass(art: ArtLike): PlayerWidthClass {
  return playerWidthClass(art.template.$player?.clientWidth || 0);
}
