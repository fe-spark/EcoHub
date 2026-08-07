"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Col,
  ConfigProvider,
  Divider,
  Flex,
  Form,
  Input,
  InputNumber,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  BellOutlined,
  CheckCircleOutlined,
  CheckOutlined,
  ClearOutlined,
  ControlOutlined,
  InfoCircleOutlined,
  LockOutlined,
  ReloadOutlined,
  RobotOutlined,
  SaveOutlined,
  SendOutlined,
  StopOutlined,
} from "@ant-design/icons";
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
  sourceConfigChanged: boolean;
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
  sourceConfigChanged: true,
};

const DEFAULT_CONFIG: NotifyConfigValues = {
  enabled: false,
  botToken: "",
  chatIds: [],
  events: { ...DEFAULT_EVENTS },
  includeFilmDetails: true,
  maxFilmsInMessage: 15,
  minIntervalSec: 60,
};

interface EventOption {
  field: keyof NotifyEventSwitches;
  label: string;
  badge: string;
  badgeColor: string;
  hint: string;
}

const EVENT_OPTIONS: EventOption[] = [
  {
    field: "collectBatchSummary",
    label: "采集结果摘要",
    badge: "批次汇总",
    badgeColor: "blue",
    hint: "整批采集结束后推送各源统计；更新列表含主站框架更新或附属站播放源更新的影片",
  },
  {
    field: "collectSourceFailed",
    label: "单源失败即时告警",
    badge: "核心告警",
    badgeColor: "red",
    hint: "某采集源连续失败达到上限被终止时立即触发推送",
  },
  {
    field: "collectFinalizeFailed",
    label: "收尾发布失败",
    badge: "系统异常",
    badgeColor: "orange",
    hint: "快照更新或摘要刷新失败时发送告警消息",
  },
  {
    field: "collectProgressStale",
    label: "采集进度超时",
    badge: "超时告警",
    badgeColor: "gold",
    hint: "采集任务卡住被强制标记为失败时提醒处理",
  },
  {
    field: "cronTaskFailed",
    label: "定时任务失败",
    badge: "任务失败",
    badgeColor: "volcano",
    hint: "后台定时调度（如数据清理/自动任务）运行失败时告警",
  },
  {
    field: "cronTaskDone",
    label: "定时任务完成",
    badge: "任务通知",
    badgeColor: "green",
    hint: "定时任务成功完成时推送通知（默认关闭，避免频发打扰）",
  },
  {
    field: "sourceConfigChanged",
    label: "采集源配置变更",
    badge: "配置变更",
    badgeColor: "cyan",
    hint: "新增/删除采集源、主站与附属站切换、接口地址变更、启用/停用等配置操作发生时推送",
  },
];

function normalizeConfig(data: Partial<NotifyConfigValues> | undefined): NotifyConfigValues {
  return {
    enabled: Boolean(data?.enabled),
    botToken: String(data?.botToken ?? "").trim(),
    chatIds: Array.isArray(data?.chatIds) ? data!.chatIds.map(String).filter(Boolean) : [],
    events: {
      ...DEFAULT_EVENTS,
      ...(data?.events ?? {}),
    },
    includeFilmDetails: data?.includeFilmDetails !== false,
    maxFilmsInMessage: Number(data?.maxFilmsInMessage || 15),
    minIntervalSec: Number(data?.minIntervalSec ?? 60),
  };
}

