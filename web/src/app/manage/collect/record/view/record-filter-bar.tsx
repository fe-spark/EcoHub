"use client";

import React from "react";
import { Select, DatePicker, Button, Segmented } from "antd";
import {
  ReloadOutlined,
  AppstoreOutlined,
  VideoCameraOutlined,
  WarningOutlined,
  SyncOutlined,
} from "@ant-design/icons";
import type { Dayjs } from "dayjs";
import { ContentFilterBar } from "@/app/manage/components/content-shell";
import styles from "./index.module.less";

const { RangePicker } = DatePicker;

export interface FilterParams {
  trigger: string;
  status: string;
  beginTime: string;
  endTime: string;
  updated: string;
  failed: string;
}

export interface RecordFilterBarProps {
  params: FilterParams;
  dateRange: [Dayjs | null, Dayjs | null] | null;
  onParamsChange: (newParams: FilterParams) => void;
  onDateRangeChange: (dates: [Dayjs | null, Dayjs | null] | null) => void;
  onReset: () => void;
}

export function RecordFilterBar({
  params,
  dateRange,
  onParamsChange,
  onDateRangeChange,
  onReset,
}: RecordFilterBarProps) {
  const activeSegment = (() => {
    if (params.status === "running") return "running";
    if (params.updated === "1") return "updated";
    if (params.failed === "1") return "failed";
    return "all";
  })();

  const handleSegmentChange = (val: string | number) => {
    const next = { ...params, updated: "", failed: "", status: "" };
    if (val === "updated") next.updated = "1";
    else if (val === "failed") next.failed = "1";
    else if (val === "running") next.status = "running";
    onParamsChange(next);
  };

  const hasFilter = Boolean(
    params.trigger || params.status || params.beginTime || params.endTime || params.updated || params.failed,
  );

  return (
    <ContentFilterBar
      lead={
        <Segmented
          value={activeSegment}
          onChange={handleSegmentChange}
          options={[
            { label: "全部", value: "all", icon: <AppstoreOutlined /> },
            { label: "有更新", value: "updated", icon: <VideoCameraOutlined /> },
            { label: "有失败", value: "failed", icon: <WarningOutlined /> },
            { label: "进行中", value: "running", icon: <SyncOutlined /> },
          ]}
        />
      }
      actions={
        <>
          <Select
            placeholder="采集方式"
            allowClear
            value={params.trigger || undefined}
            onChange={(v) => onParamsChange({ ...params, trigger: v || "" })}
            options={[
              { label: "手动采集", value: "manual" },
              { label: "定时任务", value: "cron" },
              { label: "单片更新", value: "single_update" },
              { label: "失败重试", value: "retry" },
            ]}
            className={styles.filterSelect}
          />
          <RangePicker
            value={dateRange}
            className={styles.dateRange}
            placeholder={["开始日期", "结束日期"]}
            onChange={(dates) => {
              onDateRangeChange(dates);
              if (dates && dates[0] && dates[1]) {
                onParamsChange({
                  ...params,
                  beginTime: dates[0].format("YYYY-MM-DD"),
                  endTime: dates[1].format("YYYY-MM-DD"),
                });
              } else {
                onParamsChange({ ...params, beginTime: "", endTime: "" });
              }
            }}
          />
          <Button icon={<ReloadOutlined />} onClick={onReset} disabled={!hasFilter}>
            重置
          </Button>
        </>
      }
    />
  );
}
