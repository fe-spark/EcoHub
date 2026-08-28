"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  DatePicker,
  Empty,
  Input,
  Select,
  Segmented,
  Space,
  Table,
  Tag,
} from "antd";
import {
  EyeOutlined,
  ReloadOutlined,
  WarningOutlined,
  ClockCircleOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { ApiGet } from "@/lib/client-api";
import ManagePageHeader from "@/app/manage/components/page-header";
import Sparkline from "./sparkline";
import styles from "./index.module.less";

type Overview = {
  day: string;
  pv: number;
  uv: number;
  err4: number;
  err5: number;
  p95Ms: number;
  dropped: number;
  provide?: { pv: number; err4: number; err5: number };
  client?: Record<string, number>;
  action?: Record<string, number>;
  series?: { t: string; pv: number; err4: number; err5: number; providePv: number }[];
};

type TopItem = { key: string; count: number };

type LogRow = {
  ts: string;
  method: string;
  path: string;
  status: number;
  latencyMs: number;
  clientType: string;
  internal?: string;
  ipPreview: string;
  uaFamily: string;
  resource?: string;
};

const CLIENT_ORDER = ["web", "tvbox", "ohos", "android", "manage", "crawler", "unknown"];
const CLIENT_BAR_ORDER = ["web", "tvbox", "ohos", "android", "unknown"];
const ACTION_ORDER = ["browse", "provide", "play", "classify", "search", "other"];
const CLIENT_LABEL: Record<string, string> = {
  web: "Web",
  tvbox: "TVBox",
  ohos: "OHOS",
  android: "Android",
  manage: "后台",
  crawler: "爬虫",
  unknown: "未知",
};
const ACTION_LABEL: Record<string, string> = {
  browse: "browse",
  provide: "provide",
  play: "play",
  classify: "classify",
  search: "search",
  manage: "manage",
  other: "other",
};

function fmtNum(n?: number) {
  if (n === undefined || n === null || Number.isNaN(Number(n))) return "—";
  return Number(n).toLocaleString("zh-CN");
}

function statusTag(status: number) {
  if (status >= 500) return <Tag color="error">{status}</Tag>;
  if (status >= 400) return <Tag color="warning">{status}</Tag>;
  return <Tag color="success">{status}</Tag>;
}

function barRows(map: Record<string, number> | undefined, order: string[], labels: Record<string, string>) {
  const data = map || {};
  const total = order.reduce((s, k) => s + (data[k] || 0), 0) || 1;
  return order
    .filter((k) => (data[k] || 0) > 0 || k === "web" || k === "browse")
    .map((k) => ({
      key: k,
      label: labels[k] || k,
      count: data[k] || 0,
      pct: Math.round(((data[k] || 0) / total) * 100),
    }));
}

function disabledAccessDay(d: Dayjs) {
  return d.isAfter(dayjs(), "day") || d.isBefore(dayjs().subtract(13, "day"), "day");
}

export default function AccessPageView() {
  const [day, setDay] = useState<Dayjs>(dayjs());
  const [overview, setOverview] = useState<Overview | null>(null);
  const [pathTops, setPathTops] = useState<TopItem[]>([]);
  const [searchTops, setSearchTops] = useState<TopItem[]>([]);
  const [playTops, setPlayTops] = useState<TopItem[]>([]);
  const [logs, setLogs] = useState<LogRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [logLoading, setLogLoading] = useState(false);
  const [error, setError] = useState("");
  const [logError, setLogError] = useState("");
  const [source, setSource] = useState<string>("recent");
  const [status, setStatus] = useState<string>("");
  const [client, setClient] = useState<string>("");
  const [keyword, setKeyword] = useState("");
  const [appliedQ, setAppliedQ] = useState("");
  const [logPage, setLogPage] = useState({ current: 1, pageSize: 20 });

  const dayParam = day.format("YYYY-MM-DD");
  const isToday = day.isSame(dayjs(), "day");

  const loadOverview = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [ov, path, search, play] = await Promise.all([
        ApiGet<Overview>("/manage/access/overview", { day: dayParam }),
        ApiGet<{ items: TopItem[] }>("/manage/access/tops", { day: dayParam, kind: "path", limit: 10 }),
        ApiGet<{ items: TopItem[] }>("/manage/access/tops", { day: dayParam, kind: "search", limit: 10 }),
        ApiGet<{ items: TopItem[] }>("/manage/access/tops", { day: dayParam, kind: "play", limit: 10 }),
      ]);
      if (ov.code !== 0) {
        setError(ov.msg || "访问分析暂不可用");
        setOverview(null);
        return;
      }
      setOverview(ov.data);
      setPathTops(path.data?.items || []);
      setSearchTops(search.data?.items || []);
      setPlayTops(play.data?.items || []);
    } catch {
      setError("访问分析暂不可用");
      setOverview(null);
    } finally {
      setLoading(false);
    }
  }, [dayParam]);

  const loadLogs = useCallback(async () => {
    setLogPage((p) => ({ ...p, current: 1 }));
    setLogLoading(true);
    setLogError("");
    try {
      const resp = await ApiGet<{ list: LogRow[] }>("/manage/access/logs", {
        source,
        status: status || undefined,
        client: client || undefined,
        q: appliedQ.trim() || undefined,
      });
      if (resp.code === 0) {
        setLogs(resp.data?.list || []);
      } else {
        setLogError(resp.msg || "访问日志暂不可用");
        setLogs([]);
      }
    } catch {
      setLogError("访问日志暂不可用");
      setLogs([]);
    } finally {
      setLogLoading(false);
    }
  }, [source, status, client, appliedQ]);

  useEffect(() => {
    void loadOverview();
  }, [loadOverview]);

  useEffect(() => {
    void loadLogs();
  }, [loadLogs]);

  const clientRows = useMemo(
    () => barRows(overview?.client, CLIENT_BAR_ORDER, CLIENT_LABEL),
    [overview],
  );
  const actionRows = useMemo(
    () => barRows(overview?.action, ACTION_ORDER, ACTION_LABEL),
    [overview],
  );

  const mixTops = useMemo(() => {
    const search = (searchTops || []).slice(0, 5).map((i) => ({ ...i, kind: "搜" as const }));
    const play = (playTops || []).slice(0, 5).map((i) => ({
      key: i.key.startsWith("id ") ? i.key : `id ${i.key}`,
      count: i.count,
      kind: "播" as const,
    }));
    return [...search, ...play];
  }, [searchTops, playTops]);

  const columns: ColumnsType<LogRow> = [
    {
      title: "时间",
      dataIndex: "ts",
      width: 96,
      render: (v: string) => {
        const t = dayjs(v);
        return <span title={t.toISOString()}>{t.format("HH:mm:ss")}</span>;
      },
    },
    { title: "方法", dataIndex: "method", width: 72 },
    {
      title: "路径",
      dataIndex: "path",
      ellipsis: true,
      render: (v: string) => <span className={styles.path}>{v}</span>,
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 80,
      render: (v: number) => statusTag(v),
    },
    {
      title: "耗时",
      dataIndex: "latencyMs",
      width: 88,
      render: (v: number) => (
        <span className={v >= 500 ? styles.slow : undefined}>{v}ms</span>
      ),
    },
    { title: "客户端", dataIndex: "clientType", width: 88 },
    {
      title: "IP",
      dataIndex: "ipPreview",
      width: 120,
      render: (v: string) => <span className={v === "local" ? styles.muted : undefined}>{v}</span>,
    },
    { title: "UA", dataIndex: "uaFamily", width: 120 },
  ];

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="访问分析"
        description="站点请求画像"
        actions={
          <Space>
            <DatePicker
              value={day}
              allowClear={false}
              disabledDate={disabledAccessDay}
              onChange={(v) => v && setDay(v)}
            />
            <Button icon={<ReloadOutlined />} onClick={() => { void loadOverview(); void loadLogs(); }}>
              刷新
            </Button>
          </Space>
        }
      />

      {error ? <Alert type="error" showIcon title={error} /> : null}
      {!error && logError ? <Alert type="error" showIcon title={logError} /> : null}
      {overview && overview.dropped > 0 ? (
        <Alert
          type="warning"
          showIcon
          title={`有 ${overview.dropped} 条分析事件因队列满被丢弃，站点请求未受影响`}
        />
      ) : null}

      <Card
        className={styles.panelCard}
        title={isToday ? "今日站点" : `${dayParam} 站点`}
        extra={<span className={styles.hint}>PV 来自页面埋点，不含后台 / Provide</span>}
        loading={loading}
        styles={{ body: { padding: 16 } }}
      >
        <div className={styles.stats}>
          <div className={styles.stat}>
            <div className={styles.ico}><EyeOutlined /></div>
            <div>
              <div className={styles.val}>{fmtNum(overview?.pv)}</div>
              <div className={styles.lab}>{isToday ? "今日 PV" : "当日 PV"}</div>
              <div className={styles.sub}>页面浏览次数</div>
            </div>
          </div>
          <div className={styles.stat}>
            <div className={styles.ico}>UV</div>
            <div>
              <div className={styles.val}>{fmtNum(overview?.uv)}</div>
              <div className={styles.lab}>{isToday ? "今日 UV" : "当日 UV"}</div>
              <div className={styles.sub}>独立访客</div>
            </div>
          </div>
          <div className={styles.stat}>
            <div className={`${styles.ico} ${styles.icoErr}`}><WarningOutlined /></div>
            <div>
              <div className={styles.val}>{fmtNum((overview?.err4 || 0) + (overview?.err5 || 0))}</div>
              <div className={styles.lab}>错误数</div>
              <div className={styles.sub}>
                4xx {overview?.err4 ?? 0} · 5xx {overview?.err5 ?? 0}
              </div>
            </div>
          </div>
          <div className={styles.stat}>
            <div className={styles.ico}><ClockCircleOutlined /></div>
            <div>
              <div className={styles.val}>{overview ? overview.p95Ms : "—"}</div>
              <div className={styles.lab}>近似 P95</div>
              <div className={styles.sub}>站点延迟，不含 Provide</div>
            </div>
          </div>
        </div>

        <div className={styles.provideRow}>
          <span>TVBox / Provide</span>
          <span>
            请求 <b>{fmtNum(overview?.provide?.pv)}</b>
          </span>
          <span>
            错误 <b>{fmtNum((overview?.provide?.err4 || 0) + (overview?.provide?.err5 || 0))}</b>
          </span>
          <span className={styles.muted}>不计入上方四格与 P95</span>
        </div>

        <div className={styles.row2}>
          <div>
            <div className={styles.legend}>
              <span><i className={`${styles.dot} ${styles.dotSite}`} />站点</span>
              <span><i className={`${styles.dot} ${styles.dotProvide}`} />Provide</span>
            </div>
            <Sparkline series={overview?.series} />
            <div className={styles.muted}>当日折线 · 15 分钟一点</div>
          </div>
          <div>
            <div className={styles.lab}>客户端占比</div>
            <div className={styles.bars}>
              {clientRows.map((row) => (
                <div className={styles.barRow} key={row.key}>
                  <span>{row.label}</span>
                  <div className={styles.track}>
                    <div className={`${styles.fill} ${styles[`fill_${row.key}`] || ""}`} style={{ width: `${row.pct}%` }} />
                  </div>
                  <span>{row.pct}%</span>
                </div>
              ))}
            </div>
            <div className={styles.note}>Web / OHOS 来自页面埋点，TVBox 来自 Provide</div>
          </div>
        </div>
      </Card>

      <Card className={styles.panelCard} title="请求画像" loading={loading} styles={{ body: { padding: 16 } }}>
        <div className={styles.portrait}>
          <div>
            <div className={styles.lab}>行为分布</div>
            <div className={styles.bars}>
              {actionRows.map((row) => (
                <div className={styles.barRow} key={row.key}>
                  <span>{row.label}</span>
                  <div className={styles.track}>
                    <div className={styles.fill} style={{ width: `${Math.max(row.pct, row.count > 0 ? 2 : 0)}%` }} />
                  </div>
                  <span>{fmtNum(row.count)}</span>
                </div>
              ))}
            </div>
          </div>
          <div>
            <div className={styles.lab}>热接口 Top 10</div>
            {pathTops.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无接口数据" />
            ) : (
              <ol className={styles.top}>
                {pathTops.map((item) => (
                  <li key={item.key}>
                    <span className={styles.k}>{item.key}</span>
                    <span className={styles.n}>{fmtNum(item.count)}</span>
                  </li>
                ))}
              </ol>
            )}
          </div>
          <div>
            <div className={styles.lab}>热搜 / 热播</div>
            {mixTops.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无搜索 / 暂无播放记录" />
            ) : (
              <ol className={styles.top}>
                {mixTops.map((item) => (
                  <li key={`${item.kind}-${item.key}`}>
                    <span className={styles.k}>{item.key}</span>
                    <span className={styles.n}>
                      {item.kind} {fmtNum(item.count)}
                    </span>
                  </li>
                ))}
              </ol>
            )}
            <div className={styles.note}>
              热搜为原始请求次数，与前台热搜榜不同。热播是 Web/App 拉播放信息。
            </div>
          </div>
        </div>
      </Card>

      <Card
        className={styles.panelCard}
        title="访问日志"
        extra={<span className={styles.hint}>不含 Provide 的 2xx 轮询</span>}
        styles={{ body: { padding: 16 } }}
      >
        <div className={styles.toolbar}>
          <Segmented
            value={source}
            onChange={(v) => setSource(String(v))}
            options={[
              { label: "全部", value: "recent" },
              { label: "慢请求", value: "slow" },
              { label: "错误", value: "error" },
            ]}
          />
          <Select
            value={status || "all"}
            onChange={(v) => setStatus(v === "all" ? "" : v)}
            options={[
              { value: "all", label: "状态 全部" },
              { value: "2xx", label: "2xx" },
              { value: "4xx", label: "4xx" },
              { value: "5xx", label: "5xx" },
            ]}
            className={styles.filter}
          />
          <Select
            value={client || "all"}
            onChange={(v) => setClient(v === "all" ? "" : v)}
            options={[
              { value: "all", label: "客户端 全部" },
              ...CLIENT_ORDER.map((k) => ({ value: k, label: CLIENT_LABEL[k] })),
            ]}
            className={styles.filter}
          />
          <Input
            placeholder="路径关键词"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onPressEnter={() => setAppliedQ(keyword)}
            className={styles.keyword}
            allowClear
          />
          <Button type="primary" onClick={() => setAppliedQ(keyword)}>
            查询
          </Button>
        </div>
        <Table
          rowKey={(r, i) => `${r.ts}-${r.path}-${i}`}
          columns={columns}
          dataSource={logs}
          loading={logLoading}
          pagination={{
            current: logPage.current,
            pageSize: logPage.pageSize,
            total: logs.length,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50],
            showTotal: (total) =>
              `共 ${total} 条（${source === "recent" ? "最多保留 2000" : "最多保留 200"}）`,
            onChange: (current, pageSize) => setLogPage({ current, pageSize }),
          }}
          size="middle"
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="暂无访问记录。若站点刚启动或分析已关闭，属预期。"
              />
            ),
          }}
          scroll={{ x: 960 }}
        />
      </Card>
    </div>
  );
}
