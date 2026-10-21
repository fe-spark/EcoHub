import type { FailRecord } from "./types";

export interface RecordRetryEvent {
  id: number;
  retrying: boolean;
  hasResult: boolean;
  status: number;
  retryCount: number;
  cause: string;
}

export interface RecordRetryView {
  records: FailRecord[];
  retrying: Set<number>;
}

/** 重试结果只改当前行。行留在原地，避免成功后抽行走位。 */
export function applyRecordRetry(
  records: FailRecord[],
  retrying: Set<number>,
  ev: RecordRetryEvent,
): RecordRetryView {
  const nextRetrying = new Set(retrying);
  if (ev.retrying) {
    nextRetrying.add(ev.id);
  } else {
    nextRetrying.delete(ev.id);
  }

  const index = records.findIndex((row) => row.ID === ev.id);
  if (index < 0 || !ev.hasResult) {
    return { records, retrying: nextRetrying };
  }

  const next = records.slice();
  next[index] = {
    ...records[index],
    status: ev.status,
    retryCount: ev.retryCount,
    cause: ev.cause,
  };
  return { records: next, retrying: nextRetrying };
}

/** 当前页里仍在重试队列中的 id。其它页的 id 不保留。 */
export function applyRecordSnapshot(pageIds: number[], inflightIds: number[]): Set<number> {
  const inflight = new Set(inflightIds);
  const next = new Set<number>();
  for (const id of pageIds) {
    if (inflight.has(id)) {
      next.add(id);
    }
  }
  return next;
}
