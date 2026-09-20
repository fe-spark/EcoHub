"use client";

import React, { useState, useEffect, useMemo } from "react";
import {
  Card,
  Segmented,
  Select,
  InputNumber,
  Switch,
  Button,
  Tooltip,
  Tag,
  Space,
  Typography,
} from "antd";
import {
  ControlOutlined,
  AppstoreOutlined,
  ThunderboltOutlined,
  SaveOutlined,
  EditOutlined,
  InfoCircleOutlined,
  CheckCircleFilled,
  WarningOutlined,
} from "@ant-design/icons";

import { ApiGet } from "@/lib/client-api";
import { BannerConfig } from "./types";
import styles from "./banner-config-card.module.less";

const { Text } = Typography;

const STRATEGY_OPTIONS = [
  { label: "热门优先", value: "hot_random", title: "按播放热度与点击量优先排片" },
  { label: "高分精选", value: "score_random", title: "按影片优质评分优先排片" },
  { label: "最近更新", value: "latest_random", title: "按影片最近更新时间优先排片" },
  { label: "智能综合", value: "smart_mix", title: "热门、高分与最近更新影片混合排片" },
];

interface BannerConfigCardProps {
  config: BannerConfig;
  canWrite: boolean;
  loading: boolean;
  saving: boolean;
  onSaveConfig: (updatedCfg: BannerConfig) => Promise<boolean>;
}

