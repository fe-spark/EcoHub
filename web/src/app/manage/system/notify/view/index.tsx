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
import { useManagePermission } from "@/lib/manage-permission";
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

interface NotifyTarget {
  id?: string;
  name?: string;
  chatId: string;
  threadId?: string;
  enabled?: boolean;
  minLevel?: string;
  subscribedCategories?: string[];
}

interface NotifyQuietHours {
  enabled: boolean;
  start: string;
  end: string;
  allowLevels: string[];
}

interface NotifyConfigValues {
  enabled: boolean;
  botToken: string;
  chatIds: string[];
  targets?: NotifyTarget[];
  events: NotifyEventSwitches;
  includeFilmDetails: boolean;
  onlyNotifyOnUpdate: boolean;
  maxFilmsInMessage: number;
  minIntervalSec: number;
  quietHours: NotifyQuietHours;
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

const DEFAULT_QUIET_HOURS: NotifyQuietHours = {
  enabled: false,
  start: "23:00",
  end: "07:00",
  allowLevels: ["ERROR", "CRITICAL"],
};

const DEFAULT_CONFIG: NotifyConfigValues = {
  enabled: false,
  botToken: "",
  chatIds: [],
  targets: [],
  events: { ...DEFAULT_EVENTS },
  includeFilmDetails: true,
  onlyNotifyOnUpdate: true,
  maxFilmsInMessage: 15,
  minIntervalSec: 60,
  quietHours: { ...DEFAULT_QUIET_HOURS },
};

interface EventGroup {
  key: "alert" | "digest" | "audit";
  title: string;
  badgeColor: string;
  description: string;
}

const EVENT_GROUPS: EventGroup[] = [
  {
    key: "alert",
    title: "核心故障告警",
    badgeColor: "red",
    description: "采集源失败、任务卡死、收尾异常及 Cron 报错",
  },
  {
    key: "digest",
    title: "业务简报与汇总",
    badgeColor: "blue",
    description: "采集批次完成摘要、更新列表及任务完成通告",
  },
  {
    key: "audit",
    title: "配置操作审计",
    badgeColor: "purple",
    description: "采集源新增/编辑/删除及属性变动记录",
  },
];

interface EventOption {
  field: keyof NotifyEventSwitches;
  category: "alert" | "digest" | "audit";
  label: string;
  badge: string;
  badgeColor: string;
  hint: string;
}

const EVENT_OPTIONS: EventOption[] = [
  {
    field: "collectSourceFailed",
    category: "alert",
    label: "单源失败即时告警",
    badge: "核心告警",
    badgeColor: "red",
    hint: "某采集源连续失败达到上限终止时推送",
  },
  {
    field: "collectFinalizeFailed",
    category: "alert",
    label: "收尾发布失败",
    badge: "系统异常",
    badgeColor: "orange",
    hint: "快照更新或摘要刷新失败时发送告警",
  },
  {
    field: "collectProgressStale",
    category: "alert",
    label: "采集进度超时",
    badge: "超时告警",
    badgeColor: "gold",
    hint: "采集任务卡住被强制标记为失败时提醒",
  },
  {
    field: "cronTaskFailed",
    category: "alert",
    label: "定时任务失败",
    badge: "任务失败",
    badgeColor: "volcano",
    hint: "后台定时调度（如数据清理/自动任务）运行失败告警",
  },
  {
    field: "collectBatchSummary",
    category: "digest",
    label: "采集结果摘要",
    badge: "批次汇总",
    badgeColor: "blue",
    hint: "整批采集结束后推送各源统计与更新列表",
  },
  {
    field: "cronTaskDone",
    category: "digest",
    label: "定时任务完成",
    badge: "任务通知",
    badgeColor: "green",
    hint: "定时任务成功完成时推送通知",
  },
  {
    field: "sourceConfigChanged",
    category: "audit",
    label: "采集源配置变更",
    badge: "配置变更",
    badgeColor: "cyan",
    hint: "站点新增/删除/切换/属性修改等记录推送",
  },
];

function normalizeConfig(data: Partial<NotifyConfigValues> | undefined): NotifyConfigValues {
  return {
    enabled: Boolean(data?.enabled),
    botToken: String(data?.botToken ?? "").trim(),
    chatIds: Array.isArray(data?.chatIds) ? data!.chatIds.map(String).filter(Boolean) : [],
    targets: Array.isArray(data?.targets) ? data!.targets : [],
    events: {
      ...DEFAULT_EVENTS,
      ...(data?.events ?? {}),
    },
    includeFilmDetails: data?.includeFilmDetails !== false,
    onlyNotifyOnUpdate: data?.onlyNotifyOnUpdate !== false,
    maxFilmsInMessage: Number(data?.maxFilmsInMessage || 15),
    minIntervalSec: Number(data?.minIntervalSec ?? 60),
    quietHours: {
      enabled: Boolean(data?.quietHours?.enabled),
      start: String(data?.quietHours?.start || "23:00"),
      end: String(data?.quietHours?.end || "07:00"),
      allowLevels: Array.isArray(data?.quietHours?.allowLevels)
        ? data!.quietHours!.allowLevels
        : ["ERROR", "CRITICAL"],
    },
  };
}

interface NotifyConfigPageViewProps {
  embedded?: boolean;
}

export default function NotifyConfigPageView({ embedded = false }: NotifyConfigPageViewProps) {
  const [form] = Form.useForm<NotifyConfigValues>();
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const watchedEnabled = Form.useWatch("enabled", form);
  const watchedBotToken = Form.useWatch("botToken", form);
  const watchedChatIds = Form.useWatch("chatIds", form);
  const watchedEvents = Form.useWatch("events", form);
  const configLocked = !watchedEnabled;

  const activeEventsCount = useMemo(() => {
    if (!watchedEvents) return 0;
    return Object.values(watchedEvents).filter(Boolean).length;
  }, [watchedEvents]);

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
      // chatIds 为成员真相源；不回传可能陈旧的 targets，由后端按 chatIds 重建并保留服务端已有 Thread/等级元数据
      const { targets: _omitTargets, ...payload } = values;
      const resp = await ApiPost("/manage/config/notify/update", {
        ...payload,
        chatIds: values.chatIds || [],
      });
      if (resp.code === 0) {
        message.success(resp.msg || "保存成功");
        form.setFieldsValue(normalizeConfig(resp.data));
        return;
      }
      message.error(resp.msg || "保存失败");
    } catch {
      // ignore
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
      {embedded ? null : (
        <ManagePageHeader
          title="通知设置"
          description="管理 Telegram 消息推送、接收渠道、推送策略与订阅规则。"
        />
      )}

      <Spin spinning={fetching} description="正在加载通知配置...">
        <Form
          form={form}
          layout="vertical"
          className={styles.form}
          initialValues={DEFAULT_CONFIG}
          disabled={fetching || saving}
        >
          <Flex vertical gap={16} className={styles.contentStack}>
            {/* 顶栏 Header Card: 100% 对齐全站 Tabs 标准卡片风格 */}
            <Card className={styles.overviewCard}>
              <Flex align="center" justify="space-between" wrap="wrap" gap={16}>
                <Flex align="center" gap={12} wrap="wrap">
                  <Space size={8}>
                    <RobotOutlined style={{ color: "#1677ff", fontSize: 20 }} />
                    <Typography.Text strong className={styles.overviewTitle}>
                      Telegram 通知推送
                    </Typography.Text>
                  </Space>
                  {watchedEnabled ? (
                    <Tag color="success" icon={<CheckCircleOutlined />}>
                      已启用
                    </Tag>
                  ) : (
                    <Tag color="default" icon={<StopOutlined />}>
                      已禁用
                    </Tag>
                  )}
                </Flex>

                <Flex align="center" gap={14} wrap="wrap">
                  {watchedEnabled ? (
                    <Flex align="center" gap={8} className={styles.badgeGroup}>
                      <Tag color={watchedBotToken ? "processing" : "warning"}>
                        {watchedBotToken ? "Token 已设置" : "未设 Token"}
                      </Tag>
                      <Tag color={watchedChatIds?.length ? "purple" : "default"}>
                        {watchedChatIds?.length ? `${watchedChatIds.length} 个目标` : "未设目标"}
                      </Tag>
                      <Tag color={activeEventsCount > 0 ? "blue" : "default"}>
                        {activeEventsCount} / {EVENT_OPTIONS.length} 项事件开启
                      </Tag>
                    </Flex>
                  ) : null}

                  <div className={styles.enableSwitch}>
                    <Typography.Text type="secondary" className={styles.enableLabel}>
                      启用通知
                    </Typography.Text>
                    <ConfigProvider componentDisabled={false}>
                      <Form.Item name="enabled" valuePropName="checked" noStyle>
                        <Switch
                          checkedChildren="启用"
                          unCheckedChildren="禁用"
                          disabled={fetching || saving}
                        />
                      </Form.Item>
                    </ConfigProvider>
                  </div>

                  <Space size={8}>
                    <Button
                      icon={<ReloadOutlined />}
                      loading={fetching}
                      disabled={saving}
                      onClick={() => void loadConfig()}
                    >
                      刷新
                    </Button>
                    <Button
                      type="primary"
                      icon={<SaveOutlined />}
                      loading={saving}
                      disabled={!canWrite || fetching}
                      onClick={() => void handleSave()}
                    >
                      保存配置
                    </Button>
                  </Space>
                </Flex>
              </Flex>
            </Card>

            <div className={`${styles.configBody}${configLocked ? ` ${styles.configBodyLocked}` : ""}`}>
              <fieldset disabled={configLocked || fetching || saving} className={styles.configFieldset}>
                <Flex vertical gap={16}>
                  {/* 模块 1：通信连接配置 */}
                  <Card
                    title={
                      <Space size={8}>
                        <RobotOutlined style={{ color: "#1677ff" }} />
                        <span>通信连接配置</span>
                      </Space>
                    }
                    className={styles.card}
                  >
                    <Row gutter={24}>
                      <Col xs={24} md={12}>
                        <Form.Item
                          label="Bot Token"
                          name="botToken"
                          extra="向 Telegram @BotFather 申请获得；保存后脱敏展示。"
                        >
                          <Input.Password placeholder="123456789:AAHgf..." autoComplete="off" />
                        </Form.Item>
                      </Col>

                      <Col xs={24} md={12}>
                        <Form.Item
                          label="Chat ID (接收目标)"
                          name="chatIds"
                          extra="支持个人 ID、群组 @username 或 -100xxx；按 Enter 生成标签。"
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
                      </Col>
                    </Row>

                    <div className={styles.testRow}>
                      <Typography.Text type="secondary" className={styles.testHint}>
                        使用上方当前的填写的配置联通测试，无需先点「保存配置」。
                      </Typography.Text>
                      <ConfigProvider componentDisabled={false}>
                        <Tooltip
                          title={
                            configLocked
                              ? "请先开启通知"
                              : canTest
                                ? "向当前 Chat ID 发送一条测试消息"
                                : "请先填写 Bot Token 与至少一个 Chat ID"
                          }
                        >
                          <Button
                            type="primary"
                            icon={<SendOutlined />}
                            loading={testing}
                            disabled={!canWrite || configLocked || !canTest || fetching}
                            onClick={() => void handleTest()}
                          >
                            发送测试
                          </Button>
                        </Tooltip>
                      </ConfigProvider>
                    </div>
                  </Card>

                  {/* 模块 2：内容格式与限流 */}
                  <Card
                    title={
                      <Space size={8}>
                        <ControlOutlined style={{ color: "#722ed1" }} />
                        <span>内容格式与限流</span>
                      </Space>
                    }
                    className={styles.card}
                  >
                    <Row gutter={24}>
                      <Col xs={24} md={12}>
                        <Form.Item
                          label="无更新自动静音"
                          name="onlyNotifyOnUpdate"
                          valuePropName="checked"
                          extra="仅在有更新或报错时发送通知；无更新无报错时静音。"
                        >
                          <Switch checkedChildren="启用" unCheckedChildren="禁用" />
                        </Form.Item>
                      </Col>

                      <Col xs={24} md={12}>
                        <Form.Item
                          label="摘要附带更新列表"
                          name="includeFilmDetails"
                          valuePropName="checked"
                          extra="摘要自动附带更新影片明细与 Telegram 翻页按钮。"
                        >
                          <Switch checkedChildren="启用" unCheckedChildren="禁用" />
                        </Form.Item>
                      </Col>

                      <Col xs={24} sm={12}>
                        <Form.Item
                          label="消息内最多影片数"
                          name="maxFilmsInMessage"
                          tooltip="更新列表每页最多影片数 (1–20)"
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
                          <InputNumber min={0} max={3600} addonAfter="秒" style={{ width: "100%" }} />
                        </Form.Item>
                      </Col>
                    </Row>

                    <Divider style={{ margin: "16px 0" }} />

                    {/* 免打扰 Panel */}
                    <div className={styles.quietHoursPanel}>
                      <Flex vertical gap={12}>
                        <Flex align="center" justify="space-between">
                          <Space size={8}>
                            <LockOutlined style={{ color: "#722ed1" }} />
                            <Typography.Text strong style={{ fontSize: 14 }}>
                              免打扰与夜间静音 (Quiet Hours)
                            </Typography.Text>
                          </Space>
                          <Form.Item name={["quietHours", "enabled"]} valuePropName="checked" noStyle>
                            <Switch checkedChildren="启用" unCheckedChildren="禁用" />
                          </Form.Item>
                        </Flex>

                        <Row gutter={24}>
                          <Col xs={24} sm={12}>
                            <Form.Item label="开始时间" name={["quietHours", "start"]} style={{ marginBottom: 0 }}>
                              <Input placeholder="23:00" />
                            </Form.Item>
                          </Col>
                          <Col xs={24} sm={12}>
                            <Form.Item label="结束时间" name={["quietHours", "end"]} style={{ marginBottom: 0 }}>
                              <Input placeholder="07:00" />
                            </Form.Item>
                          </Col>
                        </Row>

                        <Form.Item
                          label="静音期间允许穿透的等级"
                          name={["quietHours", "allowLevels"]}
                          style={{ marginBottom: 0 }}
                          tooltip="默认仅允许 ERROR 与 CRITICAL 等极高告警穿透"
                        >
                          <Select
                            mode="multiple"
                            placeholder="请选择可穿透的等级"
                            options={[
                              { label: "INFO (信息)", value: "INFO" },
                              { label: "NOTICE (通知)", value: "NOTICE" },
                              { label: "WARN (警告)", value: "WARN" },
                              { label: "ERROR (错误)", value: "ERROR" },
                              { label: "CRITICAL (严重)", value: "CRITICAL" },
                            ]}
                          />
                        </Form.Item>
                      </Flex>
                    </div>
                  </Card>

                  {/* 模块 3：触发事件与订阅规则 */}
                  <Card
                    title={
                      <Space size={8}>
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
                          disabled={configLocked}
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
                          disabled={configLocked}
                          onClick={() => handleSelectAllEvents(false)}
                        >
                          清空
                        </Button>
                      </Space>
                    }
                    className={styles.card}
                  >
                    <Row gutter={[16, 16]}>
                      {EVENT_GROUPS.map((group) => {
                        const groupItems = EVENT_OPTIONS.filter((opt) => opt.category === group.key);
                        if (!groupItems.length) return null;
                        return (
                          <Col xs={24} lg={8} key={group.key}>
                            <Card
                              size="small"
                              type="inner"
                              title={
                                <Flex align="center" justify="space-between">
                                  <span>{group.title}</span>
                                  <Tag color={group.badgeColor}>{groupItems.length} 项</Tag>
                                </Flex>
                              }
                              className={styles.subGroupCard}
                            >
                              <Typography.Text type="secondary" className={styles.groupDesc}>
                                {group.description}
                              </Typography.Text>
                              <Flex vertical gap={8}>
                                {groupItems.map((item) => (
                                  <Form.Item
                                    key={item.field}
                                    name={["events", item.field]}
                                    valuePropName="checked"
                                    className={styles.eventItemWrapper}
                                  >
                                    <EventCard item={item} disabled={configLocked} />
                                  </Form.Item>
                                ))}
                              </Flex>
                            </Card>
                          </Col>
                        );
                      })}
                    </Row>
                  </Card>

                  <Alert
                    className={styles.tipAlert}
                    type="info"
                    showIcon
                    icon={<InfoCircleOutlined />}
                    title="推送提醒小贴士"
                    description="生产环境中，建议开启「核心故障告警」全项与「配置操作审计」，以便第一时间掌握异常与安全改动；「业务简报」可根据群组关注程度灵活选择。"
                  />
                </Flex>
              </fieldset>
            </div>
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
  disabled?: boolean;
  onChange?: (checked: boolean) => void;
}

function EventCard({ item, checked, value, disabled, onChange }: EventCardProps) {
  const isChecked = Boolean(checked ?? value);
  return (
    <div
      className={`${styles.eventTile} ${isChecked ? styles.eventTileActive : ""}${
        disabled ? ` ${styles.eventTileDisabled}` : ""
      }`}
      onClick={() => {
        if (!disabled) {
          onChange?.(!isChecked);
        }
      }}
    >
      <Flex align="flex-start" gap={10} style={{ height: "100%" }}>
        <Checkbox
          checked={isChecked}
          disabled={disabled}
          onChange={(e) => onChange?.(e.target.checked)}
          onClick={(e) => e.stopPropagation()}
          className={styles.eventCheckbox}
        />
        <Flex vertical gap={4} style={{ flex: 1, minWidth: 0, height: "100%", justifyContent: "space-between" }}>
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
