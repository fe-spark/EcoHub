/** GET /film/danmaku 响应顶层的 meta：本站和全网弹幕各多少条，全网弹幕拉取结果 */
export interface DanmakuMeta {
  local: number;
  network: number;
  networkStatus: "disabled" | "ok" | "empty" | "error";
  networkReason?: string;
  networkCached?: boolean;
}

/** 状态点颜色：加载中 / 正常 / 没有全网弹幕 / 失败；none 不显示状态点 */
export type DanmakuStatusTone = "loading" | "ok" | "warn" | "error" | "none";

/**
 * 弹幕加载状态，显示在弹幕输入框上：
 * placeholder 是输入框里的灰字，title 是鼠标悬停时的完整说明（含失败原因），tone 决定输入框左侧状态点的颜色。
 */
export interface DanmakuStatus {
  placeholder: string;
  title: string;
  tone: DanmakuStatusTone;
}

/** 插件自带的输入框占位文字，没有任何弹幕时沿用 */
export const DEFAULT_DANMAKU_PLACEHOLDER = "发个友善的弹幕见证当下";

export const DANMAKU_LOADING_STATUS: DanmakuStatus = {
  placeholder: "弹幕加载中…",
  title: "弹幕加载中…",
  tone: "loading",
};

/** 接口本身失败（非 0 返回码或网络错误） */
export function danmakuRequestFailedStatus(msg?: string): DanmakuStatus {
  const reason = msg || "未知原因";
  return { placeholder: `弹幕加载失败：${reason}`, title: `弹幕加载失败：${reason}`, tone: "error" };
}

/** 弹幕列表装载完成后的状态。total 是本站和全网合计条数 */
export function danmakuLoadedStatus(total: number, meta?: DanmakuMeta | null): DanmakuStatus {
  const say = total > 0 ? `共 ${total} 条弹幕，说点什么吧` : DEFAULT_DANMAKU_PLACEHOLDER;
  switch (meta?.networkStatus) {
    case "ok":
      return { placeholder: say, title: `已加载 ${total} 条弹幕（全网 ${meta.network} 条）`, tone: "ok" };
    case "empty": {
      const reason = meta.networkReason ? `：${meta.networkReason}` : "";
      return {
        placeholder: total > 0 ? `共 ${total} 条弹幕，未找到全网弹幕` : "未找到全网弹幕，说点什么吧",
        title: `${total > 0 ? `已加载 ${total} 条弹幕，` : ""}未找到全网弹幕${reason}`,
        tone: "warn",
      };
    }
    case "error": {
      const reason = meta.networkReason || "未知原因";
      return {
        placeholder: `全网弹幕加载失败：${reason}`,
        title: `全网弹幕加载失败：${reason}${total > 0 ? `（已加载本站 ${total} 条）` : ""}`,
        tone: "error",
      };
    }
    default:
      // 没开全网弹幕（或老接口没有 meta）：只报本站条数，不显示状态点
      return { placeholder: say, title: total > 0 ? `已加载 ${total} 条弹幕` : "", tone: "none" };
  }
}

/**
 * 把状态画到播放器控制条上，两处都画，样式按播放器宽度档位只显示其一（见 playerLayout.ts）：
 * - 输入框：灰字 + 悬停说明 + 左侧状态点（输入框显示时）
 * - 弹幕开关：右上角状态点 + 悬停说明（窄播放器收起输入框时）
 * 插件控件可能晚于状态到达才挂上，调用方在 ready 时再调一次。
 */
export function renderDanmakuStatus($player: HTMLElement | null | undefined, status: DanmakuStatus | null): void {
  if (!$player || !status) return;
  const $input = $player.querySelector(".apd-input") as HTMLInputElement | null;
  if ($input) {
    $input.placeholder = status.placeholder;
    $input.title = status.title;
    let $dot = $player.querySelector(".ecohub-danmaku-status:not(.ecohub-danmaku-status--toggle)") as HTMLElement | null;
    if (!$dot) {
      $dot = document.createElement("span");
      $dot.className = "ecohub-danmaku-status";
      $input.before($dot);
    }
    $dot.dataset.tone = status.tone;
    $dot.title = status.title;
  }
  const $toggle = $player.querySelector(".apd-toggle") as HTMLElement | null;
  if ($toggle) {
    let $dot = $toggle.querySelector(".ecohub-danmaku-status--toggle") as HTMLElement | null;
    if (!$dot) {
      $dot = document.createElement("span");
      $dot.className = "ecohub-danmaku-status ecohub-danmaku-status--toggle";
      $toggle.append($dot);
    }
    $dot.dataset.tone = status.tone;
    $toggle.title = status.title;
  }
}
