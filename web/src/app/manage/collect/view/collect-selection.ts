import { isActiveCollectStatus, type FilmSource } from "./types";
import type { SearchIndexProgress } from "./use-collect-stream";

export interface NamedSkip {
  name: string;
  reason: string;
}

/** 按钮、提示和跳过原因共用。说清不能新开，以及正在跑的采集不会被停掉。 */
export const collectIndexUpdatingReason =
  "检索索引正在更新，现在不能新开采集。正在进行的采集会继续跑完。";

/** 分类采集中的站不能勾选，避免和正在拉取的分类冲突。 */
export function isSourceSelectable(site: FilmSource): boolean {
  return !site.categorySyncing;
}

/** 空字符串表示可以采集。多个静态原因用顿号连在一起。 */
export function collectBlockReason(
  site: FilmSource,
  searchIndex?: SearchIndexProgress | null,
): string {
  if (searchIndex?.status === "indexing") {
    return collectIndexUpdatingReason;
  }
  if (site.categorySyncing) {
    return "分类采集中";
  }
  const status = site.progress?.status;
  if (status === "stopping") {
    return "停止中";
  }
  if (status === "sealing") {
    return "待检索";
  }
  if (status === "paused") {
    return "限流中";
  }
  if (isActiveCollectStatus(status)) {
    return "采集中";
  }
  const parts: string[] = [];
  if (!site.state) {
    parts.push("未启用");
  }
  if (site.categoryReady === false) {
    parts.push("缺分类");
  }
  return parts.join("、");
}

export function formatSkipSummary(items: NamedSkip[]): string {
  const groups = new Map<string, string[]>();
  for (const item of items) {
    if (!item.reason) continue;
    const names = groups.get(item.reason) ?? [];
    names.push(item.name);
    groups.set(item.reason, names);
  }
  return Array.from(groups.entries())
    .map(([reason, names]) => `${joinNames(names)}（${reason}）`)
    .join("；");
}

function joinNames(names: string[]): string {
  const clean = names.map((name) => name.trim()).filter(Boolean);
  if (clean.length === 0) return "未命名";
  if (clean.length <= 3) return clean.join("、");
  return `${clean.slice(0, 3).join("、")} 等 ${clean.length} 个`;
}
