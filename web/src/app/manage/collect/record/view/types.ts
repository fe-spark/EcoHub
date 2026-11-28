export type CollectRecordSourceStatus =
  | "success"
  | "done"
  | "partial"
  | "failed"
  | "stopped"
  | "interrupted"
  | "running";

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
  | "partial_stopped"
  | "failed"
  | "stopped"
  | "interrupted";

export type CollectTriggerType = "manual" | "cron" | "retry" | "single_update" | string;

export function resolveBatchStatus(record: { status: string }): CollectHistoryStatus {
  if (record.status === "done") return "success";
  switch (record.status) {
    case "running":
    case "success":
    case "partial":
    case "partial_stopped":
    case "failed":
    case "stopped":
    case "interrupted":
      return record.status;
    default:
      return "success";
  }
}

export function isSingleFilmRecord(record: { trigger?: string; tag?: string }): boolean {
  return record.trigger === "single_update" || record.tag === "single_update";
}

export function canRetryHistoryRecord(
  record: Pick<CollectHistoryRecord, "status" | "trigger" | "tag" | "hour" | "films" | "filmName">,
) {
  if (record.status === "running") return false;
  if (isSingleFilmRecord(record)) {
    return (record.films?.length ?? 0) > 0 || Boolean(record.filmName?.trim());
  }
  const resolved = resolveBatchStatus(record);
  return record.hour !== 0 && (resolved === "failed" || resolved === "partial");
}

export function canRetryHistorySource(
  record: Pick<CollectHistoryRecord, "status" | "trigger" | "tag" | "hour" | "films" | "filmName">,
) {
  if (record.status === "running") return false;
  if (isSingleFilmRecord(record)) {
    return (record.films?.length ?? 0) > 0 || Boolean(record.filmName?.trim());
  }
  return record.hour !== 0;
}

export interface CollectRecordFilm {
  mid: number;
  name: string;
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
