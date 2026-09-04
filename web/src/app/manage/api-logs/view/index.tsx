"use client";

import React, { useCallback, useEffect, useState } from "react";
import {
  Badge,
  Button,
  DatePicker,
  Input,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  ReloadOutlined,
  SearchOutlined,
  ThunderboltOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import ManagePageHeader from "@/app/manage/components/page-header";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import styles from "./index.module.less";

interface ApiLogItem {
  id: number;
  createdAt: string;
  method: string;
  path: string;
  query: string;
  status: number;
  durationMs: number;
  ip: string;
  clientType: string;
  deviceId?: string;
  ua: string;
}

interface ApiLogQueryResult {
  list: ApiLogItem[];
  total: number;
  page: number;
  pageSize: number;
  totalToday: number;
  errorToday: number;
  slowToday: number;
  avgMsToday: number;
}

export interface ApiLogsPageViewProps {
  embedded?: boolean;
}

export default function ApiLogsPageView({ embedded = false }: ApiLogsPageViewProps = {}) {
  const { message } = useAppMessage();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<ApiLogQueryResult | null>(null);

  // 筛选状态
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [method, setMethod] = useState("all");
  const [status, setStatus] = useState("all");
  const [duration, setDuration] = useState("all");
  const [clientType, setClientType] = useState("all");
  const [keyword, setKeyword] = useState("");
  const [inputKeyword, setInputKeyword] = useState("");
  const [dateRange, setDateRange] = useState<[string, string] | null>(null);

  const fetchLogs = useCallback(async () => {
    setLoading(true);
    try {
      const params: Record<string, string | number> = {
        page,
        pageSize,
      };
      if (method !== "all") params.method = method;
      if (status !== "all") params.status = status;
      if (duration !== "all") params.duration = duration;
      if (clientType !== "all") params.clientType = clientType;
      if (keyword.trim()) params.q = keyword.trim();
      if (dateRange) {
        params.startTime = dateRange[0];
        params.endTime = dateRange[1];
      }

      const res = await ApiGet<ApiLogQueryResult>("/manage/api-logs/list", params);
      if (res.data) {
        setData(res.data);
      }
    } catch {
      message.error("获取接口访问记录失败");
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, method, status, duration, clientType, keyword, dateRange, message]);

  useEffect(() => {
    fetchLogs();
  }, [fetchLogs]);

  // 手动修剪 7 天前日志
  const handlePrune = async () => {
    try {
      const res = await ApiPost<{ deleted: number }>("/manage/api-logs/prune?days=7");
      message.success(`成功清理 7 天前过期记录 ${res.data?.deleted ?? 0} 条`);
      fetchLogs();
    } catch {
      message.error("清理过期记录失败");
    }
  };

  const methodTag = (m: string) => {
    const upper = m.toUpperCase();
    if (upper === "GET") return <Tag color="blue">GET</Tag>;
    if (upper === "POST") return <Tag color="green">POST</Tag>;
    if (upper === "PUT") return <Tag color="orange">PUT</Tag>;
    if (upper === "DELETE") return <Tag color="red">DELETE</Tag>;
    return <Tag>{upper}</Tag>;
  };

  const statusBadge = (s: number) => {
    if (s >= 200 && s < 300) return <Badge status="success" text={<span style={{ color: "#52c41a", fontWeight: 600 }}>{s}</span>} />;
    if (s >= 300 && s < 400) return <Badge status="warning" text={<span style={{ color: "#fa8c16", fontWeight: 600 }}>{s}</span>} />;
    if (s >= 400 && s < 500) return <Badge status="warning" text={<span style={{ color: "#fa541c", fontWeight: 700 }}>{s}</span>} />;
    return <Badge status="error" text={<span style={{ color: "#f5222d", fontWeight: 700 }}>{s}</span>} />;
  };

  const durationText = (ms: number) => {
    if (ms < 100) return <span className={styles.durationBadgeFast}>{ms} ms</span>;
    if (ms <= 500) return <span className={styles.durationBadgeMedium}>{ms} ms</span>;
    return (
      <Tooltip title=">500ms 慢请求">
        <span className={styles.durationBadgeSlow}>{ms} ms ⚡</span>
      </Tooltip>
    );
  };

  const columns: ColumnsType<ApiLogItem> = [
    {
      title: "请求时间",
      dataIndex: "createdAt",
      width: 170,
      render: (t: string) => dayjs(t).format("YYYY-MM-DD HH:mm:ss"),
    },
    {
      title: "Method",
      dataIndex: "method",
      width: 90,
      align: "center",
      render: (m: string) => methodTag(m),
    },
    {
      title: "状态码",
      dataIndex: "status",
      width: 95,
      render: (s: number) => statusBadge(s),
    },
    {
      title: "响应耗时",
      dataIndex: "durationMs",
      width: 110,
      render: (d: number) => durationText(d),
    },
    {
      title: "接口路径 (Path)",
      dataIndex: "path",
      render: (p: string, r: ApiLogItem) => (
        <span className={styles.pathCell}>
          {p}
          {r.query && <span style={{ color: "var(--ant-color-text-tertiary)", marginLeft: 4 }}>?{r.query.length > 60 ? `${r.query.slice(0, 60)}...` : r.query}</span>}
        </span>
      ),
    },
    {
      title: "客户端 IP",
      dataIndex: "ip",
      width: 140,
      render: (ip: string) => <span className={styles.ipCell}>{ip || "-"}</span>,
    },
    {
      title: "设备 ID",
      dataIndex: "deviceId",
      width: 180,
      render: (did?: string) =>
        did ? (
          <Typography.Text copyable={{ text: did }} code style={{ fontSize: 12 }}>
            {did}
          </Typography.Text>
        ) : (
          <span className={styles.ipCell}>-</span>
        ),
    },
    {
      title: "终端渠道",
      dataIndex: "clientType",
      width: 100,
      align: "center",
      render: (ct: string) => {
        if (ct === "web") return <Tag color="orange">Web</Tag>;
        if (ct === "app") return <Tag color="blue">App</Tag>;
        if (ct === "tvbox") return <Tag color="purple">TVBox</Tag>;
        return <Tag>{ct || "API"}</Tag>;
      },
    },
  ];

  const handleReset = () => {
    setMethod("all");
    setStatus("all");
    setDuration("all");
    setClientType("all");
    setKeyword("");
    setInputKeyword("");
    setDateRange(null);
    setPage(1);
  };

  const actionButtons = (
    <Space size={8}>
      <Popconfirm
        title="确认清理 7 天前的历史日志？"
        onConfirm={handlePrune}
        okText="确认清理"
        cancelText="取消"
      >
        <Button icon={<DeleteOutlined />}>清理历史日志</Button>
      </Popconfirm>
      <Button
        type="primary"
        icon={<ReloadOutlined />}
        onClick={() => fetchLogs()}
        loading={loading}
      >
        刷新
      </Button>
    </Space>
  );

  return (
    <div className={styles.pageWrapper}>
      {!embedded && (
        <ManagePageHeader
          title="接口访问记录"
          description="监控全站 HTTP 接口调用量、状态码分布、慢请求与真实客户端 IP。"
        />
      )}

      {/* 顶部指标与操作一体化栏 */}
      <div className={styles.metricActionBar}>
        <div className={styles.metricsGroup}>
          <div className={styles.metricItem}>
            <ThunderboltOutlined className={styles.metricIcon} style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
            <span className={styles.metricLabel}>今日总请求:</span>
            <span className={styles.metricValue}>{data?.totalToday?.toLocaleString() ?? 0}</span>
          </div>

          <div className={styles.metricItem}>
            <WarningOutlined className={styles.metricIcon} style={{ color: "#ff4d4f" }} />
            <span className={styles.metricLabel}>今日异常:</span>
            <span className={styles.metricValue} style={{ color: (data?.errorToday ?? 0) > 0 ? "#ff4d4f" : undefined }}>
              {data?.errorToday?.toLocaleString() ?? 0}
            </span>
            <span className={styles.metricSubText}>
              ({data?.totalToday ? (((data?.errorToday ?? 0) / data.totalToday) * 100).toFixed(1) : "0.0"}%)
            </span>
          </div>

          <div className={styles.metricItem}>
            <ClockCircleOutlined className={styles.metricIcon} style={{ color: "#fa8c16" }} />
            <span className={styles.metricLabel}>慢接口 (&gt;500ms):</span>
            <span className={styles.metricValue} style={{ color: (data?.slowToday ?? 0) > 0 ? "#fa8c16" : undefined }}>
              {data?.slowToday?.toLocaleString() ?? 0}
            </span>
          </div>

          <div className={styles.metricItem}>
            <CheckCircleOutlined className={styles.metricIcon} style={{ color: "#52c41a" }} />
            <span className={styles.metricLabel}>平均响应:</span>
            <span className={styles.metricValue}>{data?.avgMsToday ?? 0} ms</span>
          </div>
        </div>

        <div>{actionButtons}</div>
      </div>

      {/* 结构化筛选工具栏 */}
      <div className={styles.filterCard}>
        <div className={styles.filterRow}>
          <div className={styles.filterGroup}>
            <DatePicker.RangePicker
              showTime
              placeholder={["开始时间", "结束时间"]}
              style={{ width: 330 }}
              disabledDate={(d) =>
                d.isAfter(dayjs(), "day") ||
                d.isBefore(dayjs().subtract(6, "day"), "day")
              }
              value={dateRange ? [dayjs(dateRange[0]), dayjs(dateRange[1])] : null}
              onChange={(dates) => {
                if (dates && dates[0] && dates[1]) {
                  // 服务端会把单次查询收口到近 3 天，这里同步截断避免界面与数据不一致
                  let start = dates[0];
                  const end = dates[1];
                  if (end.diff(start, "day") > 3) {
                    start = end.subtract(3, "day");
                    message.info("单次查询范围上限为 3 天，已自动收口");
                  }
                  setDateRange([
                    start.format("YYYY-MM-DD HH:mm:ss"),
                    end.format("YYYY-MM-DD HH:mm:ss"),
                  ]);
                } else {
                  setDateRange(null);
                }
                setPage(1);
              }}
            />

            <Select
              value={method}
              style={{ width: 105 }}
              onChange={(v) => { setMethod(v); setPage(1); }}
              options={[
                { label: "全部 Method", value: "all" },
                { label: "GET", value: "GET" },
                { label: "POST", value: "POST" },
                { label: "PUT", value: "PUT" },
                { label: "DELETE", value: "DELETE" },
              ]}
            />

            <Select
              value={status}
              style={{ width: 115 }}
              onChange={(v) => { setStatus(v); setPage(1); }}
              options={[
                { label: "全部状态码", value: "all" },
                { label: "2xx 成功", value: "2xx" },
                { label: "4xx 异常", value: "4xx" },
                { label: "5xx 错误", value: "5xx" },
                { label: "所有异常", value: "error" },
              ]}
            />

            <Select
              value={duration}
              style={{ width: 125 }}
              onChange={(v) => { setDuration(v); setPage(1); }}
              options={[
                { label: "全部耗时", value: "all" },
                { label: "快速 (<100ms)", value: "fast" },
                { label: "正常 (100-500ms)", value: "medium" },
                { label: "慢接口 (>500ms)", value: "slow" },
              ]}
            />

            <Select
              value={clientType}
              style={{ width: 105 }}
              onChange={(v) => { setClientType(v); setPage(1); }}
              options={[
                { label: "全部终端", value: "all" },
                { label: "Web", value: "web" },
                { label: "App", value: "app" },
                { label: "TVBox", value: "tvbox" },
              ]}
            />
          </div>

          <div className={styles.filterGroup}>
            <Input.Search
              placeholder="搜索路径 / IP / 设备 ID..."
              value={inputKeyword}
              onChange={(e) => setInputKeyword(e.target.value)}
              onSearch={(v) => { setKeyword(v); setPage(1); }}
              enterButton={<SearchOutlined />}
              allowClear
              style={{ width: 250 }}
            />
            <Button icon={<ReloadOutlined />} onClick={handleReset}>
              重置
            </Button>
          </div>
        </div>
      </div>

      {/* 接口记录表格 */}
      <div className={styles.tableCard}>
        <div className={styles.tableHeader}>
          <div className={styles.tableTitle}>
            <span>调用流水</span>
            <span className={styles.tableCount}>（共 {data?.total?.toLocaleString() ?? 0} 条）</span>
          </div>
        </div>

        <Table
          rowKey="id"
          columns={columns}
          dataSource={data?.list || []}
          loading={loading}
          pagination={{
            current: page,
            pageSize,
            total: data?.total || 0,
            showSizeChanger: true,
            pageSizeOptions: ["20", "50", "100"],
            showQuickJumper: true,
            showTotal: (total) => `共 ${total.toLocaleString()} 条`,
            onChange: (p, ps) => {
              setPage(p);
              setPageSize(ps);
            },
          }}
          expandable={{
            expandedRowRender: (record) => (
              <div className={styles.expandBox}>
                <div className={styles.expandRow}>
                  <span className={styles.expandLabel}>完整请求路径:</span>
                  <span className={styles.expandVal}>{record.path}</span>
                </div>
                {record.query && (
                  <div className={styles.expandRow}>
                    <span className={styles.expandLabel}>Query 参数:</span>
                    <span className={styles.expandVal}>?{record.query}</span>
                  </div>
                )}
                <div className={styles.expandRow}>
                  <span className={styles.expandLabel}>User-Agent:</span>
                  <span className={styles.expandVal}>{record.ua || "-"}</span>
                </div>
                <div className={styles.expandRow}>
                  <span className={styles.expandLabel}>客户端 IP:</span>
                  <span className={styles.expandVal}>{record.ip || "127.0.0.1"}</span>
                </div>
              </div>
            ),
          }}
        />
      </div>
    </div>
  );
}
