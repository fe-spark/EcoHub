export interface CronTask {
  id: string;
  cid: string;
  spec: string;
  remark: string;
  model: number;
  ids: string[];
  time: number;
  state: boolean;
  preV?: string;
  next?: string;
  running?: boolean;
}

export type ScheduleMode = "interval" | "daily" | "weekly" | "monthly" | "advanced";

export const cronFieldHelp = "支持 *、*/N、数字、范围、逗号组合；不指定日期或星期时可填 ?";

export const minuteOptions = Array.from({ length: 60 }, (_, value) => ({ value, label: `${String(value).padStart(2, "0")} 分` }));
export const hourOptions = Array.from({ length: 24 }, (_, value) => ({ value, label: `${String(value).padStart(2, "0")} 点` }));
export const dayOptions = Array.from({ length: 31 }, (_, index) => ({ value: index + 1, label: `${index + 1} 日` }));
export const weekOptions = [
  { value: 0, label: "周日" },
  { value: 1, label: "周一" },
  { value: 2, label: "周二" },
  { value: 3, label: "周三" },
  { value: 4, label: "周四" },
  { value: 5, label: "周五" },
  { value: 6, label: "周六" },
];

export function getTaskActionText(model: number) {
  switch (model) {
    case 0:
      return "定时采集";
    case 1:
      return "定时采集";
    case 2:
      return "定时采集重试";
    case 3:
      return "定时孤儿清理";
    default:
      return "计划任务";
  }
}

export function getTaskTypeText(model: number) {
  switch (model) {
    case 0:
      return "自动更新";
    case 1:
      return "自定义更新";
    case 2:
      return "采集重试";
    case 3:
      return "孤儿清理";
    default:
      return "计划任务";
  }
}

export function parseCronSpec(spec: string) {
  const parts = spec.trim().split(/\s+/);
  if (parts.length < 6) {
    return {
      cronMinute: "*/20",
      cronHour: "*",
      cronDay: "*",
      cronMonth: "*",
      cronWeek: "?",
    };
  }

  return {
    cronMinute: parts[1] || "0",
    cronHour: parts[2] || "*",
    cronDay: parts[3] || "*",
    cronMonth: parts[4] || "*",
    cronWeek: parts[5] || "?",
  };
}

function isPlainNumber(value: string) {
  return /^\d+$/.test(String(value || "").trim());
}

export function detectScheduleMode(parsed: ReturnType<typeof parseCronSpec>): ScheduleMode {
  const isEveryMonth = parsed.cronMonth === "*";
  const isEveryDay = parsed.cronDay === "*";
  const isNoWeekLimit = parsed.cronWeek === "?";
  const isEveryWeek = parsed.cronWeek === "*";
  const isEveryHour = parsed.cronHour === "*";
  const minuteStep = parsed.cronMinute.match(/^\*\/(\d+)$/);

  if (isEveryMonth && isEveryDay && isNoWeekLimit && isEveryHour && minuteStep) {
    return "interval";
  }
  if (isEveryMonth && isEveryDay && (isNoWeekLimit || isEveryWeek) && isPlainNumber(parsed.cronHour) && isPlainNumber(parsed.cronMinute)) {
    return "daily";
  }
  if (isEveryMonth && parsed.cronDay === "?" && isPlainNumber(parsed.cronWeek) && isPlainNumber(parsed.cronHour) && isPlainNumber(parsed.cronMinute)) {
    return "weekly";
  }
  if (isEveryMonth && isNoWeekLimit && isPlainNumber(parsed.cronDay) && isPlainNumber(parsed.cronHour) && isPlainNumber(parsed.cronMinute)) {
    return "monthly";
  }
  return "advanced";
}

export function toTaskFormValues(task: CronTask) {
  const parsed = parseCronSpec(task.spec);
  const scheduleMode = detectScheduleMode(parsed);
  const intervalMinutes = parsed.cronMinute.match(/^\*\/(\d+)$/)?.[1];

  return {
    ...task,
    ...parsed,
    cronSpec: task.spec,
    scheduleMode,
    intervalMinutes: Number(intervalMinutes || 20),
    scheduleHour: Number(isPlainNumber(parsed.cronHour) ? parsed.cronHour : 0),
    scheduleMinute: Number(isPlainNumber(parsed.cronMinute) ? parsed.cronMinute : 0),
    scheduleWeek: Number(isPlainNumber(parsed.cronWeek) ? parsed.cronWeek : 0),
    scheduleDay: Number(isPlainNumber(parsed.cronDay) ? parsed.cronDay : 1),
  };
}

export function buildCronSpec(values: any) {
  switch (values.scheduleMode as ScheduleMode) {
    case "interval":
      return `0 */${values.intervalMinutes || 20} * * * ?`;
    case "daily":
      return `0 ${values.scheduleMinute ?? 0} ${values.scheduleHour ?? 0} * * ?`;
    case "weekly":
      return `0 ${values.scheduleMinute ?? 0} ${values.scheduleHour ?? 0} ? * ${values.scheduleWeek ?? 0}`;
    case "monthly":
      return `0 ${values.scheduleMinute ?? 0} ${values.scheduleHour ?? 0} ${values.scheduleDay ?? 1} * ?`;
    default:
      return String(values.cronSpec || "").trim();
  }
}

function formatTime(hour: string, minute: string) {
  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

export function getTaskScheduleText(task: CronTask) {
  const parsed = parseCronSpec(task.spec);
  const mode = detectScheduleMode(parsed);

  if (mode === "interval") {
    const interval = parsed.cronMinute.match(/^\*\/(\d+)$/)?.[1] || "20";
    return `每隔 ${interval} 分钟运行一次`;
  }
  if (mode === "daily") {
    return `每天 ${formatTime(parsed.cronHour, parsed.cronMinute)} 运行`;
  }
  if (mode === "weekly") {
    const weekLabel = weekOptions.find((item) => item.value === Number(parsed.cronWeek))?.label || `周${parsed.cronWeek}`;
    return `每${weekLabel} ${formatTime(parsed.cronHour, parsed.cronMinute)} 运行`;
  }
  if (mode === "monthly") {
    return `每月 ${parsed.cronDay} 日 ${formatTime(parsed.cronHour, parsed.cronMinute)} 运行`;
  }
  return task.spec || "-";
}

export function getTaskDescription(task: CronTask) {
  return getTaskActionText(task.model);
}

export function buildTaskDescription(values: any) {
  return getTaskActionText(Number(values.model));
}
