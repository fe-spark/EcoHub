"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  Card,
  Table,
  Button,
  Tag,
  Space,
  Tooltip,
  Popconfirm,
  Image as AntImage,
  Typography,
} from "antd";
import {
  EditOutlined,
  DeleteOutlined,
  PlusOutlined,
  SyncOutlined,
  LockOutlined,
  DesktopOutlined,
  PictureOutlined,
  CheckCircleFilled,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ManagePageHeader from "@/app/manage/components/page-header";
import BannerConfigCard from "./banner-config-card";
import BannerModal from "./banner-modal";
import BannerProgressModal from "./banner-progress-modal";
import { BannerRecord, BannerConfig, EditorMode, MAX_BANNER_COUNT } from "./types";
import styles from "./index.module.less";

const { Text } = Typography;

export default function BannersPageView() {
  const [banners, setBanners] = useState<BannerRecord[]>([]);
  const [config, setConfig] = useState<BannerConfig>({ mode: "manual", strategy: "hot_random", count: 6, autoTMDB: true, refreshCron: "" });
  const [loading, setLoading] = useState(false);
  const [configLoading, setConfigLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [generating, setGenerating] = useState(false);

  // 轮播异步刮削与排片进度状态
  const pollTimerRef = useRef<NodeJS.Timeout | null>(null);
  const [progressVisible, setProgressVisible] = useState(false);
  const [progressTitle, setProgressTitle] = useState("智能排片与 TMDB 刮削");
  const [progressPercent, setProgressPercent] = useState(0);
  const [progressStatus, setProgressStatus] = useState<"normal" | "active" | "success" | "exception">("active");
  const [progressText, setProgressText] = useState("");
  const [progressError, setProgressError] = useState("");

  const { message } = useAppMessage();
  const messageRef = useRef(message);
  messageRef.current = message;
  const { canWrite } = useManagePermission();

  const [editorVisible, setEditorVisible] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>("create");
  const [currentRow, setCurrentRow] = useState<BannerRecord | null>(null);

  const fetchBanners = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/banner/list");
      if (resp.code === 0) {
        setBanners((resp.data || []) as BannerRecord[]);
      } else {
        messageRef.current.error(resp.msg);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchConfig = useCallback(async () => {
    setConfigLoading(true);
    try {
      const resp = await ApiGet("/manage/banner/config");
      if (resp.code === 0 && resp.data) {
        setConfig(resp.data as BannerConfig);
      }
    } finally {
      setConfigLoading(false);
    }
  }, []);

  const stopPolling = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  }, []);

  // 仅在组件真实卸载时注销轮询定时器，杜绝因状态更新触发 effect 重复 cleanup
  useEffect(() => {
    return () => {
      stopPolling();
    };
  }, [stopPolling]);

  const startPollingProgress = useCallback(() => {
    stopPolling();
    setProgressVisible(true);
    setProgressStatus("active");
    setProgressError("");

    const pollInterval = 1000;
    const maxPollDuration = 330000; // 5.5 分钟安全保护上限
    const startTime = Date.now();

    const checkProgress = async () => {
      if (Date.now() - startTime > maxPollDuration) {
        stopPolling();
        setProgressStatus("exception");
        setProgressError("排片任务超时，请检查后端运行日志");
        setProgressText("任务超时");
        setGenerating(false);
        return;
      }

      try {
        const progResp = await ApiGet("/manage/banner/generate/progress");
        if (progResp.code !== 0) {
          return;
        }

        const prog = progResp.data;
        if (prog.running) {
          setProgressStatus("active");
          setProgressPercent(Math.max(prog.percent || 5, 5));
          if (prog.stage) {
            setProgressText(prog.stage);
          }
        } else {
          stopPolling();
          setGenerating(false);

          if (prog.error) {
            setProgressStatus("exception");
            setProgressError(prog.error);
            setProgressText(prog.stage || "排片生成失败");
          } else {
            setProgressPercent(100);
            setProgressStatus("success");
            setProgressText(prog.stage || "排片完成，实时生效");
            await fetchBanners();
            setTimeout(() => {
              setProgressVisible(false);
            }, 800);
          }
        }
      } catch (err: any) {
        console.warn("轮询排片进度异常:", err);
      }
    };

    void checkProgress();
    pollTimerRef.current = setInterval(checkProgress, pollInterval);
  }, [fetchBanners, stopPolling]);

  // 页面加载初次拉取数据；若后台已有排片任务在跑，自动恢复弹窗与轮询
  useEffect(() => {
    void fetchBanners();
    void fetchConfig();

    void ApiGet("/manage/banner/generate/progress").then((resp) => {
      if (resp.code === 0 && resp.data?.running) {
        startPollingProgress();
      }
    }).catch(() => {});
  }, [fetchBanners, fetchConfig, startPollingProgress]);

  // 统一智能排片与 TMDB 刮削生成（异步启动任务 + 实时轮询进度，彻底消除 504 网关超时）
  const handleGenerateNow = useCallback(
    async (title?: string) => {
      const isScraping = Boolean(config.autoTMDB && config.tmdbReady);
      setProgressTitle(title || (isScraping ? "智能排片与 TMDB 刮削" : "自动智能排片"));
      setGenerating(true);
      setProgressVisible(true);
      setProgressStatus("active");
      setProgressPercent(5);
      setProgressText("正在启动排片任务...");
      setProgressError("");

      try {
        // 1. 触发异步排片任务（后端毫秒级立即返回，杜绝 504 网关超时）
        const resp = await ApiPost("/manage/banner/generate", {});
        if (resp.code !== 0) {
          setProgressStatus("exception");
          setProgressError(resp.msg || "未能启动轮播生成任务");
          setProgressText("排片启动失败");
          setGenerating(false);
          return;
        }

        // 2. 轮询获取后端真实执行进度与步骤
        startPollingProgress();
      } catch (err: any) {
        setProgressStatus("exception");
        setProgressError(err?.response?.data?.msg || err?.message || "网络异常或请求超时");
        setProgressText("网络异常");
        setGenerating(false);
      }
    },
    [config.autoTMDB, config.tmdbReady, startPollingProgress],
  );

  // 统一保存排片设置：自动模式下立即联动刮削新轮播
  const handleSaveConfig = useCallback(
    async (updatedCfg: BannerConfig): Promise<boolean> => {
      setSaving(true);
      try {
        const resp = await ApiPost("/manage/banner/config/update", updatedCfg);
        if (resp.code === 0) {
          message.success("排片设置已保存");
          await fetchConfig();
          // 改了排片策略或处于自动模式，立刻按新策略生成对应轮播！
          if (updatedCfg.mode === "auto") {
            const isScraping = Boolean(updatedCfg.autoTMDB && config.tmdbReady);
            void handleGenerateNow(isScraping ? "智能排片与 TMDB 刮削" : "自动智能排片");
          }
          return true;
        }
        message.error(resp.msg || "保存配置失败");
        return false;
      } finally {
        setSaving(false);
      }
    },
    [message, fetchConfig, handleGenerateNow, config.tmdbReady],
  );

  // 删除某一项轮播
  const handleDelete = useCallback(
    async (id: string) => {
      const resp = await ApiPost("/manage/banner/del", { id: String(id) });
      if (resp.code === 0) {
        message.success(resp.msg);
        fetchBanners();
      } else {
        message.error(resp.msg);
      }
    },
    [message, fetchBanners],
  );

  const isFull = banners.length >= MAX_BANNER_COUNT;

  const openCreateEditor = () => {
    if (isFull) {
      message.warning(`已达上限（${MAX_BANNER_COUNT}部）`);
      return;
    }
    setCurrentRow(null);
    setEditorMode("create");
    setEditorVisible(true);
  };

  const openEditEditor = (record: BannerRecord) => {
    setCurrentRow(record);
    setEditorMode("edit");
    setEditorVisible(true);
  };

  const isAuto = config.mode === "auto";

  // 根据当前模式动态自适应表格列：竖屏海报列 + 横屏海报独立列
  const columns = useMemo<ColumnsType<BannerRecord>>(() => {
    const cols: ColumnsType<BannerRecord> = [
      {
        title: "排片位次",
        key: "index",
        width: 80,
        fixed: "left",
        align: "center",
        render: (_, __, index) => (
          <Tooltip title={isAuto ? "算法推荐位次" : "手动排片位次"}>
            <Tag color={isAuto ? "cyan" : "purple"} style={{ borderRadius: 4, fontWeight: 600 }}>
              #{String(index + 1).padStart(2, "0")}
            </Tag>
          </Tooltip>
        ),
      },
      {
        title: "影片与竖屏海报",
        key: "filmInfo",
        width: 300,
        align: "left",
        render: (_, record) => {
          const posterUrl =
            record.picture || record.poster || record.pictureSlide || FALLBACK_IMG;

          return (
            <div className={styles.filmCell}>
              {/* 竖屏海报缩略图 */}
              <div className={styles.posterThumbWrapper}>
                <AntImage
                  src={posterUrl}
                  width={52}
                  height={72}
                  className={styles.posterThumb}
                  fallback={FALLBACK_IMG}
                  preview={{ mask: "预览海报" }}
                  style={{ objectFit: "cover", width: 52, height: 72, display: "block" }}
                />
              </div>

              {/* 影片基础信息 */}
              <div className={styles.filmInfo}>
                <Text className={styles.filmName} ellipsis={{ tooltip: record.name }}>{record.name}</Text>
                <div className={styles.filmMeta}>
                  {record.cName && <Tag color="orange" style={{ borderRadius: 4, margin: 0 }}>{record.cName}</Tag>}
                  {record.year ? <Tag color="blue" style={{ borderRadius: 4, margin: 0 }}>{record.year}</Tag> : null}
                  <span className={styles.filmId}>MID: {record.mid || record.id}</span>
                </div>
              </div>
            </div>
          );
        },
      },
      {
        title: "横屏海报",
        key: "pictureSlide",
        width: 140,
        align: "center",
        render: (_, record) => {
          // 严格检验是否真正具备独立的横屏大图
          const hasRealSlide = Boolean(
            record.pictureSlide &&
              record.pictureSlide.trim() !== "" &&
              record.pictureSlide !== record.picture &&
              record.pictureSlide !== record.poster,
          );

          if (hasRealSlide) {
            return (
              <div className={styles.slideCell}>
                <div className={styles.slideThumbWrapper}>
                  <AntImage
                    src={record.pictureSlide}
                    width={96}
                    height={54}
                    className={styles.slideThumb}
                    fallback={FALLBACK_IMG}
                    preview={{ mask: "预览横图" }}
                    style={{ objectFit: "cover", width: 96, height: 54, display: "block" }}
                  />
                </div>
              </div>
            );
          }
          // 真实呈现状态：绝不拿竖版海报伪造兜底，清晰告知无横图
          return (
            <div className={styles.slideCell}>
              <div className={styles.emptySlideBox}>
                <PictureOutlined style={{ fontSize: 16, color: "var(--ant-color-text-tertiary)" }} />
                <span>无横屏大图</span>
              </div>
            </div>
          );
        },
      },
      {
        title: "封面模式",
        key: "posterMode",
        align: "center",
        width: 130,
        render: (_, record) => {
          const isCustom = record.isCustomPic || isAuto;
          return isCustom ? (
            <Tooltip title="自定义独立封面，不受采集与源站海报图源覆盖">
              <Tag color="warning" icon={<LockOutlined />} style={{ borderRadius: 4 }}>
                自定义封面
              </Tag>
            </Tooltip>
          ) : (
            <Tooltip title="海报与片库数据源自动联动同步">
              <Tag color="processing" style={{ borderRadius: 4 }}>
                片库联动
              </Tag>
            </Tooltip>
          );
        },
      },
    ];

    // 手动模式下展示排片权重列
    if (!isAuto) {
      cols.push({
        title: "排序权重",
        dataIndex: "sort",
        key: "sort",
        width: 90,
        align: "center",
        render: (s: number) => (
          <Tag color={s > 0 ? "orange" : "default"} style={{ borderRadius: 4 }}>
            {s ?? 0}
          </Tag>
        ),
      });
    }

    // 操作列：无论手动还是自动模式，均和手动一致提供【编辑】与【删除】
    cols.push({
      title: "操作",
      key: "action",
      align: "center",
      fixed: "right",
      width: 120,
      render: (_, record) => (
        <Space size={8}>
          <Tooltip title="修改轮播信息与自定义封面">
            <Button
              type="primary"
              shape="circle"
              size="middle"
              style={{ background: "#1890ff", borderColor: "#1890ff" }}
              icon={<EditOutlined />}
              disabled={!canWrite}
              onClick={() => openEditEditor(record)}
            />
          </Tooltip>

          <Popconfirm
            title={isAuto ? "确认从当前排片中移除？" : "确认删除该轮播图？"}
            description={
              isAuto
                ? "移除后当前轮播不再展示此影片，下次自动换一批或定时排片时重新计算。"
                : "删除后首页将不再轮播展示该影片。"
            }
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Tooltip title={isAuto ? "从当前轮播移除" : "删除轮播"}>
              <Button
                type="primary"
                danger
                shape="circle"
                size="middle"
                icon={<DeleteOutlined />}
                disabled={!canWrite}
              />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    });

    return cols;
  }, [isAuto, canWrite, handleDelete]);

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="首页轮播"
        description="维护前台首页顶置大轮播排片，竖屏海报与横屏背景大图并存，支持客户端自主按需匹配呈现。"
      />

      {/* 上层 Card：排片模式与策略设置，对齐系统设置【只读/编辑】标准 */}
      <BannerConfigCard
        config={config}
        canWrite={canWrite}
        loading={configLoading}
        saving={saving}
        onSaveConfig={handleSaveConfig}
      />

      {/* 下层 Card：轮播影片排片表与操作 */}
      <Card
        className={styles.tableCard}
        title={
          <Space size={8} align="center">
            <DesktopOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>轮播影片排片表</span>
            <span className={styles.countBadge}>{banners.length} 部影片</span>
          </Space>
        }
        extra={
          !isAuto ? (
            <Tooltip title={isFull ? `已达上限（${MAX_BANNER_COUNT}部）` : undefined}>
              <span>
                <Button
                  type="primary"
                  size="middle"
                  icon={<PlusOutlined />}
                  onClick={openCreateEditor}
                  disabled={!canWrite || isFull}
                >
                  添加轮播
                </Button>
              </span>
            </Tooltip>
          ) : (
            <Button
              type="primary"
              size="middle"
              icon={<SyncOutlined spin={generating} />}
              loading={generating}
              disabled={!canWrite}
              onClick={() => void handleGenerateNow()}
            >
              立即换一批
            </Button>
          )
        }
      >
        <Table
          bordered
          dataSource={banners}
          columns={columns}
          rowKey="id"
          loading={loading}
          size="middle"
          pagination={false}
          scroll={{ x: 780 }}
        />
      </Card>

      {/* 轮播编辑/创建弹窗 */}
      <BannerModal
        open={editorVisible}
        mode={editorMode}
        currentRow={currentRow}
        isAuto={isAuto}
        onClose={() => setEditorVisible(false)}
        onSuccess={fetchBanners}
      />

      {/* 智能排片与 TMDB 刮削进度弹窗 */}
      <BannerProgressModal
        open={progressVisible}
        title={progressTitle}
        percent={progressPercent}
        status={progressStatus}
        stepText={progressText}
        errorMsg={progressError}
        onClose={() => setProgressVisible(false)}
      />
    </div>
  );
}
