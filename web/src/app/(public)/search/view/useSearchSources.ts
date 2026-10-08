"use client";

import { useCallback, useEffect, useRef, useState } from "react";

export type SearchSourceTab = {
  id?: string;
  name?: string;
  count?: number;
  loading?: boolean;
};

type SourceCache = {
  list: any[];
  page: any;
  error: string;
};

function sid(id?: string) {
  return String(id || "");
}

function seedTabs(sources: SearchSourceTab[], loadedId: string): SearchSourceTab[] {
  if (!Array.isArray(sources)) {
    return [];
  }
  return sources.map((tab) => ({
    ...tab,
    loading: sid(tab.id) !== loadedId,
  }));
}

function buildSearchUrl(keyword: string, current: string, sort: string, source: string) {
  const params = new URLSearchParams({
    search: keyword,
    current,
  });
  if (sort) {
    params.set("sort", sort);
  }
  if (source) {
    params.set("source", source);
  }
  return `/search?${params.toString()}`;
}

function syncSearchUrl(keyword: string, current: string, sort: string, source: string) {
  if (typeof window === "undefined") {
    return;
  }
  const next = buildSearchUrl(keyword, current, sort, source);
  const now = `${window.location.pathname}${window.location.search}`;
  if (now !== next) {
    window.history.replaceState(window.history.state, "", next);
  }
}

