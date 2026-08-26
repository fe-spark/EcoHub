import React from "react";
import { Tag, Button, Tooltip, Typography } from "antd";
import {
  ReloadOutlined,
  ClockCircleOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  CheckOutlined,
  LoadingOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { FailRecord, FAILURE_RECORD_STATUS, RECOVER_MAX_RETRY_COUNT } from "./types";

export function renderStatusTag(status: number, isRetrying?: boolean) {
  if (isRetrying) {
    return (
      <Tag color="processing" icon={<LoadingOutlined />}>
        重试中
      </Tag>
    );
  }
  if (status === FAILURE_RECORD_STATUS.pending) {
    return (
      <Tag color="default" icon={<ClockCircleOutlined />}>
        待自动重试
      </Tag>
    );
  }
  if (status === FAILURE_RECORD_STATUS.success) {
    return (
      <Tag color="success" icon={<CheckCircleOutlined />}>
        重试成功
      </Tag>
    );
  }
  return (
    <Tag color="error" icon={<CloseCircleOutlined />}>
      最终失败
    </Tag>
  );
}

export function normalizeStatusOptionLabel(name: string, value: number) {
  if (value === FAILURE_RECORD_STATUS.pending) {
    return "待自动重试";
  }
  if (value === FAILURE_RECORD_STATUS.failed) {
    return "最终失败";
  }
  return name;
}

interface GetColumnsParams {
  canWrite: boolean;
  queuedRetryIds: Set<number>;
  onRetry: (id: number) => void;
}

export function getRecordColumns({
  canWrite,
  queuedRetryIds,
  onRetry,
}: GetColumnsParams): ColumnsType<FailRecord> {
  return [
    {
      title: "ID",
      dataIndex: "ID",
      width: 70,
      fixed: "left",
      align: "center",
      render: (v) => <span style={{ color: "var(--ant-color-purple)" }}>{v}</span>,
    },
    {
      title: "采集站",
      dataIndex: "originName",
      align: "center",
      render: (v) => <Tag color="blue">{v}</Tag>,
    },
    {
      title: "采集源ID",
      dataIndex: "originId",
      align: "center",
      render: (v) => <Tag color="green">{v}</Tag>,
    },
    {
      title: "分页页码",
      dataIndex: "pageNumber",
      align: "center",
      render: (v) => <Tag color="orange">{v}</Tag>,
    },
    {
      title: "采集时长",
      dataIndex: "hour",
      align: "center",
      render: (v) => <Tag color="orange">{v > 0 ? `${v}小时` : "全量"}</Tag>,
    },
    {
      title: "失败原因",
      dataIndex: "cause",
      align: "left",
      ellipsis: true,
      render: (v) => <Typography.Text type="danger">{v}</Typography.Text>,
    },
    {
      title: "状态",
      dataIndex: "status",
      align: "center",
      render: (v, record) => renderStatusTag(v, queuedRetryIds.has(record.ID)),
    },
    {
      title: "重试次数",
      dataIndex: "retryCount",
      align: "center",
      render: (v) => {
        const retryCount = v ?? 0;
        const displayCount = Math.min(retryCount, RECOVER_MAX_RETRY_COUNT);
        const color =
          retryCount >= RECOVER_MAX_RETRY_COUNT ? "error" : retryCount > 0 ? "warning" : "default";
        return (
          <Tag color={color}>
            {displayCount}/{RECOVER_MAX_RETRY_COUNT}
          </Tag>
        );
      },
    },
    {
      title: "请求时间",
      dataIndex: "CreatedAt",
      align: "center",
      render: (v) => dayjs(v).format("YYYY-MM-DD HH:mm:ss"),
    },
    {
      title: "操作",
      key: "action",
      align: "center",
      fixed: "right",
      width: 80,
      render: (_, record) => {
        const isSuccess = record.status === FAILURE_RECORD_STATUS.success;
        const isFinalFailed = record.status === FAILURE_RECORD_STATUS.failed;
        const isQueued = queuedRetryIds.has(record.ID);

        if (isSuccess) {
          return (
            <Tooltip title="已重试成功，无需重复重试">
              <Button shape="circle" size="small" disabled icon={<CheckOutlined />} />
            </Tooltip>
          );
        }

        if (isFinalFailed) {
          return (
            <Tooltip title={isQueued ? "重试中..." : "已达自动重试上限(5/5)，点击可手动再次强制尝试"}>
              <Button
                shape="circle"
                size="small"
                loading={isQueued}
                disabled={!canWrite || isQueued}
                style={{
                  color: "var(--ant-color-warning)",
                  borderColor: "var(--ant-color-warning)",
                }}
                icon={<ReloadOutlined />}
                onClick={() => onRetry(record.ID)}
              />
            </Tooltip>
          );
        }

        return (
          <Tooltip title={isQueued ? "重试中..." : "立即重试此记录"}>
            <Button
              type="primary"
              shape="circle"
              size="small"
              loading={isQueued}
              disabled={!canWrite || isQueued}
              style={{ background: "#52c41a", borderColor: "#52c41a" }}
              icon={<ReloadOutlined />}
              onClick={() => onRetry(record.ID)}
            />
          </Tooltip>
        );
      },
    },
  ];
}
