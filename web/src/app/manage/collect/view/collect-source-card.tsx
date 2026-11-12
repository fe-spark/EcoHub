import React from "react";
import { Button, Checkbox, Popconfirm, Switch, Tag, Tooltip } from "antd";
import {
  DeleteOutlined, EditOutlined,
  ExclamationCircleOutlined, LoadingOutlined, PoweroffOutlined,
  ReloadOutlined, StopOutlined, SyncOutlined,
} from "@ant-design/icons";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { FilmSource, SourceIncident } from "./types";
import { collectIndexUpdatingReason } from "./collect-selection";
import { resolveSourceStatus, type StatusTone } from "./source-status";
import CollectProgressView from "./collect-progress";
import CollectSourceCardMeta from "./collect-source-card-meta";
import { useManagePermission } from "@/lib/manage-permission";
import styles from "./index.module.less";

const toneClassMap: Record<StatusTone, string> = {
  running: styles.toneRunning, runningFailed: styles.toneRunningFailed,
  sealing: styles.toneSealing, stopped: styles.toneStopped,
  failed: styles.toneFailed, enabled: styles.toneEnabled, disabled: styles.toneDisabled,
  deleting: styles.toneDeleting,
};

const statusClassMap: Record<StatusTone, string> = {
  running: styles.statusRunning, runningFailed: styles.statusRunningFailed,
  sealing: styles.statusSealing, stopped: styles.statusStopped,
  failed: styles.statusFailed, enabled: styles.statusEnabled, disabled: styles.statusDisabled,
  deleting: styles.statusDeleting,
};

interface CollectSourceCardProps {
  record: FilmSource;
  selected: boolean;
  /** 任务仍处于采集生命周期（starting/running） */
  active: boolean;
  /** 检索索引正在重建，全部采集暂停 */
  indexing?: boolean;
  globalSpiderProxyReady?: boolean;
  headerDragProps?: React.HTMLAttributes<HTMLDivElement>;
  onSelect: (id: string, checked: boolean) => void;
  onChangeCollectDuration: (id: string, value: number) => void;
  onChangeSourceState?: (id: string, state: boolean) => void;
  onStartTask: (record: FilmSource) => void;
  onTerminateTask: (id: string) => void;
  onEditSource: (id: string) => void;
  onDeleteSource: (id: string) => void;
  deleting?: boolean;
  toggling?: boolean;
  onSyncCategory?: (id: string) => void;
  onUpdateSite?: (id: string, updater: (record: FilmSource) => FilmSource) => void;
  incident?: SourceIncident;
}

