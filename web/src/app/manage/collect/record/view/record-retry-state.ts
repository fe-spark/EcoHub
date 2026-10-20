import type { FailRecord } from "./types";

export interface RecordRetryEvent {
  id: number;
  retrying: boolean;
  removed: boolean;
  hasResult: boolean;
  status: number;
  retryCount: number;
  cause: string;
}

export interface RecordRetryView {
  records: FailRecord[];
  retrying: Set<number>;
  refetch: boolean;
}

export function applyRecordRetry(
  records: FailRecord[],
  retrying: Set<number>,
  ev: RecordRetryEvent,
  statusFilter: number,
): RecordRetryView {
  const nextRetrying = new Set(retrying);
  if (ev.retrying) {
    nextRetrying.add(ev.id);
  } else {
    nextRetrying.delete(ev.id);
  }

  const index = records.findIndex((row) => row.ID === ev.id);
  if (index < 0) {
    return { records, retrying: nextRetrying, refetch: false };
  }
  if (ev.removed) {
    const next = records.slice();
    next.splice(index, 1);
    return { records: next, retrying: nextRetrying, refetch: true };
  }
  if (!ev.hasResult) {
    return { records, retrying: nextRetrying, refetch: false };
  }
  if (statusFilter >= 0 && ev.status !== statusFilter) {
    const next = records.slice();
    next.splice(index, 1);
    return { records: next, retrying: nextRetrying, refetch: true };
  }

  const next = records.slice();
  next[index] = {
    ...records[index],
    status: ev.status,
    retryCount: ev.retryCount,
    cause: ev.cause,
  };
  return { records: next, retrying: nextRetrying, refetch: false };
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
