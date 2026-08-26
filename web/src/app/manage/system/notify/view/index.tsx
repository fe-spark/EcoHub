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
  Typography,
} from "antd";
import {
  BellOutlined,
  CheckOutlined,
  ClearOutlined,
  ControlOutlined,
  EditOutlined,
  LockOutlined,
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
  EVENT_GROUPS,
  EVENT_OPTIONS,
  normalizeConfig,
  type NotifyConfigValues,
  type NotifyEventSwitches,
} from "./constants";
import styles from "./index.module.less";

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
      const payload = { ...values, chatIds: values.chatIds || [] };
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
          disabled={!isEditing || !canWrite}
        >
          <Flex vertical gap={16} className={styles.contentStack}>
            {/* 卡片 1：Bot 与接收渠道配置 */}
            <Card
              className={styles.card}
              title={
                <Space size={8} align="center">
                  <RobotOutlined style={{ color: "var(--ant-color-primary)" }} />
                  <span>Telegram 机器人配置</span>
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
              <Flex vertical gap={16}>
                <Flex align="center" justify="space-between">
                  <Flex vertical gap={4}>
                    <Typography.Text strong>启用 Telegram 消息推送</Typography.Text>
                    <Typography.Text type="secondary">
                      开启后将按订阅规则自动向配置的 Chat ID 发送采集与系统通知
                    </Typography.Text>
                  </Flex>
                  <Form.Item name="enabled" valuePropName="checked" noStyle>
                    <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                  </Form.Item>
                </Flex>

                <Row gutter={24}>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="Bot Token"
                      name="botToken"
                      tooltip="从 @BotFather 获取的 Telegram Bot Token"
                    >
                      <Input.Password placeholder="输入 Telegram Bot Token" />
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="目标 Chat ID"
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
                  <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                    使用当前填写的 Bot Token 与 Chat ID 发送测试消息，无需先保存配置。
                  </Typography.Text>
                  <Button
                    icon={<SendOutlined />}
                    loading={testing}
                    disabled={!canTest}
                    onClick={() => void handleTest()}
                  >
                    发送测试
                  </Button>
                </div>
              </Flex>
            </Card>

            {/* 卡片 2：推送格式与限流策略 */}
            <Card
              className={styles.card}
              title={
                <Space size={8} align="center">
                  <ControlOutlined style={{ color: "#722ed1" }} />
                  <span>推送格式与限流策略</span>
                </Space>
              }
            >
              <Flex vertical gap={16}>
                <Row gutter={24}>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="无更新自动静音"
                      name="onlyNotifyOnUpdate"
                      valuePropName="checked"
                      extra="仅在有更新或报错时发送通知；无更新无报错时静音。"
                    >
                      <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="摘要附带更新列表"
                      name="includeFilmDetails"
                      valuePropName="checked"
                      extra="批次摘要自动附带更新影片明细与 Telegram 翻页按钮。"
                    >
                      <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                    </Form.Item>
                  </Col>
                  <Col xs={24} sm={12}>
                    <Form.Item
                      label="消息内最多影片数"
                      name="maxFilmsInMessage"
                      tooltip="更新列表单页展示的最大影片数量 (1–20)"
                    >
                      <InputNumber min={1} max={20} style={{ width: "100%" }} />
                    </Form.Item>
                  </Col>
                  <Col xs={24} sm={12}>
                    <Form.Item
                      label="同类事件最小推送间隔 (秒)"
                      name="minIntervalSec"
                      tooltip="同一类事件在设定的秒数间隔内最多只推送一次；0 表示不限流"
                    >
                      <InputNumber min={0} max={3600} style={{ width: "100%" }} />
                    </Form.Item>
                  </Col>
                </Row>

                <Divider style={{ margin: "8px 0" }} />

                {/* 夜间免打扰 */}
                <div className={styles.quietHoursPanel}>
                  <Flex vertical gap={12}>
                    <Flex align="center" justify="space-between">
                      <Space size={8}>
                        <LockOutlined style={{ color: "#722ed1" }} />
                        <Typography.Text strong style={{ fontSize: 14 }}>
                          夜间免打扰 (Quiet Hours)
                        </Typography.Text>
                      </Space>
                      <Form.Item name={["quietHours", "enabled"]} valuePropName="checked" noStyle>
                        <Switch checkedChildren="开启" unCheckedChildren="关闭" />
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
                  </Flex>
                </div>
              </Flex>
            </Card>

            {/* 卡片 3：触发事件订阅规则 */}
            <Card
              className={styles.card}
              title={
                <Space size={8} align="center">
                  <BellOutlined style={{ color: "#52c41a" }} />
                  <span>触发事件订阅规则</span>
                </Space>
              }
              extra={
                isEditing ? (
                  <Space size={8}>
                    <Button size="small" icon={<CheckOutlined />} onClick={() => handleSelectAllEvents(true)}>
                      全选
                    </Button>
                    <Button size="small" icon={<ClearOutlined />} onClick={() => handleSelectAllEvents(false)}>
                      清空
                    </Button>
                  </Space>
                ) : null
              }
            >
              <Row gutter={[16, 16]}>
                {EVENT_GROUPS.map((group) => {
                  const groupEvents = EVENT_OPTIONS.filter((e) => e.category === group.key);
                  return (
                    <Col xs={24} lg={8} key={group.key}>
                      <div className={styles.subGroupCard}>
                        <Typography.Text strong style={{ fontSize: 14, marginBottom: 4, display: "block" }}>
                          {group.title}
                        </Typography.Text>
                        <Typography.Text className={styles.groupDesc}>
                          {group.description}
                        </Typography.Text>
                        <Flex vertical gap={10}>
                          {groupEvents.map((event) => {
                            const checked = Boolean(watchedEvents?.[event.field]);
                            const disabled = !isEditing || !canWrite;
                            return (
                              <div
                                key={event.field}
                                className={`${styles.eventTile} ${checked ? styles.eventTileActive : ""} ${disabled ? styles.eventTileDisabled : ""}`}
                                onClick={() => {
                                  if (!disabled) {
                                    form.setFieldValue(["events", event.field], !checked);
                                  }
                                }}
                              >
                                <Flex align="flex-start" justify="space-between" gap={8}>
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
            </Card>
          </Flex>
        </Form>
      </Spin>
    </div>
  );
}
