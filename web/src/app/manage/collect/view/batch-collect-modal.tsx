import { Flex, Form, Modal, Select, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { collectDuration } from "./types";

export interface BatchCollectRow {
  id: string;
  name: string;
  reason: string;
}

interface BatchCollectModalProps {
  open: boolean;
  rows: BatchCollectRow[];
  batchTime: number;
  onCancel: () => void;
  onSubmit: () => void;
  onBatchTimeChange: (value: number) => void;
}

export default function BatchCollectModal(props: BatchCollectModalProps) {
  const { open, rows, batchTime, onCancel, onSubmit, onBatchTimeChange } = props;
  const startCount = rows.filter((item) => !item.reason).length;
  const skipCount = rows.length - startCount;

  const columns: ColumnsType<BatchCollectRow> = [
    {
      title: "采集站",
      dataIndex: "name",
      render: (value: string, record) => (
        <Flex vertical gap={4}>
          <Typography.Text strong>{value}</Typography.Text>
          <Typography.Text type="secondary">{record.id}</Typography.Text>
        </Flex>
      ),
    },
    {
      title: "状态",
      dataIndex: "reason",
      width: 160,
      render: (reason: string) =>
        reason ? (
          <Tag variant="filled">{reason}</Tag>
        ) : (
          <Tag color="success" variant="filled">
            可采集
          </Tag>
        ),
    },
  ];

  return (
    <Modal
      title="批量采集"
      open={open}
      onCancel={onCancel}
      onOk={onSubmit}
      okText={skipCount > 0 ? `将采集 ${startCount} 个，跳过 ${skipCount} 个` : `将采集 ${startCount} 个`}
      okButtonProps={{ "data-tour": "collect-batch-submit", disabled: startCount === 0 }}
      width={720}
      destroyOnHidden
    >
      <Flex vertical gap={16}>
        <Table<BatchCollectRow>
          rowKey="id"
          size="middle"
          columns={columns}
          dataSource={rows}
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
