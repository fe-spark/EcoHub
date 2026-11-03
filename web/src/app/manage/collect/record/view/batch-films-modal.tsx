"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import Link from "next/link";
import { Alert, Modal, Input, Table, Typography, Button } from "antd";
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
  const [sample, setSample] = useState(false);
  const [errorText, setErrorText] = useState("");
  const [loading, setLoading] = useState(false);
  const appliedKeyword = useRef("");

  const fetchFilms = useCallback(
    async (pCurrent: number, pPageSize: number, pKeyword: string) => {
      if (!batchId) return;
      setLoading(true);
      setErrorText("");
      try {
        const resp = await ApiGet<{
          list: FilmItem[];
          total: number;
          current: number;
          pageSize: number;
          sample?: boolean;
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
          setSample(Boolean(resp.data.sample));
          setCurrent(resp.data.current || pCurrent);
          setPageSize(resp.data.pageSize || pPageSize);
          return;
        }
        setList([]);
        setTotal(0);
        setSample(false);
        setErrorText(resp.msg || "获取影片清单失败");
      } catch (err) {
        setList([]);
        setTotal(0);
        setSample(false);
        setErrorText(err instanceof Error && err.message ? err.message : "获取影片清单失败");
      } finally {
        setLoading(false);
      }
    },
    [batchId, sourceId],
  );

  useEffect(() => {
    if (open) {
      appliedKeyword.current = "";
      setKeyword("");
      setCurrent(1);
      setSample(false);
      setErrorText("");
      void fetchFilms(1, 20, "");
    } else {
      setList([]);
      setTotal(0);
      setSample(false);
      setErrorText("");
    }
  }, [open, fetchFilms]);

  const handleSearch = (val: string) => {
    const next = val.trim();
    if (next === appliedKeyword.current) {
      return;
    }
    appliedKeyword.current = next;
    setKeyword(next);
    setCurrent(1);
    void fetchFilms(1, pageSize, next);
  };

  const handlePageChange = (newPage: number, newSize: number) => {
    setCurrent(newPage);
    setPageSize(newSize);
    void fetchFilms(newPage, newSize, appliedKeyword.current);
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
            href={resolvePlayEntryPath(record.mid, { sourceId: record.sourceId || sourceId })}
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
            href={resolvePlayEntryPath(record.mid, { sourceId: record.sourceId || sourceId })}
            target="_blank"
            className={styles.playActionBtn}
          >
            播放
          </Button>
        ),
      },
    ],
    [current, pageSize, sourceId],
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
            onChange={(e) => {
              const val = e.target.value;
              setKeyword(val);
              if (val === "" && appliedKeyword.current !== "") {
                handleSearch("");
              }
            }}
            onPressEnter={() => handleSearch(keyword)}
          />
        </div>
        {errorText ? <Alert type="error" showIcon message={errorText} /> : null}
        {sample ? <Text type="secondary" className={styles.sampleHint}>历史样例，不是全量</Text> : null}

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
