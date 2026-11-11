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
