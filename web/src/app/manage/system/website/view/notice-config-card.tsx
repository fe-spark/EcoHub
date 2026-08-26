"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Card,
  Checkbox,
  Flex,
  Input,
  Space,
  Spin,
  Switch,
  Typography,
} from "antd";
import {
  BellOutlined,
  EditOutlined,
  EyeOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import NoticeModal from "@/components/public/NoticeModal";
import {
  DEFAULT_NOTICE_TITLE,
  MAX_NOTICE_CONTENT_LEN,
  MAX_NOTICE_TITLE_LEN,
  createDefaultNoticeConfig,
  normalizeNoticeConfig,
  type NoticeConfig,
} from "@/lib/notice";
import styles from "./notice-config-card.module.less";

interface NoticeConfigCardProps {
  canWrite: boolean;
}

export default function NoticeConfigCard({ canWrite }: NoticeConfigCardProps) {
  const [data, setData] = useState<NoticeConfig>(createDefaultNoticeConfig);
  const [draft, setDraft] = useState<NoticeConfig>(createDefaultNoticeConfig);
  const [isEditing, setIsEditing] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);

  const { message } = useAppMessage();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const loadData = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/notice");
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeNoticeConfig(resp.data);
        setData(normalized);
        setDraft(normalized);
      }
    } finally {
      setFetching(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const hasDirty = useMemo(() => {
    return JSON.stringify(normalizeNoticeConfig(draft)) !== JSON.stringify(data);
  }, [draft, data]);

  const currentValues = isEditing ? draft : data;

  const handleCancel = () => {
    setDraft(data);
    setIsEditing(false);
  };

  const handleSave = async () => {
    if (!canWrite) return;
    setSaving(true);
    try {
      const nextNotice = normalizeNoticeConfig({
        ...draft,
        appVersion: "",
        version: "",
      });
      const resp = await ApiPost("/manage/config/notice/update", nextNotice);
      if (resp.code === 0) {
        message.success(resp.msg || "公告配置已保存");
        setData(nextNotice);
        setDraft(nextNotice);
        setIsEditing(false);
        await refreshSiteConfig();
      } else {
        message.error(resp.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <Card
        className={styles.card}
        title={
          <Space size={8} align="center">
            <BellOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>站点公告配置</span>
          </Space>
        }
        extra={
          <Space size={8} align="center">
            {isEditing ? (
              <>
                <Button size="small" disabled={saving} onClick={handleCancel}>
                  取消
                </Button>
                <Button
                  size="small"
                  icon={<EyeOutlined />}
                  onClick={() => setPreviewOpen(true)}
                >
                  预览
                </Button>
                <Button
                  size="small"
                  type="primary"
                  icon={<SaveOutlined />}
                  disabled={!canWrite || !hasDirty}
                  loading={saving}
                  onClick={handleSave}
                >
                  保存公告
                </Button>
              </>
            ) : (
              <>
                <Button
                  size="small"
                  icon={<EyeOutlined />}
                  onClick={() => setPreviewOpen(true)}
                >
                  预览
                </Button>
                <Button
                  size="small"
                  type="primary"
                  icon={<EditOutlined />}
                  disabled={!canWrite}
                  onClick={() => {
                    setDraft(data);
                    setIsEditing(true);
                  }}
                >
                  编辑
                </Button>
              </>
            )}
          </Space>
        }
      >
        <Spin spinning={fetching} description="正在加载公告配置...">
          <Flex vertical gap={16}>
            {/* 总开关 */}
            <Flex align="center" justify="space-between">
              <Flex vertical gap={4}>
                <Typography.Text strong>启用站点公告</Typography.Text>
                <Typography.Text type="secondary">
                  开启后向访问用户弹窗提示；关闭后任何终端均不弹出
                </Typography.Text>
              </Flex>
              <Switch
                disabled={!isEditing || !canWrite}
                checked={currentValues.enabled}
                checkedChildren="开启"
                unCheckedChildren="关闭"
                onChange={(enabled) =>
                  setDraft((prev) => ({ ...prev, enabled }))
                }
              />
            </Flex>

            {/* 展示终端 */}
            <div className={styles.field}>
              <Typography.Text strong>生效展示终端</Typography.Text>
              <Space size={24} style={{ marginTop: 4 }}>
                <Checkbox
                  disabled={!isEditing || !canWrite}
                  checked={currentValues.showInWeb}
                  onChange={(e) =>
                    setDraft((prev) => ({ ...prev, showInWeb: e.target.checked }))
                  }
                >
                  Web 浏览器端
                </Checkbox>
                <Checkbox
                  disabled={!isEditing || !canWrite}
                  checked={currentValues.showInApp}
                  onChange={(e) =>
                    setDraft((prev) => ({ ...prev, showInApp: e.target.checked }))
                  }
                >
                  App 移动客户端
                </Checkbox>
              </Space>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                可按需勾选需要弹出的端；若未勾选任何端，则等同于不弹出
              </Typography.Text>
            </div>

            {/* 公告标题 */}
            <div className={styles.field}>
              <Flex justify="space-between" align="baseline">
                <Typography.Text strong>公告标题</Typography.Text>
                <Typography.Text type="secondary">
                  {currentValues.title.length}/{MAX_NOTICE_TITLE_LEN}
                </Typography.Text>
              </Flex>
              <Input
                disabled={!isEditing || !canWrite}
                maxLength={MAX_NOTICE_TITLE_LEN}
                placeholder={DEFAULT_NOTICE_TITLE}
                value={currentValues.title}
                onChange={(e) =>
                  setDraft((prev) => ({ ...prev, title: e.target.value }))
                }
              />
            </div>

            {/* 公告正文 */}
            <div className={styles.field}>
              <Flex justify="space-between" align="baseline">
                <Typography.Text strong>公告正文</Typography.Text>
                <Typography.Text type="secondary">
                  {currentValues.content.length}/{MAX_NOTICE_CONTENT_LEN}
                </Typography.Text>
              </Flex>
              <Input.TextArea
                disabled={!isEditing || !canWrite}
                maxLength={MAX_NOTICE_CONTENT_LEN}
                rows={4}
                placeholder="请输入公告内容，支持换行..."
                value={currentValues.content}
                onChange={(e) =>
                  setDraft((prev) => ({ ...prev, content: e.target.value }))
                }
              />
            </div>
          </Flex>
        </Spin>
      </Card>

      {/* 统一使用 NoticeModal 进行真实弹窗预览 */}
      <NoticeModal
        open={previewOpen}
        notice={currentValues}
        onClose={() => setPreviewOpen(false)}
      />
    </>
  );
}
