"use client";

import React, { useEffect, useState, useCallback } from "react";
import {
  Card,
  Form,
  Input,
  InputNumber,
  Switch,
  Select,
  Button,
  Space,
  Spin,
  Tag,
  Divider,
  Flex,
  Row,
  Col,
  Typography,
} from "antd";
import {
  BellOutlined,
  ApiOutlined,
  SendOutlined,
  ControlOutlined,
  SlidersOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import ProxyOutletBanner from "@/app/manage/components/proxy-outlet-banner";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import { useFormDirtyTracker } from "@/lib/use-form-dirty";
import styles from "./style.module.css";

interface NotifyEventSwitches {
  collectBatchSummary: boolean;
  collectSourceFailed: boolean;
  cronTaskFailed: boolean;
  sourceOrderChange: boolean;
}

interface NotifyConfigValues {
  enabled: boolean;
  botToken: string;
  chatIds: string[];
  events: NotifyEventSwitches;
  onlyNotifyOnUpdate: boolean;
  maxFilmsInMessage: number;
}

const DEFAULT_EVENTS: NotifyEventSwitches = {
  collectBatchSummary: true,
  collectSourceFailed: true,
  cronTaskFailed: true,
  sourceOrderChange: true,
};

const DEFAULT_CONFIG: NotifyConfigValues = {
  enabled: false,
  botToken: "",
  chatIds: [],
  events: { ...DEFAULT_EVENTS },
  onlyNotifyOnUpdate: true,
  maxFilmsInMessage: 15,
};

const EVENT_ITEMS: {
  field: keyof NotifyEventSwitches;
  title: string;
  badge: string;
  color: string;
  desc: string;
}[] = [
  {
    field: "collectBatchSummary",
    title: "采集批次汇总",
    badge: "核心简报",
    color: "blue",
    desc: "整批采集结束后推送各源统计与分类更新片单（支持按钮翻页）",
  },
  {
    field: "collectSourceFailed",
    title: "单源采集失败",
    badge: "故障告警",
    color: "red",
    desc: "采集源连续请求失败达到阈值被终止时即时告警",
  },
  {
    field: "cronTaskFailed",
    title: "定时任务异常",
    badge: "任务告警",
    color: "volcano",
    desc: "后台定时调度执行报错时推送告警",
  },
  {
    field: "sourceOrderChange",
    title: "首选站与排序变更",
    badge: "配置变更",
    color: "cyan",
    desc: "采集站拖拽排序或首选采集站发生切换时推送通知",
  },
];

function normalizeNotifyConfig(raw?: Partial<NotifyConfigValues>): NotifyConfigValues {
  return {
    enabled: Boolean(raw?.enabled),
    botToken: String(raw?.botToken ?? "").trim(),
    chatIds: Array.isArray(raw?.chatIds) ? raw.chatIds.map(String).filter(Boolean) : [],
    events: { ...DEFAULT_EVENTS, ...(raw?.events ?? {}) },
    onlyNotifyOnUpdate: raw?.onlyNotifyOnUpdate !== false,
    maxFilmsInMessage: Number(raw?.maxFilmsInMessage || 15),
  };
}

interface NotifyConfigPageViewProps {
  embedded?: boolean;
}

/**
 * Telegram 通知推送配置视图组件
 * @param embedded 是否内嵌于系统设置 Tab 页展示
 */
export default function NotifyConfigPageView({ embedded = false }: NotifyConfigPageViewProps) {
  const [form] = Form.useForm<NotifyConfigValues>();
  const [fetching, setFetching] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [serverData, setServerData] = useState<NotifyConfigValues>(DEFAULT_CONFIG);

  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();
  const canOperate = canWrite && isAdmin;

  const { isDirty, onValuesChange: trackFormChange, setBaseline, resetToBaseline } =
    useFormDirtyTracker(form, serverData);

  const fetchConfig = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet<NotifyConfigValues>("/manage/notify/config");
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeNotifyConfig(resp.data);
        setServerData(normalized);
        form.setFieldsValue(normalized);
        setBaseline(normalized);
      }
    } finally {
      setFetching(false);
    }
  }, [form, setBaseline]);

  useEffect(() => {
    void fetchConfig();
  }, [fetchConfig]);

  const handleSave = async () => {
    if (!canOperate) return;
    try {
      const values = await form.validateFields();
      setSaving(true);
      const payload = normalizeNotifyConfig({ ...serverData, ...values });
      const resp = await ApiPost<NotifyConfigValues>("/manage/notify/config/update", payload);
      if (resp.code === 0) {
        message.success(resp.msg || "通知配置已保存");
        const updated = normalizeNotifyConfig(resp.data || payload);
        setServerData(updated);
        form.setFieldsValue(updated);
        setBaseline(updated);
        return;
      }
      message.error(resp.msg || "保存失败");
    } catch {
      // 表单校验未通过
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
    if (!canOperate) return;
    const botToken = String(form.getFieldValue("botToken") || "").trim();
    const chatIds: string[] = form.getFieldValue("chatIds") || [];
    if (!botToken) {
      message.error("请先填写 Bot Token");
      return;
    }
    if (chatIds.length === 0) {
      message.error("请至少填写一个 Chat ID");
      return;
    }
    setTesting(true);
    try {
      const resp = await ApiPost("/manage/notify/config/test", { botToken, chatIds });
      if (resp.code === 0) {
        message.success(resp.msg || "测试消息已发送");
      } else {
        message.error(resp.msg || "测试发送失败");
      }
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className={styles.page}>
      {embedded ? null : (
        <ManagePageHeader
          title="通知推送"
          description="配置 Telegram 机器人采集汇总战报、分类片单翻页与系统异常告警。"
        />
      )}

      <Card
        className={styles.card}
        styles={{
          header: {
            borderBottom: "1px solid var(--ant-color-border-secondary, rgba(0, 0, 0, 0.06))",
            padding: "0 24px",
            minHeight: 54,
          },
          body: {
            padding: "26px 30px",
          },
        }}
        title={
          <Space size={8} align="center">
            <BellOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>Telegram 机器人通知配置</span>
          </Space>
        }
        extra={!isAdmin ? <Tag color="default">仅超级管理员可操作</Tag> : null}
      >
        <Spin spinning={fetching} description="正在加载通知配置...">
          <Form
            form={form}
            layout="vertical"
            initialValues={DEFAULT_CONFIG}
            disabled={!canOperate}
            onValuesChange={trackFormChange}
          >
            <Flex vertical gap={0} className={styles.contentStack}>
              <ProxyOutletBanner module="notify" />

              <div className={styles.masterSwitchBlock}>
                <Flex align="center" justify="space-between" gap={20}>
                  <Flex vertical gap={4}>
                    <span className={styles.masterSwitchTitle}>启用 Telegram 消息通知</span>
                    <span className={styles.masterSwitchDesc}>
                      开启后自动推送采集汇总与异常告警，并启动后台轮询支持 TG 消息内分类切换与按钮翻页
                    </span>
                  </Flex>
                  <Form.Item name="enabled" valuePropName="checked" noStyle>
                    <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                  </Form.Item>
                </Flex>
              </div>

              <Divider style={{ margin: "28px 0" }} />

              <div className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <ApiOutlined style={{ color: "var(--ant-color-primary)" }} />
                  <span>机器人凭证与接收目标</span>
                </div>

                <Row gutter={[24, 16]}>
                  <Col xs={24} md={12}>
                    <Form.Item
                      noStyle
                      shouldUpdate={(prev, cur) => prev.enabled !== cur.enabled}
                    >
                      {({ getFieldValue }) => {
                        const isEnabled = Boolean(getFieldValue("enabled"));
                        return (
                          <Form.Item
                            label="Bot Token"
                            name="botToken"
                            rules={
                              isEnabled
                                ? [{ required: true, message: "请输入 Telegram Bot Token" }]
                                : []
                            }
                            tooltip="通过 @BotFather 创建机器人获取的 Token"
                          >
                            <Input.Password placeholder="123456789:AAH..." allowClear />
                          </Form.Item>
                        );
                      }}
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item
                      noStyle
                      shouldUpdate={(prev, cur) => prev.enabled !== cur.enabled}
                    >
                      {({ getFieldValue }) => {
                        const isEnabled = Boolean(getFieldValue("enabled"));
                        return (
                          <Form.Item
                            label="接收 Chat ID 列表"
                            name="chatIds"
                            rules={
                              isEnabled
                                ? [{ required: true, message: "请至少输入一个 Chat ID" }]
                                : []
                            }
                            tooltip="输入个人/群组/频道 ID 回车添加；群话题支持 chatId:threadId 格式"
                          >
                            <Select
                              mode="tags"
                              placeholder="输入 Chat ID 后回车（如 -100123456）"
                              tokenSeparators={[",", " ", "\n"]}
                              open={false}
                            />
                          </Form.Item>
                        );
                      }}
                    </Form.Item>
                  </Col>
                </Row>

                <div className={styles.testFooter}>
                  <Space size={8} align="center">
                    <SendOutlined style={{ color: "var(--ant-color-primary)" }} />
                    <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                      支持直接使用当前填写的 Token 与 Chat ID 发送测试消息，无需先保存配置。
                    </Typography.Text>
                  </Space>
                  <Button
                    icon={<SendOutlined />}
                    loading={testing}
                    disabled={!canOperate}
                    onClick={() => void handleTest()}
                  >
                    发送测试消息
                  </Button>
                </div>
              </div>

              <Divider style={{ margin: "28px 0" }} />

              <div className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <ControlOutlined style={{ color: "#1677ff" }} />
                  <span>通知事件开关</span>
                </div>

                <Row gutter={[16, 12]}>
                  {EVENT_ITEMS.map((item) => (
                    <Col xs={24} sm={12} key={item.field}>
                      <div className={styles.eventCard}>
                        <Flex justify="space-between" align="center" gap={12}>
                          <div>
                            <Space size={8}>
                              <span className={styles.eventTitle}>{item.title}</span>
                              <Tag color={item.color} style={{ marginInlineEnd: 0 }}>
                                {item.badge}
                              </Tag>
                            </Space>
                            <div className={styles.eventDesc}>{item.desc}</div>
                          </div>
                          <Form.Item
                            name={["events", item.field]}
                            valuePropName="checked"
                            noStyle
                          >
                            <Switch size="small" />
                          </Form.Item>
                        </Flex>
                      </div>
                    </Col>
                  ))}
                </Row>
              </div>

              <Divider style={{ margin: "28px 0" }} />

              <div className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <SlidersOutlined style={{ color: "#722ed1" }} />
                  <span>推送与翻页策略</span>
                </div>

                <Row gutter={[24, 16]} align="middle">
                  <Col xs={24} md={12}>
                    <div className={styles.eventCard}>
                      <Flex justify="space-between" align="center" gap={12}>
                        <div>
                          <div className={styles.eventTitle}>无更新且无失败时静默</div>
                          <div className={styles.eventDesc}>
                            本轮采集若无新增/追更影片且所有站点均无报错，自动跳过汇总推送
                          </div>
                        </div>
                        <Form.Item name="onlyNotifyOnUpdate" valuePropName="checked" noStyle>
                          <Switch size="small" />
                        </Form.Item>
                      </Flex>
                    </div>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="单页片单展示条数 (1 ~ 20)"
                      name="maxFilmsInMessage"
                      style={{ marginBottom: 0 }}
                    >
                      <InputNumber min={1} max={20} style={{ width: "100%" }} />
                    </Form.Item>
                  </Col>
                </Row>
              </div>
            </Flex>
          </Form>
        </Spin>
      </Card>

      <UnsavedChangesBar
        visible={isDirty}
        saving={saving}
        onDiscard={resetToBaseline}
        onSave={() => void handleSave()}
      />
    </div>
  );
}
