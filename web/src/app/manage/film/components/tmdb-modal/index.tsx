"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import {
  Modal, Input, Select, Button, Space, Tag, Spin, Checkbox, Typography, Empty, Image as AntImage,
} from "antd";
import {
  SearchOutlined, CompassOutlined, ArrowLeftOutlined, CheckOutlined, StarFilled, CalendarOutlined, PictureOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { TmdbCandidate, TmdbModalProps, FIELD_OPTIONS } from "./types";
import styles from "./index.module.less";

const { Text } = Typography;

function cleanInitialTitle(raw: string): string {
  let s = (raw || "").trim();
  s = s.replace(/[（(]\s*(19\d{2}|20\d{2})\s*[）)]/g, " ");
  s = s.replace(/[【\[].*?[】\]]/g, " ").replace(/[（(].*?[）)]/g, " ");
  s = s.replace(/[-_—·\s]*(预告片|预告|先导片|先导预告|片花|花絮|特辑|彩蛋|幕后|采访|纯享版|精编版|精华版|速看版|解说版|正片|抢先版|枪版|高清版|超清版|蓝光版|完整版|未删减版|无删减版|未删减|无删减|修复版|加长版)/gi, " ");
  s = s.replace(/[-_—·\s]*(普通话版|普通话|国粤双语|国语版|国语|粤语版|粤语|英语版|原声版|中配版|中配|配音版|译制版|中文字幕|中英双字|双语字幕|HD中字|HD国语)/gi, " ");
  s = s.replace(/\b(4k|1080p|720p|2160p|hd|bd|tc|ts|dvd|web-dl|bluray|trailer|teaser|pv|preview|sp)\b/gi, " ");
  s = s.replace(/[-_—·:：\s]+$/g, "").replace(/^[-_—·:：\s]+/g, "");
  s = s.replace(/\s+/g, " ").trim();
  return s || raw.trim();
}

export default function TmdbModal({
  open,
  mid,
  initialName = "",
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
    "name",
    "poster",
    "backdrop",
    "overview",
    "subTitle",
    "actor",
    "director",
    "writer",
    "area",
    "language",
    "year",
    "score",
    "tag",
  ]);
  const [submitting, setSubmitting] = useState(false);
  const { message } = useAppMessage();
  const searchSeq = useRef(0);

  const doSearch = useCallback(
    async (q: string, y?: string, t?: string) => {
      const searchKw = (q ?? "").trim();
      if (!searchKw) {
        message.warning("请输入搜索片名");
        return;
      }
      const seq = ++searchSeq.current;
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
        if (seq !== searchSeq.current) return;
        if (resp.code === 0) {
          setCandidates(resp.data || []);
        } else {
          message.error(resp.msg || "检索失败");
          setCandidates([]);
        }
      } catch {
        if (seq !== searchSeq.current) return;
        message.error("网络异常，检索 TMDB 失败");
        setCandidates([]);
      } finally {
        if (seq === searchSeq.current) setLoading(false);
      }
    },
    [message],
  );

  const handleSearch = () => {
    void doSearch(keyword, year, mediaType);
  };

  useEffect(() => {
    if (open) {
      const cleaned = cleanInitialTitle(initialName || "");
      setKeyword(cleaned);
      setYear("");
      setMediaType("");
      setSelectedCandidate(null);
      setPreviewDetail(null);
      setCandidates([]);
      setHasSearched(false);
      if (cleaned) {
        void doSearch(cleaned, "", "");
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
          onClose();
          return;
        }
        const resp = await ApiGet("/manage/tmdb/prefill", {
          id: selectedCandidate.id,
          type: selectedCandidate.mediaType,
        });
        if (resp.code === 0 && resp.data) {
          onPrefill(resp.data, selectedFields);
          onClose();
        } else {
          message.error(resp.msg || "获取预填数据失败");
        }
      } else if (mid) {
        const resp = await ApiPost("/manage/tmdb/apply", {
          mid,
          tmdbId: selectedCandidate.id,
          mediaType: selectedCandidate.mediaType,
          fields: selectedFields,
        });
        if (resp.code === 0) {
          message.success(resp.msg || "应用成功");
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
          <CompassOutlined style={{ color: "#722ed1" }} />
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
              {onPrefill ? "填入表单" : "应用"}
            </Button>
          </Space>
        ) : null
      }
      destroyOnHidden
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

            <Spin spinning={loading}>
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
                            选择
                          </Button>
                        </div>
                      </div>
                    </div>
                  ))
                ) : hasSearched && !loading ? (
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description="未找到相关条目"
                  />
                ) : null}
              </div>
            </Spin>
          </>
        )}

        {selectedCandidate && (
          <Spin spinning={detailLoading}>
            <div className={styles.previewCard}>
              <div className={styles.previewHeader}>
                <div className={styles.previewPoster}>
                  <AntImage
                    src={previewDetail?.picture || selectedCandidate.poster || FALLBACK_IMG}
                    fallback={FALLBACK_IMG}
                    alt="海报"
                    preview={{ mask: "查看" }}
                    width={140}
                    height={210}
                    style={{ objectFit: "cover", width: "100%", height: "100%" }}
                  />
                </div>

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
                    <span className={styles.metaLabel}>上映:</span>
                    <span className={styles.metaValue}>
                      {previewDetail?.releaseDate || selectedCandidate.releaseDate || previewDetail?.year || selectedCandidate.year || "暂无"}
                    </span>
                    <span className={styles.metaLabel} style={{ marginLeft: 16 }}>导演:</span>
                    <span className={styles.metaValue}>
                      {previewDetail?.director ? <Text strong style={{ color: "var(--ant-color-text)" }}>{previewDetail.director}</Text> : "暂无"}
                    </span>
                  </div>

                  <div className={styles.metaLine}>
                    <span className={styles.metaLabel}>主演:</span>
                    <span className={styles.metaValue} style={{ display: "-webkit-box", WebkitLineClamp: 1, WebkitBoxOrient: "vertical", overflow: "hidden" }}>
                      {previewDetail?.actor || "暂无"}
                    </span>
                  </div>

                  <div className={styles.metaLine}>
                    <span className={styles.metaLabel}>标签:</span>
                    <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
                      {previewDetail?.classTag ? (
                        previewDetail.classTag.split(/[,/]/).map((tag: string) => (
                          <Tag key={tag} color="cyan" style={{ margin: 0, fontSize: 11, padding: "0 5px" }}>{tag.trim()}</Tag>
                        ))
                      ) : <span className={styles.metaValue}>暂无</span>}
                    </div>
                  </div>

                  <div className={styles.metaLine} style={{ alignItems: "center", marginTop: 2 }}>
                    <span className={styles.metaLabel}>背景:</span>
                    {previewDetail?.pictureSlide || selectedCandidate.backdrop ? (
                      <div className={styles.backdropThumbWrapper}>
                        <AntImage
                          src={previewDetail?.pictureSlide || selectedCandidate.backdrop}
                          fallback={FALLBACK_IMG}
                          alt="背景图"
                          preview={{ mask: "查看" }}
                          width={128}
                          height={72}
                          style={{ objectFit: "cover", width: "100%", height: "100%" }}
                        />
                      </div>
                    ) : (
                      <span className={styles.metaValue} style={{ color: "var(--ant-color-text-tertiary)" }}>
                        暂无背景图
                      </span>
                    )}
                  </div>

                  <div className={styles.previewOverview}>
                    {previewDetail?.content || selectedCandidate.overview || "暂无简介"}
                  </div>
                </div>
              </div>

              <div className={styles.fieldsSection}>
                <div className={styles.fieldsTitle}>同步字段</div>
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
