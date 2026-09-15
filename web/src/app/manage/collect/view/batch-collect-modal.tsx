import { Alert, Flex, Form, Modal, Select, Space, Table, Tag, Typography } from "antd";
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

  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const selectedRunningNames = useMemo(
    () =>
      options
        .filter((item) => selectedSet.has(item.id) && activeCollectIds.includes(item.id))
        .map((item) => item.name),
    [activeCollectIds, options, selectedSet],
  );

  const hasSelectedWebdav = useMemo(
    () => options.some((item) => selectedSet.has(item.id) && item.sourceType === "webdav"),
    [options, selectedSet],
  );

  const columns: ColumnsType<BatchOption> = [
    {
      title: "采集站 / 媒体库",
      dataIndex: "name",
      render: (value: string, record) => (
        <Flex vertical gap={4}>
          <Space size={[8, 4]} wrap>
            <Typography.Text strong>{value}</Typography.Text>
            {record.sourceType === "webdav" ? (
              <Tag color="blue" variant="filled">
                WebDAV
              </Tag>
            ) : (
              <Tag color={record.grade === 0 ? "gold" : "default"} variant="filled">
                {record.grade === 0 ? "主采集站" : "附属采集站"}
              </Tag>
            )}
            {activeCollectIds.includes(record.id) ? (
              <Tag icon={<LoadingOutlined />} color="processing" variant="filled">
                {record.sourceType === "webdav" ? "扫描中" : "采集中"}
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
      title="批量采集与扫描"
      open={open}
      onCancel={onCancel}
      onOk={onSubmit}
      okText="开始执行"
      okButtonProps={{ "data-tour": "collect-batch-submit" }}
      width={960}
      destroyOnHidden
    >
      <Flex vertical gap={16}>
        {selectedRunningNames.length > 0 ? (
          <Alert
            showIcon
            type="warning"
            title="已选择的部分站点正在运行"
            description={`${selectedRunningNames.join("、")} 正在运行中，重复启动会被后端自动跳过。`}
          />
        ) : null}

        {hasSelectedWebdav ? (
          <Alert
            showIcon
            type="info"
            title="增量扫描说明"
            description="选中的 WebDAV 媒体库将执行增量扫描，不受下方采集时长限制。"
          />
        ) : null}

        <Space size={[8, 8]} wrap>
          <Tag variant="filled">将处理 {selectedIds.length} 个站点/媒体库</Tag>
          <Tag variant="filled">运行中 {activeCollectIds.length}</Tag>
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
          <Form.Item
            label="采集时长（仅适用于 MacCMS 采集站）"
            style={{ marginBottom: 0 }}
          >
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
