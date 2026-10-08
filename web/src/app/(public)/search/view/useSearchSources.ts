"use client";

import { useCallback, useEffect, useRef, useState } from "react";

type ResultCache = {
  list: any[];
  page: any;
  error: string;
};

function buildSearchUrl(keyword: string, current: string, sort: string) {
  const params = new URLSearchParams({
    search: keyword,
    current,
  });
  if (sort) {
    params.set("sort", sort);
  }
  return `/search?${params.toString()}`;
}

function syncSearchUrl(keyword: string, current: string, sort: string) {
  if (typeof window === "undefined") {
    return;
  }
  const next = buildSearchUrl(keyword, current, sort);
  const now = `${window.location.pathname}${window.location.search}`;
  if (now !== next) {
    window.history.replaceState(window.history.state, "", next);
  }
}

async function fetchSearchFilm(keyword: string, current: number, sort: string) {
  const params = new URLSearchParams({
    keyword,
    current: String(current),
    pageSize: "12",
  });
  if (sort) {
    params.set("sort", sort);
  }
  try {
    const res = await fetch(`/api/searchFilm?${params.toString()}`, { credentials: "include" });
    if (!res.ok) {
      return null;
    }
    const json = await res.json();
    if (json?.code !== 0) {
      return null;
    }
    return json.data;
  } catch {
    return null;
  }
}

function emptyCache(error = ""): ResultCache {
  return {
    list: [],
    page: { current: 1, pageSize: 12, total: 0, pageCount: 0 },
    error,
  };
}

function cacheFromResult(result: any, fallbackList: any[] = []): ResultCache {
  const list = Array.isArray(result?.list) ? result.list : fallbackList;
  const page = result?.page || { current: 1, pageSize: 12, total: list.length, pageCount: list.length ? 1 : 0 };
  return { list, page, error: String(result?.error || "").trim() };
}

export default function useSearchSources({
  keyword,
  sort: propSort = "",
  current,
  data,
}: {
  keyword: string;
  sort?: string;
  current: string;
  data: any;
}) {
  const [activeSort, setActiveSort] = useState(propSort);
  const [list, setList] = useState<any[]>(() => (Array.isArray(data?.list) ? data.list : []));
  const [page, setPage] = useState<any>(() => data?.page || {});
  const [sourceError, setSourceError] = useState(() => String(data?.error || "").trim());
  const [listLoading, setListLoading] = useState(false);
  const cacheRef = useRef<Record<string, ResultCache>>({});
  const sortRef = useRef(propSort);
  const genRef = useRef(0);

  useEffect(() => {
    const gen = ++genRef.current;
    sortRef.current = propSort;
    setActiveSort(propSort);

    const seeded = cacheFromResult(data, Array.isArray(data?.list) ? data.list : []);
    cacheRef.current = { [propSort]: seeded };
    setList(seeded.list);
    setPage(seeded.page);
    setSourceError(seeded.error || "");
    setListLoading(false);
    syncSearchUrl(keyword, current, propSort);

    return () => {
      genRef.current = gen + 1;
    };
  }, [keyword, propSort, current, data]);

  const changeSort = useCallback(
    async (nextSort: string) => {
      if (nextSort === sortRef.current) {
        return;
      }
      sortRef.current = nextSort;
      setActiveSort(nextSort);
      const hit = cacheRef.current[nextSort];
      if (hit) {
        setList(hit.list);
        setPage(hit.page);
        setSourceError(hit.error || "");
        setListLoading(false);
        syncSearchUrl(keyword, "1", nextSort);
        return;
      }

      setList([]);
      setPage({ current: 1, pageSize: 12, total: 0, pageCount: 0 });
      setSourceError("");
      setListLoading(true);
      syncSearchUrl(keyword, "1", nextSort);

      const gen = ++genRef.current;
      const trimmed = keyword.trim();
      if (!trimmed) {
        setListLoading(false);
        return;
      }

      const result = await fetchSearchFilm(trimmed, 1, nextSort);
      if (gen !== genRef.current) {
        return;
      }
      const cached = result ? cacheFromResult(result) : emptyCache("搜索失败");
      cacheRef.current[nextSort] = cached;
      setList(cached.list);
      setPage(cached.page);
      setSourceError(cached.error || "");
      setListLoading(false);
    },
    [keyword],
  );

  const changePage = useCallback(
    async (pageNum: number) => {
      const sort = sortRef.current;
      const trimmed = keyword.trim();
      const gen = ++genRef.current;
      if (!trimmed) {
        return;
      }
      setListLoading(true);
      const result = await fetchSearchFilm(trimmed, pageNum, sort);
      if (gen !== genRef.current) {
        return;
      }
      const cached = result ? cacheFromResult(result) : emptyCache("搜索失败");
      setList(cached.list);
      setPage(cached.page);
      setSourceError(cached.error || "");
      setListLoading(false);
      syncSearchUrl(keyword, String(pageNum), sort);
    },
    [keyword],
  );

  return {
    activeSort,
    list,
    page,
    sourceError,
    listLoading,
    changePage,
    changeSort,
  };
}
