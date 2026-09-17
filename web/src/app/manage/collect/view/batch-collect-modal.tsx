import { Alert, Flex, Form, Modal, Select, Space, Table, Tag, Typography } from "antd";
import { useEffect, useMemo, useState } from "react";
import { LoadingOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import type { BatchOption } from "./types";
import { collectDuration } from "./types";

interface BatchCollectModalProps {
  open: boolean;
  options: BatchOption[];
  selectedIds: string[];
  activeCollectIds: string[];
  batchTime: number;
  onCancel: () => void;
  onSubmit: () => void;
  onBatchTimeChange: (value: number) => void;
}

export default function BatchCollectModal(props: BatchCollectModalProps) {
  const {
    open,
    options,
    selectedIds,
    activeCollectIds,
    batchTime,
    onCancel,
    onSubmit,
    onBatchTimeChange,
  } = props;

  // 保持弹窗内的运行态稳定：仅在弹窗打开状态下同步，避免关闭及退场动画期间因外部乐观置为 starting 而闪烁 Alert
  const [stableActiveIds, setStableActiveIds] = useState<string[]>([]);

  useEffect(() => {
    if (open) {
      setStableActiveIds(activeCollectIds);
    }
  }, [open, activeCollectIds]);

  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const selectedRunningNames = useMemo(
    () =>
      options
        .filter((item) => selectedSet.has(item.id) && stableActiveIds.includes(item.id))
        .map((item) => item.name),
    [options, selectedSet, stableActiveIds],
  );

  const columns: ColumnsType<BatchOption> = [
    {
      title: "采集站",
      dataIndex: "name",
      render: (value: string, record) => (
        <Flex vertical gap={4}>
          <Space size={[8, 4]} wrap>
            <Typography.Text strong>{value}</Typography.Text>
            <Tag color={record.grade === 0 ? "gold" : "default"} variant="filled">
              {record.grade === 0 ? "主采集站" : "附属采集站"}
            </Tag>
            {stableActiveIds.includes(record.id) ? (
              <Tag icon={<LoadingOutlined />} color="processing" variant="filled">
                采集中
              </Tag>
            ) : null}
          </Space>
          <Typography.Text type="secondary">{record.id}</Typography.Text>
        </Flex>
      ),
    },
  ];

  return (
    <Modal
      title="批量采集"
      open={open}
      onCancel={onCancel}
      onOk={onSubmit}
      okText="开始采集"
      okButtonProps={{ "data-tour": "collect-batch-submit" }}
      width={960}
      destroyOnHidden
    >
      <Flex vertical gap={16}>
        {selectedRunningNames.length > 0 ? (
          <Alert
            showIcon
            type="warning"
            title="已选择的部分采集站正在运行"
            description={`${selectedRunningNames.join("、")} 正在采集中，重复启动会被后端自动跳过。`}
          />
        ) : null}

        <Space size={[8, 8]} wrap>
          <Tag variant="filled">将采集 {selectedIds.length} 个采集站</Tag>
          <Tag variant="filled">运行中 {stableActiveIds.length}</Tag>
        </Space>

        <Table<BatchOption>
          rowKey="id"
          size="middle"
          columns={columns}
          dataSource={options}
          pagination={false}
          scroll={{ y: 360 }}
        />

        <Form layout="vertical">
          <Form.Item label="采集时长" style={{ marginBottom: 0 }}>
            <Select
              value={batchTime}
              onChange={onBatchTimeChange}
              options={collectDuration.map((item) => ({ label: item.label, value: item.time }))}
            />
          </Form.Item>
        </Form>
      </Flex>
    </Modal>
  );
}
