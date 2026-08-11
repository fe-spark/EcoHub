import { Alert, Button, Empty, Modal, Space, Spin, Tag, Typography } from "antd";
import { DeleteOutlined } from "@ant-design/icons";
import type { CleanupSkippedItem, InvalidSourceItem } from "./types";
import styles from "./index.module.less";

interface CleanupInvalidModalProps {
  open: boolean;
  scanning: boolean;
  deleting: boolean;
  invalidSources: InvalidSourceItem[];
  skipped: CleanupSkippedItem[];
  onCancel: () => void;
  onConfirm: () => void;
}

export default function CleanupInvalidModal({
  open,
  scanning,
  deleting,
  invalidSources,
  skipped,
  onCancel,
  onConfirm,
}: CleanupInvalidModalProps) {
  return (
    <Modal
      title="清理失效采集源"
      open={open}
      onCancel={onCancel}
      width={640}
      footer={null}
      destroyOnHidden
    >
      {scanning ? (
        <div className={styles.cleanupLoading}>
          <Spin size="large" description="正在逐一检测采集站接口，请稍候…" />
        </div>
      ) : (
        <div className={styles.cleanupContent}>
          <Alert
            type="warning"
            showIcon
            title={`检测到 ${invalidSources.length} 个采集不通的采集站`}
            description="删除后无法恢复；已禁用的采集站也会一并删除，请确认无误后再操作。"
          />

          <div className={styles.cleanupList}>
            {invalidSources.map((item) => (
              <div key={item.id} className={styles.cleanupItem}>
                <div className={styles.cleanupItemHead}>
                  <Typography.Text strong ellipsis className={styles.cleanupItemName}>
                    {item.name}
                  </Typography.Text>
                  <Tag color={item.grade === 0 ? "gold" : "default"} variant="filled">
                    {item.grade === 0 ? "主采集站" : "附属采集站"}
                  </Tag>
                  {!item.state ? (
                    <Tag color="default" variant="filled">
                      已禁用
                    </Tag>
                  ) : null}
                </div>
                <div className={styles.cleanupItemUri} title={item.uri}>
                  {item.uri}
                </div>
                <div className={styles.cleanupItemReason}>{item.reason}</div>
              </div>
            ))}
            {invalidSources.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有需要清理的采集站" />
            ) : null}
          </div>

          {skipped.length > 0 ? (
            <div className={styles.cleanupSkipped}>
              <Typography.Text type="secondary">
                已跳过 {skipped.length} 个：{skipped.map((item) => item.name || item.id).join("、")}
                （{skipped[0].reason}）
              </Typography.Text>
            </div>
          ) : null}

          <div className={styles.cleanupFooter}>
            <Space>
              <Button onClick={onCancel}>取消</Button>
              <Button
                type="primary"
                danger
                icon={<DeleteOutlined />}
                loading={deleting}
                disabled={invalidSources.length === 0}
                onClick={onConfirm}
              >
                删除 {invalidSources.length} 个失效采集站
              </Button>
            </Space>
          </div>
        </div>
      )}
    </Modal>
  );
}
