"use client";

import React from "react";
import { Button, Checkbox, Col, Flex, Form, Row, Space, Tag } from "antd";
import type { FormInstance } from "antd";
import {
  BellOutlined,
  CheckOutlined,
  ClearOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import {
  EVENT_GROUPS,
  EVENT_OPTIONS,
  type NotifyConfigValues,
  type NotifyEventSwitches,
} from "./constants";
import styles from "./index.module.less";

interface NotifyEventsBlockProps {
  form: FormInstance<NotifyConfigValues>;
  canOperate: boolean;
  watchedEvents?: NotifyEventSwitches;
  onSetAllEvents: (enabled: boolean) => void;
  onResetDefaultEvents: () => void;
}

export default function NotifyEventsBlock({
  form,
  canOperate,
  watchedEvents,
  onSetAllEvents,
  onResetDefaultEvents,
}: NotifyEventsBlockProps) {
  return (
    <div className={styles.sectionBlock}>
      <Flex align="center" justify="space-between" wrap="wrap" gap={12}>
        <div className={styles.sectionHeader}>
          <BellOutlined style={{ color: "#52c41a" }} />
          <span>触发事件订阅规则</span>
        </div>
        {canOperate && (
          <Space size={8}>
            <Button
              size="small"
              icon={<CheckOutlined />}
              onClick={() => onSetAllEvents(true)}
            >
              全选
            </Button>
            <Button
              size="small"
              icon={<ReloadOutlined />}
              onClick={onResetDefaultEvents}
            >
              恢复默认
            </Button>
            <Button
              size="small"
              icon={<ClearOutlined />}
              onClick={() => onSetAllEvents(false)}
            >
              清空
            </Button>
          </Space>
        )}
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
                    const disabled = !canOperate;
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
  );
}
