export interface FilmPageDecision {
  applyList: boolean;
  indexing: boolean;
  schedulePoll: boolean;
  refreshList: boolean;
  clearIndexing: boolean;
}

export function isCurrentFilmRequest(seq: number, current: number): boolean {
  return seq === current;
}

/** 静默刷新只更新索引进度。索引结束再拉一次列表。失败则关掉构建告警。 */
export function decideFilmPageUpdate(input: {
  silent: boolean;
  ok: boolean;
  indexing: boolean;
  wasIndexing: boolean;
}): FilmPageDecision {
  if (!input.ok) {
    return {
      applyList: false,
      indexing: false,
      schedulePoll: false,
      refreshList: false,
      clearIndexing: true,
    };
  }
  return {
    applyList: !input.silent,
    indexing: input.indexing,
    schedulePoll: input.indexing,
    refreshList: input.silent && input.wasIndexing && !input.indexing,
    clearIndexing: false,
  };
}
