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
  if (!source && sort) {
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
  } else if (sort) {
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

function cacheFromResult(result: any, fallbackList: any[] = []): SourceCache {
  const list = Array.isArray(result?.list) ? result.list : fallbackList;
  const page = result?.page || { current: 1, pageSize: 12, total: list.length, pageCount: list.length ? 1 : 0 };
  return { list, page };
}

export default function useSearchSources({
  keyword,
  sort,
  source,
  current,
  data,
}: {
  keyword: string;
  sort: string;
  source: string;
  current: string;
  data: any;
}) {
  const loadedId = sid(source);
  const [activeId, setActiveId] = useState(loadedId);
  const [tabs, setTabs] = useState<SearchSourceTab[]>(() => seedTabs(data?.sources || [], loadedId));
  const [list, setList] = useState<any[]>(() => (Array.isArray(data?.list) ? data.list : []));
  const [page, setPage] = useState<any>(() => data?.page || {});
  const [listLoading, setListLoading] = useState(false);
  const cacheRef = useRef<Record<string, SourceCache>>({});
  const activeRef = useRef(loadedId);
  const genRef = useRef(0);

  const applyCache = (id: string) => {
    const hit = cacheRef.current[id];
    if (hit) {
      setList(hit.list);
      setPage(hit.page);
      setListLoading(false);
      return true;
    }
    setList([]);
    setPage({ current: 1, pageSize: 12, total: 0, pageCount: 0 });
    setListLoading(true);
    return false;
  };

  const patchTab = (id: string, count: number) => {
    setTabs((prev) =>
      prev.map((tab) => (sid(tab.id) === id ? { ...tab, count, loading: false } : tab)),
    );
  };

  useEffect(() => {
    const seedId = sid(source);
    const gen = ++genRef.current;
    activeRef.current = seedId;
    setActiveId(seedId);

    const sources = Array.isArray(data?.sources) ? data.sources : [];
    const seeded = cacheFromResult(data, Array.isArray(data?.list) ? data.list : []);
    cacheRef.current = { [seedId]: seeded };
    setTabs(seedTabs(sources, seedId));
    setList(seeded.list);
    setPage(seeded.page);
    setListLoading(false);

    const trimmed = keyword.trim();
    if (!trimmed || sources.length <= 1) {
      return () => {
        genRef.current += 1;
      };
    }

    const pending = sources.map((tab: SearchSourceTab) => sid(tab.id)).filter((id: string) => id !== seedId);
    pending.forEach((id: string) => {
      void (async () => {
        const result = await fetchSearchFilm(trimmed, id, 1, id ? "" : sort);
        if (gen !== genRef.current) {
          return;
        }
        const cached = cacheFromResult(result);
        cacheRef.current[id] = cached;
        const total = Number(cached.page?.total);
        patchTab(id, Number.isFinite(total) ? total : cached.list.length);
        if (activeRef.current === id) {
          setList(cached.list);
          setPage(cached.page);
          setListLoading(false);
        }
      })();
    });

    return () => {
      genRef.current += 1;
    };
  }, [keyword, sort, source, current, data]);

  const changeSource = useCallback(
    (id: string) => {
      if (id === activeRef.current) {
        return;
      }
      activeRef.current = id;
      setActiveId(id);
      applyCache(id);
      const hit = cacheRef.current[id];
      syncSearchUrl(keyword, String(hit?.page?.current || 1), sort, id);
    },
    [keyword, sort],
  );

  const changePage = useCallback(
    async (pageNum: number) => {
      const id = activeRef.current;
      const trimmed = keyword.trim();
      const gen = genRef.current;
      if (!trimmed) {
        return;
      }
      setListLoading(true);
      const result = await fetchSearchFilm(trimmed, id, pageNum, id ? "" : sort);
      if (gen !== genRef.current || activeRef.current !== id) {
        return;
      }
      const cached = cacheFromResult(result);
      cacheRef.current[id] = cached;
      const total = Number(cached.page?.total);
      patchTab(id, Number.isFinite(total) ? total : cached.list.length);
      setList(cached.list);
      setPage(cached.page);
      setListLoading(false);
      syncSearchUrl(keyword, String(pageNum), sort, id);
    },
    [keyword, sort],
  );

  return {
    sources: tabs,
    activeId,
    list,
    page,
    listLoading,
    changeSource,
    changePage,
  };
}
