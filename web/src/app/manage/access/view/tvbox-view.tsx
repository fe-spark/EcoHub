"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Card, Empty, Input, Space, Table, Tag, Typography } from "antd";
import {
  DesktopOutlined,
  PlayCircleOutlined,
  SearchOutlined,
  SettingOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { ApiGet } from "@/lib/client-api";
import TrendChart from "./trend-chart";
import { type DonutSlice } from "./donut-chart";
import DistBarChart from "./dist-bar-chart";
import BusinessRankings from "./business-rankings";
import type { Overview, TopItem, LogRow } from "./types";
import ResourceCell from "./resource-cell";
import styles from "./index.module.less";

function formatTvboxAction(action?: string, path?: string, query?: string) {
  const full = `${path || ""} ${query || ""}`;
  if (action === "play" || full.includes("ac=detail") || full.includes("ac=videolist")) {
    return { label: "影视点播", color: "#fa8c16", icon: <PlayCircleOutlined /> };
  }
  if (action === "search" || full.includes("ac=search") || full.includes("wd=")) {
    return { label: "寻片搜索", color: "#fa541c", icon: <SearchOutlined /> };
  }
  if (action === "config" || full.includes("/config")) {
    return { label: "配置同步", color: "#722ed1", icon: <SettingOutlined /> };
  }
  if (action === "classify" || full.includes("ac=list")) {
    return { label: "分类与列表", color: "#52c41a", icon: <DesktopOutlined /> };
  }
  return { label: "源数据刷新", color: "#52c41a", icon: <DesktopOutlined /> };
}

export default function TvboxAnalyticsView({ dayStr, refreshKey }: { dayStr: string; refreshKey?: number }) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [playTops, setPlayTops] = useState<TopItem[]>([]);
  const [searchTops, setSearchTops] = useState<TopItem[]>([]);
  const [classifyTops, setClassifyTops] = useState<TopItem[]>([]);
  const [logs, setLogs] = useState<LogRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [logSearch, setLogSearch] = useState("");
  const isFirstMount = useRef(true);
  const lastDayRef = useRef(dayStr);
  // 竞态防护：静默轮询在途即跳过（避免请求无界叠加），响应只接受最新一批（防止慢响应覆盖新日期结果）
  const reqSeqRef = useRef(0);
  const inFlightRef = useRef(false);

  const fetchData = useCallback(async (silent = false) => {
    if (silent && inFlightRef.current) {
      return;
    }
    inFlightRef.current = true;
    const seq = ++reqSeqRef.current;
    if (!silent) {
      setLoading(true);
    }
    try {
      const [ovRes, playRes, searchRes, classifyRes, logRes] = await Promise.all([
        ApiGet<Overview>(`/manage/access/overview?day=${dayStr}&module=tvbox`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=tvbox&kind=play&limit=10`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=tvbox&kind=search&limit=10`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=tvbox&kind=classify&limit=10`),
        ApiGet<{ list: LogRow[] }>(`/manage/access/logs?day=${dayStr}&module=tvbox&limit=100`),
      ]);
      if (seq !== reqSeqRef.current) {
        return;
      }
      if (ovRes.code === 0 && ovRes.data) {
        setOverview(ovRes.data);
      }
      if (playRes.code === 0 && playRes.data) {
        setPlayTops(playRes.data.items || []);
      }
      if (searchRes.code === 0 && searchRes.data) {
        setSearchTops(searchRes.data.items || []);
      }
      if (classifyRes.code === 0 && classifyRes.data) {
        setClassifyTops(classifyRes.data.items || []);
      }
      if (logRes.code === 0 && logRes.data) {
        const list = (logRes.data.list || []).map((item, idx) => ({
          ...item,
          _key: `${item.ts}-${item.path}-${item.resource}-${idx}`,
        }));
        setLogs(list);
      }
    } catch {
      // 忽略请求异常
    } finally {
      if (seq === reqSeqRef.current) {
        inFlightRef.current = false;
        if (!silent) {
          setLoading(false);
        }
      }
    }
  }, [dayStr]);

  useEffect(() => {
    const dayChanged = lastDayRef.current !== dayStr;
    lastDayRef.current = dayStr;
    const isInitial = isFirstMount.current || dayChanged;
    isFirstMount.current = false;

    void fetchData(!isInitial);
  }, [fetchData, refreshKey, dayStr]);

  const filteredLogs = logs.filter((log) => {
    if (!logSearch) return true;
    const q = logSearch.toLowerCase();
    return (
      (log.path && log.path.toLowerCase().includes(q)) ||
      (log.query && log.query.toLowerCase().includes(q)) ||
      (log.resource && log.resource.toLowerCase().includes(q)) ||
      (log.resourceTitle && log.resourceTitle.toLowerCase().includes(q)) ||
      (log.deviceId && log.deviceId.toLowerCase().includes(q)) ||
      (log.ipPreview && log.ipPreview.toLowerCase().includes(q)) ||
      (log.uaFamily && log.uaFamily.toLowerCase().includes(q))
    );
  });

  const columns: ColumnsType<LogRow> = [
    {
      title: "调用时间",
      dataIndex: "ts",
      key: "ts",
      width: 170,
      render: (ts: string) => dayjs(ts).format("YYYY-MM-DD HH:mm:ss"),
    },
    {
      title: "操作分类",
      key: "actionType",
      width: 130,
      render: (_, record) => {
        const info = formatTvboxAction(record.action, record.path, record.query);
        return (
          <Tag color={info.color} icon={info.icon}>
            {info.label}
          </Tag>
        );
      },
    },
    {
      title: "接口路径与参数",
      dataIndex: "path",
      key: "path",
      render: (path: string, record) => (
        <Typography.Text code>
          {path}
          {record.query ? `?${record.query}` : ""}
        </Typography.Text>
      ),
    },
    {
      title: "关联资源 / 关键词",
      dataIndex: "resource",
      key: "resource",
      width: 240,
      render: (_, record) => <ResourceCell record={record} />,
    },
    {
      title: "设备 ID",
      dataIndex: "deviceId",
      key: "deviceId",
      width: 220,
      render: (did?: string) =>
        did ? (
          <Typography.Text
            copyable={{ text: did }}
            code
            style={{ fontSize: 12, whiteSpace: "nowrap" }}
          >
            {did}
          </Typography.Text>
        ) : (
          <span style={{ color: "#bbb" }}>-</span>
        ),
    },
    {
      title: "电视设备 IP",
      dataIndex: "ipPreview",
      key: "ipPreview",
      width: 140,
      render: (ip: string) => <Typography.Text code>{ip || "local"}</Typography.Text>,
    },
    {
      title: "电视客户端环境",
      dataIndex: "uaFamily",
      key: "uaFamily",
      width: 140,
      render: (ua: string) => <Tag>{ua || "tvbox"}</Tag>,
    },
  ];

  // TVBox 接口调用分布（优先读取全天概览 action 真实汇总，无概览时以最近 logs 采样作为兜底）
  const hasActionOverview = Boolean(overview?.action && Object.keys(overview.action).length > 0);
  let detailCount = 0;
  let searchCount = 0;
  let configCount = 0;
  let listCount = 0;

  if (hasActionOverview) {
    detailCount = overview?.action?.play ?? 0;
    searchCount = overview?.action?.search ?? 0;
    configCount = overview?.action?.config ?? 0;
    listCount = (overview?.action?.classify ?? 0) + (overview?.action?.browse ?? 0);
  } else {
    detailCount = logs.filter((l) => formatTvboxAction(l.action, l.path, l.query).label === "影视点播").length;
    searchCount = logs.filter((l) => formatTvboxAction(l.action, l.path, l.query).label === "寻片搜索").length;
    configCount = logs.filter((l) => formatTvboxAction(l.action, l.path, l.query).label === "配置同步").length;
    listCount = Math.max(0, logs.length - detailCount - searchCount - configCount);
  }

  const tvboxSlices: DonutSlice[] = [
    {
      name: "寻片搜索",
      value: searchCount,
      color: "#fa541c",
      icon: <SearchOutlined />,
      desc: "电视端关键字与首字母搜片 (/vod ac=search)",
    },
    {
      name: "分类与列表",
      value: listCount,
      color: "#52c41a",
      icon: <DesktopOutlined />,
      desc: "分类筛选与分页列表浏览 (/vod ac=list)",
    },
    {
      name: "影视点播",
      value: detailCount,
      color: "#fa8c16",
      icon: <PlayCircleOutlined />,
      desc: "影片详情与播放地址解析 (/vod ac=detail)",
    },
    {
      name: "源配置同步",
      value: configCount,
      color: "#722ed1",
      icon: <SettingOutlined />,
      desc: "TVBox 单仓/多仓配置拉取与订阅更新 (/config)",
    },
  ].filter((s) => s.value > 0);

  return (
    <div className={styles.subModuleWrapper}>
      {/* 板块一：调用趋势 */}
      <div className={styles.sectionBlock}>
        <div className={styles.sectionHeader}>
          <span className={styles.sectionTitle}>
            <DesktopOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
            TVBox 调用趋势
          </span>
        </div>

        {/* 核心指标 4 列横排卡片 */}
        <div className={styles.statsGrid}>
          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>今日调用量 (PV)</span>
              <div className={styles.statValue}>{overview?.pv?.toLocaleString() ?? 0}</div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconPv}`}>
              <DesktopOutlined />
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>活跃设备 (UV)</span>
              <div className={styles.statValue}>{overview?.uv?.toLocaleString() ?? 0}</div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconUv}`}>
              <UserOutlined />
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>影视点播量</span>
              <div className={styles.statValue}>
                {((overview?.action?.play ?? 0) > 0
                  ? overview!.action.play
                  : playTops.reduce((acc, cur) => acc + cur.count, 0)
                ).toLocaleString()}
              </div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconPlay}`}>
              <PlayCircleOutlined />
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>寻片搜索量</span>
              <div className={styles.statValue}>
                {((overview?.action?.search ?? 0) > 0
                  ? overview!.action.search
                  : searchTops.reduce((acc, cur) => acc + cur.count, 0)
                ).toLocaleString()}
              </div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconSearch}`}>
              <SearchOutlined />
            </div>
          </div>
        </div>

        {/* 24 小时流量走势全宽卡片 */}
        <Card title="24 小时调用走势" className={styles.chartCard} loading={loading}>
          {overview?.series && overview.series.length > 0 ? (
            <TrendChart series={overview.series} activeTab="all" />
          ) : (
            <Empty description="暂无 TVBox 流量趋势数据" />
          )}
        </Card>
      </div>

      {/* 板块二：影视与搜索热度 */}
      <div className={styles.sectionBlock}>
        <div className={styles.sectionHeader}>
          <span className={styles.sectionTitle}>
            <ThunderboltOutlined style={{ color: "#fa541c" }} />
            影视与搜索热度
          </span>
        </div>

        <BusinessRankings
          playTops={playTops}
          searchTops={searchTops}
          classifyTops={classifyTops}
          loading={loading}
          dayStr={dayStr}
          module="tvbox"
        />
      </div>

      {/* 板块三：接口调用分布 */}
      <div className={styles.sectionBlock}>
        <div className={styles.sectionHeader}>
          <span className={styles.sectionTitle}>
            <ThunderboltOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
            接口调用分布
          </span>
        </div>

        <Card
          className={styles.chartCard}
          classNames={{ body: styles.centeredCardBody }}
          loading={loading}
        >
          {tvboxSlices.length === 0 ? (
            <Empty description="暂无接口调用分布数据" />
          ) : (
            <DistBarChart slices={tvboxSlices} unit="次" />
          )}
        </Card>
      </div>

      {/* 板块四：实时客户端流水 */}
      <div className={styles.sectionBlock}>
        <Card
          title={
            <div className={styles.tableTitleRow}>
              <span style={{ fontWeight: 600 }}>调用流水</span>
              <Space>
                <Input.Search
                  placeholder="搜索接口路径 / 关键词 / IP"
                  allowClear
                  value={logSearch}
                  onChange={(e) => setLogSearch(e.target.value)}
                  style={{ width: 240 }}
                />
              </Space>
            </div>
          }
          className={styles.tableCard}
        >
          <Table
            rowKey="_key"
            columns={columns}
            dataSource={filteredLogs}
            loading={loading}
            pagination={{ pageSize: 10, showTotal: (t) => `共 ${t} 条记录` }}
            size="middle"
          />
        </Card>
      </div>
    </div>
  );
}
