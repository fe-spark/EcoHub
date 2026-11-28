"use client";

import { Card, Col, Row, Skeleton, Statistic, Tag, Typography, theme } from "antd";
import {
  ClockCircleOutlined,
  CompassOutlined,
  EyeOutlined,
  PlayCircleOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from "@ant-design/icons";
import type { AnalyticsModule, AppPlatform, Overview } from "../types";
import styles from "../index.module.less";

interface KpiOverviewGridProps {
  globalOverview?: Overview | null;
  scopedOverview?: Overview | null;
  module: AnalyticsModule;
  platform: AppPlatform;
  loading?: boolean;
}

export default function KpiOverviewGrid({
  globalOverview,
  scopedOverview,
  module,
  platform,
  loading,
}: KpiOverviewGridProps) {
  const { token } = theme.useToken();
  const active = (module === "all" ? globalOverview : scopedOverview) ?? globalOverview;
  const pv = active?.pv ?? 0;
  const uv = active?.uv ?? 0;
  const depth = uv > 0 ? Number((pv / uv).toFixed(1)) : 0;
  const playCount = active?.action?.play ?? 0;
  const searchCount = active?.action?.search ?? 0;
  const interactionCount = playCount + searchCount;

  const globalPv = globalOverview?.pv ?? 0;
  const sharePct = globalPv > 0 ? ((pv / globalPv) * 100).toFixed(1) : "0.0";
  const errTotal = (active?.err4 ?? 0) + (active?.err5 ?? 0);
  const successRate = pv > 0 ? Math.max(0, ((pv - errTotal) / pv) * 100).toFixed(1) : "100.0";
  const p95Ms = active?.p95Ms ?? 0;
  const dropped = active?.dropped ?? 0;

  const scopeLabel =
    module === "all"
      ? "全站"
      : module === "web"
        ? "Web"
        : module === "tvbox"
          ? "TVBox"
          : platform === "all"
            ? "App"
            : platform.toUpperCase();

  const webPv = globalOverview?.client?.web ?? 0;
  const appPv =
    (globalOverview?.client?.android ?? 0) +
    (globalOverview?.client?.harmony ?? 0) +
    (globalOverview?.client?.ios ?? 0) +
    (globalOverview?.client?.app ?? 0);
  const tvboxPv = globalOverview?.provide?.pv ?? 0;

  // 第四张卡按终端语义呈现：全站看延迟健康、TVBox 看接口成功率、Web/App 看浏览与分类频次
  const fourthCard =
    module === "all"
      ? {
          key: "health",
          title: "服务响应 (P95)",
          value: p95Ms,
          suffix: "ms",
          icon: (
            <ClockCircleOutlined
              style={{ color: errTotal > 0 ? token.colorWarning : token.colorSuccess }}
            />
          ),
          sub: (
            <>
              <Tag color={errTotal > 0 ? "warning" : "success"} style={{ margin: 0 }}>
                4xx/5xx: {errTotal}
              </Tag>
              <Tag color={dropped > 0 ? "error" : "default"} style={{ margin: 0 }}>
                丢弃: {dropped}
              </Tag>
            </>
          ),
        }
      : module === "tvbox"
        ? {
            key: "tvbox-health",
            title: "订阅成功率",
            value: successRate,
            suffix: "%",
            icon: (
              <SafetyCertificateOutlined
                style={{ color: errTotal > 0 ? token.colorWarning : token.colorSuccess }}
              />
            ),
            sub: (
              <>
                <Tag color={active?.err4 ? "warning" : "default"} style={{ margin: 0 }}>
                  4xx: {active?.err4 ?? 0}
                </Tag>
                <Tag color={active?.err5 ? "error" : "default"} style={{ margin: 0 }}>
                  5xx: {active?.err5 ?? 0}
                </Tag>
              </>
            ),
          }
        : {
            key: "depth",
            title: "浏览 / 分类频次",
            value: (active?.action?.browse ?? 0) + (active?.action?.classify ?? 0),
            icon: <CompassOutlined style={{ color: "#722ed1" }} />,
            sub: (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                浏览 {(active?.action?.browse ?? 0).toLocaleString()} · 分类{" "}
                {(active?.action?.classify ?? 0).toLocaleString()}
              </Typography.Text>
            ),
          };

  const cards = [
    {
      key: "pv",
      title: `${scopeLabel}访问量 (PV)`,
      value: pv,
      icon: <EyeOutlined style={{ color: token.colorPrimary }} />,
      sub:
        module === "all" ? (
          <Typography.Text type="secondary" style={{ fontSize: 12 }} ellipsis>
            Web {webPv.toLocaleString()} · App {appPv.toLocaleString()} · TVBox{" "}
            {tvboxPv.toLocaleString()}
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            占全站流量 {sharePct}%
          </Typography.Text>
        ),
    },
    {
      key: "uv",
      title: `${scopeLabel}独立访客 (UV)`,
      value: uv,
      icon: <UserOutlined style={{ color: token.colorInfo }} />,
      sub: (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          人均访问深度 {depth.toFixed(1)} 次/人
        </Typography.Text>
      ),
    },
    {
      key: "interaction",
      title: `${scopeLabel}影视互动`,
      value: interactionCount,
      icon: <PlayCircleOutlined style={{ color: token.colorSuccess }} />,
      sub: (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          点播 {playCount.toLocaleString()} · 搜索 {searchCount.toLocaleString()}
        </Typography.Text>
      ),
    },
    fourthCard,
  ];

  return (
    <Row gutter={[16, 16]}>
      {cards.map((item) => (
        <Col key={item.key} xs={24} sm={12} xl={6}>
          <Card size="small" style={{ height: "100%" }}>
            {loading && !active ? (
              <Skeleton active paragraph={{ rows: 1 }} />
            ) : (
              <div className={styles.kpiCardBody}>
                <Statistic
                  title={
                    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                      {item.icon}
                      <span>{item.title}</span>
                    </span>
                  }
                  value={item.value}
                  suffix={item.suffix}
                />
                <div className={styles.kpiSubRow}>{item.sub}</div>
              </div>
            )}
          </Card>
        </Col>
      ))}
    </Row>
  );
}
