"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Avatar,
  Button,
  Card,
  Flex,
  Input,
  Space,
  Spin,
  Switch,
  Typography,
} from "antd";
import {
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  HeartOutlined,
  PictureOutlined,
  PlusOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import ImagePicker from "@/app/manage/components/image-picker";
import TipModal from "@/components/public/TipModal";
import {
  DEFAULT_TIP_MESSAGE,
  DEFAULT_TIP_TITLE,
  MAX_TIP_CHANNELS,
  MAX_TIP_LABEL_LEN,
  MAX_TIP_LINK_LEN,
  MAX_TIP_MESSAGE_LEN,
  MAX_TIP_TITLE_LEN,
  createDefaultTipConfig,
  normalizeTipConfig,
  serializeTipConfig,
  type TipChannel,
  type TipChannelKey,
  type TipConfig,
} from "@/lib/tip";
import styles from "./tip-config-card.module.less";

interface TipConfigCardProps {
  canWrite: boolean;
}

type DraftChannel = TipChannel & { uid: string };

interface DraftTip {
  enabled: boolean;
  title: string;
  message: string;
  channels: DraftChannel[];
}

function newChannelUid() {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `ch-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function toDraft(tip?: TipConfig | null): DraftTip {
  const normalized = normalizeTipConfig(tip);
  return {
    ...normalized,
    channels: normalized.channels.map((channel) => ({ ...channel, uid: newChannelUid() })),
  };
}

function emptyChannel(key: TipChannelKey, label: string): DraftChannel {
  return { uid: newChannelUid(), key, label, qrImage: "", link: "" };
}

export default function TipConfigCard({ canWrite }: TipConfigCardProps) {
  const [data, setData] = useState<TipConfig>(createDefaultTipConfig);
  const [draft, setDraft] = useState<DraftTip>(() => toDraft(createDefaultTipConfig()));
  const [isEditing, setIsEditing] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [pickerUid, setPickerUid] = useState<string | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);

  const { message } = useAppMessage();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const loadData = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/tip");
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeTipConfig(resp.data);
        setData(normalized);
        setDraft(toDraft(normalized));
      }
    } finally {
      setFetching(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const hasDirty = useMemo(() => {
    const currentTip = normalizeTipConfig({
      enabled: draft.enabled,
      title: draft.title,
      message: draft.message,
      channels: draft.channels,
    });
    return serializeTipConfig(currentTip) !== serializeTipConfig(data);
  }, [draft, data]);

  const canAdd = draft.channels.length < MAX_TIP_CHANNELS;

  const updateChannel = (uid: string, patch: Partial<TipChannel>) => {
    setDraft((prev) => ({
      ...prev,
      channels: prev.channels.map((channel) =>
        channel.uid === uid ? { ...channel, ...patch } : channel
      ),
    }));
  };

  const addChannel = () => {
    if (!canAdd) return;
    setDraft((prev) => ({
      ...prev,
      channels: [...prev.channels, emptyChannel("custom", "")],
    }));
  };

  const removeChannel = (uid: string) => {
    if (pickerUid === uid) {
      setPickerUid(null);
    }
    setDraft((prev) => {
      const next = prev.channels.filter((channel) => channel.uid !== uid);
      const fallback = createDefaultTipConfig().channels.map((channel) => ({
        ...channel,
        uid: newChannelUid(),
      }));
      return {
        ...prev,
        channels: next.length > 0 ? next : fallback,
      };
    });
  };

  const handleCancel = () => {
    setDraft(toDraft(data));
    setIsEditing(false);
  };

  const handleSave = async () => {
    if (!canWrite) return;
    setSaving(true);
    try {
      const nextTip = normalizeTipConfig({
        enabled: draft.enabled,
        title: draft.title,
        message: draft.message,
        channels: draft.channels,
      });
      const resp = await ApiPost("/manage/config/tip/update", nextTip);
      if (resp.code === 0) {
        message.success(resp.msg || "赞赏配置已保存");
        setData(nextTip);
        setDraft(toDraft(nextTip));
        setIsEditing(false);
        await refreshSiteConfig();
      } else {
        message.error(resp.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  const currentValues = isEditing ? draft : toDraft(data);

  const previewTip = useMemo(
    () =>
      normalizeTipConfig({
        enabled: currentValues.enabled,
        title: currentValues.title,
        message: currentValues.message,
        channels: currentValues.channels,
      }),
    [currentValues]
  );

  return (
    <Card
      className={styles.card}
      title={
        <Space size={8} align="center">
          <HeartOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>赞赏支持配置</span>
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
                保存赞赏
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
                  setDraft(toDraft(data));
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
      <Spin spinning={fetching} description="正在加载赞赏配置...">
        <Flex vertical gap={16}>
          {/* 前台显示开关 */}
          <Flex align="center" justify="space-between" gap={12} wrap>
            <div>
              <Typography.Text strong>前台显示赞赏</Typography.Text>
              <div>
                <Typography.Text type="secondary">
                  开启且至少配置一个收款码或外链后，页脚才会出现入口。
                </Typography.Text>
              </div>
            </div>
            <Switch
              checked={currentValues.enabled}
              checkedChildren="开启"
              unCheckedChildren="关闭"
              disabled={!isEditing || !canWrite}
              onChange={(enabled) => setDraft((prev) => ({ ...prev, enabled }))}
            />
          </Flex>

          {/* 赞赏弹窗标题 */}
          <div className={styles.field}>
            <Flex justify="space-between" align="baseline">
              <Typography.Text strong>赞赏弹窗标题</Typography.Text>
              <Typography.Text type="secondary">
                {currentValues.title.length}/{MAX_TIP_TITLE_LEN}
              </Typography.Text>
            </Flex>
            <Input
              value={currentValues.title}
              maxLength={MAX_TIP_TITLE_LEN}
              placeholder={DEFAULT_TIP_TITLE}
              disabled={!isEditing || !canWrite}
              onChange={(event) =>
                setDraft((prev) => ({ ...prev, title: event.target.value }))
              }
            />
          </div>

          {/* 引导文案 */}
          <div className={styles.field}>
            <Flex justify="space-between" align="baseline">
              <Typography.Text strong>引导文案</Typography.Text>
              <Typography.Text type="secondary">
                {currentValues.message.length}/{MAX_TIP_MESSAGE_LEN}
              </Typography.Text>
            </Flex>
            <Input.TextArea
              value={currentValues.message}
              maxLength={MAX_TIP_MESSAGE_LEN}
              placeholder={DEFAULT_TIP_MESSAGE}
              rows={2}
              disabled={!isEditing || !canWrite}
              onChange={(event) =>
                setDraft((prev) => ({ ...prev, message: event.target.value }))
              }
            />
          </div>

          {/* 渠道列表 */}
          <div className={styles.channelsHead}>
            <Typography.Text strong>收款渠道与外链</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              支持微信、支付宝、爱发电等；可同时上传收款码图片或填写外部赞助链接。
            </Typography.Text>
          </div>

          {currentValues.channels.map((channel, index) => {
            const preview = channel.qrImage.trim();
            return (
              <div key={channel.uid} className={styles.channel}>
                <div className={styles.channelMain}>
                  <Input
                    className={styles.channelLabel}
                    value={channel.label}
                    maxLength={MAX_TIP_LABEL_LEN}
                    placeholder={`渠道 ${index + 1}`}
                    disabled={!isEditing || !canWrite}
                    onChange={(event) =>
                      updateChannel(channel.uid, { label: event.target.value })
                    }
                  />
                  <div className={styles.channelQr}>
                    {preview ? (
                      <Avatar
                        src={preview}
                        shape="square"
                        size={32}
                        style={{ borderRadius: 6, flexShrink: 0 }}
                      />
                    ) : null}
                    <Space.Compact className={styles.channelQrInput}>
                      <Input
                        value={channel.qrImage}
                        placeholder="收款码图片链接"
                        disabled={!isEditing || !canWrite}
                        onChange={(event) =>
                          updateChannel(channel.uid, { qrImage: event.target.value })
                        }
                      />
                      <Button
                        icon={<PictureOutlined />}
                        disabled={!isEditing || !canWrite}
                        onClick={() => setPickerUid(channel.uid)}
                      >
                        选图
                      </Button>
                    </Space.Compact>
                  </div>
                  <Input
                    className={styles.channelLink}
                    value={channel.link}
                    maxLength={MAX_TIP_LINK_LEN}
                    placeholder="可选外链，如 afdian.com"
                    disabled={!isEditing || !canWrite}
                    onChange={(event) =>
                      updateChannel(channel.uid, { link: event.target.value })
                    }
                  />
                </div>
                <Button
                  type="text"
                  danger
                  icon={<DeleteOutlined />}
                  disabled={!isEditing || !canWrite}
                  onClick={() => removeChannel(channel.uid)}
                >
                  删除
                </Button>
              </div>
            );
          })}

          <div>
            <Button
              icon={<PlusOutlined />}
              disabled={!isEditing || !canWrite || !canAdd}
              onClick={addChannel}
            >
              添加渠道
            </Button>
          </div>
        </Flex>
      </Spin>

      <ImagePicker
        open={pickerUid !== null}
        title="从素材中心选择收款码"
        onCancel={() => setPickerUid(null)}
        onSelect={(link) => {
          if (pickerUid) {
            updateChannel(pickerUid, { qrImage: link });
          }
          setPickerUid(null);
        }}
      />

      <TipModal
        open={previewOpen}
        tip={previewTip}
        onClose={() => setPreviewOpen(false)}
      />
    </Card>
  );
}
