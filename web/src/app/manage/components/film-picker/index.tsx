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
} from "antd";
import {
  CheckOutlined,
  SearchOutlined,
  CloseCircleFilled,
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

  const inputRef = useRef<HTMLInputElement>(null);

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
    fetchList(1, kw, sortField);
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
      title={title}
      onCancel={onCancel}
      width={960}
      styles={{ body: { paddingTop: 8, paddingBottom: 6 } }}
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
        {/* 顶部居中胶囊搜索区域 */}
        <div className={styles.searchSection}>
          <div className={styles.searchBarWrapper}>
            <SearchOutlined style={{ fontSize: 18, color: "var(--ant-color-text-tertiary)", marginRight: 4 }} />
            <input
              ref={inputRef}
              className={styles.searchInput}
              placeholder="输入片名、主演或关键词检索全站片库..."
              value={inputValue}
              onChange={(e) => {
                const val = e.target.value;
                setInputValue(val);
                if (!val.trim()) {
                  handleSearch("");
                }
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  handleSearch(inputValue);
                }
              }}
            />
            {inputValue && (
              <CloseCircleFilled
                style={{ color: "var(--ant-color-text-tertiary)", cursor: "pointer", marginRight: 8, fontSize: 14 }}
                onClick={() => {
                  setInputValue("");
                  handleSearch("");
                  inputRef.current?.focus();
                }}
              />
            )}
            <Button
              type="primary"
              className={styles.searchBtn}
              onClick={() => handleSearch(inputValue)}
            >
              搜索
            </Button>
          </div>

          {/* 居中排序与检索统计 */}
          <div className={styles.filterRow}>
            <Segmented
              size="small"
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
            {page.total > 0 && keyword && (
              <span className={styles.searchStats}>
                找到关于「<b>{keyword}</b>」的 <b>{page.total}</b> 部影片
              </span>
            )}
          </div>
        </div>

        {/* 影视卡片流展示区 */}
        <div className={styles.listScrollArea}>
          <Spin spinning={loading}>
            {list.length > 0 ? (
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
                      title="双击直接选定此影片"
                    >
                      <div className={styles.posterContainer}>
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
            ) : (
              !loading && (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  style={{ margin: "48px 0" }}
                  description={
                    keyword ? (
                      <span>未找到与「{keyword}」相关的影片，请尝试更换关键词</span>
                    ) : (
                      <span>请输入影片名称开始搜索</span>
                    )
                  }
                />
              )
            )}
          </Spin>
        </div>

        {/* 底部摘要与分页栏 */}
        <div className={styles.footerWrap}>
          {selectedFilm ? (
            <div className={styles.selectedChip}>
              <img
                src={selectedFilm.picture || FALLBACK_IMG}
                alt=""
                className={styles.chipThumb}
                onError={(e) => {
                  (e.target as HTMLImageElement).src = FALLBACK_IMG;
                }}
              />
              <span className={styles.chipTitle}>{selectedFilm.name}</span>
              <span className={styles.chipMeta}>
                {[selectedFilm.cName, selectedFilm.year].filter(Boolean).join(" · ")}
              </span>
            </div>
          ) : (
            <span className={styles.footerPlaceholder}>
              点击卡片单选 / 双击卡片直接确认
            </span>
          )}

          {page.total > page.pageSize && (
            <Pagination
              current={page.current}
              pageSize={page.pageSize}
              total={page.total}
              onChange={(p) => fetchList(p, keyword, sortField)}
              showSizeChanger={false}
              size="small"
            />
          )}
        </div>
      </div>
    </Modal>
  );
}
