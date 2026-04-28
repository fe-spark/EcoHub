import { Alert, Button, Checkbox, Flex, Form, Modal, Select, Space, Table, Tag, Typography } from "antd";
import { useMemo } from "react";
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
  onSelectionChange: (ids: string[]) => void;
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
    onSelectionChange,
    onBatchTimeChange,
  } = props;

  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const enabledIds = useMemo(
    () => options.filter((item) => item.state).map((item) => item.id),
    [options],
  );
  const selectedRunningNames = useMemo(
    () => options.filter((item) => selectedSet.has(item.id) && activeCollectIds.includes(item.id)).map((item) => item.name),
    [activeCollectIds, options, selectedSet],
  );

  const columns: ColumnsType<BatchOption> = [
    {
      title: "站点",
      dataIndex: "name",
      render: (value: string, record) => (
        <Flex vertical gap={4}>
          <Space size={[8, 4]} wrap>
            <Typography.Text strong>{value}</Typography.Text>
            <Tag color={record.grade === 0 ? "gold" : "default"} bordered={false}>
              {record.grade === 0 ? "主站" : "附属站"}
            </Tag>
            <Tag color={record.state ? "success" : "default"} bordered={false}>
              {record.state ? "已启用" : "已停用"}
            </Tag>
            {activeCollectIds.includes(record.id) ? (
              <Tag icon={<LoadingOutlined />} color="processing" bordered={false}>
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
      width={960}
      destroyOnHidden
    >
      <Flex vertical gap={16}>
        {selectedRunningNames.length > 0 ? (
          <Alert
            showIcon
            type="warning"
            message="已选择的部分站点正在运行"
            description={`${selectedRunningNames.join("、")} 正在采集中，重复启动会被后端自动跳过。`}
          />
        ) : null}

        <Flex justify="space-between" align="center" wrap="wrap" gap={12}>
          <Space size={[8, 8]} wrap>
            <Button onClick={() => onSelectionChange(options.map((item) => item.id))}>全选</Button>
            <Button onClick={() => onSelectionChange(enabledIds)}>仅选启用站点</Button>
            <Button
              onClick={() =>
                onSelectionChange(
                  options.filter((item) => !selectedSet.has(item.id)).map((item) => item.id),
                )
              }
            >
              反选
            </Button>
            <Button onClick={() => onSelectionChange([])}>清空</Button>
          </Space>
          <Space size={[8, 8]} wrap>
            <Tag bordered={false}>已选 {selectedIds.length}</Tag>
            <Tag bordered={false}>启用 {enabledIds.length}</Tag>
            <Tag bordered={false}>运行中 {activeCollectIds.length}</Tag>
          </Space>
        </Flex>

        <Table<BatchOption>
          rowKey="id"
          size="middle"
          columns={columns}
          dataSource={options}
          pagination={false}
          scroll={{ y: 360 }}
          rowSelection={{
            selectedRowKeys: selectedIds,
            onChange: (keys) => onSelectionChange(keys as string[]),
          }}
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
