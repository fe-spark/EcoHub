import { Button, Flex, Popconfirm, Progress, Select, Space, Switch, Tag, Tooltip, Typography } from "antd";
import { DeleteOutlined, EditOutlined, PoweroffOutlined, StopOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { collectDuration, type FilmSource } from "./types";

interface CollectTableColumnsOptions {
  activeCollectIds: string[];
  onUpdateItem: (id: string, updater: (record: FilmSource) => FilmSource) => void;
  onChangeCollectDuration: (id: string, value: number) => void;
  onChangeSourceState: (record: FilmSource) => void;
  onStartTask: (record: FilmSource) => void;
  onTerminateTask: (id: string) => void;
  onEditSource: (id: string) => void;
  onDeleteSource: (id: string) => void;
}

export function createCollectTableColumns({
  activeCollectIds,
  onUpdateItem,
  onChangeCollectDuration,
  onChangeSourceState,
  onStartTask,
  onTerminateTask,
  onEditSource,
  onDeleteSource,
}: CollectTableColumnsOptions): ColumnsType<FilmSource> {
  return [
    {
      title: "ID",
      dataIndex: "id",
      width: 80,
      fixed: "left",
      align: "center",
      render: (value: number) => <Tag bordered={false}>{value}</Tag>,
    },
    {
      title: "站点",
      dataIndex: "name",
      align: "left",
      render: (name: string, record) => (
        <Flex vertical gap={6}>
          <Space size={[8, 4]} wrap>
            <Typography.Text strong>{name}</Typography.Text>
            <Tag color={record.grade === 0 ? "gold" : "default"} bordered={false}>
              {record.grade === 0 ? "主站" : "附属站"}
            </Tag>
            <Tag color={record.state ? "success" : "default"} bordered={false}>
              {record.state ? "已启用" : "已禁用"}
            </Tag>
          </Space>
          <Typography.Link href={record.uri} target="_blank" rel="noopener noreferrer">
            {record.uri}
          </Typography.Link>
        </Flex>
      ),
    },
    {
      title: "采集进度",
      dataIndex: "progress",
      render: (_, record) => {
        const progress = record.progress;
        if (!progress) {
          return <Typography.Text type="secondary">未采集</Typography.Text>;
        }

        const total = Math.max(progress.total, 0);
        const finished = Math.max(progress.success + progress.failed, 0);
        const done = Math.min(finished, total || finished);
        const isDone = progress.status === "done";
        const rawPercent = total > 0 ? Math.floor((done / total) * 100) : 0;
        const percent = isDone ? 100 : Math.min(rawPercent, 99);
        const progressText = total > 0
          ? `${done}/${total}`
          : done > 0
            ? `${done}`
            : "即将开始采集";
        const statusText = progress.status === "starting"
          ? "等待中"
          : progress.status === "failed"
            ? "失败"
            : progress.status === "stopped"
              ? "已停止"
              : progress.status === "finalizing"
                ? "收尾发布中"
                : progress.status === "done"
                  ? "已完成"
                  : "采集中";
        const progressStatus = progress.status === "running" || progress.status === "finalizing"
          ? "active"
          : "normal";
        const progressStrokeColor = progress.failed > 0 ? "#faad14" : undefined;
        return (
          <Flex vertical gap={4}>
            <Typography.Text type={progress.status === "starting" ? "secondary" : undefined}>{statusText}</Typography.Text>
            <Progress
              percent={percent}
              size="small"
              status={progressStatus}
              strokeColor={progressStrokeColor}
              format={(value) => `${value ?? 0}%`}
            />
            <Typography.Text type="secondary">
              {progressText}
              {progress.failed > 0 ? `，失败 ${progress.failed}` : ""}
            </Typography.Text>
          </Flex>
        );
      },
    },
    {
      title: "上次采集",
      dataIndex: "lastCollectTime",
      align: "center",
      render: (value?: string) => (
        value
          ? <Typography.Text>{dayjs(value).format("YYYY-MM-DD HH:mm:ss")}</Typography.Text>
          : <Typography.Text type="secondary">暂无</Typography.Text>
      ),
    },
    {
      title: "图片同步",
      dataIndex: "syncPictures",
      align: "center",
      render: (value: boolean, record) => {
        const isRunning = activeCollectIds.includes(record.id);
        return (
          <Switch
            checked={value}
            disabled={record.grade === 1 || isRunning}
            checkedChildren="开启"
            unCheckedChildren="关闭"
            onChange={(checked) => {
              onUpdateItem(record.id, (item) => ({ ...item, syncPictures: checked }));
              onChangeSourceState({ ...record, syncPictures: checked });
            }}
          />
        );
      },
    },
    {
      title: "启用状态",
      dataIndex: "state",
      align: "center",
      render: (value: boolean, record) => {
        const isRunning = activeCollectIds.includes(record.id);
        if (isRunning) {
          return (
            <Tag color={record.state ? "processing" : "warning"}>
              {record.state ? "采集中" : "已终止·等待完成"}
            </Tag>
          );
        }
        return (
          <Switch
            checked={value}
            checkedChildren="启用"
            unCheckedChildren="禁用"
            onChange={(checked) => {
              onUpdateItem(record.id, (item) => ({ ...item, state: checked }));
              onChangeSourceState({ ...record, state: checked });
            }}
          />
        );
      },
    },
    {
      title: "请求间隔",
      dataIndex: "interval",
      align: "center",
      render: (value: number) => <Tag bordered={false}>{value > 0 ? `${value} ms` : "无限制"}</Tag>,
    },
    {
      title: "采集时长",
      align: "center",
      render: (_, record) => {
        const isRunning = activeCollectIds.includes(record.id);
        return (
          <Select
            size="small"
            value={record.cd}
            disabled={isRunning}
            style={{ width: "100%" }}
            options={collectDuration.map((item) => ({ value: item.time, label: item.label }))}
            onChange={(value) => {
              onChangeCollectDuration(record.id, value);
            }}
          />
        );
      },
    },
    {
      title: "操作",
      key: "action",
      fixed: "right",
      align: "center",
      render: (_, record) => {
        const isRunning = activeCollectIds.includes(record.id);
        if (isRunning) {
          return (
            <Popconfirm
              title="停止该站点后续请求？"
              description="将禁用该站点；已请求数据会继续入库。"
              onConfirm={() => onTerminateTask(record.id)}
              disabled={!record.state}
              okText="停止请求"
              cancelText="取消"
              okButtonProps={{ danger: true }}
            >
              <Button
                danger
                icon={<StopOutlined />}
                disabled={!record.state}
              >
                {record.state ? "停止请求" : "已停止"}
              </Button>
            </Popconfirm>
          );
        }
        return (
          <Space size={4}>
            <Tooltip title="开始采集">
              <Button type="primary" icon={<PoweroffOutlined />} onClick={() => onStartTask(record)} />
            </Tooltip>
            <Tooltip title="编辑站点">
              <Button icon={<EditOutlined />} onClick={() => onEditSource(record.id)} />
            </Tooltip>
            <Popconfirm title="确认删除此采集站？" onConfirm={() => onDeleteSource(record.id)}>
              <Button danger icon={<DeleteOutlined />} />
            </Popconfirm>
          </Space>
        );
      },
    },
  ];
}
