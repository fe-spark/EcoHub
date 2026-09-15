import { Button, Checkbox, Popconfirm, Select, Tag, Tooltip } from "antd";
import {
  ApiOutlined,
  DeleteOutlined,
  EditOutlined,
  FileTextOutlined,
  PoweroffOutlined,
  StopOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import { useMemo } from "react";
import { collectDuration, type FilmSource } from "./types";
import { resolveSourceStatus, type StatusTone } from "./source-status";
import CollectProgressView from "./collect-progress";
import { useManagePermission } from "@/lib/manage-permission";
import styles from "./index.module.less";

const toneClassMap: Record<StatusTone, string> = {
  running: styles.toneRunning,
  enabled: styles.toneEnabled,
  disabled: styles.toneDisabled,
  stopping: styles.toneStopping,
  failed: styles.toneFailed,
};

const statusClassMap: Record<StatusTone, string> = {
  running: styles.statusRunning,
  enabled: styles.statusEnabled,
  disabled: styles.statusDisabled,
  stopping: styles.statusStopping,
  failed: styles.statusFailed,
};

interface CollectSourceCardProps {
  record: FilmSource;
  selected: boolean;
  /** 任务仍处于生命周期（starting/running/page_done/waiting_publish/finalizing） */
  active: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onChangeCollectDuration: (id: string, value: number) => void;
  onStartTask: (record: FilmSource) => void;
  onTerminateTask: (id: string) => void;
  onEditSource: (id: string) => void;
  onDeleteSource: (id: string) => void;
  onTestSource?: (record: FilmSource) => void;
  onOpenReport?: (id: string) => void;
}

/** 采集站/媒体库卡片（主站 / 附属站统一形态，主站用徽章区分） */
export default function CollectSourceCard({
  record,
  selected,
  active,
  onSelect,
  onChangeCollectDuration,
  onStartTask,
  onTerminateTask,
  onEditSource,
  onDeleteSource,
  onTestSource,
  onOpenReport,
}: CollectSourceCardProps) {
  const isRunning = active;
  const isWebdav = record.sourceType === "webdav";
  const isMaster = record.grade === 0;
  const { canWrite } = useManagePermission();
  const { label: statusLabel, tone: statusTone } = resolveSourceStatus(record, active);
  const phase = record.progress?.status;
  const canStop = phase === "starting" || phase === "running";

  const displayUri =
    isWebdav && record.webdav?.serverUrl
      ? `${record.webdav.serverUrl.replace(/\/+$/, "")}${record.webdav.rootPath || "/"}`
      : record.uri;

  const lastScanTime = record.scanSummary?.lastScan || record.lastCollectTime;

  const cardClassNames = [
    styles.sourceCard,
    isMaster ? styles.sourceCardMaster : "",
    toneClassMap[statusTone],
    selected ? styles.sourceCardSelected : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div
      className={cardClassNames}
      onClick={() => onSelect(record.id, !selected)}
    >
      {isMaster ? (
        <span className={styles.masterRibbon} title="全局唯一数据源" aria-label="主站">
          <span className={styles.masterRibbonText}>主站</span>
        </span>
      ) : null}
      <div className={styles.cardHead}>
        <Checkbox
          checked={selected}
          onClick={(event) => event.stopPropagation()}
          onChange={(event) => onSelect(record.id, event.target.checked)}
        />
        <div className={styles.cardHeadMain}>
          <div className={styles.cardTitleRow}>
            <span className={styles.cardName}>{record.name}</span>
            {isWebdav ? (
              <Tag color="blue" variant="filled" style={{ marginInlineEnd: 0 }}>
                WebDAV
              </Tag>
            ) : (
              <Tag variant="filled" style={{ marginInlineEnd: 0 }}>
                MacCMS
              </Tag>
            )}
            {isWebdav && record.webdav?.mediaType ? (
              <Tag variant="filled" style={{ marginInlineEnd: 0 }}>
                {record.webdav.mediaType === "tv" ? "剧集" : "电影"}
              </Tag>
            ) : null}
            {record.isPosterSource ? (
              <span className={styles.posterSourceTag} title="全局优先海报图源">
                海报源
              </span>
            ) : null}
          </div>
          <Tooltip title={displayUri}>
            {displayUri.startsWith("http") ? (
              <a
                href={displayUri}
                target="_blank"
                rel="noopener noreferrer"
                className={styles.cardUri}
                onClick={(event) => event.stopPropagation()}
              >
                {displayUri}
              </a>
            ) : (
              <span className={styles.cardUri} onClick={(event) => event.stopPropagation()}>
                {displayUri}
              </span>
            )}
          </Tooltip>
        </div>
        <span className={`${styles.statusPill} ${statusClassMap[statusTone]}`}>
          <span className={styles.statusDot} />
          {statusLabel}
        </span>
      </div>

      {/* 底部信息/操作沉底：进度改为环形固定占位，卡片高度恒定 */}
      <div className={styles.cardFoot}>
        <div className={styles.cardMeta} onClick={(event) => event.stopPropagation()}>
          {isWebdav ? (
            <>
              <div className={styles.metaItem}>
                <dt className={styles.metaLabel}>上次扫描：</dt>
                <dd
                  className={`${styles.metaValue}${
                    lastScanTime ? "" : ` ${styles.metaValueMuted}`
                  }`}
                >
                  {lastScanTime
                    ? dayjs(lastScanTime).format("YYYY-MM-DD HH:mm")
                    : "暂无"}
                </dd>
              </div>
              <div className={styles.metaItem}>
                <dt className={styles.metaLabel}>扫描摘要：</dt>
                <dd className={styles.metaValue}>
                  {record.scanSummary?.status === "failed" ||
                  record.scanSummary?.status === "storage_unmounted"
                    ? record.scanSummary.errorSummary || "扫描失败"
                    : record.scanSummary
                      ? `${record.scanSummary.found} 文件 / ${record.scanSummary.tmdbHit} 刮削 / ${record.scanSummary.skipped} 跳过`
                      : "尚未扫描"}
                </dd>
              </div>
              <div className={styles.metaItem}>
                <dt className={styles.metaLabel}>线路名称：</dt>
                <dd className={styles.metaValue} title={record.webdav?.playFromName || record.name}>
                  {record.webdav?.playFromName || record.name}
                </dd>
              </div>
            </>
          ) : (
            <>
              <div className={styles.metaItem}>
                <dt className={styles.metaLabel}>上次采集：</dt>
                <dd
                  className={`${styles.metaValue}${
                    record.lastCollectTime ? "" : ` ${styles.metaValueMuted}`
                  }`}
                >
                  {record.lastCollectTime
                    ? dayjs(record.lastCollectTime).format("YYYY-MM-DD HH:mm")
                    : "暂无"}
                </dd>
              </div>
              <div className={styles.metaItem}>
                <dt className={styles.metaLabel}>请求间隔：</dt>
                <dd className={styles.metaValue}>
                  {record.interval > 0 ? `${record.interval} ms` : "无限制"}
                </dd>
              </div>
              <div className={styles.metaItem}>
                <dt className={styles.metaLabel}>采集时长：</dt>
                <dd className={styles.metaValue}>
                  <Select
                    size="small"
                    value={record.cd}
                    disabled={!canWrite || isRunning || !record.state}
                    style={{ width: "100%" }}
                    options={collectDuration.map((item) => ({
                      value: item.time,
                      label: item.label,
                    }))}
                    onChange={(value) => {
                      onChangeCollectDuration(record.id, value);
                    }}
                  />
                </dd>
              </div>
            </>
          )}
        </div>

        <div className={styles.cardActions} onClick={(event) => event.stopPropagation()}>
          <div className={styles.actionGroup}>
            {isRunning ? (
              canStop ? (
                <Popconfirm
                  title={isWebdav ? "停止当前扫描任务？" : "停止当前采集任务？"}
                  description={
                    isWebdav
                      ? "仅停止扫描任务，媒体库保持启用；已发现的媒体会继续处理完成。"
                      : "仅停止采集任务，采集站保持启用；已抓取数据会继续处理完成。"
                  }
                  onConfirm={() => onTerminateTask(record.id)}
                  disabled={!record.state}
                  okText={isWebdav ? "停止扫描" : "停止采集"}
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                >
                  <Button
                    danger
                    icon={<StopOutlined />}
                    disabled={!canWrite || !record.state}
                  >
                    {record.state ? (isWebdav ? "停止扫描" : "停止采集") : "已停止"}
                  </Button>
                </Popconfirm>
              ) : (
                <Tooltip
                  title={
                    phase === "finalizing"
                      ? "正在收尾发布，无法停止"
                      : phase === "waiting_publish" || phase === "page_done"
                        ? "等待收尾中，无法停止"
                        : "数据抓取已完成，正在等待落库"
                  }
                >
                  <span>
                    <Button danger icon={<StopOutlined />} disabled>
                      {isWebdav ? "停止扫描" : "停止采集"}
                    </Button>
                  </span>
                </Tooltip>
              )
            ) : (
              <Tooltip
                title={
                  !record.state
                    ? isWebdav
                      ? "该媒体库已被禁用，无法发起扫描"
                      : "该采集站已被禁用，无法发起采集"
                    : undefined
                }
              >
                <span>
                  <Button
                    type="primary"
                    icon={<PoweroffOutlined />}
                    onClick={() => onStartTask(record)}
                    disabled={!canWrite || !record.state}
                  >
                    {isWebdav ? "扫描" : "开始采集"}
                  </Button>
                </span>
              </Tooltip>
            )}
            {isWebdav && onTestSource ? (
              <Tooltip title="测试连通">
                <Button
                  icon={<ApiOutlined />}
                  disabled={!canWrite}
                  onClick={() => onTestSource(record)}
                />
              </Tooltip>
            ) : null}
            {isWebdav && onOpenReport ? (
              <Tooltip title="扫描报告">
                <Button
                  icon={<FileTextOutlined />}
                  onClick={() => onOpenReport(record.id)}
                />
              </Tooltip>
            ) : null}
            <Tooltip
              title={
                isRunning
                  ? isWebdav
                    ? "扫描进行中，禁止编辑"
                    : "采集进行中，禁止编辑"
                  : isWebdav
                    ? "编辑媒体库"
                    : "编辑采集站"
              }
            >
              <Button
                icon={<EditOutlined />}
                disabled={!canWrite || isRunning}
                onClick={() => onEditSource(record.id)}
              />
            </Tooltip>
            {isMaster ? (
              <Tooltip title="主站不可直接删除，请先改为附属站">
                <Button danger icon={<DeleteOutlined />} disabled />
              </Tooltip>
            ) : isRunning ? (
              <Tooltip
                title={
                  isWebdav ? "扫描进行中，禁止删除" : "采集进行中，禁止删除"
                }
              >
                <Button danger icon={<DeleteOutlined />} disabled />
              </Tooltip>
            ) : (
              <Popconfirm
                title={
                  isWebdav
                    ? "确认删除此媒体库？将级联删除该媒体库的扫描记录和线路数据。"
                    : "确认删除此采集站？"
                }
                onConfirm={() => onDeleteSource(record.id)}
              >
                <Button danger icon={<DeleteOutlined />} disabled={!canWrite} />
              </Popconfirm>
            )}
          </div>

          <div className={styles.ringGroup}>
            <CollectProgressView progress={record.progress} variant="ring" />
          </div>
        </div>
      </div>
    </div>
  );
}
