import React, { useCallback, useEffect, useState } from "react";
import {
  Button,
  Drawer,
  Form,
  InputNumber,
  Modal,
  Radio,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  message,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  LinkOutlined,
  ReloadOutlined,
  SyncOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import type {
  CollectProgress,
  FilmSource,
  WebdavBindRequest,
  WebdavRescrapeRequest,
  WebdavScanItem,
  WebdavScanReport,
} from "./types";
import { isActiveCollectStatus } from "./types";
import styles from "./scan-report-drawer.module.less";

interface ScanReportDrawerProps {
  open: boolean;
  source: FilmSource | null;
  onClose: () => void;
  onRefreshSource?: () => void;
}

const statusTagMap: Record<string, { color: string; label: string }> = {
  scraped: { color: "green", label: "已刮削" },
  bound: { color: "blue", label: "已绑定" },
  unmatched: { color: "orange", label: "未匹配" },
  category_mismatch: { color: "volcano", label: "分类不符" },
  parse_failed: { color: "default", label: "解析失败" },
  missing: { color: "default", label: "已下线" },
  failed: { color: "red", label: "失败" },
};

function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

function formatReportDuration(report: WebdavScanReport): string {
  const start = Date.parse(report.startedAt);
  const end = Date.parse(report.finishedAt);
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) {
    return "-";
  }
  const ms = end - start;
  if (ms < 1000) {
    return `${ms}ms`;
  }
  return `${(ms / 1000).toFixed(1)}s`;
}

