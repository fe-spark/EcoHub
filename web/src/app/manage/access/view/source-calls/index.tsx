"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Card, Skeleton, Space, Tag, Tooltip, Typography } from "antd";
import { PlaySquareOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import CardEmpty, { fillCardStyles } from "../card-empty";
import styles from "./index.module.less";

const PALETTE = [
  "#fa8c16", // 橙金
  "#1677ff", // 科技蓝
  "#52c41a", // 翡翠绿
  "#722ed1", // 极客紫
  "#13c2c2", // 明青
  "#eb2f96", // 品红
];

type SourceCallRow = {
  id: string;
  name: string;
  enabled: boolean;
  isPrimary: boolean;
  count: number;
};

export default function SourceCalls({
  dayStr,
  refreshKey,
}: {
  dayStr: string;
  refreshKey?: number;
}) {
  const [rows, setRows] = useState<SourceCallRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [hoveredId, setHoveredId] = useState<string | null>(null);
  const reqSeq = useRef(0);

  const load = useCallback(async () => {
    const seq = ++reqSeq.current;
    setLoading(true);
    try {
      const res = await ApiGet<{ list: SourceCallRow[] }>(`/manage/access/sources?day=${dayStr}`);
      if (seq !== reqSeq.current) {
        return;
      }
      if (res.code === 0 && res.data) {
        setRows(res.data.list || []);
      }
    } catch {
      if (seq === reqSeq.current) {
        setRows([]);
      }
    } finally {
      if (seq === reqSeq.current) {
        setLoading(false);
      }
    }
  }, [dayStr]);

  useEffect(() => {
    void load();
  }, [load, refreshKey]);

  const { validRows, totalCount } = useMemo(() => {
    const valid = (rows || []).filter((r) => (r.count || 0) > 0);
    const sum = valid.reduce((acc, r) => acc + (r.count || 0), 0);
    return { validRows: valid, totalCount: sum };
  }, [rows]);

  return (
    <Card
      styles={fillCardStyles}
      title={
        <Space size={8} align="center">
          <PlaySquareOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
          <span>片源线路点播分布</span>
        </Space>
      }
      extra={
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          按影片点播所选用的线路统计
        </Typography.Text>
      }
    >
      {loading && validRows.length === 0 ? (
        <Skeleton active paragraph={{ rows: 3 }} />
      ) : validRows.length === 0 ? (
        <CardEmpty description="今日暂无片源点播记录" />
      ) : (
        <div className={styles.container}>
          {/* 顶部：全景分段比例胶囊条 */}
          <div className={styles.topSection}>
            <div className={styles.ratioHeader}>
              <span className={styles.ratioTitle}>线路点播份额</span>
              <span className={styles.ratioTotal}>
                点播总计：<b>{totalCount.toLocaleString()}</b> 次
              </span>
            </div>

            <div className={styles.segmentTrack}>
              {validRows.map((item, idx) => {
                const pct = totalCount > 0 ? (item.count / totalCount) * 100 : 0;
                const color = PALETTE[idx % PALETTE.length];
                const isActive = hoveredId === item.id;
                return (
                  <Tooltip
                    key={item.id}
                    title={`${item.name || item.id} · ${pct.toFixed(1)}% (${item.count.toLocaleString()} 次)`}
                  >
                    <div
                      className={`${styles.segmentSlice} ${isActive ? styles.segmentActive : ""}`}
                      style={{
                        width: `${pct}%`,
                        background: `linear-gradient(90deg, ${color}cc, ${color})`,
                        boxShadow: isActive ? `0 0 10px ${color}` : undefined,
                      }}
                      onMouseEnter={() => setHoveredId(item.id)}
                      onMouseLeave={() => setHoveredId(null)}
                    />
                  </Tooltip>
                );
              })}
            </div>
          </div>

          {/* 下方：按调用量排行的横向进度列表 */}
          <div className={styles.rankedList}>
            {validRows.map((item, idx) => {
              const pct = totalCount > 0 ? (item.count / totalCount) * 100 : 0;
              const color = PALETTE[idx % PALETTE.length];
              const isActive = hoveredId === item.id;
              const isDimmed = hoveredId !== null && !isActive;
              const rankCls =
                idx === 0
                  ? styles.rank1
                  : idx === 1
                    ? styles.rank2
                    : idx === 2
                      ? styles.rank3
                      : "";

              return (
                <div
                  key={item.id}
                  className={`${styles.rankedRow} ${isActive ? styles.rowActive : ""} ${
                    isDimmed ? styles.rowDimmed : ""
                  }`}
                  onMouseEnter={() => setHoveredId(item.id)}
                  onMouseLeave={() => setHoveredId(null)}
                >
                  {/* 左侧：排名、源名称与标签 */}
                  <div className={styles.rowMeta}>
                    <span className={`${styles.rankNum} ${rankCls}`}>#{idx + 1}</span>
                    <div className={styles.sourceNameWrap}>
                      <span className={styles.sourceName} title={item.name || item.id}>
                        {item.name || item.id}
                      </span>
                      {item.isPrimary ? (
                        <Tag color="gold" style={{ marginInlineStart: 2 }}>
                          首选站
                        </Tag>
                      ) : null}
                      {!item.enabled ? (
                        <Tag style={{ marginInlineStart: 2 }}>已停用</Tag>
                      ) : null}
                    </div>
                  </div>

                  {/* 中间：进度条 */}
                  <div className={styles.rowBarTrack}>
                    <div
                      className={styles.rowBarFill}
                      style={{
                        width: `${pct}%`,
                        background: `linear-gradient(90deg, ${color}99, ${color})`,
                      }}
                    />
                  </div>

                  {/* 右侧：次数与百分比 */}
                  <div className={styles.rowStat}>
                    <span className={styles.countText}>{item.count.toLocaleString()} 次</span>
                    <span className={styles.pctText}>({pct.toFixed(1)}%)</span>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </Card>
  );
}