/** 采集站卡片（仅顶部 Header 可拖拽，首位启用站点自动展现主站徽章） */
export default function CollectSourceCard({
  record,
  selected,
  active,
  indexing = false,
  globalSpiderProxyReady = false,
  headerDragProps,
  onSelect,
  onChangeCollectDuration,
  onChangeSourceState,
  onStartTask,
  onTerminateTask,
  onEditSource,
  onDeleteSource,
  deleting = false,
  toggling = false,
  onSyncCategory,
  onUpdateSite,
  incident,
}: CollectSourceCardProps) {
  const isDeleting = deleting || Boolean(record.deleting);
  const phase = record.progress?.status;
  const isStopping = phase === "stopping";
  const isRunning = active;
  const sealing = phase === "sealing";
  // 抓页、停止收尾、待检索都算这次采集还没结束。
  const busy = isRunning || isStopping || sealing;
  const { canWrite } = useManagePermission();
  const resolved = resolveSourceStatus(record, active || isStopping, isDeleting);
  const statusLabel = resolved.label;
  const statusTone = resolved.tone;
  const canStop = phase === "starting" || phase === "running";
  const startDisabled =
    !canWrite ||
    !record.state ||
    isDeleting ||
    busy ||
    indexing ||
    sealing ||
    Boolean(record.categorySyncing) ||
    record.categoryReady === false;
  const startTitle = indexing
    ? collectIndexUpdatingReason
    : sealing
      ? "待检索"
      : !record.state
        ? "已禁用"
        : record.categorySyncing
          ? "分类采集中"
          : record.categoryReady === false
            ? "缺分类"
            : undefined;
  const categoryTitle = record.categorySyncing
    ? "分类采集中"
    : record.categoryReady === false
      ? "获取分类"
      : "重新获取分类";
  const editTitle = sealing
    ? "待检索"
    : busy
      ? "采集进行中，禁止编辑"
      : "编辑采集站";

  const hasProxy = Boolean(record.proxyCollect);
  const displaySuccessPages =
    record.progress && record.progress.success > 0
      ? record.progress.success
      : record.lastRunPageCount;

  const cardClassNames = [
    styles.sourceCard,
    toneClassMap[statusTone],
    record.isPrimary ? styles.sourceCardMaster : "",
    selected ? styles.sourceCardSelected : "",
  ]
    .filter(Boolean)
    .join(" ");


  return (
    <div className={cardClassNames}>
      <div className={styles.cardHead} {...headerDragProps}>
        <div className={styles.cardHeadTop}>
        <span
          onPointerDown={(event) => event.stopPropagation()}
          style={{ display: "inline-flex", lineHeight: 1 }}
        >
          <Checkbox
            checked={selected}
            disabled={isDeleting || Boolean(record.categorySyncing)}
            onChange={(event) => onSelect(record.id, event.target.checked)}
          />
        </span>
        <div className={styles.cardHeadMain}>
          <div className={styles.cardTitleRow}>
            <span className={styles.cardName}>{record.name}</span>
            {incident ? (
              <Tooltip
                title={
                  <div>
                    <div>{incident.tripped ? "站点已熔断" : `采集异常（连续失败 ${incident.consecutiveFailures} 次）`}</div>
                    {incident.latestError ? (
                      <div style={{ fontSize: 12, marginTop: 4, wordBreak: "break-all" }}>
                        {incident.latestError}
                      </div>
                    ) : null}
                  </div>
                }
              >
                <Tag
                  icon={<ExclamationCircleOutlined />}
                  color={incident.tripped ? "error" : "warning"}
                  variant="filled"
                  style={{ marginInlineEnd: 0, cursor: "pointer" }}
                >
                  {incident.tripped ? "已熔断" : "异常"}
                </Tag>
              </Tooltip>
            ) : null}
            {record.isPrimary ? (
              <Tag color="gold" variant="filled" style={{ marginInlineEnd: 0 }}>
                主站
              </Tag>
            ) : null}
            {record.isPosterSource ? (
              <span className={styles.posterSourceTag} title="全局优先海报图源">
                海报源
              </span>
            ) : null}
            {record.format === "xml" ? (
              <Tag color="purple" variant="filled" style={{ marginInlineEnd: 0 }}>
                XML
              </Tag>
            ) : null}
            {record.categorySyncing ? (
              <Tag
                icon={<LoadingOutlined spin />}
                color="processing"
                variant="filled"
                style={{ marginInlineEnd: 0 }}
              >
                分类采集中
              </Tag>
            ) : record.categoryReady === false ? (
              <Tag color="warning" variant="filled" style={{ marginInlineEnd: 0 }}>
                缺分类
              </Tag>
            ) : null}
          </div>
        </div>
        <div
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => event.stopPropagation()}
          style={{ display: "flex", alignItems: "center", gap: 8, flexShrink: 0 }}
        >
          {(isRunning || isStopping || isDeleting || phase === "failed" || phase === "stopping" || phase === "stopped" || sealing) && (
            <span className={`${styles.statusPill} ${statusClassMap[statusTone]}`}>
              <span className={styles.statusDot} />
              {statusLabel}
            </span>
          )}
          <Tooltip title={!canWrite ? "无操作权限" : busy ? "任务运行中不可切换" : record.state ? "点击禁用采集站" : "点击启用采集站"}>
            <Switch
              checked={Boolean(record.state)}
              disabled={!canWrite || isDeleting || busy || toggling}
              loading={toggling}
              checkedChildren="启用"
              unCheckedChildren="禁用"
              onChange={(checked) => {
                onChangeSourceState?.(record.id, checked);
              }}
            />
          </Tooltip>
        </div>
        </div>
        <div className={styles.cardHeadBody}>
          <Tooltip title={record.uri}>
            <a
              href={record.uri}
              target="_blank"
              rel="noopener noreferrer"
              className={styles.cardUri}
              onClick={(event) => event.stopPropagation()}
            >
              {record.uri}
            </a>
          </Tooltip>
        </div>
      </div>

      {/* 底部信息/操作沉底：进度改为环形固定占位，卡片高度恒定 */}
      <div className={styles.cardFoot}>
        <CollectSourceCardMeta
          record={record}
          canWrite={canWrite}
          isDeleting={isDeleting}
          busy={busy}
          deleting={deleting}
          hasProxy={hasProxy}
          globalSpiderProxyReady={globalSpiderProxyReady}
          displaySuccessPages={displaySuccessPages}
          onChangeCollectDuration={onChangeCollectDuration}
          onUpdateSite={onUpdateSite}
        />

        <div
          className={styles.cardActions}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => event.stopPropagation()}
        >
          <div className={styles.actionGroup}>
            {isDeleting ? (
              <Button danger icon={<LoadingOutlined spin />} disabled>
                删除中
              </Button>
            ) : isStopping || sealing ? (
              <Tooltip title={sealing ? "待检索" : undefined}>
                <span>
                  <Button danger icon={<StopOutlined />} loading={isStopping} disabled>
                    停止采集
                  </Button>
                </span>
              </Tooltip>
            ) : isRunning ? (
              canStop ? (
                <Popconfirm
                  title="停止采集？"
                  onConfirm={() => onTerminateTask(record.id)}
                  disabled={!record.state || isDeleting}
                  okText="停止"
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                >
                  <Button
                    danger
                    icon={<StopOutlined />}
                    disabled={!canWrite || !record.state || isDeleting}
                  >
                    {record.state ? "停止采集" : "已停止"}
                  </Button>
                </Popconfirm>
              ) : (
                <Tooltip title="数据抓取已完成，正在保存影片更新">
                  <span>
                    <Button danger icon={<StopOutlined />} disabled>
                      停止采集
                    </Button>
                  </span>
                </Tooltip>
              )
            ) : (
              <Tooltip title={startTitle}>
                <span>
                  <Button
                    type="primary"
                    icon={<PoweroffOutlined />}
                    onClick={() => onStartTask(record)}
                    disabled={startDisabled}
                  >
                    开始采集
                  </Button>
                </span>
              </Tooltip>
            )}
            <Tooltip title={categoryTitle}>
              <span>
                <Button
                  icon={<SyncOutlined />}
                  loading={Boolean(record.categorySyncing)}
                  disabled={!canWrite || busy || isDeleting || Boolean(record.categorySyncing)}
                  onClick={() => onSyncCategory?.(record.id)}
                />
              </span>
            </Tooltip>
            <Tooltip title={editTitle}>
              <span>
                <Button
                  icon={<EditOutlined />}
                  disabled={!canWrite || busy || isDeleting || Boolean(record.categorySyncing)}
                  onClick={() => onEditSource(record.id)}
                />
              </span>
            </Tooltip>
            <Popconfirm
              title="确认删除该采集站？"
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true, loading: isDeleting }}
              disabled={!canWrite || isDeleting || busy}
              onConfirm={() => onDeleteSource(record.id)}
            >
              <Tooltip title={busy ? "采集中" : undefined}>
                <span>
                  <Button
                    danger
                    icon={<DeleteOutlined />}
                    loading={isDeleting}
                    disabled={!canWrite || isDeleting || busy}
                  />
                </span>
              </Tooltip>
            </Popconfirm>
          </div>

          <div className={styles.ringGroup}>
            <CollectProgressView progress={record.progress} variant="ring" />
          </div>
        </div>
      </div>
    </div>
  );
}

export function SortableCollectSourceCard(props: CollectSourceCardProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: props.record.id });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    zIndex: isDragging ? 1 : undefined,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      id={`source-card-${props.record.id}`}
      className={isDragging ? styles.dragSourcePlaceholder : undefined}
    >
      <CollectSourceCard
        {...props}
        headerDragProps={{ ...attributes, ...listeners }}
      />
    </div>
  );
}
