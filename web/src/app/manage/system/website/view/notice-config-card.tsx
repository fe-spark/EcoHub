"use client";

import React, { useEffect, useMemo, useState } from "react";
import { Button, Card, Flex, Input, Modal, Select, Space, Switch, Typography } from "antd";
import {
  BellOutlined,
  EyeOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import {
  DEFAULT_NOTICE_TITLE,
  MAX_NOTICE_CONTENT_LEN,
  MAX_NOTICE_TITLE_LEN,
  normalizeNoticeConfig,
  type NoticeConfig,
} from "@/lib/notice";
import type { SiteBasicPayload } from "./tip-config-card";
import styles from "./notice-config-card.module.less";

interface NoticeConfigCardProps {
  siteConfig: SiteBasicPayload;
  canWrite: boolean;
  onSave: (next: SiteBasicPayload) => Promise<boolean>;
}

export default function NoticeConfigCard({
  siteConfig,
  canWrite,
  onSave,
}: NoticeConfigCardProps) {
  const [draft, setDraft] = useState<NoticeConfig>(() =>
    normalizeNoticeConfig(siteConfig.notice)
  );
  const [previewOpen, setPreviewOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setDraft(normalizeNoticeConfig(siteConfig.notice));
  }, [siteConfig.notice]);

  const hasDirty = useMemo(() => {
    const orig = normalizeNoticeConfig(siteConfig.notice);
    return JSON.stringify(draft) !== JSON.stringify(orig);
  }, [draft, siteConfig.notice]);

  const versionTags = useMemo(() => {
    if (!draft.version.trim()) return [];
    return draft.version
      .split(/[,;\s]+/)
      .map((v) => v.trim())
      .filter(Boolean);
  }, [draft.version]);

  const handleVersionChange = (tags: string[]) => {
    const cleaned = tags.map((t) => t.trim()).filter(Boolean);
    setDraft((prev) => ({
      ...prev,
      version: cleaned.join(", "),
    }));
  };

  const handleSave = async () => {
    if (!canWrite) return;
    setSaving(true);
    try {
      const ok = await onSave({
        ...siteConfig,
        notice: normalizeNoticeConfig(draft),
      });
      if (ok) {
        setDraft(normalizeNoticeConfig(draft));
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
            <span>开屏公告配置</span>
          </Space>
        }
        extra={
          <Space size={8} align="center">
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
          </Space>
        }
      >
        <Flex vertical gap={20}>
          {/* 开关 */}
          <Flex align="center" justify="space-between">
            <Flex vertical gap={2}>
              <Typography.Text strong>启用开屏公告</Typography.Text>
              <Typography.Text type="secondary">
                开启后，App / Web 重新打开时将主动弹出提示窗口
              </Typography.Text>
            </Flex>
            <Switch
              disabled={!canWrite}
              checked={draft.enabled}
              onChange={(enabled) =>
                setDraft((prev) => ({ ...prev, enabled }))
              }
            />
          </Flex>

          {/* 公告标题 */}
          <div className={styles.field}>
            <Flex justify="space-between" align="baseline">
              <Typography.Text strong>公告标题</Typography.Text>
              <Typography.Text type="secondary">
                {draft.title.length}/{MAX_NOTICE_TITLE_LEN}
              </Typography.Text>
            </Flex>
            <Input
              disabled={!canWrite}
              maxLength={MAX_NOTICE_TITLE_LEN}
              placeholder={DEFAULT_NOTICE_TITLE}
              value={draft.title}
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
                {draft.content.length}/{MAX_NOTICE_CONTENT_LEN}
              </Typography.Text>
            </Flex>
            <Input.TextArea
              disabled={!canWrite}
              maxLength={MAX_NOTICE_CONTENT_LEN}
              rows={4}
              placeholder="请输入公告内容，支持换行..."
              value={draft.content}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, content: e.target.value }))
              }
            />
          </div>

          {/* 目标版本号匹配（多 Tag 标签输入） */}
          <div className={styles.field}>
            <Flex justify="space-between" align="baseline">
              <Typography.Text strong>目标版本号（留空所有版本都弹）</Typography.Text>
              <Typography.Text type="secondary">
                {versionTags.length > 0 ? `已选 ${versionTags.length} 个版本` : "全部版本"}
              </Typography.Text>
            </Flex>
            <Select
              mode="tags"
              disabled={!canWrite}
              style={{ width: "100%" }}
              placeholder="默认留空面向所有版本生效；输入版本号后按回车添加（如 1.0.2、1.0.3）"
              value={versionTags}
              onChange={handleVersionChange}
              tokenSeparators={[",", " ", "，", "；", ";"]}
              options={[]}
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              留空表示全部版本都会弹出公告；若添加版本标签（如 1.0.3），则仅当客户端版本匹配时才弹出。
            </Typography.Text>
          </div>
        </Flex>
      </Card>

      {/* 预览弹窗 */}
      <Modal
        open={previewOpen}
        title={draft.title || DEFAULT_NOTICE_TITLE}
        onCancel={() => setPreviewOpen(false)}
        footer={[
          <Button
            key="ok"
            type="primary"
            onClick={() => setPreviewOpen(false)}
          >
            我知道了
          </Button>,
        ]}
      >
        <div className={styles.modalContent}>
          {draft.content || <Typography.Text type="secondary">（暂无公告正文内容）</Typography.Text>}
        </div>
      </Modal>
    </>
  );
}
