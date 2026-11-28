"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ApiGet } from "@/lib/client-api";
import type { AnalyticsModule, AppPlatform, DashboardPayload, LogPageResult } from "./types";

interface UseDashboardOptions {
  dayStr: string;
  module: AnalyticsModule;
  platform: AppPlatform;
  refreshKey: number;
}

export function useAnalyticsDashboard({
  dayStr,
  module,
  platform,
  refreshKey,
}: UseDashboardOptions) {
  const [data, setData] = useState<DashboardPayload | null>(null);
  const [loading, setLoading] = useState(false);

  const [logs, setLogs] = useState<DashboardPayload["logs"]>([]);
  const [logsTotal, setLogsTotal] = useState(0);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logAction, setLogAction] = useState("all");
  const [logKeyword, setLogKeyword] = useState("");
  const [logPage, setLogPage] = useState(1);
  const [logPageSize, setLogPageSize] = useState(10);

  const lastParamsRef = useRef({ dayStr, module, platform });
  const isFirstMount = useRef(true);
  const reqSeqRef = useRef(0);
  const inFlightRef = useRef(false);

  // 独立流水查询序列号，避免慢响应覆盖最新筛选/翻页结果
  const logsReqSeqRef = useRef(0);
  const logFilterRef = useRef({ logPage, logPageSize, logAction, logKeyword });
  logFilterRef.current = { logPage, logPageSize, logAction, logKeyword };
  const logEffectInitRef = useRef(true);

  const fetchDashboard = useCallback(
    async (silent = false) => {
      if (silent && inFlightRef.current) return;
      inFlightRef.current = true;
      const seq = ++reqSeqRef.current;
      if (!silent) setLoading(true);
      try {
        const params = new URLSearchParams({
          day: dayStr,
          module,
          platform: module === "app" ? platform : "all",
        });
        const res = await ApiGet<DashboardPayload>(`/manage/analytics/dashboard?${params.toString()}`);
        if (seq !== reqSeqRef.current) return;
        if (res.code === 0 && res.data) {
          setData(res.data);
          // 仅当流水处于默认首页无筛选状态时直接复用聚合首屏流水
          const cur = logFilterRef.current;
          if (cur.logPage === 1 && cur.logPageSize === 10 && cur.logAction === "all" && !cur.logKeyword.trim()) {
            setLogs(
              (res.data.logs || []).map((item, idx) => ({
                ...item,
                _key: `${item.ts}-${item.path}-${item.ipPreview}-${idx}`,
              })),
            );
            setLogsTotal(res.data.logsTotal || 0);
          }
        }
      } catch {
        // 保持已有视图数据，不闪断界面
      } finally {
        if (seq === reqSeqRef.current) {
          inFlightRef.current = false;
          if (!silent) setLoading(false);
        }
      }
    },
    [dayStr, module, platform],
  );

  const fetchLogs = useCallback(
    async (silent = false) => {
      const seq = ++logsReqSeqRef.current;
      if (!silent) setLogsLoading(true);
      try {
        const params = new URLSearchParams({
          day: dayStr,
          module,
          platform: module === "app" ? platform : "all",
          action: logAction,
          q: logKeyword.trim(),
          page: String(logPage),
          pageSize: String(logPageSize),
        });
        const res = await ApiGet<LogPageResult>(`/manage/analytics/logs?${params.toString()}`);
        if (seq !== logsReqSeqRef.current) return;
        if (res.code === 0 && res.data) {
          setLogs(
            (res.data.list || []).map((item, idx) => ({
              ...item,
              _key: `${item.ts}-${item.path}-${item.ipPreview}-${idx}`,
            })),
          );
          setLogsTotal(res.data.total || 0);
        }
      } catch {
        // ignore
      } finally {
        if (seq === logsReqSeqRef.current && !silent) {
          setLogsLoading(false);
        }
      }
    },
    [dayStr, module, platform, logAction, logKeyword, logPage, logPageSize],
  );

  useEffect(() => {
    const changed =
      lastParamsRef.current.dayStr !== dayStr ||
      lastParamsRef.current.module !== module ||
      lastParamsRef.current.platform !== platform;
    lastParamsRef.current = { dayStr, module, platform };
    const showLoading = isFirstMount.current || changed;
    isFirstMount.current = false;
    void fetchDashboard(!showLoading);

    // 若当前流水存在自定义筛选或非首页，手动/自动刷新时同步静默刷新流水
    const cur = logFilterRef.current;
    const isCustomLog =
      cur.logPage !== 1 || cur.logPageSize !== 10 || cur.logAction !== "all" || Boolean(cur.logKeyword.trim());
    if (isCustomLog) {
      void fetchLogs(!changed);
    }
    // 仅在日期、终端维度或刷新信号变化时触发看板全量请求，不监听流水筛选状态
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fetchDashboard, refreshKey, dayStr, module, platform]);

  useEffect(() => {
    if (logEffectInitRef.current) {
      logEffectInitRef.current = false;
      return;
    }
    const timer = window.setTimeout(() => {
      void fetchLogs(false);
    }, 200);
    return () => window.clearTimeout(timer);
    // 仅响应流水自身筛选/分页参数变更，避免与看板刷新重入
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [logPage, logPageSize, logAction, logKeyword]);

  const handleLogFilterChange = (nextAction: string, nextKeyword: string) => {
    setLogAction(nextAction);
    setLogKeyword(nextKeyword);
    setLogPage(1);
  };

  return {
    data,
    loading,
    logs,
    logsTotal,
    logsLoading,
    logAction,
    logKeyword,
    logPage,
    logPageSize,
    setLogPage,
    setLogPageSize,
    handleLogFilterChange,
  };
}
