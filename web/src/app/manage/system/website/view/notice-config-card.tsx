"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Button,
  Card,
  Checkbox,
  Flex,
  Input,
  Segmented,
  Space,
  Spin,
  Switch,
  Tooltip,
  Typography,
} from "antd";
import type { GetRef } from "antd";
import {
  BellOutlined,
  BoldOutlined,
  CodeOutlined,
  EditOutlined,
  EyeOutlined,
  FontSizeOutlined,
  ItalicOutlined,
  LinkOutlined,
  SaveOutlined,
  UnorderedListOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import NoticeMarkdown from "@/components/public/NoticeMarkdown";
import NoticeModal from "@/components/public/NoticeModal";
import {
  DEFAULT_NOTICE_TITLE,
  MAX_NOTICE_CONTENT_LEN,
  MAX_NOTICE_TITLE_LEN,
  createDefaultNoticeConfig,
  normalizeNoticeConfig,
  type NoticeConfig,
} from "@/lib/notice";
import {
  type MarkdownActionResult,
  toggleLinePrefix,
  toggleMarkdownLink,
  toggleMarkdownWrap,
} from "@/lib/markdown-helper";
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
  const [editorTab, setEditorTab] = useState<"write" | "preview">("write");
  const textareaRef = useRef<GetRef<typeof Input.TextArea>>(null);
  const { message } = useAppMessage();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const applyToolbarResult = (
    res: MarkdownActionResult,
    textarea: HTMLTextAreaElement
  ) => {
    if (res.newContent.length > MAX_NOTICE_CONTENT_LEN) {
      message.warning(`公告正文不能超过 ${MAX_NOTICE_CONTENT_LEN} 字`);
      return;
    }
    setDraft((prev) => ({ ...prev, content: res.newContent }));
    setTimeout(() => {
      textarea.focus();
      textarea.setSelectionRange(res.selectionStart, res.selectionEnd);
    }, 0);
  };

  const handleToggleWrap = (tag: string, placeholder: string = "文本") => {
    if (!isEditing || !canWrite) return;
    const textarea = textareaRef.current?.resizableTextArea?.textArea;
    if (!textarea) return;

    applyToolbarResult(
      toggleMarkdownWrap(
        draft.content || "",
        textarea.selectionStart,
        textarea.selectionEnd,
        tag,
        placeholder
      ),
      textarea
    );
  };

  const handleTogglePrefix = (prefix: string, placeholder: string = "内容") => {
    if (!isEditing || !canWrite) return;
    const textarea = textareaRef.current?.resizableTextArea?.textArea;
    if (!textarea) return;

    applyToolbarResult(
      toggleLinePrefix(
        draft.content || "",
        textarea.selectionStart,
        textarea.selectionEnd,
        prefix,
        placeholder
      ),
      textarea
    );
  };

  const handleToggleLink = () => {
    if (!isEditing || !canWrite) return;
    const textarea = textareaRef.current?.resizableTextArea?.textArea;
    if (!textarea) return;

    applyToolbarResult(
      toggleMarkdownLink(
        draft.content || "",
        textarea.selectionStart,
        textarea.selectionEnd
      ),
      textarea
    );
  };

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
          <div className={styles.extraActions}>
            <Space size={8} align="center" wrap>
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
          </div>
        }
      >
        <Spin spinning={fetching} description="正在加载公告配置...">
          <Flex vertical gap={16}>
            {/* 总开关 */}
            <Flex align="center" justify="space-between" wrap="wrap" gap={12}>
              <Flex vertical gap={4} style={{ flex: 1, minWidth: 180 }}>
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
              <Space size={16} wrap style={{ marginTop: 4 }}>
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

              <div className={styles.editorContainer}>
                {/* 顶部工具栏与编写/预览 Tab 切换 */}
                <div className={styles.editorHeader}>
                  <Segmented
                    size="small"
                    value={editorTab}
                    onChange={(val) => setEditorTab(val as "write" | "preview")}
                    options={[
                      { label: "编写", value: "write", icon: <EditOutlined /> },
                      { label: "预览", value: "preview", icon: <EyeOutlined /> },
                    ]}
                  />

                  {editorTab === "write" && (
                    <div className={styles.editorToolbar}>
                      <Tooltip title="标题 (### )">
                        <Button
                          type="text"
                          size="small"
                          disabled={!isEditing || !canWrite}
                          icon={<FontSizeOutlined />}
                          className={styles.toolBtn}
                          onClick={() => handleTogglePrefix("### ", "标题内容")}
                        />
                      </Tooltip>
                      <Tooltip title="加粗 (**text**)">
                        <Button
                          type="text"
                          size="small"
                          disabled={!isEditing || !canWrite}
                          icon={<BoldOutlined />}
                          className={styles.toolBtn}
                          onClick={() => handleToggleWrap("**", "粗体内容")}
                        />
                      </Tooltip>
                      <Tooltip title="斜体 (*text*)">
                        <Button
                          type="text"
                          size="small"
                          disabled={!isEditing || !canWrite}
                          icon={<ItalicOutlined />}
                          className={styles.toolBtn}
                          onClick={() => handleToggleWrap("*", "斜体内容")}
                        />
                      </Tooltip>
                      <Tooltip title="无序列表 (- text)">
                        <Button
                          type="text"
                          size="small"
                          disabled={!isEditing || !canWrite}
                          icon={<UnorderedListOutlined />}
                          className={styles.toolBtn}
                          onClick={() => handleTogglePrefix("- ", "列表条目")}
                        />
                      </Tooltip>
                      <Tooltip title="超链接 [text](url)">
                        <Button
                          type="text"
                          size="small"
                          disabled={!isEditing || !canWrite}
                          icon={<LinkOutlined />}
                          className={styles.toolBtn}
                          onClick={handleToggleLink}
                        />
                      </Tooltip>
                      <Tooltip title="行内代码 (`code`)">
                        <Button
                          type="text"
                          size="small"
                          disabled={!isEditing || !canWrite}
                          icon={<CodeOutlined />}
                          className={styles.toolBtn}
                          onClick={() => handleToggleWrap("`", "代码")}
                        />
                      </Tooltip>
                    </div>
                  )}
                </div>

                {/* 编辑/预览主区域 */}
                {editorTab === "write" ? (
                  <Input.TextArea
                    ref={textareaRef}
                    className={styles.editorTextarea}
                    disabled={!isEditing || !canWrite}
                    maxLength={MAX_NOTICE_CONTENT_LEN}
                    rows={6}
                    placeholder={`支持 Markdown 排版，例如：\n### 维护通知\n- 站点优化升级完成\n- 支持 [访问帮助文档](https://...)\n**重要提示**：请按需刷新页面`}
                    value={currentValues.content}
                    onChange={(e) =>
                      setDraft((prev) => ({ ...prev, content: e.target.value }))
                    }
                  />
                ) : (
                  <div className={styles.previewPanel}>
                    {currentValues.content ? (
                      <div className={styles.markdownBody}>
                        <NoticeMarkdown content={currentValues.content} />
                      </div>
                    ) : (
                      <p className={styles.previewEmpty}>暂无可预览的 Markdown 内容</p>
                    )}
                  </div>
                )}

                <div className={styles.editorFooter}>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    Markdown
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    {currentValues.content.length} / {MAX_NOTICE_CONTENT_LEN}
                  </Typography.Text>
                </div>
              </div>
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
