export interface FilmSource {
  id: string;
  name: string;
  uri: string;
  syncPictures: boolean;
  state: boolean;
  grade: number;
  interval: number;
  cd?: number;
  lastCollectTime?: string;
  progress?: CollectProgress | null;
}

export interface CollectProgress {
  id: string;
  name: string;
  total: number;
  current: number;
  success: number;
  failed: number;
  status: string;
}

export interface BatchOption {
  id: string;
  name: string;
  grade?: number;
  state?: boolean;
}

export interface SourceFormValues {
  name: string;
  uri: string;
  syncPictures: boolean;
  state: boolean;
  grade: number;
  interval: number;
}

export const collectDuration = [
  { label: "采集今日", time: 24 },
  { label: "采集三天", time: 72 },
  { label: "采集一周", time: 168 },
  { label: "采集半月", time: 360 },
  { label: "采集一月", time: 720 },
  { label: "采集三月", time: 2160 },
  { label: "采集半年", time: 4320 },
  { label: "全量采集", time: -1 },
];
