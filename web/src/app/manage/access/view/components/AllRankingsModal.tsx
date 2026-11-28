"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Alert, Empty, Input, Modal, Pagination, Progress, Space, Spin, Tag, Typography } from "antd";
import { isSafeInternalHref } from "../constants";
import { ApiGet } from "@/lib/client-api";
import type { AnalyticsModule, AppPlatform, TopItem, TopPageResult } from "../types";

interface AllRankingsModalProps {
  open: boolean;
  onClose: () => void;
  kind: "play" | "search" | "classify" | "page";
  dayStr: string;
  module: AnalyticsModule;
  platform: AppPlatform;
}

const TITLE_MAP: Record<AllRankingsModalProps["kind"], string> = {
  play: "热门点播榜",
  search: "热门搜索榜",
  classify: "高频分类榜",
  page: "热门页面榜",
};

export default function AllRankingsModal({
  open,
  onClose,
  kind,
  dayStr,
  module,
  platform,
}: AllRankingsModalProps) {
  const [items, setItems] = useState<TopItem[]>([]);
  const [total, setTotal] = useState(0);
  const [cappedAt, setCappedAt] = useState(0);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const loadPage = useCallback(async () => {
    if (!open) return;
    setLoading(true);
    try {
      const params = new URLSearchParams({
        day: dayStr,
        kind,
        module,
        platform: module === "app" ? platform : "all",
        q: keyword.trim(),
        page: String(page),
        pageSize: String(pageSize),
      });
      const res = await ApiGet<TopPageResult>(`/manage/analytics/tops?${params.toString()}`);
      if (res.code === 0 && res.data) {
        setItems(res.data.items || []);
        setTotal(res.data.total || 0);
        setCappedAt(res.data.cappedAt || 0);
      } else {
        setItems([]);
        setTotal(0);
      }
    } catch {
      setItems([]);
      setTotal(0);
    } finally {
      setLoading(false);
    }
  }, [open, dayStr, kind, module, platform, keyword, page, pageSize]);

  useEffect(() => {
    if (open) {
      const timer = window.setTimeout(() => {
        void loadPage();
      }, 150);
      return () => window.clearTimeout(timer);
    }
  }, [open, loadPage]);

  useEffect(() => {
    if (open) {
      setPage(1);
      setKeyword("");
    }
  }, [open, kind, dayStr, module, platform]);

  const maxCount = items[0]?.count || 1;

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={720}
      destroyOnHidden
      title={
        <Space size={8}>
          <span>{TITLE_MAP[kind]}</span>
          <Tag>{dayStr}</Tag>
        </Space>
      }
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <Input.Search
            allowClear
            placeholder="搜索名称或关键字..."
            value={keyword}
            onChange={(e) => {
              setKeyword(e.target.value);
              setPage(1);
            }}
            style={{ width: 260 }}
          />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            共 {total} 条记录{cappedAt > 0 ? `（单日最多保留 Top ${cappedAt}）` : ""}
          </Typography.Text>
        </div>

        {cappedAt > 0 && cappedAt <= 50 && (
          <Alert
            type="info"
            showIcon
            message={`历史日期归档榜单最多保留前 ${cappedAt} 名`}
          />
        )}

        <Spin spinning={loading}>
          {items.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无匹配记录" />
          ) : (
            <div style={{ maxHeight: 420, overflowY: "auto", display: "flex", flexDirection: "column", gap: 10 }}>
              {items.map((item, idx) => {
                const rank = (page - 1) * pageSize + idx + 1;
                const pct = Math.max(4, Math.round((item.count / maxCount) * 100));
                const rawId = item.key.replace(/^id\s+/, "").trim();
                const isNumericId = /^\d+$/.test(rawId);

                let label: React.ReactNode = item.key;
                if (kind === "play") {
                  label = (
                    <Space size={6}>
                      <Link href={`/play?id=${rawId}`} target="_blank" style={{ fontWeight: 500 }}>
                        {item.title || `影片 #${rawId}`}
                      </Link>
                      {item.category && <Tag color="blue">{item.category}</Tag>}
                    </Space>
                  );
                } else if (kind === "search") {
                  label = (
                    <Link href={`/search?search=${encodeURIComponent(item.key)}`} target="_blank" style={{ fontWeight: 500 }}>
                      {item.key}
                    </Link>
                  );
                } else if (kind === "classify") {
                  const text = item.title || item.category || (isNumericId ? `分类 #${rawId}` : item.key);
                  label = isNumericId ? (
                    <Link href={`/category?pid=${encodeURIComponent(rawId)}`} target="_blank" style={{ fontWeight: 500 }}>
                      {text}
                    </Link>
                  ) : (
                    <Typography.Text strong>{text}</Typography.Text>
                  );
                } else if (isSafeInternalHref(item.key)) {
                  label = (
                    <Link href={item.key} target="_blank" style={{ fontWeight: 500 }}>
                      {item.key}
                    </Link>
                  );
                }

                return (
                  <div
                    key={`${item.key}-${rank}`}
                    style={{
                      padding: "8px 12px",
                      borderRadius: 8,
                      border: "1px solid var(--ant-color-border-secondary)",
                    }}
                  >
                    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: 8 }}>
                      <Space size={8}>
                        <Tag color={rank <= 3 ? "orange" : "default"} style={{ margin: 0 }}>
                          #{rank}
                        </Tag>
                        {label}
                      </Space>
                      <Typography.Text strong>{item.count.toLocaleString()} 次</Typography.Text>
                    </div>
                    <Progress percent={pct} showInfo={false} size="small" />
                  </div>
                );
              })}
            </div>
          )}
        </Spin>

        {total > 0 && (
          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <Pagination
              size="small"
              current={page}
              pageSize={pageSize}
              total={total}
              showSizeChanger
              pageSizeOptions={[10, 20, 50]}
              onChange={(nextPage, nextSize) => {
                setPage(nextPage);
                setPageSize(nextSize);
              }}
            />
          </div>
        )}
      </div>
    </Modal>
  );
}
