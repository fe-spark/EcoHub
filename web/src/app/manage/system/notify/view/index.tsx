"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Card,
  Checkbox,
  Col,
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
  TimePicker,
  Typography,
} from "antd";
import dayjs from "dayjs";
import {
  BellOutlined,
  CheckOutlined,
  ClearOutlined,
  ControlOutlined,
  EditOutlined,
  LockOutlined,
  ReloadOutlined,
  RobotOutlined,
  SaveOutlined,
  SendOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import {
  DEFAULT_CONFIG,
  DEFAULT_EVENTS,
  DEFAULT_QUIET_HOURS,
  EVENT_GROUPS,
  EVENT_OPTIONS,
  normalizeConfig,
  type NotifyConfigValues,
  type NotifyEventSwitches,
} from "./constants";
import styles from "./index.module.less";

const HHMM_FORMAT = "HH:mm";

function hhmmToDayjs(value: unknown) {
  if (value == null || value === "") return undefined;
  if (dayjs.isDayjs(value)) return value.isValid() ? value : undefined;
  const match = /^(\d{1,2}):([0-5]\d)$/.exec(String(value).trim());
  if (!match) return undefined;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  if (hour > 23) return undefined;
  return dayjs().hour(hour).minute(minute).second(0).millisecond(0);
}

function hhmmFromPicker(value: unknown) {
  const next = hhmmToDayjs(value);
  return next ? next.format(HHMM_FORMAT) : "";
}

interface NotifyConfigPageViewProps {
  embedded?: boolean;
}

export default function NotifyConfigPageView({ embedded = false }: NotifyConfigPageViewProps) {
  const [form] = Form.useForm<NotifyConfigValues>();
  const [isEditing, setIsEditing] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [serverData, setServerData] = useState<NotifyConfigValues>(DEFAULT_CONFIG);

  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const watchedBotToken = Form.useWatch("botToken", form);
  const watchedChatIds = Form.useWatch("chatIds", form);
  const watchedEvents = Form.useWatch("events", form);
  const watchedQuietHoursEnabled = Form.useWatch(["quietHours", "enabled"], form);

  const canTest = useMemo(() => {
    const token = String(watchedBotToken ?? "").trim();
    const chats = Array.isArray(watchedChatIds) ? watchedChatIds.filter(Boolean) : [];
    return token.length > 0 && chats.length > 0;
  }, [watchedBotToken, watchedChatIds]);

  const loadConfig = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/notify");
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeConfig(resp.data);
        setServerData(normalized);
        form.setFieldsValue(normalized);
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

  const handleCancel = () => {
    form.setFieldsValue(serverData);
    setIsEditing(false);
  };

  const handleSave = async () => {
    if (!canWrite) return;
    try {
      const values = await form.validateFields();
      setSaving(true);
      const stored = form.getFieldsValue(true) as NotifyConfigValues;
      const payload: NotifyConfigValues = {
        ...serverData,
        ...stored,
        ...values,
        chatIds: values.chatIds || stored.chatIds || [],
        events: {
          ...DEFAULT_EVENTS,
          ...serverData.events,
          ...stored.events,
          ...values.events,
        },
        quietHours: {
          ...DEFAULT_QUIET_HOURS,
          ...serverData.quietHours,
          ...stored.quietHours,
          ...values.quietHours,
        },
      };
      const resp = await ApiPost("/manage/config/notify/update", payload);
      if (resp.code === 0) {
        message.success(resp.msg || "通知配置已保存");
        const normalized = normalizeConfig(resp.data || payload);
        setServerData(normalized);
        form.setFieldsValue(normalized);
        setIsEditing(false);
        return;
      }
      message.error(resp.msg || "保存失败");
    } catch {
      // validation error
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
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
    try {
      const resp = await ApiPost("/manage/config/notify/test", { botToken, chatIds });
      if (resp.code === 0) {
        message.success(resp.msg || "测试消息已发送");
      } else {
        message.error(resp.msg || "测试发送失败");
      }
    } finally {
      setTesting(false);
    }
  };

  const handleSetAllEvents = (status: boolean) => {
    const nextEvents = { ...DEFAULT_EVENTS };
    (Object.keys(nextEvents) as (keyof NotifyEventSwitches)[]).forEach((key) => {
      nextEvents[key] = status;
    });
    form.setFieldsValue({ events: nextEvents });
  };

  const handleResetDefaultEvents = () => {
    form.setFieldsValue({ events: { ...DEFAULT_EVENTS } });
  };

  return (
    <div className={styles.page}>
      {embedded ? null : (
        <ManagePageHeader
          title="通知设置"
          description="管理 Telegram 消息推送、接收渠道、推送策略与订阅规则。"
        />
      )}

      <Card
        className={styles.card}
        title={
          <Space size={8} align="center">
            <BellOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>Telegram 消息通知配置</span>
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
                  type="primary"
                  icon={<SaveOutlined />}
                  loading={saving}
                  disabled={!canWrite}
                  onClick={() => void handleSave()}
                >
                  保存配置
                </Button>
              </>
            ) : (
              <Button
                size="small"
                type="primary"
                icon={<EditOutlined />}
                disabled={!canWrite}
                onClick={() => setIsEditing(true)}
              >
                编辑
              </Button>
            )}
          </Space>
        }
      >
        <Spin spinning={fetching} description="正在加载通知配置...">
          <Form
            form={form}
            layout="vertical"
            className={styles.form}
            initialValues={DEFAULT_CONFIG}
            disabled={!isEditing || !canWrite}
          >
            <Flex vertical gap={0} className={styles.contentStack}>
              {/* 最上层：启用/禁用消息推送总开关卡片 */}
              <div className={styles.masterSwitchBlock}>
                <Flex align="center" justify="space-between" gap={20}>
                  <Flex vertical gap={4}>
                    <span className={styles.masterSwitchTitle}>启用 Telegram 消息推送</span>
                    <span className={styles.masterSwitchDesc}>
                      开启后将按订阅规则自动向配置的 Chat ID 发送采集与系统通知；关闭后停止所有业务推送
                    </span>
                  </Flex>
                  <Form.Item name="enabled" valuePropName="checked" noStyle>
                    <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                  </Form.Item>
                </Flex>
              </div>

              <Divider style={{ margin: "28px 0" }} />

                  {/* 模块 1：机器人与接收渠道 */}
                  <div className={styles.sectionBlock}>
                    <div className={styles.sectionHeader}>
                      <RobotOutlined style={{ color: "var(--ant-color-primary)" }} />
                      <span>机器人与接收渠道</span>
                    </div>

                    <Row gutter={[24, 16]}>
                      <Col xs={24} md={12}>
                        <Form.Item
                          label="Telegram Bot Token"
                          name="botToken"
                          tooltip="从 @BotFather 获取的 Telegram Bot Token"
                        >
                          <Input.Password placeholder="输入 Telegram Bot Token" />
                        </Form.Item>
                      </Col>
                      <Col xs={24} md={12}>
                        <Form.Item
                          label="目标接收 Chat ID"
                          name="chatIds"
                          tooltip="接收通知的群组、频道或个人 Chat ID；输入后按回车添加"
                        >
                          <Select
                            mode="tags"
                            tokenSeparators={[",", " ", "\n"]}
                            placeholder="例如 123456789 或 -100123456789"
                          />
                        </Form.Item>
                      </Col>
                    </Row>

                    <div className={styles.testFooter}>
                      <Space size={8} align="center">
                        <SendOutlined style={{ color: "var(--ant-color-primary)" }} />
                        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                          支持使用当前填写的 Bot Token 与 Chat ID 发送即时测试消息，无需先保存配置。
                        </Typography.Text>
                      </Space>
                      <Button
                        icon={<SendOutlined />}
                        loading={testing}
                        disabled={!canTest}
                        onClick={() => void handleTest()}
                      >
                        发送测试
                      </Button>
                    </div>
                  </div>

                  <Divider style={{ margin: "28px 0" }} />

                  {/* 模块 2：推送策略与限流规则 */}
                  <div className={styles.sectionBlock}>
                    <div className={styles.sectionHeader}>
                      <ControlOutlined style={{ color: "#722ed1" }} />
                      <span>推送策略与限流规则</span>
                    </div>

                    <Row gutter={[20, 16]}>
                      <Col xs={24} md={12}>
                        <div className={styles.settingSwitchCard}>
                          <div className={styles.settingSwitchText}>
                            <span className={styles.settingSwitchTitle}>无更新自动静音</span>
                            <span className={styles.settingSwitchDesc}>
                              仅在有更新或报错时发送通知；无更新无报错时静音
                            </span>
                          </div>
                          <Form.Item name="onlyNotifyOnUpdate" valuePropName="checked" noStyle>
                            <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                          </Form.Item>
                        </div>
                      </Col>
                      <Col xs={24} md={12}>
                        <div className={styles.settingSwitchCard}>
                          <div className={styles.settingSwitchText}>
                            <span className={styles.settingSwitchTitle}>摘要附带更新列表</span>
                            <span className={styles.settingSwitchDesc}>
                              批次摘要自动附带更新影片明细与 Telegram 翻页按钮
                            </span>
                          </div>
                          <Form.Item name="includeFilmDetails" valuePropName="checked" noStyle>
                            <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                          </Form.Item>
                        </div>
                      </Col>
                    </Row>

                    <Row gutter={[24, 16]}>
                      <Col xs={24} sm={12}>
                        <Form.Item
                          label="消息内最多影片数"
                          name="maxFilmsInMessage"
                          tooltip="更新列表单页展示的最大影片数量 (1–20)"
                        >
                          <InputNumber min={1} max={20} style={{ width: "100%" }} placeholder="15" />
                        </Form.Item>
                      </Col>
                      <Col xs={24} sm={12}>
                        <Form.Item
                          label="同类事件最小推送间隔 (秒)"
                          name="minIntervalSec"
                          tooltip="同一类事件在设定的秒数间隔内最多只推送一次；0 表示不限流"
                        >
                          <InputNumber min={0} max={3600} style={{ width: "100%" }} placeholder="60" />
                        </Form.Item>
                      </Col>
                    </Row>

                    {/* 夜间免打扰 */}
                    <div className={styles.quietHoursPanel}>
                      <Flex vertical gap={14}>
                        <Flex align="center" justify="space-between" gap={16}>
                          <Space size={8} align="center">
                            <LockOutlined style={{ color: "#722ed1" }} />
                            <Typography.Text strong style={{ fontSize: 14 }}>
                              夜间免打扰
                            </Typography.Text>
                          </Space>
                          <Form.Item name={["quietHours", "enabled"]} valuePropName="checked" noStyle>
                            <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                          </Form.Item>
                        </Flex>

                        <Row gutter={[24, 16]}>
                          <Col xs={24} sm={12}>
                            <Form.Item
                              label="开始时间"
                              name={["quietHours", "start"]}
                              style={{ marginBottom: 0 }}
                              getValueFromEvent={hhmmFromPicker}
                              getValueProps={(value) => ({ value: hhmmToDayjs(value) })}
                              rules={[
                                ({ getFieldValue }) => ({
                                  validator(_, value) {
                                    if (!getFieldValue(["quietHours", "enabled"])) {
                                      return Promise.resolve();
                                    }
                                    if (!hhmmToDayjs(value)) {
                                      return Promise.reject(new Error("请选择 HH:mm 格式的开始时间"));
                                    }
                                    return Promise.resolve();
                                  },
                                }),
                              ]}
                            >
                              <TimePicker
                                format={HHMM_FORMAT}
                                placeholder="23:00"
                                allowClear={false}
                                style={{ width: "100%" }}
                                disabled={!isEditing || !canWrite || !watchedQuietHoursEnabled}
                              />
                            </Form.Item>
                          </Col>
                          <Col xs={24} sm={12}>
                            <Form.Item
                              label="结束时间"
                              name={["quietHours", "end"]}
                              style={{ marginBottom: 0 }}
                              getValueFromEvent={hhmmFromPicker}
                              getValueProps={(value) => ({ value: hhmmToDayjs(value) })}
                              rules={[
                                ({ getFieldValue }) => ({
                                  validator(_, value) {
                                    if (!getFieldValue(["quietHours", "enabled"])) {
                                      return Promise.resolve();
                                    }
                                    if (!hhmmToDayjs(value)) {
                                      return Promise.reject(new Error("请选择 HH:mm 格式的结束时间"));
                                    }
                                    return Promise.resolve();
                                  },
                                }),
                              ]}
                            >
                              <TimePicker
                                format={HHMM_FORMAT}
                                placeholder="07:00"
                                allowClear={false}
                                style={{ width: "100%" }}
                                disabled={!isEditing || !canWrite || !watchedQuietHoursEnabled}
                              />
                            </Form.Item>
                          </Col>
                        </Row>
                      </Flex>
                    </div>
                  </div>

                  <Divider style={{ margin: "28px 0" }} />

                  {/* 模块 3：触发事件订阅规则 */}
                  <div className={styles.sectionBlock}>
                    <Flex align="center" justify="space-between" wrap="wrap" gap={12}>
                      <div className={styles.sectionHeader}>
                        <BellOutlined style={{ color: "#52c41a" }} />
                        <span>触发事件订阅规则</span>
                      </div>
                      {isEditing ? (
                        <Space size={8}>
                          <Button
                            size="small"
                            icon={<CheckOutlined />}
                            onClick={() => handleSetAllEvents(true)}
                          >
                            全选
                          </Button>
                          <Button
                            size="small"
                            icon={<ReloadOutlined />}
                            onClick={handleResetDefaultEvents}
                          >
                            恢复默认
                          </Button>
                          <Button
                            size="small"
                            icon={<ClearOutlined />}
                            onClick={() => handleSetAllEvents(false)}
                          >
                            清空
                          </Button>
                        </Space>
                      ) : null}
                    </Flex>

                    <Row gutter={[20, 20]}>
                      {EVENT_GROUPS.map((group) => {
                        const groupEvents = EVENT_OPTIONS.filter((e) => e.category === group.key);
                        return (
                          <Col xs={24} lg={8} key={group.key}>
                            <div className={styles.subGroupCard}>
                              <span className={styles.groupTitle}>{group.title}</span>
                              <span className={styles.groupDesc}>{group.description}</span>
                              <Flex vertical gap={10}>
                                {groupEvents.map((event) => {
                                  const checked = Boolean(watchedEvents?.[event.field]);
                                  const disabled = !isEditing || !canWrite;
                                  return (
                                    <div
                                      key={event.field}
                                      className={`${styles.eventTile} ${
                                        checked ? styles.eventTileActive : ""
                                      } ${disabled ? styles.eventTileDisabled : ""}`}
                                      onClick={() => {
                                        if (!disabled) {
                                          form.setFieldValue(["events", event.field], !checked);
                                        }
                                      }}
                                    >
                                      <Flex align="center" justify="space-between" gap={8}>
                                        <Space size={8} align="center">
                                          <Form.Item
                                            name={["events", event.field]}
                                            valuePropName="checked"
                                            noStyle
                                          >
                                            <Checkbox
                                              disabled={disabled}
                                              className={styles.eventCheckbox}
                                              onChange={(e) => e.stopPropagation()}
                                            />
                                          </Form.Item>
                                          <span className={styles.eventTitle}>{event.label}</span>
                                        </Space>
                                        <Tag color={event.badgeColor} className={styles.eventBadge}>
                                          {event.badge}
                                        </Tag>
                                      </Flex>
                                      {event.hint ? (
                                        <span className={styles.eventHint}>{event.hint}</span>
                                      ) : null}
                                    </div>
                                  );
                                })}
                              </Flex>
                            </div>
                          </Col>
                        );
                      })}
                    </Row>
                  </div>
            </Flex>
          </Form>
        </Spin>
      </Card>
    </div>
  );
}
