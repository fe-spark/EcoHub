/** GET /film/danmaku 响应顶层的 meta：本站和全网弹幕各多少条，全网弹幕拉取结果 */
export interface DanmakuMeta {
  local: number;
  network: number;
  networkStatus: "disabled" | "ok" | "empty" | "error";
  networkReason?: string;
  networkCached?: boolean;
}

/** 弹幕列表装载完成后，在播放器 notice 里给的一句提示；返回空串表示不提示 */
export function danmakuNoticeText(total: number, meta?: DanmakuMeta | null): string {
  if (!meta) return total > 0 ? `已加载 ${total} 条弹幕` : "";
  switch (meta.networkStatus) {
    case "ok":
      return `已加载 ${total} 条弹幕（全网 ${meta.network} 条）`;
    case "empty":
      return total > 0 ? `已加载 ${total} 条弹幕，未找到全网弹幕` : "未找到全网弹幕";
    case "error": {
      const reason = meta.networkReason || "未知原因";
      return total > 0 ? `全网弹幕加载失败：${reason}（已加载本站 ${total} 条）` : `全网弹幕加载失败：${reason}`;
    }
    default:
      // 没开全网弹幕：只有本站弹幕时报个数，没有就不打扰
      return total > 0 ? `已加载 ${total} 条弹幕` : "";
  }
}
