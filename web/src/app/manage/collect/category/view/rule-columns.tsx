import { Button, Popconfirm, Space, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { ROOT_GROUP, resolveGroupLabel, type MappingRuleRecord } from "./types";

interface BuildRuleColumnsOptions {
  canWrite: boolean;
  onEdit: (record: MappingRuleRecord) => void;
  onDelete: (id: number) => void;
}

/** 生成分类映射规则表格列定义，保持规则工作区主文件职责单一。 */
export function buildRuleColumns({
  canWrite,
  onEdit,
  onDelete,
}: BuildRuleColumnsOptions): ColumnsType<MappingRuleRecord> {
  return [
    {
      title: "ID",
      dataIndex: "id",
      width: 70,
      fixed: "left",
      align: "center",
      render: (value: number) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          #{value}
        </Typography.Text>
      ),
    },
    {
      title: "规则分组",
      dataIndex: "group",
      width: 120,
      align: "center",
      render: (value: string) => (
        <Tag color={value === ROOT_GROUP ? "orange" : "blue"} variant="filled">
          {resolveGroupLabel(value)}
        </Tag>
      ),
    },
    {
      title: "原始值",
      dataIndex: "raw",
      align: "left",
      render: (value: string) => <Typography.Text strong>{value}</Typography.Text>,
    },
    {
      title: "匹配方式",
      dataIndex: "matchType",
      width: 100,
      align: "center",
      render: (value: string) => (
        <Typography.Text type="secondary">
          {value === "regex" ? "正则匹配" : "精确匹配"}
        </Typography.Text>
      ),
    },
    {
      title: "目标展示值",
      dataIndex: "target",
      align: "left",
      render: (value: string) =>
        value ? (
          <Typography.Text strong style={{ color: "var(--ant-color-primary)" }}>
            {value}
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">未设置</Typography.Text>
        ),
    },
    {
      title: "说明",
      dataIndex: "remarks",
      align: "left",
      render: (value: string) => value || <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: "操作",
      key: "action",
      fixed: "right",
      align: "center",
      render: (_, record) => (
        <Space size={8}>
          <Button type="link" size="small" disabled={!canWrite} onClick={() => onEdit(record)}>
            编辑
          </Button>
          <Popconfirm
            title="删除该规则？"
            okText="删除"
            cancelText="取消"
            onConfirm={() => onDelete(record.id)}
          >
            <Button type="link" size="small" danger disabled={!canWrite}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
}
