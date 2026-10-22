import { Button, Drawer, Flex, Form, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { collectDuration } from "./types";

export interface BatchCollectRow {
  id: string;
  name: string;
  reason: string;
}

interface BatchCollectDrawerProps {
  open: boolean;
  rows: BatchCollectRow[];
  batchTime: number;
  onCancel: () => void;
  onSubmit: () => void;
  onBatchTimeChange: (value: number) => void;
}

export default function BatchCollectDrawer(props: BatchCollectDrawerProps) {
  const { open, rows, batchTime, onCancel, onSubmit, onBatchTimeChange } = props;
  const startCount = rows.filter((item) => !item.reason).length;
  const skipCount = rows.length - startCount;

  const columns: ColumnsType<BatchCollectRow> = [
    {
      title: "采集站",
      dataIndex: "name",
      render: (value: string, record) => (
        <Flex vertical gap={2}>
          <Typography.Text strong>{value}</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {record.id}
          </Typography.Text>
        </Flex>
      ),
    },
    {
      title: "状态",
      dataIndex: "reason",
      width: 140,
      render: (reason: string) =>
        reason ? (
          <Tag variant="filled">{reason}</Tag>
        ) : (
          <Tag color="success" variant="filled">
            就绪
          </Tag>
        ),
    },
  ];

  const submitText =
    skipCount > 0 ? `采集 (${startCount}) · 跳过 (${skipCount})` : `开始采集 (${startCount})`;

  return (
    <Drawer
      title="批量采集"
      open={open}
      onClose={onCancel}
      width={600}
      destroyOnClose
      extra={
        <Space>
          <Button onClick={onCancel}>取消</Button>
          <Button
            type="primary"
            data-tour="collect-batch-submit"
            disabled={startCount === 0}
            onClick={onSubmit}
          >
            {submitText}
          </Button>
        </Space>
      }
    >
      <Flex vertical gap={16} style={{ height: "100%" }}>
        <Form layout="vertical">
          <Form.Item label="采集范围" style={{ marginBottom: 0 }}>
            <Select
              value={batchTime}
              onChange={onBatchTimeChange}
              options={collectDuration.map((item) => ({ label: item.label, value: item.time }))}
            />
          </Form.Item>
        </Form>

        <Table<BatchCollectRow>
          rowKey="id"
          size="middle"
          columns={columns}
          dataSource={rows}
          pagination={false}
          scroll={{ y: "calc(100vh - 280px)" }}
        />
      </Flex>
    </Drawer>
  );
}
