export type CollectRecordSourceStatus =
  | "success"
  | "done"
  | "failed"
  | "stopped"
  | "running"
  | "partial"
  | "starting"
  | "waiting";

export interface CollectRecordSourceDetail {
  sourceId: string;
  sourceName: string;
  status: CollectRecordSourceStatus;
  hour?: number;
  pageTotal: number;
  pageCurrent: number;
  successCnt: number;
  failedCnt: number;
  filmsTotal: number;
  error?: string;
  failedPages?: number[];
}

export type CollectHistoryStatus =
  | "running"
  | "success"
  | "partial"
  | "failed"
  | "stopped"
  | "interrupted";

export type CollectTriggerType = "manual" | "cron" | "retry" | "single_update" | string;

export interface SourceIncident {
  sourceId: string;
  sourceName: string;
  consecutiveFailures: number;
  latestFailedAt: string;
  latestError?: string;
  latestBatchId: string;
  tripped: boolean;
}

export function resolveBatchStatus(record: {
  status: string;
  pageSuccess?: number;
  pageFailed?: number;
  failedSources?: number;
  successSources?: number;
}): string {
  if (record.status === "running" || record.status === "stopped" || record.status === "interrupted") {
    return record.status;
  }
  const succ = record.pageSuccess ?? 0;
  const fail = record.pageFailed ?? 0;
  const failSrc = record.failedSources ?? 0;
  const succSrc = record.successSources ?? 0;

  if (fail > 0 || failSrc > 0) {
    if (succ > 0 || succSrc > 0) return "partial";
    return "failed";
  }
  return record.status || "success";
}

export function canRetryHistoryRecord(record: Pick<CollectHistoryRecord, "status" | "failedSources" | "trigger" | "hour" | "filmIds"> & {
  pageSuccess?: number;
  pageFailed?: number;
  successSources?: number;
}) {
  if (record.status === "running") return false;
  if (record.trigger === "single_update") return Boolean(record.filmIds?.trim());
  const resolved = resolveBatchStatus(record);
  return record.hour !== 0 && (resolved === "failed" || resolved === "partial");
}

export function canRetryHistorySource(record: Pick<CollectHistoryRecord, "status" | "trigger" | "hour" | "filmIds">) {
  if (record.status === "running") return false;
  if (record.trigger === "single_update") return Boolean(record.filmIds?.trim());
  return record.hour !== 0;
}

export interface CollectHistoryRecord {
  id: number;
  batchId: string;
  parentBatchId?: string;
  trigger: CollectTriggerType;
  tag: string;
  hour: number;
  status: CollectHistoryStatus;
  sourceCount: number;
  successSources: number;
  failedSources: number;
  filmCount: number;
  filmIds?: string;
  filmName?: string;
  films?: CollectRecordFilm[];
  pageTotal: number;
  pageSuccess: number;
  pageFailed: number;
  durationMs: number;
  startedAt: string;
  finishedAt?: string;
  errorMsg?: string;
  detailJson?: string;
}

export interface CollectRecordFilm {
  mid: number;
  name: string;
}