export default function NotifyConfigPageView() {
  const [form] = Form.useForm<NotifyConfigValues>();
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const { message } = useAppMessage();

  // Watch real-time form values for header summary status
  const watchedEnabled = Form.useWatch("enabled", form);
  const watchedBotToken = Form.useWatch("botToken", form);
  const watchedChatIds = Form.useWatch("chatIds", form);
  const watchedEvents = Form.useWatch("events", form);

  const activeEventsCount = useMemo(() => {
    if (!watchedEvents) return 0;
    return Object.values(watchedEvents).filter(Boolean).length;
  }, [watchedEvents]);

  // Token（含已保存脱敏）+ 至少一个 Chat ID 即可测，无需先保存
  const canTest = useMemo(() => {
    const token = String(watchedBotToken ?? "").trim();
    const chats = Array.isArray(watchedChatIds)
      ? watchedChatIds.map(String).map((s) => s.trim()).filter(Boolean)
      : [];
    return token.length > 0 && chats.length > 0;
  }, [watchedBotToken, watchedChatIds]);

  const loadConfig = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/notify");
      if (resp.code === 0) {
        form.setFieldsValue(normalizeConfig(resp.data));
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
      // validateFields 校验失败时不需额外提示
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
    try {
      const values = form.getFieldsValue(true) as NotifyConfigValues;
      const botToken = String(values.botToken ?? "").trim();
      const chatIds = (values.chatIds || []).map(String).map((s) => s.trim()).filter(Boolean);
      if (!botToken) {
        message.error("请填写 Bot Token");
        return;
      }
      if (!chatIds.length) {
        message.error("请至少填写一个 Chat ID");
        return;
      }
      setTesting(true);
      // 直接提交当前表单草稿，后端不落库；脱敏 Token 会沿用已保存值
      const resp = await ApiPost("/manage/config/notify/test", {
        botToken,
        chatIds,
      });
      const data = resp.data as
        | { sent?: number; failed?: { chatId: string; error: string }[] }
        | undefined;
      const failedList = data?.failed ?? [];
      const failedDetail =
        failedList.length > 0
          ? failedList.map((f) => `${f.chatId}: ${f.error}`).join("；")
          : "";

      if (resp.code === 0) {
        const sent = data?.sent ?? 0;
        if (failedList.length > 0) {
          message.warning(`已发送 ${sent} 个，失败 ${failedList.length} 个：${failedDetail}`);
        } else {
          message.success(resp.msg || `测试消息已发送（${sent}）`);
        }
        return;
      }
      message.error(
        failedDetail ? `${resp.msg || "测试发送失败"}（${failedDetail}）` : resp.msg || "测试发送失败",
      );
    } catch {
      // ignore
    } finally {
      setTesting(false);
    }
  };

  const handleSelectAllEvents = (status: boolean) => {
    const nextEvents: NotifyEventSwitches = {
      collectBatchSummary: status,
      collectSourceFailed: status,
      collectFinalizeFailed: status,
      collectProgressStale: status,
      cronTaskFailed: status,
      cronTaskDone: status,
      sourceConfigChanged: status,
    };
    form.setFieldsValue({ events: nextEvents });
  };

  return (
    <div className={styles.page}>
      <ManagePageHeader
        title="通知设置"
        description="配置 Telegram Bot 消息推送。在「通信连接配置」填好 Token 与 Chat ID 后可直接发送测试，无需先保存。"
        actions={
          <Space wrap>
            <Button icon={<ReloadOutlined />} loading={fetching} onClick={() => void loadConfig()}>
              刷新
            </Button>
            <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={() => void handleSave()}>
              保存配置
            </Button>
          </Space>
        }
      />

      <Spin spinning={fetching} description="正在加载通知配置...">
        <Form
          form={form}
          layout="vertical"
          className={styles.form}
          initialValues={DEFAULT_CONFIG}
          disabled={fetching || saving}
        >
        <Flex vertical gap={20} className={styles.contentStack}>
          {/* Status Summary Header Card + 启用开关 */}
          <Card size="small" className={styles.overviewCard}>
            <Flex align="center" justify="space-between" wrap="wrap" gap={16}>
              <Flex align="center" gap={12} wrap="wrap" style={{ flex: 1, minWidth: 220 }}>
                <div className={styles.botIconWrapper}>
                  <RobotOutlined className={styles.botIcon} />
                </div>
                <Flex vertical gap={2}>
                  <Flex align="center" gap={8} wrap="wrap">
                    <Typography.Text strong className={styles.overviewTitle}>
                      Telegram Bot 推送状态
                    </Typography.Text>
                    {watchedEnabled ? (
                      <Tag color="success" icon={<CheckCircleOutlined />}>
                        推送已开启
                      </Tag>
                    ) : (
                      <Tag color="default" icon={<StopOutlined />}>
                        推送已禁用
                      </Tag>
                    )}
                  </Flex>
                  <Typography.Text type="secondary" className={styles.overviewSub}>
                    实时捕获采集、抓取告警与后台定时任务事件并推送至 Telegram；关闭开关后停止业务推送，联通测试不受影响。
                  </Typography.Text>
                </Flex>
              </Flex>

              <Flex align="center" gap={12} wrap="wrap" className={styles.badgeGroup}>
                <Tooltip title="Token 是否填写或存在已有凭证">
                  <Tag color={watchedBotToken ? "processing" : "warning"}>
                    {watchedBotToken ? "Bot Token 已设置" : "未设置 Token"}
                  </Tag>
                </Tooltip>
                <Tooltip title="当前配置的接收目标 Chat ID 数量">
                  <Tag color={watchedChatIds?.length ? "purple" : "default"}>
                    {watchedChatIds?.length ? `${watchedChatIds.length} 个 Chat ID` : "未配置 Chat ID"}
                  </Tag>
                </Tooltip>
                <Tooltip title="已开启的事件订阅数量">
                  <Tag color={activeEventsCount > 0 ? "blue" : "default"}>
                    {activeEventsCount} / {EVENT_OPTIONS.length} 个事件开启
                  </Tag>
                </Tooltip>

                <div className={styles.enableSwitch}>
                  <Typography.Text type="secondary" className={styles.enableLabel}>
                    启用通知
                  </Typography.Text>
                  <Form.Item name="enabled" valuePropName="checked" noStyle>
                    <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                  </Form.Item>
                </div>
              </Flex>
            </Flex>
          </Card>

          <Row gutter={[16, 16]} align="stretch">
            {/* Left Grid: Connection & Rules */}
            <Col xs={24} lg={11} xl={10}>
              <Flex vertical gap={16} style={{ height: "100%" }}>
                {/* Bot Connection Card */}
                <Card
                  size="small"
                  title={
                    <Space>
                      <RobotOutlined style={{ color: "#1677ff" }} />
                      <span>通信连接配置</span>
                    </Space>
                  }
                  className={styles.card}
                >
                  <Form.Item
                    label="Bot Token"
                    name="botToken"
                    extra="向 @BotFather 创建 Bot 获得；保存后自动脱敏隐藏，留空或保持脱敏表示不修改。"
                  >
                    <Input.Password
                      placeholder="例如 123456789:AAHgf..."
                      autoComplete="off"
                    />
                  </Form.Item>

                  <Form.Item
                    label="Chat ID (接收目标)"
                    name="chatIds"
                    extra="支持个人数字 ID、群组/频道 @username 或 -100xxxxxxxxxx；需先向 Bot 发送 /start。输入后按 Enter 或逗号生成标签。"
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
                      className={styles.chatIdsSelect}
                      mode="tags"
                      tokenSeparators={[",", " ", "\n"]}
                      placeholder="例如 123456789 或 -100123456789"
                      allowClear
                    />
                  </Form.Item>

                  <div className={styles.testRow}>
                    <Typography.Text type="secondary" className={styles.testHint}>
                      使用上方当前填写内容联通测试，无需先点「保存配置」。
                    </Typography.Text>
                    {/* 跳出 Form disabled，仅按 canTest 控制可点 */}
                    <ConfigProvider componentDisabled={false}>
                      <Tooltip
                        title={
                          canTest
                            ? "向当前 Chat ID 发送一条测试消息"
                            : "请先填写 Bot Token 与至少一个 Chat ID"
                        }
                      >
                        <Button
                          type="primary"
                          icon={<SendOutlined />}
                          loading={testing}
                          disabled={!canTest || fetching}
                          onClick={() => void handleTest()}
                        >
                          发送测试
                        </Button>
                      </Tooltip>
                    </ConfigProvider>
                  </div>
                </Card>

                {/* Content & Limitation Card */}
                <Card
                  size="small"
                  title={
                    <Space>
                      <ControlOutlined style={{ color: "#722ed1" }} />
                      <span>内容格式与限流</span>
                    </Space>
                  }
                  className={styles.card}
                >
                  <Form.Item
                    label="摘要展示更新列表"
                    name="includeFilmDetails"
                    valuePropName="checked"
                    tooltip="开启后批次采集摘要将附带更新列表（含新增/更新的影片）；单片更新始终不带列表"
                    extra="禁用后推送仅包含成功与失败统计计数，缩减消息体积"
                  >
                    <Switch checkedChildren="启用" unCheckedChildren="禁用" />
                  </Form.Item>

                  <Row gutter={16}>
                    <Col xs={24} sm={12}>
                      <Form.Item
                        label="消息内最多影片数"
                        name="maxFilmsInMessage"
                        tooltip="更新列表每页最多影片数（范围 1–20，与 Telegram 按钮布局一致）"
                      >
                        <InputNumber min={1} max={20} style={{ width: "100%" }} />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={12}>
                      <Form.Item
                        label="同类事件最小间隔"
                        name="minIntervalSec"
                        tooltip="同一类事件在设定的秒数间隔内最多只推送一次；0 表示不受防刷限流"
                      >
                        <InputNumber
                          min={0}
                          max={3600}
                          addonAfter="秒"
                          style={{ width: "100%" }}
                        />
                      </Form.Item>
                    </Col>
                  </Row>
                </Card>
              </Flex>
            </Col>

            {/* Right Grid: Notification Event Subscription Rules */}
            <Col xs={24} lg={13} xl={14}>
              <Card
                size="small"
                title={
                  <Space>
                    <BellOutlined style={{ color: "#fa8c16" }} />
                    <span>触发事件与订阅规则</span>
                  </Space>
                }
                extra={
                  <Space size={4}>
                    <Button
                      type="link"
                      size="small"
                      icon={<CheckOutlined />}
                      onClick={() => handleSelectAllEvents(true)}
                    >
                      全选
                    </Button>
                    <Divider type="vertical" />
                    <Button
                      type="link"
                      size="small"
                      danger
                      icon={<ClearOutlined />}
                      onClick={() => handleSelectAllEvents(false)}
                    >
                      清空
                    </Button>
                  </Space>
                }
                className={styles.card}
              >
                <div className={styles.eventGrid}>
                  {EVENT_OPTIONS.map((item) => (
                    <Form.Item
                      key={item.field}
                      name={["events", item.field]}
                      valuePropName="checked"
                      className={styles.eventItemWrapper}
                    >
                      <EventCard item={item} />
                    </Form.Item>
                  ))}
                </div>

                <Alert
                  className={styles.tipAlert}
                  type="info"
                  showIcon
                  icon={<InfoCircleOutlined />}
                  message="推送提醒小贴士"
                  description="针对生产环境，建议保持「单源失败即时告警」与「收尾发布失败」开启，以便第一时间掌握采集源状态；若频繁发布定时任务，可保持「定时任务完成」关闭。"
                />
              </Card>
            </Col>
          </Row>
        </Flex>
        </Form>
      </Spin>
    </div>
  );
}

interface EventCardProps {
  item: EventOption;
  checked?: boolean;
  value?: boolean;
  onChange?: (checked: boolean) => void;
}

function EventCard({ item, checked, value, onChange }: EventCardProps) {
  const isChecked = Boolean(checked ?? value);
  return (
    <div
      className={`${styles.eventTile} ${isChecked ? styles.eventTileActive : ""}`}
      onClick={() => onChange?.(!isChecked)}
    >
      <Flex align="flex-start" gap={10}>
        <Checkbox
          checked={isChecked}
          onChange={(e) => onChange?.(e.target.checked)}
          onClick={(e) => e.stopPropagation()}
          className={styles.eventCheckbox}
        />
        <Flex vertical gap={4} style={{ flex: 1, minWidth: 0 }}>
          <Flex align="center" justify="space-between" gap={8} wrap="wrap">
            <Typography.Text strong className={styles.eventTitle}>
              {item.label}
            </Typography.Text>
            <Tag color={item.badgeColor} className={styles.eventBadge}>
              {item.badge}
            </Tag>
          </Flex>
          <Typography.Text type="secondary" className={styles.eventHint}>
            {item.hint}
          </Typography.Text>
        </Flex>
      </Flex>
    </div>
  );
}
