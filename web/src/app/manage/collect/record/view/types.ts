export type CollectRecordSourceStatus = "success" | "done" | "failed" | "stopped" | "running";

export interface CollectRecordSourceDetail {
  sourceId: string;
  sourceName: string;
  status: CollectRecordSourceStatus;
  pageTotal: number;
  pageCurrent: number;
  successCnt: number;
  failedCnt: number;
  filmsTotal: number;
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
  pageTotal: number;
  pageSuccess: number;
  pageFailed: number;
  durationMs: number;
  startedAt: string;
  finishedAt?: string;
  errorMsg?: string;
  detailJson?: string;
}
