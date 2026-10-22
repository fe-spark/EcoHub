import dayjs from "dayjs";

export type DateRangeValue = [dayjs.Dayjs, dayjs.Dayjs] | null;

export const RANGE_TIME_DEFAULT: [dayjs.Dayjs, dayjs.Dayjs] = [
  dayjs("00:00:00", "HH:mm:ss"),
  dayjs("23:59:59", "HH:mm:ss"),
];

export function formatDateRange(range: DateRangeValue) {
  if (!range?.[0] || !range?.[1]) {
    return { beginTime: "", endTime: "" };
  }
  return {
    beginTime: range[0].format("YYYY-MM-DD HH:mm:ss"),
    endTime: range[1].format("YYYY-MM-DD HH:mm:ss"),
  };
}

export interface PhotoItem {
  ID: number;
  link: string;
  name: string;
  fid: string;
  CreatedAt?: string;
}

export interface StorageStatus {
  volumeMounted: boolean;
  missingCount: number;
}

export interface StorageAlert {
  key: string;
  title: string;
  detail: string;
  steps: string[];
}

export function buildStorageAlerts(storage?: StorageStatus | null): StorageAlert[] {
  if (!storage) {
    return [];
  }
  const alerts: StorageAlert[] = [];
  if (storage.volumeMounted === false) {
    alerts.push({
      key: "volume",
      title: "素材目录未挂载发布卷",
      detail:
        "当前写入容器可写层，升级重建会丢掉已上传素材。发布卷应为 /app/static/upload。",
      steps: [
        "发布版 compose 为 ecohub 增加卷：./data/uploads:/app/static/upload",
        "源码版 compose 为 server 增加卷：eco-server-uploads:/app/static/upload",
        "改完后执行 docker compose up -d 重启对应容器",
      ],
    });
  }
  if (storage.missingCount > 0) {
    alerts.push({
      key: "missing",
      title: `有 ${storage.missingCount} 条素材文件缺失`,
      detail: "在服务器 compose 目录执行（1Panel 若挂的是 ./uploads，把 data/uploads 改成 uploads）：",
      steps: [
        "mkdir -p ./data/uploads/gallery && docker cp Eco-hub:/app/server/static/upload/gallery/. ./data/uploads/gallery/",
        "拷不出来就重新上传这些图",
      ],
    });
  }
  return alerts;
}