export default function ScanReportDrawer({
  open,
  source,
  onClose,
  onRefreshSource,
}: ScanReportDrawerProps) {
  const [loading, setLoading] = useState(false);
  const [report, setReport] = useState<WebdavScanReport | null>(null);
  const [liveProgress, setLiveProgress] = useState<CollectProgress | null>(null);
  const [items, setItems] = useState<WebdavScanItem[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);

  // 手动绑定弹窗状态
  const [bindModalOpen, setBindModalOpen] = useState(false);
  const [bindTargetItems, setBindTargetItems] = useState<WebdavScanItem[]>([]);
  const [bindForm] = Form.useForm();
  const [bindSubmitting, setBindSubmitting] = useState(false);

  // 重新刮削加载状态
  const [rescrapeLoading, setRescrapeLoading] = useState(false);

  const sourceId = source?.id;

  const fetchReportData = useCallback(
    async (currentPage = page, currentStatus = statusFilter, silent = false) => {
      if (!sourceId) return;
      if (!silent) setLoading(true);
      try {
        const res = await ApiGet("/manage/collect/webdav/report", {
          sourceId,
          page: currentPage,
          pageSize,
          status: currentStatus === "all" ? undefined : currentStatus,
        });
        if (res.code === 0 && res.data) {
          setReport(res.data.report || null);
          setLiveProgress(res.data.progress || null);
          setItems(res.data.items || []);
          setTotal(res.data.total || 0);
        } else if (!silent) {
          message.error(res.msg || "获取扫描报告失败");
        }
      } catch (err: any) {
        if (!silent) {
          message.error(err.message || "请求扫描报告异常");
        }
      } finally {
        if (!silent) setLoading(false);
      }
    },
    [sourceId, page, pageSize, statusFilter]
  );

  useEffect(() => {
    if (!open || !sourceId) {
      return;
    }
    setSelectedRowKeys([]);
    setPage(1);
    void fetchReportData(1, statusFilter, false);
  }, [open, sourceId, statusFilter, fetchReportData]);

  const scanning =
    isActiveCollectStatus(liveProgress?.status) ||
    isActiveCollectStatus(source?.progress?.status) ||
    report?.status === "running";

  useEffect(() => {
    if (!open || !sourceId || !scanning) {
      return;
    }
    const timer = window.setInterval(() => {
      void fetchReportData(page, statusFilter, true);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [open, sourceId, scanning, page, statusFilter, fetchReportData]);

  // 打开绑定 Modal
  const handleOpenBind = (targets: WebdavScanItem[]) => {
    setBindTargetItems(targets);
    bindForm.resetFields();
    bindForm.setFieldsValue({
      mediaType: source?.webdav?.mediaType || "movie",
      tmdbId: targets.find((t) => t.tmdbId > 0)?.tmdbId || undefined,
    });
    setBindModalOpen(true);
  };

  // 提交绑定
  const handleConfirmBind = async () => {
    try {
      const values = await bindForm.validateFields();
      if (!source?.id || bindTargetItems.length === 0) return;

      setBindSubmitting(true);
      const req: WebdavBindRequest = {
        sourceId: source.id,
        itemIds: bindTargetItems.map((it) => it.id),
        tmdbId: values.tmdbId,
        mediaType: values.mediaType,
      };

      const res = await ApiPost("/manage/collect/webdav/bind", req);
      if (res.code === 0) {
        message.success("绑定成功并已同步线路");
        setBindModalOpen(false);
        setSelectedRowKeys([]);
        fetchReportData();
        onRefreshSource?.();
      } else {
        message.error(res.msg || "绑定失败");
      }
    } catch (err: any) {
      if (err.errorFields) return;
      message.error(err.message || "绑定请求异常");
    } finally {
      setBindSubmitting(false);
    }
  };

  // 重新刮削
  const handleRescrape = async (targets?: WebdavScanItem[]) => {
    if (!source?.id) return;
    setRescrapeLoading(true);
    try {
      const req: WebdavRescrapeRequest = {
        sourceId: source.id,
        itemIds: targets ? targets.map((t) => t.id) : selectedRowKeys.map((k) => Number(k)),
      };
      const res = await ApiPost("/manage/collect/webdav/rescrape", req);
      if (res.code === 0) {
        message.success(`重新刮削完成，成功处理 ${res.data?.successCount ?? 0} 条`);
        setSelectedRowKeys([]);
        fetchReportData();
        onRefreshSource?.();
      } else {
        message.error(res.msg || "重新刮削失败");
      }
    } catch (err: any) {
      message.error(err.message || "重新刮削异常");
    } finally {
      setRescrapeLoading(false);
    }
  };

  const columns: ColumnsType<WebdavScanItem> = [
    {
      title: "相对路径",
      dataIndex: "relPath",
      key: "relPath",
      render: (val: string, record) => (
        <div className={styles.pathCell}>
          <div title={val}>{val}</div>
          <Space size={4}>
            <span style={{ color: "#8c8c8c" }}>{formatBytes(record.size)}</span>
            {record.lastModified && (
              <span style={{ color: "#bfbfbf" }}>• {record.lastModified.slice(0, 10)}</span>
            )}
          </Space>
        </div>
      ),
    },
    {
      title: "识别信息",
      key: "mediaInfo",
      width: 180,
      render: (_, record) => (
        <div>
          <div>
            <strong>{record.title || "未知"}</strong>
            {record.year > 0 && <span style={{ color: "#8c8c8c" }}> ({record.year})</span>}
          </div>
          {record.season > 0 && (
            <Tag color="cyan">
              S{record.season} E{record.episode}
            </Tag>
          )}
          {record.tmdbId > 0 && (
            <Tag color="geekblue" icon={<LinkOutlined />}>
              TMDB: {record.tmdbId}
            </Tag>
          )}
        </div>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      width: 130,
      render: (status: string, record) => {
        const conf = statusTagMap[status] || { color: "default", label: status };
        return (
          <Space direction="vertical" size={2}>
            <Tag color={conf.color}>{conf.label}</Tag>
            {record.lastError && (
              <Tooltip title={record.lastError}>
                <span className={styles.errorText}>
                  {record.lastError.length > 12
                    ? `${record.lastError.slice(0, 12)}...`
                    : record.lastError}
                </span>
              </Tooltip>
            )}
          </Space>
        );
      },
    },
    {
      title: "操作",
      key: "actions",
      width: 140,
      render: (_, record) => (
        <Space size={4}>
          <Button
            type="link"
            size="small"
            icon={<LinkOutlined />}
            onClick={() => handleOpenBind([record])}
          >
            绑定
          </Button>
          <Button
            type="link"
            size="small"
            icon={<ReloadOutlined />}
            onClick={() => handleRescrape([record])}
          >
            重试
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Drawer
        title={
          <Space>
            <span>{source?.name} - 扫描报告</span>
            {scanning && <Tag color="processing">扫描中</Tag>}
            {!scanning && report?.status === "done" && <Tag color="success">扫描完成</Tag>}
            {!scanning && report?.status === "failed" && <Tag color="error">扫描失败</Tag>}
            {report?.status === "storage_unmounted" && <Tag color="warning">已熔断</Tag>}
            {report?.truncated && <Tag color="warning">列举截断</Tag>}
          </Space>
        }
        width={820}
        open={open}
        onClose={onClose}
        extra={
          <Space>
            <Button
              icon={<SyncOutlined spin={loading} />}
              onClick={() => fetchReportData(page, statusFilter)}
            >
              刷新
            </Button>
          </Space>
        }
      >
        <div className={styles.drawerContent}>
          {scanning && !report?.errorSummary && (
            <div className={styles.liveHint}>
              {liveProgress?.phase === "listing"
                ? `正在扫描目录${liveProgress.found ? `，已发现 ${liveProgress.found} 个文件` : "…"}`
                : liveProgress?.phase === "parsing"
                  ? "正在解析文件名…"
                  : liveProgress?.phase === "matching"
                    ? "正在匹配 TMDB / 主站…"
                    : "扫描进行中，报告会自动刷新"}
            </div>
          )}
          {(report || scanning) && (
            <div className={styles.reportSummary}>
              <div className={styles.summaryItem}>
                <span className={styles.summaryLabel}>发现文件</span>
                <span className={styles.summaryValue}>
                  {Math.max(report?.found ?? 0, liveProgress?.found ?? 0)}
                </span>
              </div>
              <div className={styles.summaryItem}>
                <span className={styles.summaryLabel}>解析成功</span>
                <span className={styles.summaryValue}>{report?.parsed ?? liveProgress?.parsed ?? 0}</span>
              </div>
              <div className={styles.summaryItem}>
                <span className={styles.summaryLabel}>TMDB 命中</span>
                <span className={styles.summaryValue}>{report?.tmdbHit ?? liveProgress?.tmdbHit ?? 0}</span>
              </div>
              <div className={styles.summaryItem}>
                <span className={styles.summaryLabel}>未匹配</span>
                <span className={styles.summaryValue} style={{ color: "#fa8c16" }}>
                  {report?.unmatched ?? liveProgress?.unmatched ?? 0}
                </span>
              </div>
              <div className={styles.summaryItem}>
                <span className={styles.summaryLabel}>挂载线路</span>
                <span className={styles.summaryValue} style={{ color: "#52c41a" }}>
                  {report?.saved ?? liveProgress?.success ?? 0}
                </span>
              </div>
              <div className={styles.summaryItem}>
                <span className={styles.summaryLabel}>已下线</span>
                <span className={styles.summaryValue}>{report?.deleted ?? 0}</span>
              </div>
              <div className={styles.summaryItem}>
                <span className={styles.summaryLabel}>耗时</span>
                <span className={styles.summaryValue}>
                  {report ? formatReportDuration(report) : "-"}
                </span>
              </div>
            </div>
          )}
          {report?.truncated && (
            <div className={styles.errorText}>
              列举达到上限，未扫描到的文件不会下线。请拆分 RootPath 后重试。
            </div>
          )}
          {(report?.errorSummary || liveProgress?.error) && (
            <div className={styles.errorText}>
              {report?.errorSummary || liveProgress?.error}
            </div>
          )}

          <div className={styles.filterBar}>
            <Space>
              <Select
                value={statusFilter}
                onChange={(val) => {
                  setStatusFilter(val);
                  setPage(1);
                }}
                style={{ width: 140 }}
                options={[
                  { label: "全部状态", value: "all" },
                  { label: "未匹配", value: "unmatched" },
                  { label: "分类不符", value: "category_mismatch" },
                  { label: "解析失败", value: "parse_failed" },
                  { label: "已刮削", value: "scraped" },
                  { label: "已绑定", value: "bound" },
                  { label: "已下线", value: "missing" },
                ]}
              />
              <Button
                disabled={selectedRowKeys.length === 0}
                icon={<LinkOutlined />}
                onClick={() => {
                  const targets = items.filter((it) => selectedRowKeys.includes(it.id));
                  handleOpenBind(targets);
                }}
              >
                批量绑定 ({selectedRowKeys.length})
              </Button>
              <Button
                disabled={selectedRowKeys.length === 0}
                loading={rescrapeLoading}
                icon={<ReloadOutlined />}
                onClick={() => handleRescrape()}
              >
                批量重试 ({selectedRowKeys.length})
              </Button>
            </Space>
          </div>

          <div className={styles.tableWrapper}>
            <Table
              rowKey="id"
              loading={loading}
              columns={columns}
              dataSource={items}
              rowSelection={{
                selectedRowKeys,
                onChange: (keys) => setSelectedRowKeys(keys),
              }}
              pagination={{
                current: page,
                pageSize,
                total,
                showSizeChanger: true,
                showTotal: (totalCount) => `共 ${totalCount} 条`,
                onChange: (p, ps) => {
                  setPage(p);
                  setPageSize(ps);
                  fetchReportData(p, statusFilter);
                },
              }}
            />
          </div>
        </div>
      </Drawer>

      <Modal
        title={`手动绑定 TMDB (${bindTargetItems.length} 个文件)`}
        open={bindModalOpen}
        onCancel={() => setBindModalOpen(false)}
        onOk={handleConfirmBind}
        confirmLoading={bindSubmitting}
        destroyOnClose
      >
        <Form form={bindForm} layout="vertical">
          <Form.Item
            name="tmdbId"
            label="TMDB ID"
            rules={[{ required: true, message: "请输入有效的 TMDB ID" }]}
            extra="可前往 the-movie-database 网站查询对应影视的数字 ID"
          >
            <InputNumber
              style={{ width: "100%" }}
              placeholder="例如: 550 (搏击俱乐部)"
              min={1}
            />
          </Form.Item>
          <Form.Item
            name="mediaType"
            label="媒体类型"
            rules={[{ required: true, message: "请选择媒体类型" }]}
          >
            <Radio.Group>
              <Radio value="movie">电影 (Movie)</Radio>
              <Radio value="tv">电视剧 (TV)</Radio>
            </Radio.Group>
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
