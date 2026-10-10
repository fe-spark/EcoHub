import { Button, Checkbox, Popconfirm, Select, Tag, Tooltip } from "antd";
import {
  DeleteOutlined,
  DisconnectOutlined,
  EditOutlined,
  GlobalOutlined,
  PoweroffOutlined,
  StopOutlined,
} from "@ant-design/icons";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import dayjs from "dayjs";
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
  /** 任务仍处于采集生命周期（starting/running/page_done/waiting_publish/finalizing） */
  active: boolean;
  globalSpiderProxyReady?: boolean;
  headerDragProps?: React.HTMLAttributes<HTMLDivElement>;
  onSelect: (id: string, checked: boolean) => void;
  onChangeCollectDuration: (id: string, value: number) => void;
  onStartTask: (record: FilmSource) => void;
  onTerminateTask: (id: string) => void;
  onEditSource: (id: string) => void;
  onDeleteSource: (id: string) => void;
}

/** 采集站卡片（仅顶部 Header 可拖拽，首位启用站点自动展现首选站徽章） */
export default function CollectSourceCard({
  record,
  selected,
  active,
  globalSpiderProxyReady = false,
  headerDragProps,
  onSelect,
  onChangeCollectDuration,
  onStartTask,
  onTerminateTask,
  onEditSource,
  onDeleteSource,
}: CollectSourceCardProps) {
  const isRunning = active;
  const { canWrite } = useManagePermission();
  const { label: statusLabel, tone: statusTone } = resolveSourceStatus(record, active);
  const phase = record.progress?.status;
  const canStop = phase === "starting" || phase === "running";

  const hasProxy = Boolean(record.proxyCollect);

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
        <span
          onPointerDown={(event) => event.stopPropagation()}
          style={{ display: "inline-flex", lineHeight: 1 }}
        >
          <Checkbox
            checked={selected}
            onChange={(event) => onSelect(record.id, event.target.checked)}
          />
        </span>
        <div className={styles.cardHeadMain}>
          <div className={styles.cardTitleRow}>
            <span className={styles.cardName}>{record.name}</span>
            {record.isPrimary ? (
              <Tag color="gold" bordered={false} style={{ marginInlineEnd: 0 }}>
                首选站
              </Tag>
            ) : null}
            {record.isPosterSource ? (
              <span className={styles.posterSourceTag} title="全局优先海报图源">
                海报源
              </span>
            ) : null}
            {record.format === "xml" ? (
              <Tag color="purple" bordered={false} style={{ marginInlineEnd: 0 }}>
                XML
              </Tag>
            ) : null}
            {record.categoryReady === false ? (
              <Tag color="orange" bordered={false} style={{ marginInlineEnd: 0 }}>
                缺分类
              </Tag>
            ) : null}
          </div>
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
        <span
          className={`${styles.statusPill} ${statusClassMap[statusTone]}`}
          onPointerDown={(event) => event.stopPropagation()}
        >
          <span className={styles.statusDot} />
          {statusLabel}
        </span>
      </div>

      {/* 底部信息/操作沉底：进度改为环形固定占位，卡片高度恒定 */}
      <div className={styles.cardFoot}>

        <div
          className={styles.cardMeta}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => event.stopPropagation()}
        >
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
            <dt className={styles.metaLabel}>网络代理：</dt>
            <dd className={styles.metaValue}>
              {hasProxy ? (
                <Tooltip
                  title={
                    globalSpiderProxyReady
                      ? "采集接口走系统网络代理"
                      : "代理未开启（系统网络代理或爬虫模块未启用）"
                  }
                >
                  <Tag
                    icon={globalSpiderProxyReady ? <GlobalOutlined /> : <DisconnectOutlined />}
                    color={globalSpiderProxyReady ? "cyan" : "default"}
                    bordered={false}
                    style={{ marginInlineEnd: 0 }}
                  >
                    代理
                  </Tag>
                </Tooltip>
              ) : (
                <span className={styles.metaValueMuted}>直连</span>
              )}
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
                options={collectDuration.map((item) => ({ value: item.time, label: item.label }))}
                onChange={(value) => {
                  onChangeCollectDuration(record.id, value);
                }}
              />
            </dd>
          </div>
        </div>

        <div
          className={styles.cardActions}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => event.stopPropagation()}
        >
          <div className={styles.actionGroup}>
            {isRunning ? (
              canStop ? (
                <Popconfirm
                  title="停止采集？"
                  onConfirm={() => onTerminateTask(record.id)}
                  disabled={!record.state}
                  okText="停止"
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                >
                  <Button
                    danger
                    icon={<StopOutlined />}
                    disabled={!canWrite || !record.state}
                  >
                    {record.state ? "停止采集" : "已停止"}
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
                      停止采集
                    </Button>
                  </span>
                </Tooltip>
              )
            ) : (
              <Tooltip
                title={
                  record.categoryReady === false
                    ? "该采集站还没有分类，请重新保存后再采集"
                    : !record.state
                      ? "该采集站已被禁用，无法发起采集"
                      : undefined
                }
              >
                <span>
                  <Button
                    type="primary"
                    icon={<PoweroffOutlined />}
                    onClick={() => onStartTask(record)}
                    disabled={!canWrite || !record.state || record.categoryReady === false}
                  >
                    开始采集
                  </Button>
                </span>
              </Tooltip>
            )}
            <Tooltip title={isRunning ? "采集进行中，禁止编辑" : "编辑采集站"}>
              <Button
                icon={<EditOutlined />}
                disabled={!canWrite || isRunning}
                onClick={() => onEditSource(record.id)}
              />
            </Tooltip>
            {isRunning ? (
              <Tooltip title="采集进行中，禁止删除">
                <Button danger icon={<DeleteOutlined />} disabled />
              </Tooltip>
            ) : (
              <Popconfirm title="删除此采集站？" onConfirm={() => onDeleteSource(record.id)}>
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
      className={isDragging ? styles.dragSourcePlaceholder : undefined}
    >
      <CollectSourceCard
        {...props}
        headerDragProps={{ ...attributes, ...listeners }}
      />
    </div>
  );
}
