"use client";

import React from "react";
import { Form, Row, Col, Switch, Flex } from "antd";
import type { FormInstance } from "antd";
import { GlobalOutlined } from "@ant-design/icons";
import type { ProxyConfigValues } from "./types";
import styles from "./index.module.less";

interface ProxyModulesScopeProps {
  form: FormInstance<ProxyConfigValues>;
  canOperate: boolean;
}

export default function ProxyModulesScope({
  form,
  canOperate,
}: ProxyModulesScopeProps) {
  const globalEnabled = Form.useWatch("enabled", form);
  const isEnabled = Boolean(globalEnabled);

  return (
    <div className={styles.sectionBlock}>
      <div className={styles.sectionHeader}>
        <GlobalOutlined style={{ color: "var(--ant-color-primary)" }} />
        <span>生效功能模块</span>
        {!isEnabled && (
          <span
            style={{
              fontSize: 12,
              color: "var(--ant-color-text-tertiary, #8c8c8c)",
              fontWeight: "normal",
            }}
          >
            （全局代理未开启，所有模块保持直连）
          </span>
        )}
      </div>

      <Row gutter={[16, 12]}>
        <Col xs={24} sm={12}>
          <div
            className={styles.moduleCard}
            style={{
              opacity: isEnabled ? 1 : 0.55,
              transition: "opacity 0.2s ease",
            }}
          >
            <Flex justify="space-between" align="center">
              <div>
                <div className={styles.moduleTitle}>影视采集爬虫</div>
                <div className={styles.moduleDesc}>
                  {isEnabled
                    ? "全局采集通道（各源站按自身策略生效）"
                    : "全局代理未开启，所有采集源直连"}
                </div>
              </div>
              <Form.Item name={["modules", "spider"]} valuePropName="checked" noStyle>
                <Switch
                  size="small"
                  disabled={!isEnabled || !canOperate}
                />
              </Form.Item>
            </Flex>
          </div>
        </Col>
        <Col xs={24} sm={12}>
          <div
            className={styles.moduleCard}
            style={{
              opacity: isEnabled ? 1 : 0.55,
              transition: "opacity 0.2s ease",
            }}
          >
            <Flex justify="space-between" align="center">
              <div>
                <div className={styles.moduleTitle}>TMDB 刮削</div>
                <div className={styles.moduleDesc}>
                  {isEnabled
                    ? "海报、演职员与影视元数据抓取"
                    : "全局代理未开启，TMDB 直连访问"}
                </div>
              </div>
              <Form.Item name={["modules", "tmdb"]} valuePropName="checked" noStyle>
                <Switch
                  size="small"
                  disabled={!isEnabled || !canOperate}
                />
              </Form.Item>
            </Flex>
          </div>
        </Col>
        <Col xs={24} sm={12}>
          <div
            className={styles.moduleCard}
            style={{
              opacity: isEnabled ? 1 : 0.55,
              transition: "opacity 0.2s ease",
            }}
          >
            <Flex justify="space-between" align="center">
              <div>
                <div className={styles.moduleTitle}>Telegram 机器人</div>
                <div className={styles.moduleDesc}>
                  {isEnabled
                    ? "消息推送与交互通知"
                    : "全局代理未开启，Telegram 直连访问"}
                </div>
              </div>
              <Form.Item name={["modules", "notify"]} valuePropName="checked" noStyle>
                <Switch
                  size="small"
                  disabled={!isEnabled || !canOperate}
                />
              </Form.Item>
            </Flex>
          </div>
        </Col>
        <Col xs={24} sm={12}>
          <div
            className={styles.moduleCard}
            style={{
              opacity: isEnabled ? 1 : 0.55,
              transition: "opacity 0.2s ease",
            }}
          >
            <Flex justify="space-between" align="center">
              <div>
                <div className={styles.moduleTitle}>系统版本更新</div>
                <div className={styles.moduleDesc}>
                  {isEnabled
                    ? "GitHub Releases 检查与升级"
                    : "全局代理未开启，GitHub 直连访问"}
                </div>
              </div>
              <Form.Item name={["modules", "upgrade"]} valuePropName="checked" noStyle>
                <Switch
                  size="small"
                  disabled={!isEnabled || !canOperate}
                />
              </Form.Item>
            </Flex>
          </div>
        </Col>
      </Row>
    </div>
  );
}
