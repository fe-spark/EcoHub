import { Button, Checkbox, Popconfirm, Select, Tooltip } from "antd";
import { DeleteOutlined, EditOutlined, PoweroffOutlined, StopOutlined } from "@ant-design/icons";
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
};

const statusClassMap: Record<StatusTone, string> = {
  running: styles.statusRunning,
  enabled: styles.statusEnabled,
  disabled: styles.statusDisabled,
  stopping: styles.statusStopping,
};

interface CollectSourceCardProps {
  record: FilmSource;
  selected: boolean;
  /** 任务仍处于采集生命周期（starting/running/page_done/waiting_publish/finalizing） */
  active: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onChangeCollectDuration: (id: string, value: number) => void;
  onStartTask: (record: FilmSource) => void;
  onTerminateTask: (id: string) => void;
  onEditSource: (id: string) => void;
  onDeleteSource: (id: string) => void;
}

/** 采集站卡片（主站 / 附属站统一形态，主站用徽章区分） */
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
}: CollectSourceCardProps) {
  const isRunning = active;
  const isMaster = record.grade === 0;
  const { canWrite } = useManagePermission();
  const { label: statusLabel, tone: statusTone } = resolveSourceStatus(record, active);

  const cardClassNames = [
    styles.sourceCard,
    isMaster ? styles.sourceCardMaster : "",
    toneClassMap[statusTone],
    selected ? styles.sourceCardSelected : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={cardClassNames} onClick={() => onSelect(record.id, !selected)}>
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
        <span className={`${styles.statusPill} ${statusClassMap[statusTone]}`}>
          <span className={styles.statusDot} />
          {statusLabel}
        </span>
      </div>

      {/* 底部信息/操作沉底：进度改为环形固定占位，卡片高度恒定 */}
      <div className={styles.cardFoot}>
        <div className={styles.cardMeta} onClick={(event) => event.stopPropagation()}>
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
                options={collectDuration.map((item) => ({ value: item.time, label: item.label }))}
                onChange={(value) => {
                  onChangeCollectDuration(record.id, value);
                }}
              />
            </dd>
          </div>
        </div>

        <div className={styles.cardActions} onClick={(event) => event.stopPropagation()}>
          <div className={styles.actionGroup}>
            {isRunning ? (
            <Popconfirm
              title="停止当前采集任务？"
              description="仅停止采集任务，采集站保持启用；已请求数据会继续入库。"
              onConfirm={() => onTerminateTask(record.id)}
                disabled={!record.state}
                okText="停止采集"
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
              <Tooltip title={!record.state ? "该采集站已被禁用，无法发起采集" : undefined}>
                <span>
                  <Button
                    type="primary"
                    icon={<PoweroffOutlined />}
                    onClick={() => onStartTask(record)}
                    disabled={!canWrite || !record.state}
                  >
                    开始采集
                  </Button>
                </span>
              </Tooltip>
            )}
            <Tooltip title="编辑采集站">
              <Button
                icon={<EditOutlined />}
                disabled={!canWrite}
                onClick={() => onEditSource(record.id)}
              />
            </Tooltip>
            {isMaster ? (
              <Tooltip title="主站不可直接删除，请先改为附属站">
                <Button danger icon={<DeleteOutlined />} disabled />
              </Tooltip>
            ) : (
              <Popconfirm title="确认删除此采集站？" onConfirm={() => onDeleteSource(record.id)}>
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
