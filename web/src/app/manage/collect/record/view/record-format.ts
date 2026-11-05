import dayjs from "dayjs";

export function formatDuration(ms: number): string {
  if (ms <= 0) return "< 1 秒";
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const remainingSec = seconds % 60;
  if (minutes < 60) {
    return remainingSec > 0 ? `${minutes} 分 ${remainingSec} 秒` : `${minutes} 分钟`;
  }
  const hours = Math.floor(minutes / 60);
  const remainingMin = minutes % 60;
  return `${hours} 小时 ${remainingMin} 分`;
}

export function formatCollectPages(record: {
  status?: string;
  pageSuccess?: number;
  pageTotal?: number;
  pageFailed?: number;
}): { primary: string; secondary: string; alert: boolean } {
  const succ = Math.max(0, record.pageSuccess ?? 0);
  const total = Math.max(0, record.pageTotal ?? 0);
  const fail = Math.max(0, record.pageFailed ?? 0);
  const status = record.status ?? "";
  if (fail > 0) {
    return {
      primary: `${fail} 页失败`,
      secondary: total > 0 ? `成功 ${succ} / 共 ${total} 页` : `成功 ${succ} 页`,
      alert: true,
    };
  }
  if (status === "failed") {
    return {
      primary: "未能拉取",
      secondary: total === 0 ? "探测失败" : `成功 0 / 共 ${total} 页`,
      alert: true,
    };
  }
  if (status === "stopped" || status === "interrupted") {
    return {
      primary: `成功 ${succ} 页`,
      secondary: total > 0 ? `共 ${total} 页` : "已停止",
      alert: false,
    };
  }
  if (total > 0 && succ < total) {
    return {
      primary: `成功 ${succ} / 共 ${total} 页`,
      secondary: "",
      alert: false,
    };
  }
  return { primary: `共 ${total} 页`, secondary: "", alert: false };
}

export function formatSourcePages(item: {
  status?: string;
  pageTotal?: number;
  successCnt?: number;
  failedCnt?: number;
  failedPages?: number[];
  error?: string;
}): string {
  const total = Math.max(0, item.pageTotal ?? 0);
  const succ = Math.max(0, item.successCnt ?? 0);
  const fail = Math.max(0, item.failedCnt ?? 0, item.failedPages?.length ?? 0);
  if (total <= 0) {
    if (fail > 0) return `失败 ${fail} 页`;
    const isProbeErr = item.error ? item.error.includes("探测") : true;
    if (item.status === "failed") return isProbeErr ? "探测失败" : "抓取失败";
    if (item.status === "stopped" || item.status === "interrupted") return "已停止";
    return "0/0";
  }
  const parts = [`成功 ${succ} / 共 ${total} 页`];
  if (fail > 0) parts.push(`失败 ${fail} 页`);
  return parts.join("，");
}

export function formatSourceWindow(hour: number | undefined, batchHour: number): string {
  if (hour == null || hour === 0 || hour === batchHour) return "";
  if (hour === -1) return "全量";
  return hour > 0 ? `${hour} 小时` : "";
}

export function formatCollectFilms(count: number, _record?: { status?: string }) {
  if (count <= 0) {
    return "无更新";
  }
  return `+${count} 部`;
}

export function formatStartedAt(val: string) {
  if (!val) return { primary: "-", full: "" };
  const t = dayjs(val);
  if (!t.isValid()) return { primary: val, full: val };
  return { primary: t.format("YYYY-MM-DD HH:mm:ss"), full: t.format("YYYY-MM-DD HH:mm:ss") };
}
