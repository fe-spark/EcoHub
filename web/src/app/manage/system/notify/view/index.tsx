"use client";

import { useCallback, useEffect, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Flex,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Switch,
  Typography,
} from "antd";
import { ReloadOutlined, SendOutlined, SaveOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

interface NotifyEventSwitches {
  collectBatchSummary: boolean;
  collectSourceFailed: boolean;
  collectFinalizeFailed: boolean;
  collectProgressStale: boolean;
  cronTaskFailed: boolean;
  cronTaskDone: boolean;
}

interface NotifyConfigValues {
  enabled: boolean;
  botToken: string;
  chatIds: string[];
  events: NotifyEventSwitches;
  includeFilmDetails: boolean;
  maxFilmsInMessage: number;
  minIntervalSec: number;
}

const DEFAULT_EVENTS: NotifyEventSwitches = {
  collectBatchSummary: true,
  collectSourceFailed: true,
  collectFinalizeFailed: true,
  collectProgressStale: true,
  cronTaskFailed: true,
  cronTaskDone: false,
};

const DEFAULT_CONFIG: NotifyConfigValues = {
  enabled: false,
  botToken: "",
  chatIds: [],
  events: { ...DEFAULT_EVENTS },
  includeFilmDetails: true,
  maxFilmsInMessage: 30,
  minIntervalSec: 60,
};

const EVENT_OPTIONS: { field: keyof NotifyEventSwitches; label: string; hint: string }[] = [
  { field: "collectBatchSummary", label: "采集结果摘要", hint: "整批结束后推送成功/失败源与影片明细" },
  { field: "collectSourceFailed", label: "单源失败即时告警", hint: "某采集源连续失败被终止时立即通知" },
  { field: "collectFinalizeFailed", label: "收尾发布失败", hint: "快照/摘要刷新失败" },
  { field: "collectProgressStale", label: "进度超时", hint: "采集进度卡住被标失败" },
  { field: "cronTaskFailed", label: "定时任务失败", hint: "如孤儿清理失败" },
  { field: "cronTaskDone", label: "定时任务完成", hint: "默关，避免打扰" },
];

function normalizeConfig(data: Partial<NotifyConfigValues> | undefined): NotifyConfigValues {
  return {
    enabled: Boolean(data?.enabled),
    botToken: String(data?.botToken ?? ""),
    chatIds: Array.isArray(data?.chatIds) ? data!.chatIds.map(String).filter(Boolean) : [],
    events: {
      ...DEFAULT_EVENTS,
      ...(data?.events ?? {}),
    },
    includeFilmDetails: data?.includeFilmDetails !== false,
    maxFilmsInMessage: Number(data?.maxFilmsInMessage || 30),
    minIntervalSec: Number(data?.minIntervalSec ?? 60),
  };
}

export default function NotifyConfigPageView() {
  const [form] = Form.useForm<NotifyConfigValues>();
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const { message } = useAppMessage();

  const loadConfig = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/notify");
      if (resp.code === 0) {
        const next = normalizeConfig(resp.data);
        form.setFieldsValue(next);
        return;
      }
      message.error(resp.msg || "加载通知配置失败");
    } finally {
      setFetching(false);
    }
  }, [form, message]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  const handleSave = async () => {
    try {
      const values = await form.validateFields();
      setSaving(true);
      const resp = await ApiPost("/manage/config/notify/update", {
        ...values,
        chatIds: values.chatIds || [],
      });
      if (resp.code === 0) {
        message.success(resp.msg || "保存成功");
        form.setFieldsValue(normalizeConfig(resp.data));
        return;
      }
      message.error(resp.msg || "保存失败");
    } catch {
      // validateFields 失败时不提示
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
    setTesting(true);
    try {
      const resp = await ApiPost("/manage/config/notify/test", {});
      if (resp.code === 0) {
        const data = resp.data as { sent?: number; failed?: { chatId: string; error: string }[] } | undefined;
        const sent = data?.sent ?? 0;
        const failed = data?.failed?.length ?? 0;
        if (failed > 0) {
          message.warning(`已发送 ${sent} 个，失败 ${failed} 个`);
        } else {
          message.success(resp.msg || `测试消息已发送（${sent}）`);
        }
        return;
      }
      message.error(resp.msg || "测试发送失败");
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className={styles.formPanel}>
      <ManagePageHeader
        title="通知设置"
        description="配置 Telegram Bot，在采集完成、失败或定时任务异常时推送到一个或多个 Chat。"
        actions={
          <Space wrap>
            <Button icon={<ReloadOutlined />} loading={fetching} onClick={() => void loadConfig()}>
              刷新
            </Button>
            <Button icon={<SendOutlined />} loading={testing} onClick={() => void handleTest()}>
              发送测试
            </Button>
            <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={() => void handleSave()}>
              保存
            </Button>
          </Space>
        }
      />

      <Spin spinning={fetching} description="正在加载通知配置...">
        <Form form={form} layout="vertical" initialValues={DEFAULT_CONFIG} disabled={fetching || saving}>
          <Card size="small" title="基础配置">
            <Form.Item label="启用通知" name="enabled" valuePropName="checked">
              <Switch checkedChildren="开" unCheckedChildren="关" />
            </Form.Item>

            <Form.Item
              label="Bot Token"
              name="botToken"
              extra="通过 @BotFather 创建机器人后获得。保存后展示为脱敏值，留空或保持脱敏内容表示不修改 Token。"
            >
              <Input.Password placeholder="例如 123456:ABC-DEF..." autoComplete="off" />
            </Form.Item>

            <Form.Item
              label="Chat ID 列表"
              name="chatIds"
              extra="支持个人/群/频道。数字 ID（群组可为负数）或 @channelusername。可添加多个。"
              rules={[
                {
                  validator: async (_, value: string[]) => {
                    const enabled = form.getFieldValue("enabled");
                    if (enabled && (!value || value.length === 0)) {
                      throw new Error("启用通知时至少填写一个 Chat ID");
                    }
                  },
                },
              ]}
            >
              <Select
                className={styles.chatIds}
                mode="tags"
                tokenSeparators={[",", " ", "\n"]}
                placeholder="输入后回车添加，例如 123456789 或 -100xxxxxxxxxx"
                allowClear
              />
            </Form.Item>

            <Alert
              type="info"
              showIcon
              className={styles.helpText}
              message="获取 Chat ID"
              description="个人可向 @userinfobot 发消息获取；群组可将机器人拉入后使用群组 ID（通常为负数）。配置保存后再点「发送测试」验证。"
            />
          </Card>

          <Card size="small" title="通知事件" style={{ marginTop: 16 }}>
            <div className={styles.eventGrid}>
              {EVENT_OPTIONS.map((item) => (
                <Form.Item
                  key={item.field}
                  name={["events", item.field]}
                  valuePropName="checked"
                  style={{ marginBottom: 8 }}
                >
                  <Checkbox>
                    <Flex vertical gap={0}>
                      <Typography.Text>{item.label}</Typography.Text>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {item.hint}
                      </Typography.Text>
                    </Flex>
                  </Checkbox>
                </Form.Item>
              ))}
            </div>
          </Card>

          <Card size="small" title="内容与限流" style={{ marginTop: 16 }}>
            <Form.Item
              label="采集摘要附带影片明细"
              name="includeFilmDetails"
              valuePropName="checked"
              extra="关闭后只发送各源成功/失败计数。单片更新始终不附带影片明细。"
            >
              <Switch checkedChildren="开" unCheckedChildren="关" />
            </Form.Item>

            <Form.Item
              label="消息内最多展示影片数"
              name="maxFilmsInMessage"
              extra="范围 1–80，超出部分显示「另有 N 部」。"
            >
              <InputNumber min={1} max={80} style={{ width: 160 }} />
            </Form.Item>

            <Form.Item
              label="同类事件最小间隔（秒）"
              name="minIntervalSec"
              extra="同一事件源在间隔内只推送一次，避免刷屏。0 表示不限流。"
            >
              <InputNumber min={0} max={3600} style={{ width: 160 }} addonAfter="秒" />
            </Form.Item>
          </Card>
        </Form>
      </Spin>
    </div>
  );
}
