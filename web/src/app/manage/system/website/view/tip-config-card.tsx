"use client";

import React, { useEffect, useMemo, useState } from "react";
import { Avatar, Button, Card, Flex, Input, Space, Switch, Typography } from "antd";
import {
  DeleteOutlined,
  EyeOutlined,
  HeartOutlined,
  PictureOutlined,
  PlusOutlined,
  SaveOutlined,
} from "@ant-design/icons";
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
import { type NoticeConfig } from "@/lib/notice";
import styles from "./tip-config-card.module.less";

export interface SiteBasicPayload {
  siteName: string;
  siteUrl: string;
  keyword: string;
  logo: string;
  state: boolean;
  describe: string;
  hint: string;
  tip: TipConfig;
  notice?: NoticeConfig;
}

interface TipConfigCardProps {
  siteConfig: SiteBasicPayload;
  canWrite: boolean;
  onSave: (next: SiteBasicPayload) => Promise<boolean>;
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

function toDraft(tip: TipConfig): DraftTip {
  const normalized = normalizeTipConfig(tip);
  return {
    ...normalized,
    channels: normalized.channels.map((channel) => ({ ...channel, uid: newChannelUid() })),
  };
}

function emptyChannel(key: TipChannelKey, label: string): DraftChannel {
  return { uid: newChannelUid(), key, label, qrImage: "", link: "" };
}

export default function TipConfigCard({
  siteConfig,
  canWrite,
  onSave,
}: TipConfigCardProps) {
  const serverTipKey = useMemo(() => serializeTipConfig(siteConfig.tip), [siteConfig.tip]);
  const [draft, setDraft] = useState<DraftTip>(() => toDraft(siteConfig.tip));
  const [pickerUid, setPickerUid] = useState<string | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setDraft(toDraft(JSON.parse(serverTipKey) as TipConfig));
  }, [serverTipKey]);

  const canAdd = draft.channels.length < MAX_TIP_CHANNELS;

  const updateChannel = (uid: string, patch: Partial<TipChannel>) => {
    setDraft((prev) => ({
      ...prev,
      channels: prev.channels.map((channel) => (channel.uid === uid ? { ...channel, ...patch } : channel)),
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

  const handleSave = async () => {
    setSaving(true);
    try {
      const nextTip = normalizeTipConfig({
        enabled: draft.enabled,
        title: draft.title,
        message: draft.message,
        channels: draft.channels,
      });
      const ok = await onSave({ ...siteConfig, tip: nextTip });
      if (ok) {
        setDraft(toDraft(nextTip));
      }
    } finally {
      setSaving(false);
    }
  };

  const previewTip = useMemo(
    () =>
      normalizeTipConfig({
        enabled: draft.enabled,
        title: draft.title,
        message: draft.message,
        channels: draft.channels,
      }),
    [draft],
  );

  return (
    <Card
      title={
        <Space size={8}>
          <HeartOutlined style={{ color: "#eb2f96" }} />
          <span>赞赏</span>
        </Space>
      }
      extra={
        <Space>
          <Button icon={<EyeOutlined />} onClick={() => setPreviewOpen(true)}>
            预览
          </Button>
          <Button
            type="primary"
            icon={<SaveOutlined />}
            loading={saving}
            disabled={!canWrite}
            onClick={() => void handleSave()}
          >
            保存
          </Button>
        </Space>
      }
      className={styles.card}
    >
      <Flex vertical gap={16}>
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
            checked={draft.enabled}
            checkedChildren="开启"
            unCheckedChildren="关闭"
            disabled={!canWrite}
            onChange={(enabled) => setDraft((prev) => ({ ...prev, enabled }))}
          />
        </Flex>

        <div className={styles.field}>
          <Typography.Text strong>标题</Typography.Text>
          <Input
            value={draft.title}
            maxLength={MAX_TIP_TITLE_LEN}
            placeholder={DEFAULT_TIP_TITLE}
            disabled={!canWrite}
            onChange={(event) => setDraft((prev) => ({ ...prev, title: event.target.value }))}
          />
        </div>

        <div className={styles.field}>
          <Typography.Text strong>感谢文案</Typography.Text>
          <Input.TextArea
            value={draft.message}
            maxLength={MAX_TIP_MESSAGE_LEN}
            autoSize={{ minRows: 2, maxRows: 4 }}
            placeholder={DEFAULT_TIP_MESSAGE}
            disabled={!canWrite}
            onChange={(event) => setDraft((prev) => ({ ...prev, message: event.target.value }))}
          />
        </div>

        <div className={styles.channelsHead}>
          <Typography.Text strong>收款渠道</Typography.Text>
          <Typography.Text type="secondary">
            最多 {MAX_TIP_CHANNELS} 个；外链可只填域名，保存时自动补 https://。
          </Typography.Text>
        </div>

        {draft.channels.map((channel) => {
          const preset = channel.key === "wechat" || channel.key === "alipay";
          return (
            <div key={channel.uid} className={styles.channel}>
              <div className={styles.channelMain}>
                <Input
                  className={styles.channelLabel}
                  value={channel.label}
                  maxLength={MAX_TIP_LABEL_LEN}
                  placeholder={preset ? (channel.key === "wechat" ? "微信" : "支付宝") : "渠道名称"}
                  disabled={!canWrite}
                  onChange={(event) => updateChannel(channel.uid, { label: event.target.value })}
                />
                <div className={styles.channelQr}>
                  {channel.qrImage ? (
                    <Avatar src={channel.qrImage} shape="square" size={32} style={{ borderRadius: 6, flex: "none" }} />
                  ) : null}
                  <Space.Compact className={styles.channelQrInput}>
                    <Input
                      value={channel.qrImage}
                      placeholder="收款码图片地址"
                      disabled={!canWrite}
                      onChange={(event) => updateChannel(channel.uid, { qrImage: event.target.value })}
                    />
                    <Button
                      icon={<PictureOutlined />}
                      disabled={!canWrite}
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
                  disabled={!canWrite}
                  onChange={(event) => updateChannel(channel.uid, { link: event.target.value })}
                />
              </div>
              <Button
                type="text"
                danger
                icon={<DeleteOutlined />}
                disabled={!canWrite}
                onClick={() => removeChannel(channel.uid)}
              >
                删除
              </Button>
            </div>
          );
        })}

        <div>
          <Button icon={<PlusOutlined />} disabled={!canWrite || !canAdd} onClick={addChannel}>
            添加渠道
          </Button>
        </div>
      </Flex>

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

      <TipModal open={previewOpen} tip={previewTip} onClose={() => setPreviewOpen(false)} />
    </Card>
  );
}
