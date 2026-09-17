"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Modal, Input, Select, Button, Space, Tag, Spin, Alert, Checkbox, Typography, Empty, Image as AntImage,
} from "antd";
import {
  SearchOutlined, CompassOutlined, ArrowLeftOutlined, CheckOutlined, StarFilled, CalendarOutlined, PictureOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import styles from "./index.module.less";

const { Text } = Typography;

export interface TmdbCandidate {
  id: number;
  mediaType: "movie" | "tv";
  title: string;
  originalTitle: string;
  releaseDate: string;
  year: string;
  poster: string;
  backdrop: string;
  voteAverage: number;
  overview: string;
}

interface TmdbModalProps {
  open: boolean;
  mid?: number;
  initialName?: string;
  initialYear?: string | number;
  onClose: () => void;
  onSuccess?: () => void;
  onPrefill?: (data: any, fields?: string[]) => void;
}

const FIELD_OPTIONS = [
  { label: "竖版海报", value: "poster" },
  { label: "横版幻灯背景", value: "backdrop" },
  { label: "剧情简介", value: "overview" },
  { label: "副标题/原名", value: "subTitle" },
  { label: "主演阵容", value: "actor" },
  { label: "导演", value: "director" },
  { label: "上映年份/日期", value: "year" },
  { label: "TMDB 评分", value: "score" },
  { label: "分类标签", value: "tag" },
];

export default function TmdbModal({
  open,
  mid,
  initialName = "",
  initialYear = "",
  onClose,
  onSuccess,
  onPrefill,
}: TmdbModalProps) {
  const [keyword, setKeyword] = useState("");
  const [year, setYear] = useState("");
  const [mediaType, setMediaType] = useState("");
  const [candidates, setCandidates] = useState<TmdbCandidate[]>([]);
  const [loading, setLoading] = useState(false);
  const [hasSearched, setHasSearched] = useState(false);
  const [selectedCandidate, setSelectedCandidate] = useState<TmdbCandidate | null>(null);
  const [previewDetail, setPreviewDetail] = useState<any>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [selectedFields, setSelectedFields] = useState<string[]>([
    "poster",
    "backdrop",
    "overview",
    "subTitle",
    "actor",
    "director",
    "year",
    "score",
    "tag",
  ]);
  const [submitting, setSubmitting] = useState(false);
  const { message } = useAppMessage();

  const doSearch = useCallback(
    async (q: string, y?: string, t?: string) => {
      const searchKw = (q ?? "").trim();
      if (!searchKw) {
        message.warning("请输入搜索片名");
        return;
      }
      setLoading(true);
      setHasSearched(true);
      setSelectedCandidate(null);
      setPreviewDetail(null);
      try {
        const resp = await ApiGet("/manage/tmdb/search", {
          query: searchKw,
          year: (y ?? "").trim(),
          type: (t ?? "").trim(),
        });
        if (resp.code === 0) {
          setCandidates(resp.data || []);
        } else {
          message.error(resp.msg || "检索失败");
          setCandidates([]);
        }
      } catch {
        message.error("网络异常，检索 TMDB 失败");
        setCandidates([]);
      } finally {
        setLoading(false);
      }
    },
    [message],
  );

  const handleSearch = () => {
    void doSearch(keyword, year, mediaType);
  };

  useEffect(() => {
    if (open) {
      const initialKw = (initialName || "").trim();
      setKeyword(initialKw);
      setYear("");
      setMediaType("");
      setSelectedCandidate(null);
      setPreviewDetail(null);
      setCandidates([]);
      setHasSearched(false);
      if (initialKw) {
        void doSearch(initialKw, "", "");
      }
    }
  }, [open, initialName, doSearch]);

  const handleSelectCandidate = async (item: TmdbCandidate) => {
    setSelectedCandidate(item);
    setDetailLoading(true);
    // 先赋予基础兜底信息
    setPreviewDetail({
      name: item.title,
      subTitle: item.originalTitle,
      picture: item.poster,
      pictureSlide: item.backdrop,
      content: item.overview,
      year: item.year,
      releaseDate: item.releaseDate,
      dbScore: item.voteAverage > 0 ? item.voteAverage.toFixed(1) : "",
    });
    try {
      const resp = await ApiGet("/manage/tmdb/prefill", {
        id: item.id,
        type: item.mediaType,
      });
      if (resp.code === 0 && resp.data) {
        setPreviewDetail(resp.data);
      }
    } catch {
      // 保持基础数据
    } finally {
      setDetailLoading(false);
    }
  };

  const handleApply = async () => {
    if (!selectedCandidate) return;
    if (selectedFields.length === 0) {
      message.warning("请至少勾选一个需要应用的字段");
      return;
    }
    setSubmitting(true);
    try {
      if (onPrefill) {
        if (previewDetail) {
          onPrefill(previewDetail, selectedFields);
          message.success("TMDB 元数据已成功填充至表单！");
          onClose();
          return;
        }
        const resp = await ApiGet("/manage/tmdb/prefill", {
          id: selectedCandidate.id,
          type: selectedCandidate.mediaType,
        });
        if (resp.code === 0 && resp.data) {
          onPrefill(resp.data, selectedFields);
          message.success("TMDB 元数据已成功填充至表单！");
          onClose();
        } else {
          message.error(resp.msg || "获取 TMDB 预填数据失败");
        }
      } else if (mid) {
        const resp = await ApiPost("/manage/tmdb/apply", {
          mid,
          tmdbId: selectedCandidate.id,
          mediaType: selectedCandidate.mediaType,
          fields: selectedFields,
        });
        if (resp.code === 0) {
          message.success(resp.msg || "TMDB 元数据已成功应用！");
          onSuccess?.();
          onClose();
        } else {
          message.error(resp.msg || "应用失败");
        }
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      open={open}
      onCancel={onClose}
      title={
        <Space size={8}>
          <CompassOutlined style={{ color: "#1890ff" }} />
          <span>TMDB 影视刮削匹配</span>
        </Space>
      }
      width={720}
      footer={
        selectedCandidate ? (
          <Space>
            <Button
              icon={<ArrowLeftOutlined />}
              onClick={() => {
                setSelectedCandidate(null);
                setPreviewDetail(null);
              }}
              disabled={submitting || detailLoading}
            >
              重新选择
            </Button>
            <Button
              type="primary"
              icon={<CheckOutlined />}
              loading={submitting}
              disabled={detailLoading}
              onClick={handleApply}
            >
              {onPrefill ? "填充至表单" : "确认应用并更新"}
            </Button>
          </Space>
        ) : null
      }
      destroyOnClose
    >
      <div className={styles.modalContent}>
        {!selectedCandidate && (
          <>
            <div className={styles.searchBar}>
              <Input
                placeholder="片名"
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                onPressEnter={() => handleSearch()}
                style={{ flex: 2, minWidth: 160 }}
                allowClear
              />
              <Input
                placeholder="年份"
                value={year}
                onChange={(e) => setYear(e.target.value)}
                onPressEnter={() => handleSearch()}
                style={{ width: 88 }}
                allowClear
              />
              <Select
                value={mediaType}
                onChange={setMediaType}
                style={{ width: 88 }}
                options={[
                  { label: "全部", value: "" },
                  { label: "剧集", value: "tv" },
                  { label: "电影", value: "movie" },
                ]}
              />
              <Button
                type="primary"
                icon={<SearchOutlined />}
                onClick={() => handleSearch()}
                loading={loading}
              >
                检索
              </Button>
            </div>

            <Spin spinning={loading} tip={loading && candidates.length === 0 ? "正在检索 TMDB..." : undefined}>
              <div className={candidates.length > 0 ? styles.candidateList : styles.spinPlaceholder}>
                {candidates.length > 0 ? (
                  candidates.map((item) => (
                    <div key={`${item.mediaType}-${item.id}`} className={styles.candidateCard}>
                      <div className={styles.candidatePoster}>
                        <AntImage
                          src={item.poster || FALLBACK_IMG}
                          fallback={FALLBACK_IMG}
                          alt={item.title}
                          preview={false}
                          width={72}
                          height={104}
                          style={{ objectFit: "cover", width: "100%", height: "100%" }}
                          className={styles.candidatePosterImg}
                        />
                      </div>
                      <div className={styles.cardContent}>
                        <div>
                          <div className={styles.cardHeader}>
                            <div className={styles.titleWrapper}>
                              <span className={styles.mainTitle}>{item.title}</span>
                              {item.originalTitle && item.originalTitle !== item.title && (
                                <span className={styles.subTitle}>({item.originalTitle})</span>
                              )}
                              <Tag color={item.mediaType === "movie" ? "blue" : "purple"}>
                                {item.mediaType === "movie" ? "电影" : "剧集"}
                              </Tag>
                            </div>
                            {item.voteAverage > 0 && (
                              <Tag color="gold" icon={<StarFilled />}>
                                {item.voteAverage.toFixed(1)}
                              </Tag>
                            )}
                          </div>
                          <div className={styles.metaRow}>
                            {item.releaseDate && (
                              <span>
                                <CalendarOutlined style={{ marginRight: 4 }} />
                                {item.releaseDate}
                              </span>
                            )}
                          </div>
                          <div className={styles.overview}>
                            {item.overview || "暂无简介"}
                          </div>
                        </div>
                        <div className={styles.cardFooter}>
                          <Button
                            type="primary"
                            size="small"
                            onClick={() => void handleSelectCandidate(item)}
                          >
                            选择此项
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))
                ) : hasSearched && !loading ? (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description={
                      <div>
                        <span>未在 TMDB 找到相关条目</span>
                        <div style={{ fontSize: 12, color: "var(--ant-color-text-tertiary)", marginTop: 4 }}>
                          提示：综艺与动漫请选「剧集」或「全部」；可尝试清空年份或简化片名重新搜索
                        </div>
                      </div>
                    }
                  />
                ) : null}
              </div>
            </Spin>
          </>
        )}

        {selectedCandidate && (
          <Spin spinning={detailLoading} tip="正在拉取完整详情...">
            <div className={styles.previewCard}>
              <div className={styles.previewHeader}>
                {/* 竖版海报 */}
                <div className={styles.previewPoster}>
                  <AntImage
                    src={previewDetail?.picture || selectedCandidate.poster || FALLBACK_IMG}
                    fallback={FALLBACK_IMG}
                    alt="竖版海报"
                    preview={{ mask: "查看海报" }}
                    width={140}
                    height={210}
                    style={{ objectFit: "cover", width: "100%", height: "100%" }}
                  />
                </div>

                {/* 详情核心元数据 */}
                <div className={styles.previewInfo}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                    <Text strong style={{ fontSize: 16 }}>
                      {previewDetail?.name || selectedCandidate.title}
                    </Text>
                    <Tag color={selectedCandidate.mediaType === "movie" ? "blue" : "purple"}>
                      {selectedCandidate.mediaType === "movie" ? "电影" : "剧集"}
                    </Tag>
                    {(previewDetail?.dbScore || selectedCandidate.voteAverage > 0) && (
                      <Tag color="gold" icon={<StarFilled />}>
                        {previewDetail?.dbScore || selectedCandidate.voteAverage.toFixed(1)}
                      </Tag>
                    )}
                    {(previewDetail?.subTitle || selectedCandidate.originalTitle) && (
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        ({previewDetail?.subTitle || selectedCandidate.originalTitle})
                      </Text>
                    )}
                  </div>

                  <div className={styles.metaLine}>
                    <span className={styles.metaLabel}>上映日期:</span>
                    <span className={styles.metaValue}>
                      {previewDetail?.releaseDate || selectedCandidate.releaseDate || previewDetail?.year || selectedCandidate.year || "暂无"}
                    </span>
                    <span className={styles.metaLabel} style={{ marginLeft: 16 }}>导演:</span>
                    <span className={styles.metaValue}>
                      {previewDetail?.director ? (
                        <Text strong style={{ color: "var(--ant-color-text)" }}>
                          {previewDetail.director}
                        </Text>
                      ) : (
                        "暂无"
                      )}
                    </span>
                  </div>

                  <div className={styles.metaLine}>
                    <span className={styles.metaLabel}>主演阵容:</span>
                    <span
                      className={styles.metaValue}
                      style={{
                        display: "-webkit-box",
                        WebkitLineClamp: 1,
                        WebkitBoxOrient: "vertical",
                        overflow: "hidden",
                      }}
                    >
                      {previewDetail?.actor || "暂无"}
                    </span>
                  </div>

                  <div className={styles.metaLine}>
                    <span className={styles.metaLabel}>分类标签:</span>
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
                      {previewDetail?.classTag ? (
                        previewDetail.classTag.split(/[,/]/).map((tag: string) => (
                          <Tag key={tag} color="cyan" style={{ margin: 0, fontSize: 11, padding: "0 5px" }}>
                            {tag.trim()}
                          </Tag>
                        ))
                      ) : (
                        <span className={styles.metaValue}>暂无</span>
                      )}
                    </div>
                  </div>

                  {/* 横版幻灯背景（16:9缩略图，点击可全屏查看大图） */}
                  <div className={styles.metaLine} style={{ alignItems: "center", marginTop: 2 }}>
                    <span className={styles.metaLabel}>横版背景:</span>
                    {previewDetail?.pictureSlide || selectedCandidate.backdrop ? (
                      <div className={styles.backdropThumbWrapper}>
                        <AntImage
                          src={previewDetail?.pictureSlide || selectedCandidate.backdrop}
                          fallback={FALLBACK_IMG}
                          alt="横版幻灯背景"
                          preview={{ mask: "点击看大图" }}
                          width={128}
                          height={72}
                          style={{ objectFit: "cover", width: "100%", height: "100%" }}
                        />
                      </div>
                    ) : (
                      <span className={styles.metaValue} style={{ color: "var(--ant-color-text-tertiary)" }}>
                        TMDB 暂未收录该影视横版背景
                      </span>
                    )}
                  </div>

                  <div className={styles.previewOverview}>
                    {previewDetail?.content || selectedCandidate.overview || "暂无剧情简介"}
                  </div>
                </div>
              </div>

              {/* 覆盖字段选择 */}
              <div className={styles.fieldsSection}>
                <div className={styles.fieldsTitle}>
                  {onPrefill ? "选择需要填充至表单的元数据字段：" : "选择需要覆盖落库的元数据字段："}
                </div>
                <Checkbox.Group
                  options={FIELD_OPTIONS}
                  value={selectedFields}
                  onChange={(vals) => setSelectedFields(vals as string[])}
                />
              </div>
            </div>
          </Spin>
        )}
      </div>
    </Modal>
  );
}
