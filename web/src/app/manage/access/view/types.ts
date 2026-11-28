export type AnalyticsModule = "all" | "web" | "app" | "tvbox";
export type AppPlatform = "all" | "android" | "harmony" | "ios";

export interface SeriesPoint {
  t: string;
  pv: number;
  err4: number;
  err5: number;
  providePv: number;
  webPv?: number;
  appPv?: number;
  androidPv?: number;
  harmonyPv?: number;
  iosPv?: number;
}

export interface ProvideStats {
  pv: number;
  err4: number;
  err5: number;
}

export interface Overview {
  day: string;
  pv: number;
  uv: number;
  err4: number;
  err5: number;
  p95Ms: number;
  dropped: number;
  provide?: ProvideStats;
  client: Record<string, number>;
  action: Record<string, number>;
  hist: Record<string, number>;
  series: SeriesPoint[];
  platforms?: Record<string, number>;
  versions?: Record<string, number>;
  browsers?: Record<string, number>;
  models?: Record<string, number>;
  os?: Record<string, number>;
}

export interface TopItem {
  key: string;
  count: number;
  title?: string;
  category?: string;
  poster?: string;
  year?: number;
  href?: string;
}

export interface TopPageResult {
  kind: string;
  items: TopItem[];
  total: number;
  cappedAt: number;
}

export interface SourceCallRow {
  id: string;
  name: string;
  enabled: boolean;
  isPrimary: boolean;
  count: number;
}

export interface LogRow {
  _key?: string;
  ts: string;
  method: string;
  path: string;
  route?: string;
  page?: string;
  pageTitle?: string;
  status: number;
  latencyMs?: number;
  clientType: string;
  ipPreview: string;
  uaFamily: string;
  os?: string;
  resource?: string;
  resourceTitle?: string;
  resourcePoster?: string;
  resourceCat?: string;
  resourceHref?: string;
  action?: string;
  appVersion?: string;
  deviceModel?: string;
  deviceId?: string;
  query?: string;
}

export interface LogPageResult {
  list: LogRow[];
  total: number;
}

export interface DashboardPayload {
  day: string;
  module: AnalyticsModule;
  platform: AppPlatform;
  global: Overview;
  scoped: Overview;
  sources: SourceCallRow[];
  playTops: TopItem[];
  searchTops: TopItem[];
  classifyTops: TopItem[];
  pageTops: TopItem[];
  logs: LogRow[];
  logsTotal: number;
  availableDays: string[];
}
