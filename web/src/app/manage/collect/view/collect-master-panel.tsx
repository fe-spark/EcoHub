import { Button, Checkbox, Popconfirm, Select, Tooltip } from "antd";
import { DeleteOutlined, EditOutlined, PoweroffOutlined, StopOutlined } from "@ant-design/icons";
import dayjs from "dayjs";
import { collectDuration, type FilmSource } from "./types";
import { resolveSourceStatus, type StatusTone } from "./source-status";
import CollectProgressView from "./collect-progress";
import styles from "./index.module.less";

const statusClassMap: Record<StatusTone, string> = {
  running: styles.statusRunning,
  enabled: styles.statusEnabled,
  disabled: styles.statusDisabled,
  stopping: styles.statusStopping,
};

const toneClassMap: Record<StatusTone, string> = {
  running: styles.masterToneRunning,
  enabled: styles.masterToneEnabled,
  disabled: styles.masterToneDisabled,
  stopping: styles.masterToneStopping,
};

interface CollectMasterPanelProps {
  record: FilmSource;
  selected: boolean;
  active: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onChangeCollectDuration: (id: string, value: number) => void;
  onStartTask: (record: FilmSource) => void;
  onTerminateTask: (id: string) => void;
  onEditSource: (id: string) => void;
  onDeleteSource: (id: string) => void;
}

/** 主采集站唯一：横向操作条，形态与附属采集站卡片区分 */
export default function CollectMasterPanel({
  record,
  selected,
  active,
  onSelect,
  onChangeCollectDuration,
  onStartTask,
  onTerminateTask,
  onEditSource,
  onDeleteSource,
}: CollectMasterPanelProps) {
  const isRunning = active;
  const { label: statusLabel, tone: statusTone } = resolveSourceStatus(record, active);

  const panelClass = [
    styles.masterPanel,
    toneClassMap[statusTone],
    selected ? styles.masterPanelSelected : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={panelClass} onClick={() => onSelect(record.id, !selected)}>
      <div className={styles.masterPanelMain}>
        <Checkbox
          checked={selected}
          onClick={(event) => event.stopPropagation()}
          onChange={(event) => onSelect(record.id, event.target.checked)}
        />

        <div className={styles.masterPanelIdentity}>
          <div className={styles.masterPanelTitleRow}>
            <span className={styles.masterPanelName}>{record.name}</span>
            <span className={`${styles.statusPill} ${statusClassMap[statusTone]}`}>
              <span className={styles.statusDot} />
              {statusLabel}
            </span>
          </div>
          <Tooltip title={record.uri}>
            <a
              href={record.uri}
              target="_blank"
              rel="noopener noreferrer"
              className={styles.masterPanelUri}
              onClick={(event) => event.stopPropagation()}
            >
              {record.uri}
            </a>
          </Tooltip>
          <div className={styles.masterPanelMeta}>
            <span>
              上次采集{" "}
              {record.lastCollectTime
                ? dayjs(record.lastCollectTime).format("YYYY-MM-DD HH:mm")
                : "暂无"}
            </span>
            <span className={styles.masterPanelMetaSep}>·</span>
            <span>间隔 {record.interval > 0 ? `${record.interval} ms` : "无限制"}</span>
            {record.syncPictures ? (
              <>
                <span className={styles.masterPanelMetaSep}>·</span>
                <span>图片同步</span>
              </>
            ) : null}
          </div>
          {/* 有进度才展示，贴在信息区下方，避免中间空挂「暂无记录」 */}
          {record.progress ? (
            <div
              className={styles.masterPanelProgress}
              onClick={(event) => event.stopPropagation()}
            >
              <CollectProgressView progress={record.progress} compact />
            </div>
          ) : null}
        </div>
      </div>

      <div className={styles.masterPanelOps} onClick={(event) => event.stopPropagation()}>
        <Select
          size="small"
          value={record.cd}
          disabled={isRunning}
          className={styles.masterDuration}
          popupMatchSelectWidth={false}
          options={collectDuration.map((item) => ({ value: item.time, label: item.label }))}
          onChange={(value) => onChangeCollectDuration(record.id, value)}
        />
        {isRunning ? (
          <Popconfirm
            title="停止该采集站后续请求？"
            description="将禁用该采集站；已请求数据会继续入库。"
            onConfirm={() => onTerminateTask(record.id)}
            disabled={!record.state}
            okText="停止请求"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Button danger icon={<StopOutlined />} disabled={!record.state} className={styles.masterActionBtn}>
              {record.state ? "停止请求" : "已停止"}
            </Button>
          </Popconfirm>
        ) : (
          <Button
            type="primary"
            icon={<PoweroffOutlined />}
            onClick={() => onStartTask(record)}
            className={styles.masterActionBtn}
          >
            开始采集
          </Button>
        )}
        <Tooltip title="编辑主采集站">
          <Button icon={<EditOutlined />} onClick={() => onEditSource(record.id)} />
        </Tooltip>
        <Popconfirm title="确认删除此主采集站？" onConfirm={() => onDeleteSource(record.id)}>
          <Button danger icon={<DeleteOutlined />} />
        </Popconfirm>
      </div>
    </div>
  );
}
