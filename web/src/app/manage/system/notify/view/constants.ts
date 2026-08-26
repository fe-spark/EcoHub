export interface NotifyEventSwitches {
  collectBatchSummary: boolean;
  collectSourceFailed: boolean;
  collectFinalizeFailed: boolean;
  collectProgressStale: boolean;
  cronTaskFailed: boolean;
  cronTaskDone: boolean;
  sourceConfigChanged: boolean;
}

export interface NotifyQuietHours {
  enabled: boolean;
  start: string;
  end: string;
  allowLevels: string[];
}

export interface NotifyConfigValues {
  enabled: boolean;
  botToken: string;
  chatIds: string[];
  events: NotifyEventSwitches;
  includeFilmDetails: boolean;
  onlyNotifyOnUpdate: boolean;
  maxFilmsInMessage: number;
  minIntervalSec: number;
  quietHours: NotifyQuietHours;
}

export const DEFAULT_EVENTS: NotifyEventSwitches = {
  collectBatchSummary: true,
  collectSourceFailed: true,
  collectFinalizeFailed: true,
  collectProgressStale: true,
  cronTaskFailed: true,
  cronTaskDone: false,
  sourceConfigChanged: true,
};

export const DEFAULT_QUIET_HOURS: NotifyQuietHours = {
  enabled: false,
  start: "23:00",
  end: "07:00",
  allowLevels: ["ERROR", "CRITICAL"],
};

export const DEFAULT_CONFIG: NotifyConfigValues = {
  enabled: false,
  botToken: "",
  chatIds: [],
  events: { ...DEFAULT_EVENTS },
  includeFilmDetails: true,
  onlyNotifyOnUpdate: true,
  maxFilmsInMessage: 15,
  minIntervalSec: 60,
  quietHours: { ...DEFAULT_QUIET_HOURS },
};

export interface EventOption {
  field: keyof NotifyEventSwitches;
  category: "alert" | "digest" | "audit";
  label: string;
  badge: string;
  badgeColor: string;
  hint: string;
}

export const EVENT_GROUPS = [
  { key: "alert", title: "核心故障告警", description: "采集源失败、任务卡死、收尾异常及 Cron 报错" },
  { key: "digest", title: "业务简报与汇总", description: "采集批次完成摘要、更新列表及任务完成通告" },
  { key: "audit", title: "配置操作审计", description: "采集源新增/编辑/删除及属性变动记录" },
];

export const EVENT_OPTIONS: EventOption[] = [
  { field: "collectSourceFailed", category: "alert", label: "单源失败即时告警", badge: "核心告警", badgeColor: "red", hint: "某采集源连续失败达到上限终止时推送" },
  { field: "collectFinalizeFailed", category: "alert", label: "收尾发布失败", badge: "系统异常", badgeColor: "orange", hint: "快照更新或摘要刷新失败时发送告警" },
  { field: "collectProgressStale", category: "alert", label: "采集进度超时", badge: "超时告警", badgeColor: "gold", hint: "采集任务卡住被强制标记为失败时提醒" },
  { field: "cronTaskFailed", category: "alert", label: "定时任务失败", badge: "任务失败", badgeColor: "volcano", hint: "后台定时调度运行失败告警" },
  { field: "collectBatchSummary", category: "digest", label: "采集结果摘要", badge: "批次汇总", badgeColor: "blue", hint: "整批采集结束后推送各源统计与更新列表" },
  { field: "cronTaskDone", category: "digest", label: "定时任务完成", badge: "任务通知", badgeColor: "green", hint: "定时任务成功完成时推送通知" },
  { field: "sourceConfigChanged", category: "audit", label: "采集源配置变更", badge: "配置变更", badgeColor: "cyan", hint: "站点新增/删除/切换/属性修改等记录推送" },
];

export function normalizeConfig(data: Partial<NotifyConfigValues> | undefined): NotifyConfigValues {
  return {
    enabled: Boolean(data?.enabled),
    botToken: String(data?.botToken ?? "").trim(),
    chatIds: Array.isArray(data?.chatIds) ? data!.chatIds.map(String).filter(Boolean) : [],
    events: { ...DEFAULT_EVENTS, ...(data?.events ?? {}) },
    includeFilmDetails: data?.includeFilmDetails !== false,
    onlyNotifyOnUpdate: data?.onlyNotifyOnUpdate !== false,
    maxFilmsInMessage: Number(data?.maxFilmsInMessage || 15),
    minIntervalSec: Number(data?.minIntervalSec ?? 60),
    quietHours: {
      enabled: Boolean(data?.quietHours?.enabled),
      start: String(data?.quietHours?.start || "23:00"),
      end: String(data?.quietHours?.end || "07:00"),
      allowLevels: Array.isArray(data?.quietHours?.allowLevels)
        ? data!.quietHours!.allowLevels
        : ["ERROR", "CRITICAL"],
    },
  };
}