async function fetchSearchFilm(keyword: string, source: string, current: number, sort: string) {
  const params = new URLSearchParams({
    keyword,
    current: String(current),
    pageSize: "12",
  });
  if (source) {
    params.set("source", source);
  }
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

function emptySourceCache(error = ""): SourceCache {
  return {
    list: [],
    page: { current: 1, pageSize: 12, total: 0, pageCount: 0 },
    error,
  };
}

function cacheFromResult(result: any, fallbackList: any[] = []): SourceCache {
  const list = Array.isArray(result?.list) ? result.list : fallbackList;
  const page = result?.page || { current: 1, pageSize: 12, total: list.length, pageCount: list.length ? 1 : 0 };
  return { list, page, error: String(result?.error || "").trim() };
}

export default function useSearchSources({
  keyword,
  sort: propSort = "",
  source,
  current,
  data,
}: {
  keyword: string;
  sort?: string;
  source: string;
  current: string;
  data: any;
}) {
  const initialSource = sid(source) || sid(data?.sources?.[0]?.id);
  const [activeId, setActiveId] = useState(initialSource);
  const [activeSort, setActiveSort] = useState(propSort);
  const [tabs, setTabs] = useState<SearchSourceTab[]>(() => seedTabs(data?.sources || [], initialSource));
  const [list, setList] = useState<any[]>(() => (Array.isArray(data?.list) ? data.list : []));
  const [page, setPage] = useState<any>(() => data?.page || {});
  const [sourceError, setSourceError] = useState(() => String(data?.error || "").trim());
  const [listLoading, setListLoading] = useState(false);
  const cacheRef = useRef<Record<string, SourceCache>>({});
  const activeRef = useRef(initialSource);
  const sortRef = useRef(propSort);
  const genRef = useRef(0);

  const patchTab = (id: string, count: number) => {
    setTabs((prev) =>
      prev.map((tab) => (sid(tab.id) === id ? { ...tab, count, loading: false } : tab)),
    );
  };

  useEffect(() => {
    const seedId = sid(source) || sid(data?.sources?.[0]?.id);
    const gen = ++genRef.current;
    activeRef.current = seedId;
    sortRef.current = propSort;
    setActiveId(seedId);
    setActiveSort(propSort);

    const sources = Array.isArray(data?.sources) ? data.sources : [];
    const seeded = cacheFromResult(data, Array.isArray(data?.list) ? data.list : []);
    cacheRef.current = { [`${seedId}:${propSort}`]: seeded, [seedId]: seeded };
    setTabs(seedTabs(sources, seedId));
    setList(seeded.list);
    setPage(seeded.page);
    setSourceError(seeded.error || "");
    setListLoading(false);

    if (seedId && (!source || propSort)) {
      syncSearchUrl(keyword, current, propSort, seedId);
    }

    const trimmed = keyword.trim();
    if (!trimmed || sources.length <= 1) {
      return () => {
        genRef.current += 1;
      };
    }

    const pending = sources.map((tab: SearchSourceTab) => sid(tab.id)).filter((id: string) => id !== seedId);
    pending.forEach((id: string) => {
      void (async () => {
        const result = await fetchSearchFilm(trimmed, id, 1, propSort);
        if (gen !== genRef.current) {
          return;
        }
        const cached = result ? cacheFromResult(result) : emptySourceCache("搜索失败");
        cacheRef.current[`${id}:${propSort}`] = cached;
        cacheRef.current[id] = cached;
        const total = Number(cached.page?.total);
        patchTab(id, Number.isFinite(total) ? total : cached.list.length);
        if (activeRef.current === id) {
          setList(cached.list);
          setPage(cached.page);
          setSourceError(cached.error || "");
          setListLoading(false);
        }
      })();
    });

    return () => {
      genRef.current += 1;
    };
  }, [keyword, propSort, source, current, data]);

  const changeSort = useCallback(
    async (nextSort: string) => {
      if (nextSort === sortRef.current) {
        return;
      }
      sortRef.current = nextSort;
      setActiveSort(nextSort);
      const id = activeRef.current;
      const cacheKey = `${id}:${nextSort}`;
      const hit = cacheRef.current[cacheKey];
      if (hit) {
        setList(hit.list);
        setPage(hit.page);
        setSourceError(hit.error || "");
        setListLoading(false);
        syncSearchUrl(keyword, "1", nextSort, id);
        return;
      }

      setList([]);
      setPage({ current: 1, pageSize: 12, total: 0, pageCount: 0 });
      setSourceError("");
      setListLoading(true);
      syncSearchUrl(keyword, "1", nextSort, id);

      const gen = ++genRef.current;
      const trimmed = keyword.trim();
      if (!trimmed) {
        setListLoading(false);
        return;
      }

      const result = await fetchSearchFilm(trimmed, id, 1, nextSort);
      if (gen !== genRef.current || activeRef.current !== id) {
        return;
      }
      const cached = result ? cacheFromResult(result) : emptySourceCache("搜索失败");
      cacheRef.current[cacheKey] = cached;
      const total = Number(cached.page?.total);
      patchTab(id, Number.isFinite(total) ? total : cached.list.length);
      setList(cached.list);
      setPage(cached.page);
      setSourceError(cached.error || "");
      setListLoading(false);
    },
    [keyword],
  );

  const changeSource = useCallback(
    (id: string) => {
      if (id === activeRef.current) {
        return;
      }
      activeRef.current = id;
      setActiveId(id);
      const sort = sortRef.current;
      const cacheKey = `${id}:${sort}`;
      const hit = cacheRef.current[cacheKey] || cacheRef.current[id];
      syncSearchUrl(keyword, String(hit?.page?.current || 1), sort, id);
      if (hit) {
        setList(hit.list);
        setPage(hit.page);
        setSourceError(hit.error || "");
        setListLoading(false);
        return;
      }

      setList([]);
      setPage({ current: 1, pageSize: 12, total: 0, pageCount: 0 });
      setSourceError("");
      setListLoading(true);

      const gen = ++genRef.current;
      const trimmed = keyword.trim();
      if (!trimmed) {
        setListLoading(false);
        return;
      }
      void (async () => {
        const result = await fetchSearchFilm(trimmed, id, 1, sort);
        if (gen !== genRef.current || activeRef.current !== id) {
          return;
        }
        const cached = result ? cacheFromResult(result) : emptySourceCache("搜索失败");
        cacheRef.current[cacheKey] = cached;
        const total = Number(cached.page?.total);
        patchTab(id, Number.isFinite(total) ? total : cached.list.length);
        setList(cached.list);
        setPage(cached.page);
        setSourceError(cached.error || "");
        setListLoading(false);
      })();
    },
    [keyword],
  );

  const changePage = useCallback(
    async (pageNum: number) => {
      const id = activeRef.current;
      const sort = sortRef.current;
      const trimmed = keyword.trim();
      const gen = ++genRef.current;
      if (!trimmed) {
        return;
      }
      setListLoading(true);
      const result = await fetchSearchFilm(trimmed, id, pageNum, sort);
      if (gen !== genRef.current || activeRef.current !== id) {
        return;
      }
      const cached = result ? cacheFromResult(result) : emptySourceCache("搜索失败");
      setList(cached.list);
      setPage(cached.page);
      setSourceError(cached.error || "");
      setListLoading(false);
      syncSearchUrl(keyword, String(pageNum), sort, id);
    },
    [keyword],
  );

  return {
    sources: tabs,
    activeId,
    activeSort,
    list,
    page,
    sourceError,
    listLoading,
    changeSource,
    changePage,
    changeSort,
  };
}
