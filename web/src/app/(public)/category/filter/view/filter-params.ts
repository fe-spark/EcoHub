export const DEFAULT_SORT_VALUE = "update_stamp";

export function normalizeTagValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

export function isDefaultSortValue(key: string, value: string): boolean {
  return key === "Sort" && (value === DEFAULT_SORT_VALUE || value === "default");
}

export function resolveActiveTagValue(key: string, raw: string): string {
  return key === "Sort" ? raw || DEFAULT_SORT_VALUE : raw;
}

/** Category 只有「全部」时不算可展示的筛选维度 */
export function hasVisibleCategoryTags(tags: unknown[] | undefined): boolean {
  return (tags?.length ?? 0) > 1;
}
