"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import Link from "next/link";
import { Modal, Input, Table, Typography, Button } from "antd";
import {
  VideoCameraOutlined,
  SearchOutlined,
  ExportOutlined,
  PlayCircleOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet } from "@/lib/client-api";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import styles from "./batch-films-modal.module.less";

const { Text } = Typography;

export interface BatchFilmsModalProps {
  open: boolean;
  batchId: string;
  sourceId: string;
  sourceName: string;
  onClose: () => void;
}

interface FilmItem {
  mid: number;
  name: string;
  sourceId?: string;
}

export function BatchFilmsModal({
  open,
  batchId,
  sourceId,
  sourceName,
  onClose,
}: BatchFilmsModalProps) {
  const [keyword, setKeyword] = useState("");
  const [current, setCurrent] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [list, setList] = useState<FilmItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  const fetchFilms = useCallback(
    async (pCurrent: number, pPageSize: number, pKeyword: string) => {
      if (!batchId) return;
      setLoading(true);
      try {
        const resp = await ApiGet<{
          list: FilmItem[];
          total: number;
          current: number;
          pageSize: number;
        }>("/manage/collect/history/films", {
          batchId,
          sourceId,
          current: pCurrent,
          pageSize: pPageSize,
          keyword: pKeyword.trim(),
        });
        if (resp.code === 0 && resp.data) {
          setList(resp.data.list || []);
          setTotal(resp.data.total || 0);
          setCurrent(resp.data.current || pCurrent);
          setPageSize(resp.data.pageSize || pPageSize);
        }
      } finally {
        setLoading(false);
      }
    },
    [batchId, sourceId],
  );

  useEffect(() => {
    if (open) {
      setKeyword("");
      setCurrent(1);
      void fetchFilms(1, 20, "");
    } else {
      setList([]);
      setTotal(0);
    }
  }, [open, fetchFilms]);

  const handleSearch = (val: string) => {
    setKeyword(val);
    setCurrent(1);
    void fetchFilms(1, pageSize, val);
  };

  const handlePageChange = (newPage: number, newSize: number) => {
    setCurrent(newPage);
    setPageSize(newSize);
    void fetchFilms(newPage, newSize, keyword);
  };

  const columns: ColumnsType<FilmItem> = useMemo(
    () => [
      {
        title: "#",
        key: "index",
        width: 60,
        align: "center",
        render: (_, __, index) => (
          <Text type="secondary">{(current - 1) * pageSize + index + 1}</Text>
        ),
      },
      {
        title: "影片名称",
        dataIndex: "name",
        key: "name",
        render: (name: string, record) => (
          <Link
            href={resolvePlayEntryPath(record.mid)}
            target="_blank"
            className={styles.filmTitleLink}
          >
            <VideoCameraOutlined className={styles.filmIcon} />
            <span className={styles.filmNameText}>{name}</span>
          </Link>
        ),
      },
      {
        title: "MID",
        dataIndex: "mid",
        key: "mid",
        width: 110,
        align: "center",
        render: (mid: number) => <Text type="secondary">#{mid}</Text>,
      },
      {
        title: "操作",
        key: "action",
        width: 90,
        align: "center",
        render: (_, record) => (
          <Button
            type="link"
            size="small"
            icon={<PlayCircleOutlined />}
            href={resolvePlayEntryPath(record.mid)}
            target="_blank"
            className={styles.playActionBtn}
          >
            播放
          </Button>
        ),
      },
    ],
    [current, pageSize],
  );

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={720}
      title={
        <div className={styles.modalHeader}>
          <div className={styles.modalTitle}>
            <VideoCameraOutlined className={styles.titleIcon} />
            <span>本次入库影片清单</span>
            <span className={styles.sourceSubtitle}>
              （{sourceName || sourceId || "全部站点"} · 共 {total.toLocaleString("zh-CN")} 部）
            </span>
          </div>
          <Link
            href={sourceId ? `/manage/film?sourceId=${encodeURIComponent(sourceId)}` : "/manage/film"}
            target="_blank"
            className={styles.jumpLink}
          >
            <span>前往影片库</span>
            <ExportOutlined />
          </Link>
        </div>
      }
      styles={{ body: { padding: "16px 20px" } }}
    >
      <div className={styles.modalBody}>
        <div className={styles.searchBar}>
          <Input
            placeholder="搜索片名..."
            prefix={<SearchOutlined style={{ color: "var(--ant-color-text-tertiary)" }} />}
            allowClear
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onPressEnter={() => handleSearch(keyword)}
            onBlur={() => handleSearch(keyword)}
          />
        </div>

        <Table<FilmItem>
          rowKey={(record) => `${record.sourceId ?? sourceId}-${record.mid}`}
          size="small"
          loading={loading}
          columns={columns}
          dataSource={list}
          scroll={{ y: 380 }}
          pagination={{
            current,
            pageSize,
            total,
            size: "small",
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50, 100],
            showTotal: (t) => `共 ${t} 部`,
            onChange: handlePageChange,
          }}
        />
      </div>
    </Modal>
  );
}
