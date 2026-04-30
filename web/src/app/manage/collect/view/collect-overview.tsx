import { Card, Col, Descriptions, Row, Statistic, Tag, Typography } from "antd";
import type { FilmSource } from "./types";
import styles from "./index.module.less";

interface CollectStats {
  total: number;
  enabled: number;
  running: number;
  waiting: number;
  masters: number;
}

interface MasterStatus {
  text: string;
  color: "success" | "warning" | "error";
}

interface CollectOverviewProps {
  stats: CollectStats;
  masterSite: FilmSource | null;
  masterStatus: MasterStatus;
}

export default function CollectOverview({
  stats,
  masterSite,
  masterStatus,
}: CollectOverviewProps) {
  return (
    <>
      <Card
        size="small"
        title="运行概览"
        className={styles.summaryCard}
        styles={{ body: { height: "100%" } }}
      >
        <Row gutter={[16, 16]} className={styles.overviewRow}>
          <Col xs={12} lg={6} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="站点总数" value={stats.total} />
            </div>
          </Col>
          <Col xs={12} lg={6} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="启用站点" value={stats.enabled} />
            </div>
          </Col>
          <Col xs={12} lg={6} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="采集中" value={stats.running} suffix={stats.waiting > 0 ? `等待 ${stats.waiting}` : undefined} />
            </div>
          </Col>
          <Col xs={12} lg={6} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic
                title="主站状态"
                value={stats.masters}
                suffix={<Tag color={masterStatus.color}>{masterStatus.text}</Tag>}
              />
            </div>
          </Col>
        </Row>
      </Card>

      <Card
        size="small"
        title="当前主站"
        className={styles.summaryCard}
        styles={{ body: { height: "100%" } }}
        extra={masterSite ? <Tag color="gold">已生效</Tag> : <Tag color="error">未配置</Tag>}
      >
        {masterSite ? (
          <Descriptions column={1} size="small" className={styles.masterDescriptions}>
            <Descriptions.Item label="站点名称">{masterSite.name}</Descriptions.Item>
            <Descriptions.Item label="接口地址">
              <Typography.Link
                href={masterSite.uri}
                target="_blank"
                rel="noopener noreferrer"
                className={styles.masterLink}
              >
                {masterSite.uri}
              </Typography.Link>
            </Descriptions.Item>
            <Descriptions.Item label="启用状态">
              <Tag color={masterSite.state ? "success" : "default"} bordered={false}>
                {masterSite.state ? "启用中" : "已停用"}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="图片同步">
              <Tag color={masterSite.syncPictures ? "processing" : "default"} bordered={false}>
                {masterSite.syncPictures ? "开启" : "关闭"}
              </Tag>
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Descriptions column={1} size="small">
            <Descriptions.Item label="状态">
              <Tag color="warning">未配置</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="说明">需要先指定一个主站</Descriptions.Item>
          </Descriptions>
        )}
      </Card>
    </>
  );
}
