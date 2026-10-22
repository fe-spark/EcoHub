import dayjs from "dayjs";

export type DateRangeValue = [dayjs.Dayjs, dayjs.Dayjs] | null;

export const RANGE_TIME_DEFAULT: [dayjs.Dayjs, dayjs.Dayjs] = [
  dayjs("00:00:00", "HH:mm:ss"),
  dayjs("23:59:59", "HH:mm:ss"),
];

export function formatDateRange(range: DateRangeValue) {
  if (!range?.[0] || !range?.[1]) {
    return { beginTime: "", endTime: "" };
  }
  return {
    beginTime: range[0].format("YYYY-MM-DD HH:mm:ss"),
    endTime: range[1].format("YYYY-MM-DD HH:mm:ss"),
  };
}

export interface PhotoItem {
  ID: number;
  link: string;
  name: string;
  fid: string;
  CreatedAt?: string;
}
