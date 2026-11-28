import type { AnalyticsModule, AppPlatform } from "./types";

export const MODULE_TABS: { value: AnalyticsModule; label: string }[] = [
  { value: "all", label: "全站总览" },
  { value: "web", label: "Web 网页" },
  { value: "app", label: "App 客户端" },
  { value: "tvbox", label: "TVBox 订阅" },
];

export const PLATFORM_TABS: { value: AppPlatform; label: string }[] = [
  { value: "all", label: "全部平台" },
  { value: "android", label: "Android" },
  { value: "harmony", label: "HarmonyOS" },
  { value: "ios", label: "iOS" },
];

export const ACTION_META: Record<string, { label: string; color: string }> = {
  play: { label: "影视点播", color: "#fa8c16" },
  search: { label: "寻片搜索", color: "#fa541c" },
  browse: { label: "页面浏览", color: "#52c41a" },
  classify: { label: "分类筛选", color: "#722ed1" },
  config: { label: "配置同步", color: "#13c2c2" },
  provide: { label: "订阅访问", color: "#1677ff" },
};

export const CLIENT_META: Record<string, { label: string; color: string }> = {
  web: { label: "Web 网页", color: "#1677ff" },
  app: { label: "App 客户端", color: "#52c41a" },
  android: { label: "Android", color: "#52c41a" },
  harmony: { label: "HarmonyOS", color: "#1677ff" },
  ios: { label: "iOS", color: "#722ed1" },
  tvbox: { label: "TVBox", color: "#fa8c16" },
  crawler: { label: "爬虫", color: "#8c8c8c" },
  manage: { label: "后台", color: "#13c2c2" },
};

export const BROWSER_META: Record<string, { label: string; color: string }> = {
  chrome: { label: "Chrome", color: "#1677ff" },
  safari: { label: "Safari", color: "#52c41a" },
  edge: { label: "Edge", color: "#13c2c2" },
  firefox: { label: "Firefox", color: "#fa541c" },
  other: { label: "其他", color: "#8c8c8c" },
};

export const OS_META: Record<string, { label: string; color: string }> = {
  windows: { label: "Windows", color: "#1677ff" },
  macos: { label: "macOS", color: "#722ed1" },
  android: { label: "Android", color: "#52c41a" },
  ios: { label: "iOS", color: "#eb2f96" },
  harmonyos: { label: "HarmonyOS", color: "#13c2c2" },
  linux: { label: "Linux", color: "#fa8c16" },
  other: { label: "其他", color: "#8c8c8c" },
};

export const CHART_PALETTE = [
  "#fa8c16",
  "#1677ff",
  "#52c41a",
  "#722ed1",
  "#13c2c2",
  "#eb2f96",
  "#fa541c",
  "#2f54eb",
];

export const HIST_BUCKETS: { key: string; label: string }[] = [
  { key: "b50", label: "<50ms" },
  { key: "b100", label: "50-100ms" },
  { key: "b200", label: "100-200ms" },
  { key: "b500", label: "200-500ms" },
  { key: "b1000", label: "500-1000ms" },
  { key: "bInf", label: ">1000ms" },
];

export function isSafeInternalHref(v: string): boolean {
  const s = String(v ?? "").trim();
  if (!s || s.startsWith("//")) return false;
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(s)) return false;
  return s.startsWith("/");
}
