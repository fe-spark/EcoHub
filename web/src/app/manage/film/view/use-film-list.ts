"use client";

import { useCallback, useEffect, useRef, type Dispatch, type SetStateAction } from "react";
import { ApiGet } from "@/lib/client-api";
import { decideFilmPageUpdate, isCurrentFilmRequest } from "./film-list-request";

interface FilmPaging {
  current?: number;
  pageSize?: number;
  total?: number;
}

interface IndexingProgress {
  total?: number;
  current?: number;
  percent?: number;
  status?: string;
}

interface UseFilmListOptions {
  page: FilmPaging;
  params: Record<string, any>;
  setLoading: (loading: boolean) => void;
  setList: (list: any[]) => void;
  setPage: (page: any) => void;
  setParams: Dispatch<SetStateAction<any>>;
  setTableSourceId: (id: string) => void;
  setIndexing: (indexing: boolean) => void;
  setIndexingProgress: (progress: IndexingProgress | null) => void;
  notifyError: (text: string) => void;
}

export function useFilmList({
  page,
  params,
  setLoading,
  setList,
  setPage,
  setParams,
  setTableSourceId,
  setIndexing,
  setIndexingProgress,
  notifyError,
}: UseFilmListOptions) {
  const indexingTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const filmReqSeqRef = useRef(0);
  const indexingActiveRef = useRef(false);
  const aliveRef = useRef(true);

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
      filmReqSeqRef.current += 1;
      if (indexingTimerRef.current) {
        clearTimeout(indexingTimerRef.current);
        indexingTimerRef.current = null;
      }
    };
  }, []);

  const getFilmPage = useCallback(
    async (p?: FilmPaging, overrideParams?: Record<string, any>, silent = false) => {
      if (!aliveRef.current) return;
      if (indexingTimerRef.current) {
        clearTimeout(indexingTimerRef.current);
        indexingTimerRef.current = null;
      }
      const seq = ++filmReqSeqRef.current;
      if (!aliveRef.current) return;
      if (!silent) setLoading(true);
      const pg = p || page;
      const reqParams = overrideParams || params;
      const current = () => isCurrentFilmRequest(seq, filmReqSeqRef.current);
      try {
        const resp = await ApiGet("/manage/film/search/list", {
          ...reqParams,
          current: pg.current,
          pageSize: pg.pageSize,
        });
        if (!current()) return;
        const decision = decideFilmPageUpdate({
          silent,
          ok: resp.code === 0,
          indexing: Boolean(resp.data?.indexing),
          wasIndexing: indexingActiveRef.current,
        });
        if (decision.clearIndexing) {
          notifyError(resp.msg || "影片列表获取失败");
          indexingActiveRef.current = false;
          setIndexing(false);
          setIndexingProgress(null);
          return;
        }
        indexingActiveRef.current = decision.indexing;
        setIndexing(decision.indexing);
        setIndexingProgress(resp.data?.indexingProgress || null);
        if (decision.applyList) {
          setList(
            resp.data?.list?.map((item: any) => ({
              ...item,
              year: item.year <= 0 ? "未知" : item.year,
              score: item.score === 0 ? "暂无" : item.score,
            })) || [],
          );
          setPage(resp.data.params.paging);
          const activeSource = reqParams.sourceId || resp.data.params?.sourceId || "";
          if (activeSource) {
            setTableSourceId(activeSource);
            if (!reqParams.sourceId) {
              setParams((prev: any) => ({ ...prev, sourceId: activeSource }));
            }
          }
        }
        if (!aliveRef.current || !current()) return;
        if (decision.schedulePoll) {
          indexingTimerRef.current = setTimeout(() => {
            void getFilmPage(pg, reqParams, true);
          }, 2500);
        } else if (decision.refreshList) {
          void getFilmPage(pg, reqParams, false);
        }
      } catch {
        if (!current()) return;
        notifyError("影片列表获取失败");
        indexingActiveRef.current = false;
        setIndexing(false);
        setIndexingProgress(null);
      } finally {
        if (current() && !silent) setLoading(false);
      }
    },
    [notifyError, page, params, setIndexing, setIndexingProgress, setList, setLoading, setPage, setParams, setTableSourceId],
  );

  return getFilmPage;
}
