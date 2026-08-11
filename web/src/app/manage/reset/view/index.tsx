"use client";

import React, { useCallback, useEffect, useState } from "react";
import { Card, Col, Row, Statistic, Typography } from "antd";
import { DatabaseOutlined, FileTextOutlined, FolderOpenOutlined, VideoCameraOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import ManagePageHeader from "@/app/manage/components/page-header";
import ResetSiteDataCard from "@/app/manage/components/reset-site-data-card";
import styles from "./index.module.less";

interface ResetImpactStats {
  films: number;
  snapshots: number;
  categories: number;
  failures: number;
}

export default function ResetPageView() {
  const [stats, setStats] = useState<ResetImpactStats | null>(null);

  const loadStats = useCallback(async (): Promise<ResetImpactStats | null> => {
    try {
      const resp = await ApiGet<ResetImpactStats>("/manage/spider/clear/stats");
      return resp.code === 0 && resp.data ? resp.data : null;
    } catch {
      return null;
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    void loadStats().then((data) => {
      if (!cancelled && data) {
        setStats(data);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [loadStats]);

  return (
    <div className={styles.formPanel}>
      <ManagePageHeader
        title="数据重置"
        description="清空所有影视与采集数据；完成后自动同步主站分类，可直接重新采集。"
      />

      <Card size="small" title="当前数据规模（重置将清空以下数据）">
        <Row gutter={[16, 16]}>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title="影视库存"
              value={stats?.films ?? "—"}
              prefix={<VideoCameraOutlined />}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title="列表快照"
              value={stats?.snapshots ?? "—"}
              prefix={<DatabaseOutlined />}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title="分类"
              value={stats?.categories ?? "—"}
              prefix={<FolderOpenOutlined />}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title="失败记录"
              value={stats?.failures ?? "—"}
              prefix={<FileTextOutlined />}
            />
          </Col>
        </Row>
        <Typography.Text type="secondary" style={{ fontSize: 12, display: "block", marginTop: 8 }}>
          重置后以上数据将全部清空，并自动同步主站分类；账号、网站配置、采集源、定时任务等不受影响。
        </Typography.Text>
      </Card>

      <ResetSiteDataCard
        onResetComplete={() => {
          void loadStats().then((data) => {
            if (data) {
              setStats(data);
            }
          });
        }}
      />
    </div>
  );
}
