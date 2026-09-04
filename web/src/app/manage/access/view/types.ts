import type { SeriesPoint } from "./trend-chart";

export type Overview = {
  day: string;
  pv: number;
  uv: number;
  client?: Record<string, number>;
  action?: Record<string, number>;
  series?: SeriesPoint[];
  platforms?: Record<string, number>;
  versions?: Record<string, number>;
  browsers?: Record<string, number>;
  models?: Record<string, number>;
  os?: Record<string, number>;
};

export type TopItem = {
  key: string;
  count: number;
  title?: string;
  category?: string;
  poster?: string;
  year?: number;
};

export type LogRow = {
  _key?: string;
  ts: string;
  method: string;
  path: string;
  page?: string;
  pageTitle?: string;
  status: number;
  clientType: string;
  ipPreview: string;
  uaFamily: string;
  resource?: string;
  resourceTitle?: string;
  resourcePoster?: string;
  resourceCat?: string;
  action?: string;
  appVersion?: string;
  deviceModel?: string;
  deviceId?: string;
  query?: string;
};
