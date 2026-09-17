"use client";

import React, { useCallback, useEffect, useState, useRef } from "react";
import {
  Modal,
  Spin,
  Empty,
  Pagination,
  Button,
  Segmented,
  Tooltip,
  Input,
  Space,
  Tag,
} from "antd";
import type { InputRef } from "antd";
import {
  CheckOutlined,
  SearchOutlined,
  ReloadOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { FilmOption } from "@/app/manage/banners/view/types";
import styles from "./index.module.less";

interface FilmPickerProps {
  open: boolean;
  title?: string;
  selectedMid?: number;
  onCancel: () => void;
  onSelect: (film: FilmOption) => void;
}

export default function FilmPicker({
  open,
  title = "从片库选择影片",
  selectedMid,
  onCancel,
  onSelect,
}: FilmPickerProps) {
  const [list, setList] = useState<FilmOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedFilm, setSelectedFilm] = useState<FilmOption | null>(null);
  const [keyword, setKeyword] = useState("");
  const [inputValue, setInputValue] = useState("");
  const [sortField, setSortField] = useState<string>("");
  const [page, setPage] = useState({ current: 1, pageSize: 15, total: 0 });

  const inputRef = useRef<InputRef>(null);

  const fetchList = useCallback(
    async (current = 1, query = "", sort = "") => {
      setLoading(true);
      try {
        const resp = await ApiGet("/searchFilm", {
          keyword: query,
          current,
          pageSize: 15,
          sort: sort || undefined,
        });
        if (resp.code === 0 && resp.data) {
          const rawList = (resp.data.list || []) as FilmOption[];
          setList(
            rawList.map((f) => ({
              ...f,
              label: f.name || "未知影片",
              value: f.id,
            })),
          );
          if (resp.data.page) {
            setPage({
              current: resp.data.page.current || current,
              pageSize: resp.data.page.pageSize || 15,
              total: resp.data.page.total || 0,
            });
          }
        } else {
          setList([]);
          setPage((p) => ({ ...p, total: 0 }));
        }
      } catch {
        setList([]);
        setPage((p) => ({ ...p, total: 0 }));
      } finally {
        setLoading(false);
      }
    },
    [],
  );

  useEffect(() => {
    if (open) {
      setSelectedFilm(null);
      setInputValue("");
      setKeyword("");
      setSortField("");
      setList([]);
      fetchList(1, "", "");
      setTimeout(() => {
        inputRef.current?.focus();
      }, 100);
    }
  }, [open, fetchList]);

  // 当初次打开时若有预选 mid，尝试从当前列表中匹配
  useEffect(() => {
    if (selectedMid && list.length > 0 && !selectedFilm) {
      const match = list.find((item) => String(item.id) === String(selectedMid));
      if (match) {
        setSelectedFilm(match);
      }
    }
  }, [selectedMid, list, selectedFilm]);

  const handleSearch = (value: string) => {
    const kw = value.trim();
    setKeyword(kw);
    setInputValue(value);
    setList([]);
    fetchList(1, kw, sortField);
  };

  const handleResetSearch = () => {
    setInputValue("");
    setKeyword("");
    setList([]);
    fetchList(1, "", sortField);
    inputRef.current?.focus();
  };

  const handleSortChange = (newSort: string) => {
    setSortField(newSort);
    fetchList(1, keyword, newSort);
  };

  const handleConfirm = () => {
    if (selectedFilm) {
      onSelect(selectedFilm);
    }
  };

  const handleItemDoubleClick = (item: FilmOption) => {
    setSelectedFilm(item);
    onSelect(item);
  };

  return (
    <Modal
      open={open}
      title={
        <div className={styles.modalTitleWrap}>
          <VideoCameraOutlined style={{ color: "var(--ant-color-primary)", fontSize: 18 }} />
          <span>{title}</span>
        </div>
      }
      onCancel={onCancel}
      width={960}
      centered
      styles={{
        body: {
          paddingTop: 12,
          paddingBottom: 12,
        },
      }}
      footer={[
        <Button key="cancel" onClick={onCancel}>
          取消
        </Button>,
        <Button
          key="confirm"
          type="primary"
          disabled={!selectedFilm}
          onClick={handleConfirm}
        >
          确定选择
        </Button>,
      ]}
    >
      <div className={styles.pickerContainer}>
        {/* 顶部综合检索工具栏 */}
        <div className={styles.toolbar}>
          <div className={styles.toolbarLeft}>
            <Space.Compact style={{ width: 360 }}>
              <Input
                ref={inputRef}
                placeholder="输入片名、主演或关键词检索全站片库..."
                prefix={<SearchOutlined style={{ color: "var(--ant-color-text-tertiary)" }} />}
                allowClear
                value={inputValue}
                onChange={(e) => {
                  const val = e.target.value;
                  setInputValue(val);
                  if (!val.trim() && keyword) {
                    handleResetSearch();
                  }
                }}
                onPressEnter={() => handleSearch(inputValue)}
              />
              <Button
                type="primary"
                onClick={() => handleSearch(inputValue)}
                loading={loading}
              >
                搜索
              </Button>
            </Space.Compact>
          </div>

          <div className={styles.toolbarRight}>
            <span className={styles.sortLabel}>排序：</span>
            <Segmented
              value={sortField}
              onChange={(val) => handleSortChange(String(val))}
              options={[
                { label: "综合排序", value: "" },
                { label: "最新更新", value: "update_stamp" },
                { label: "播放热度", value: "hits" },
                { label: "豆瓣评分", value: "score" },
                { label: "上映年份", value: "year" },
              ]}
            />
          </div>
        </div>

        {/* 搜索提示/结果统计条（仅在有搜索词时显示） */}
        {keyword && (
          <div className={styles.searchSummaryBar}>
            <span>
              检索到关于「<b>{keyword}</b>」的影片共 <b>{page.total}</b> 部
            </span>
            <Button
              type="link"
              size="small"
              icon={<ReloadOutlined />}
              onClick={handleResetSearch}
              className={styles.resetBtn}
            >
              重置并显示全库
            </Button>
          </div>
        )}

        {/* 影视卡片流展示区 */}
        <div className={styles.listScrollArea}>
          {loading && list.length === 0 ? (
            <div className={styles.loadingContainer}>
              <Spin size="large" tip="正在从片库检索影片..." />
            </div>
          ) : list.length > 0 ? (
            <Spin spinning={loading} tip="更新列表中...">
              <div className={styles.grid}>
                {list.map((item) => {
                  const isSelected =
                    selectedFilm?.id === item.id ||
                    (!selectedFilm && String(selectedMid) === String(item.id));

                  return (
                    <div
                      key={item.id}
                      className={`${styles.filmCard} ${isSelected ? styles.cardActive : ""}`}
                      onClick={() => setSelectedFilm(item)}
                      onDoubleClick={() => handleItemDoubleClick(item)}
                      title="单击单选，双击直接确定选定"
                    >
                      <div className={styles.posterContainer}>
                        {/* eslint-disable-next-line @next/next/no-img-element */}
                        <img
                          src={item.picture || FALLBACK_IMG}
                          alt={item.name || ""}
                          className={styles.poster}
                          loading="lazy"
                          onError={(e) => {
                            (e.target as HTMLImageElement).src = FALLBACK_IMG;
                          }}
                        />
                        {item.cName && (
                          <div className={styles.topBadge}>
                            {item.cName}
                          </div>
                        )}
                        <div className={styles.bottomOverlay}>
                          <span className={styles.remarkText}>
                            {item.remarks || "正片"}
                          </span>
                        </div>
                      </div>

                      <div className={styles.cardBody}>
                        <div className={styles.cardTitle} title={item.name}>
                          {item.name}
                        </div>
                        <div className={styles.cardMetaRow}>
                          <span>{item.year || "未知年份"}</span>
                          <span>{item.area || ""}</span>
                        </div>
                        {(item.director || item.actor) && (
                          <Tooltip title={`导演: ${item.director || "暂无"} | 主演: ${item.actor || "暂无"}`}>
                            <div className={styles.cardSub}>
                              {item.director ? `导: ${item.director}` : `演: ${item.actor}`}
                            </div>
                          </Tooltip>
                        )}
                      </div>

                      {isSelected && (
                        <div className={styles.checkBadge}>
                          <CheckOutlined style={{ fontSize: 13, strokeWidth: 4 }} />
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </Spin>
          ) : (
            <div className={styles.emptyContainer}>
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  keyword ? (
                    <div>
                      <div>
                        未找到与「<span className={styles.highlightText}>{keyword}</span>」相关的影片
                      </div>
                      <div className={styles.emptyTip}>
                        请检查片名拼写，或尝试更简短的关键词检索
                      </div>
                    </div>
                  ) : (
                    <span>片库暂无影片数据</span>
                  )
                }
              >
                {keyword && (
                  <Button size="small" onClick={handleResetSearch}>
                    清除搜索条件
                  </Button>
                )}
              </Empty>
            </div>
          )}
        </div>

        {/* 底部摘要与分页栏 */}
        <div className={styles.footerWrap}>
          <div className={styles.footerLeft}>
            {selectedFilm ? (
              <div className={styles.selectedChip}>
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={selectedFilm.picture || FALLBACK_IMG}
                  alt=""
                  className={styles.chipThumb}
                  onError={(e) => {
                    (e.target as HTMLImageElement).src = FALLBACK_IMG;
                  }}
                />
                <div className={styles.chipInfo}>
                  <div className={styles.chipTitle} title={selectedFilm.name}>
                    {selectedFilm.name}
                  </div>
                  <div className={styles.chipMeta}>
                    {[selectedFilm.cName, selectedFilm.year, selectedFilm.area]
                      .filter(Boolean)
                      .join(" · ")}
                  </div>
                </div>
                <Tag color="orange" style={{ margin: "0 0 0 4px", fontSize: 11 }}>
                  已选定
                </Tag>
              </div>
            ) : (
              <span className={styles.footerPlaceholder}>
                💡 点击卡片单选 / 双击卡片直接确认
              </span>
            )}
          </div>

          <div className={styles.footerRight}>
            {page.total > 0 && (
              <Pagination
                current={page.current}
                pageSize={page.pageSize}
                total={page.total}
                onChange={(p) => fetchList(p, keyword, sortField)}
                showSizeChanger={false}
                size="small"
                showTotal={(total) => `共 ${total} 部`}
              />
            )}
          </div>
        </div>
      </div>
    </Modal>
  );
}