export default function BannerConfigCard({
  config,
  canWrite,
  loading,
  saving,
  onSaveConfig,
}: BannerConfigCardProps) {
  const [isEditing, setIsEditing] = useState(false);
  const [draftConfig, setDraftConfig] = useState<BannerConfig>(config);
  const [categoryOptions, setCategoryOptions] = useState<{ label: string; value: number }[]>([]);

  useEffect(() => {
    if (categoryOptions.length > 0) {
      const validIds = new Set(categoryOptions.map((c) => c.value));
      setDraftConfig({
        ...config,
        categories: (config.categories || []).filter((id) => validIds.has(id)),
      });
    } else {
      setDraftConfig(config);
    }
  }, [config, categoryOptions]);

  useEffect(() => {
    let isMounted = true;
    const loadCategories = async () => {
      try {
        const resp = await ApiGet("/manage/film/class/tree");
        if (resp.code === 0 && resp.data?.children && Array.isArray(resp.data.children)) {
          if (isMounted) {
            // 分类管理中设置不显示的，不可选：仅保留 show !== false 且有效开启的分类
            const shownCategories = resp.data.children.filter((c: any) => c.show !== false && Boolean(c.show));
            const shownIds = new Set(shownCategories.map((c: any) => Number(c.id)));
            setCategoryOptions(
              shownCategories.map((c: any) => ({
                label: c.name,
                value: Number(c.id),
              }))
            );
            // 哪怕之前选中的时候显示，后面分类设置为不显示，依旧过滤
            setDraftConfig((prev) => ({
              ...prev,
              categories: (prev.categories || []).filter((id) => shownIds.has(id)),
            }));
            return;
          }
        }
      } catch {
        // fallback
      }
      try {
        const navResp = await ApiGet("/navCategory");
        if (navResp.code === 0 && Array.isArray(navResp.data)) {
          if (isMounted) {
            const shownIds = new Set(navResp.data.map((c: any) => Number(c.id)));
            setCategoryOptions(
              navResp.data.map((c: any) => ({
                label: c.name,
                value: Number(c.id),
              }))
            );
            setDraftConfig((prev) => ({
              ...prev,
              categories: (prev.categories || []).filter((id) => shownIds.has(id)),
            }));
          }
        }
      } catch {
        // ignore
      }
    };
    void loadCategories();
    return () => {
      isMounted = false;
    };
  }, []);

  const selectedCategoryLabels = useMemo(() => {
    const validIds = new Set(categoryOptions.map((c) => c.value));
    // 哪怕之前选中的时候显示，后面分类设置为不显示，展示时依旧严格过滤已隐藏的分类
    const cats = (config.categories || []).filter((id) => validIds.has(id));
    if (cats.length === 0) return [];
    const catMap = new Map<number, string>(categoryOptions.map((c) => [c.value, c.label]));
    return cats.map((id) => catMap.get(id) || `分类#${id}`);
  }, [config.categories, categoryOptions]);

  // 对比草稿与服务端配置，检测变更
  const hasDirty = useMemo(() => {
    const validIds = new Set(categoryOptions.map((c) => c.value));
    const a = (draftConfig.categories || []).filter((id) => validIds.has(id));
    const b = (config.categories || []).filter((id) => validIds.has(id));
    const categoriesChanged =
      a.length !== b.length || a.some((val) => !b.includes(val));

    return (
      draftConfig.mode !== config.mode ||
      draftConfig.strategy !== config.strategy ||
      draftConfig.count !== config.count ||
      draftConfig.autoTMDB !== config.autoTMDB ||
      categoriesChanged
    );
  }, [draftConfig, config, categoryOptions]);

  const currentStrategyLabel = useMemo(() => {
    const matched = STRATEGY_OPTIONS.find((s) => s.value === config.strategy);
    return matched ? matched.label : config.strategy;
  }, [config.strategy]);

  const handleModeChange = (val: string | number) => {
    setDraftConfig((prev) => ({ ...prev, mode: val as "manual" | "auto" }));
  };

  const handleStrategyChange = (val: BannerConfig["strategy"]) => {
    setDraftConfig((prev) => ({ ...prev, strategy: val }));
  };

  const handleCountChange = (val: number | null) => {
    let num = Number(val) || 6;
    if (num > 12) num = 12;
    if (num < 1) num = 1;
    setDraftConfig((prev) => ({ ...prev, count: num }));
  };

  const handleTMDBChange = (checked: boolean) => {
    setDraftConfig((prev) => ({ ...prev, autoTMDB: checked }));
  };

  const handleCategoriesChange = (vals: number[]) => {
    const validIds = new Set(categoryOptions.map((c) => c.value));
    setDraftConfig((prev) => ({
      ...prev,
      categories: vals.filter((id) => validIds.has(id)),
    }));
  };

  const handleCancel = () => {
    setDraftConfig(config);
    setIsEditing(false);
  };

  const handleSave = async () => {
    const validIds = new Set(categoryOptions.map((c) => c.value));
    const cleanConfig: BannerConfig = {
      ...draftConfig,
      categories: (draftConfig.categories || []).filter((id) => validIds.has(id)),
    };
    const success = await onSaveConfig(cleanConfig);
    if (success) {
      setIsEditing(false);
    }
  };

  return (
    <Card
      className={styles.card}
      title={
        <Space size={8} align="center" wrap>
          <ControlOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>首页轮播排片模式</span>
          {config.mode === "auto" ? (
            <Tag color="success" icon={<CheckCircleFilled />}>
              自动智能排片
            </Tag>
          ) : (
            <Tag color="purple">手动精选模式</Tag>
          )}
          {config.tmdbReady ? (
            <Tag color="cyan">TMDB 已就绪</Tag>
          ) : (
            <Tag color="warning" icon={<WarningOutlined />}>
              TMDB 未开启
            </Tag>
          )}
        </Space>
      }
      extra={
        <Space size={8} align="center">
          {isEditing ? (
            <>
              <Button size="middle" disabled={saving} onClick={handleCancel}>
                取消
              </Button>
              <Button
                type="primary"
                size="middle"
                icon={<SaveOutlined />}
                loading={saving}
                disabled={!canWrite || !hasDirty}
                onClick={handleSave}
              >
                保存设置
              </Button>
            </>
          ) : (
            <Button
              type="primary"
              size="middle"
              icon={<EditOutlined />}
              disabled={!canWrite || loading}
              onClick={() => {
                setDraftConfig(config);
                setIsEditing(true);
              }}
            >
              编辑
            </Button>
          )}
        </Space>
      }
    >
      {!isEditing ? (
        /* 只读浏览模式 */
        <div className={styles.viewMode}>
          {config.mode === "manual" ? (
            <div className={styles.viewInfoBlock}>
              <div className={styles.viewRow}>
                <span className={styles.viewLabel}>当前运行模式:</span>
                <Tag color="purple" style={{ borderRadius: 4, fontWeight: 600 }}>
                  手动精选排片模式
                </Tag>
              </div>
              <Text type="secondary" className={styles.viewDesc}>
                大轮播固定展示下方列表影片，支持自主选片与排序。
              </Text>
            </div>
          ) : (
            <div className={styles.viewInfoBlock}>
              <div className={styles.viewMetaGrid}>
                <div className={styles.metaItem}>
                  <span className={styles.viewLabel}>排片策略:</span>
                  <Text strong>{currentStrategyLabel}</Text>
                </div>
                <div className={styles.metaItem}>
                  <span className={styles.viewLabel}>轮播数量:</span>
                  <Text strong>{config.count || 6} 部影片</Text>
                </div>
                <div className={styles.metaItem}>
                  <span className={styles.viewLabel}>排片分类:</span>
                  {selectedCategoryLabels.length > 0 ? (
                    <Space size={4} wrap>
                      {selectedCategoryLabels.map((lbl) => (
                        <Tag key={lbl} color="blue">{lbl}</Tag>
                      ))}
                    </Space>
                  ) : (
                    <Tag>全部分类</Tag>
                  )}
                </div>
                <div className={styles.metaItem}>
                  <span className={styles.viewLabel}>影片是否刮削:</span>
                  <Tag color={config.autoTMDB && config.tmdbReady ? "processing" : config.autoTMDB && !config.tmdbReady ? "warning" : "default"}>
                    {config.autoTMDB && config.tmdbReady
                      ? "开启刮削"
                      : config.autoTMDB && !config.tmdbReady
                      ? "待配置密钥"
                      : "不刮削"}
                  </Tag>
                </div>
              </div>
              <div className={styles.viewNotice}>
                <span>自动模式按计划任务定时更新，支持点击【立即换一批】。</span>
              </div>
            </div>
          )}
        </div>
      ) : (
        /* 编辑配置模式 */
        <div className={styles.editMode}>
          <div className={styles.editRow}>
            <span className={styles.fieldLabel}>选择排片模式:</span>
            <Segmented
              disabled={!canWrite || saving}
              value={draftConfig.mode}
              onChange={handleModeChange}
              options={[
                {
                  label: "手动精选排片",
                  value: "manual",
                  icon: <AppstoreOutlined />,
                },
                {
                  label: "自动智能排片",
                  value: "auto",
                  icon: <ThunderboltOutlined />,
                },
              ]}
            />
          </div>

          {draftConfig.mode === "manual" ? (
            <div className={styles.manualNotice}>
              <InfoCircleOutlined className={styles.noticeIcon} />
              <span>
                切换为<strong>手动模式</strong>：大轮播固定展示下方列表中的影片。
              </span>
            </div>
          ) : (
            <div className={styles.autoFormStack}>
              <div className={styles.formControls}>
                <div className={styles.fieldItem}>
                  <span className={styles.fieldLabel}>排片策略:</span>
                  <Select
                    disabled={!canWrite}
                    value={draftConfig.strategy}
                    onChange={handleStrategyChange}
                    options={STRATEGY_OPTIONS}
                    style={{ width: 140 }}
                  />
                </div>

                <div className={styles.fieldItem}>
                  <span className={styles.fieldLabel}>轮播数量:</span>
                  <InputNumber
                    disabled={!canWrite}
                    min={1}
                    max={12}
                    step={1}
                    precision={0}
                    value={draftConfig.count}
                    onChange={handleCountChange}
                    style={{ width: 95 }}
                    addonAfter="部"
                  />
                </div>

                <div className={styles.fieldItem}>
                  <span className={styles.fieldLabel}>
                    排片分类:
                    <Tooltip title="仅在分类管理中设置显示的一级分类可选；若分类后续被设为不显示，排片时将自动严格过滤。留空则在全部显示分类中随机。">
                      <InfoCircleOutlined
                        style={{ marginLeft: 4, cursor: "pointer", color: "#8c8c8c" }}
                      />
                    </Tooltip>
                  </span>
                  <Select
                    mode="multiple"
                    allowClear
                    disabled={!canWrite}
                    placeholder="全部分类 (留空不限)"
                    value={draftConfig.categories || []}
                    onChange={handleCategoriesChange}
                    options={categoryOptions}
                    maxTagCount="responsive"
                    style={{ minWidth: 180, maxWidth: 320 }}
                  />
                </div>

                <div className={styles.fieldItem}>
                  <span className={styles.fieldLabel}>
                    影片是否刮削:
                    <Tooltip
                      title={
                        !config.tmdbReady
                          ? "未配置 TMDB API Key，直接使用片库既有图片排片。"
                          : "开启后自动刮削补齐横屏大图，关闭则使用片库已有数据。"
                      }
                    >
                      <InfoCircleOutlined
                        style={{ marginLeft: 4, cursor: "pointer", color: !config.tmdbReady ? "#faad14" : "#8c8c8c" }}
                      />
                    </Tooltip>
                  </span>
                  <Switch
                    disabled={!canWrite || !config.tmdbReady}
                    checked={config.tmdbReady ? draftConfig.autoTMDB : false}
                    onChange={handleTMDBChange}
                    checkedChildren="开启"
                    unCheckedChildren="关闭"
                  />
                </div>
              </div>

              <div className={styles.hintText}>
                <span>自动模式按计划任务定时更新，支持点击【立即换一批】。</span>
              </div>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
