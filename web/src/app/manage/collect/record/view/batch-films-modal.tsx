"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import Link from "next/link";
import { Alert, Modal, Input, Button, Empty, Pagination, Spin, Space } from "antd";
import {
  VideoCameraOutlined,
  SearchOutlined,
  ExportOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import styles from "./batch-films-modal.module.less";

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
  const [pageSize, setPageSize] = useState(10);
  const [list, setList] = useState<FilmItem[]>([]);
  const [total, setTotal] = useState(0);
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
          return;
        }
        setList([]);
        setTotal(0);
        setErrorText(resp.msg || "获取影片清单失败");
      } catch (err) {
        setList([]);
        setTotal(0);
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
      setErrorText("");
      void fetchFilms(1, 10, "");
    } else {
      setList([]);
      setTotal(0);
      setErrorText("");
    }
  }, [open, fetchFilms]);

  const handleExecuteSearch = () => {
    const next = keyword.trim();
    appliedKeyword.current = next;
    setCurrent(1);
    void fetchFilms(1, pageSize, next);
  };

  const handleResetSearch = () => {
    appliedKeyword.current = "";
    setKeyword("");
    setCurrent(1);
    void fetchFilms(1, pageSize, "");
  };

  const handlePageChange = (newPage: number, newSize: number) => {
    setCurrent(newPage);
    setPageSize(newSize);
    void fetchFilms(newPage, newSize, appliedKeyword.current);
  };

  const sourceDesc = sourceName || sourceId ? `来源站点：${sourceName || sourceId}` : "涵盖全部站点";
  const filmManageUrl = sourceId ? `/manage/film?sourceId=${encodeURIComponent(sourceId)}` : "/manage/film";

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={680}
      title={
        <div className={styles.modalHeader}>
          <div className={styles.headerIconWrap}>
            <VideoCameraOutlined />
          </div>
          <div className={styles.headerTextGroup}>
            <div className={styles.headerTitleRow}>
              <span className={styles.headerTitle}>更新影片清单</span>
            </div>
            <div className={styles.headerSubtitle}>
              {sourceDesc} · 共 {total.toLocaleString("zh-CN")} 部更新影片
            </div>
          </div>
        </div>
      }
    >
      <div className={styles.modalBody}>
        {errorText ? <Alert type="error" showIcon message={errorText} className={styles.errorAlert} /> : null}

        <div className={styles.toolbar}>
          <Space size={8} className={styles.filterGroup}>
            <Input
              placeholder="输入片名搜索..."
              allowClear
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              className={styles.searchInput}
            />
            <Button
              type="primary"
              icon={<SearchOutlined />}
              onClick={handleExecuteSearch}
              loading={loading}
            >
              搜索
            </Button>
            <Button
              icon={<ReloadOutlined />}
              onClick={handleResetSearch}
            >
              重置
            </Button>
          </Space>
          <Button
            icon={<ExportOutlined />}
            href={filmManageUrl}
            target="_blank"
            className={styles.jumpBtn}
          >
            前往影片库
          </Button>
        </div>

        <Spin spinning={loading}>
          {list.length === 0 && !loading ? (
            <div className={styles.emptyWrapper}>
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  appliedKeyword.current ? (
                    <span>
                      未搜索到与「<strong>{appliedKeyword.current}</strong>」匹配的影片
                    </span>
                  ) : (
                    <span>该站点在本次采集批次中未产生新增或更新影片</span>
                  )
                }
              >
                {appliedKeyword.current ? (
                  <Button size="small" icon={<ReloadOutlined />} onClick={handleResetSearch}>
                    清除搜索条件
                  </Button>
                ) : null}
              </Empty>
            </div>
          ) : (
            <div className={styles.listWrapper}>
              {list.map((film, index) => {
                const num = (current - 1) * pageSize + index + 1;
                const playUrl = resolvePlayEntryPath(film.mid, { sourceId: film.sourceId || sourceId });
                return (
                  <div key={`${film.sourceId ?? sourceId}-${film.mid}`} className={styles.listItem}>
                    <div className={styles.rankBadge}>{num}</div>
                    <div className={styles.filmInfo}>
                      <Link href={playUrl} target="_blank" className={styles.filmName} title={film.name}>
                        {film.name}
                      </Link>
                      <span className={styles.midTag}>#{film.mid}</span>
                    </div>
                    <Button
                      type="primary"
                      ghost
                      size="small"
                      icon={<PlayCircleOutlined />}
                      href={playUrl}
                      target="_blank"
                      className={styles.playBtn}
                    >
                      播放
                    </Button>
                  </div>
                );
              })}
            </div>
          )}
        </Spin>

        {total > pageSize && (
          <div className={styles.paginationWrapper}>
            <Pagination
              current={current}
              pageSize={pageSize}
              total={total}
              size="small"
              showSizeChanger
              pageSizeOptions={[10, 20, 50, 100]}
              showTotal={(t) => `共 ${t.toLocaleString("zh-CN")} 部`}
              onChange={handlePageChange}
            />
          </div>
        )}
      </div>
    </Modal>
  );
}
