"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Skeleton } from "antd";
import {
  DashboardOutlined,
  PlayCircleOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import type { Overview } from "../types";
import styles from "./index.module.less";

interface GlobalOverviewBarProps {
  dayStr: string;
  refreshKey?: number;
}

export default function GlobalOverviewBar({
  dayStr,
  refreshKey = 0,
}: GlobalOverviewBarProps) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [loading, setLoading] = useState(false);
  const lastDayRef = useRef(dayStr);
  const inFlightRef = useRef(false);
  const reqSeqRef = useRef(0);

  const fetchData = useCallback(
    async (silent = false) => {
      if (inFlightRef.current) {
        return;
      }
      inFlightRef.current = true;
      const seq = ++reqSeqRef.current;
      if (!silent) {
        setLoading(true);
      }
      try {
        const res = await ApiGet<Overview>(`/manage/access/overview?day=${dayStr}`);
        if (seq !== reqSeqRef.current) {
          return;
        }
        if (res.code === 0 && res.data) {
          setOverview(res.data);
        }
      } catch (err) {
        console.error("fetch global overview failed:", err);
      } finally {
        if (seq === reqSeqRef.current) {
          inFlightRef.current = false;
          setLoading(false);
        }
      }
    },
    [dayStr],
  );

  useEffect(() => {
    fetchData(false);
  }, [fetchData]);

  useEffect(() => {
    if (refreshKey === 0) return;
    const dayChanged = lastDayRef.current !== dayStr;
    lastDayRef.current = dayStr;
    fetchData(!dayChanged);
  }, [fetchData, refreshKey, dayStr]);

  const totalPv = overview?.pv ?? 0;
  const totalUv = overview?.uv ?? 0;
  const playCount = overview?.action?.play ?? 0;
  const searchCount = overview?.action?.search ?? 0;
  const depth = totalUv > 0 ? (totalPv / totalUv).toFixed(1) : "0.0";

  return (
    <div className={styles.globalBarContainer}>
      <div className={styles.statsGrid}>
        {/* 卡片 1：全站总访问量 (PV) */}
        <div className={styles.statCard}>
          <div className={styles.statBody}>
            <span className={styles.statLabel}>全站总访问量 (PV)</span>
            {loading && !overview ? (
              <Skeleton.Input active size="small" style={{ width: 100, height: 26 }} />
            ) : (
              <div className={styles.statValue}>{totalPv.toLocaleString()}</div>
            )}
            <div className={styles.statSub}>
              <span>含全端交互与接口请求</span>
            </div>
          </div>
          <div className={`${styles.statIconWrap} ${styles.iconTotalPv}`}>
            <DashboardOutlined />
          </div>
        </div>

        {/* 卡片 2：全站独立访客 (UV) */}
        <div className={styles.statCard}>
          <div className={styles.statBody}>
            <span className={styles.statLabel}>全站独立访客 (UV)</span>
            {loading && !overview ? (
              <Skeleton.Input active size="small" style={{ width: 80, height: 26 }} />
            ) : (
              <div className={styles.statValue}>{totalUv.toLocaleString()}</div>
            )}
            <div className={styles.statSub}>
              <span>人均访问深度 {depth} 次/人</span>
            </div>
          </div>
          <div className={`${styles.statIconWrap} ${styles.iconTotalUv}`}>
            <UserOutlined />
          </div>
        </div>

        {/* 卡片 3：全站影视互动总量 */}
        <div className={styles.statCard}>
          <div className={styles.statBody}>
            <span className={styles.statLabel}>全站影视互动量</span>
            {loading && !overview ? (
              <Skeleton.Input active size="small" style={{ width: 90, height: 26 }} />
            ) : (
              <div className={styles.statValue}>{(playCount + searchCount).toLocaleString()}</div>
            )}
            <div className={styles.statSub}>
              <span>
                点播 {playCount.toLocaleString()} · 搜索 {searchCount.toLocaleString()}
              </span>
            </div>
          </div>
          <div className={`${styles.statIconWrap} ${styles.iconAction}`}>
            <PlayCircleOutlined />
          </div>
        </div>
      </div>
    </div>
  );
}
