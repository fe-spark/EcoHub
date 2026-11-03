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
  pageTotal: number;
  pageCurrent: number;
  successCnt: number;
  failedCnt: number;
  filmsTotal: number;
  films?: CollectRecordFilm[];
  error?: string;
}

export type CollectHistoryStatus =
  | "running"
  | "success"
  | "partial"
  | "failed"
  | "stopped"
  | "interrupted";

export type CollectTriggerType = "manual" | "cron" | "retry" | "single_update" | "recover" | string;

export interface SourceIncident {
  sourceId: string;
  sourceName: string;
  consecutiveFailures: number;
  latestFailedAt: string;
  latestError?: string;
  latestBatchId: string;
  tripped: boolean;
}

export function canRetryHistoryRecord(record: Pick<CollectHistoryRecord, "status" | "failedSources" | "trigger" | "hour" | "filmIds">) {
  if (record.status === "running" || record.failedSources <= 0) return false;
  if (record.trigger === "single_update") return Boolean(record.filmIds?.trim());
  return record.hour !== 0;
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
