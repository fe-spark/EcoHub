"use client";

import React from "react";
import { Space, Select, DatePicker, Button, Segmented } from "antd";
import {
  ReloadOutlined,
  SearchOutlined,
  AppstoreOutlined,
  VideoCameraOutlined,
  WarningOutlined,
  SyncOutlined,
} from "@ant-design/icons";
import type { Dayjs } from "dayjs";
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
  onSearch: () => void;
  onReset: () => void;
}

export function RecordFilterBar({
  params,
  dateRange,
  onParamsChange,
  onDateRangeChange,
  onSearch,
  onReset,
}: RecordFilterBarProps) {
  // 核心视图切换：全部 / 有更新 / 有失败 / 执行中
  const activeSegment = (() => {
    if (params.status === "running") return "running";
    if (params.updated === "1") return "updated";
    if (params.failed === "1") return "failed";
    return "all";
  })();

  const handleSegmentChange = (val: string | number) => {
    const next = { ...params };
    if (val === "all") {
      next.updated = "";
      next.failed = "";
      next.status = "";
    } else if (val === "updated") {
      next.updated = "1";
      next.failed = "";
      next.status = "";
    } else if (val === "failed") {
      next.updated = "";
      next.failed = "1";
      next.status = "";
    } else if (val === "running") {
      next.updated = "";
      next.failed = "";
      next.status = "running";
    }
    onParamsChange(next);
  };

  return (
    <div className={styles.filterBarWrapper}>
      <div className={styles.filterBarLeft}>
        <Segmented
          value={activeSegment}
          onChange={handleSegmentChange}
          options={[
            { label: "全部批次", value: "all", icon: <AppstoreOutlined /> },
            { label: "有更新", value: "updated", icon: <VideoCameraOutlined /> },
            { label: "有失败", value: "failed", icon: <WarningOutlined /> },
            { label: "执行中", value: "running", icon: <SyncOutlined /> },
          ]}
        />
      </div>

      <Space size={[8, 8]} wrap className={styles.filterBarRight}>
        <Select
          placeholder="采集方式"
          allowClear
          value={params.trigger || undefined}
          onChange={(v) => onParamsChange({ ...params, trigger: v || "" })}
          options={[
            { label: "全部方式", value: "" },
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

        <Button type="primary" icon={<SearchOutlined />} onClick={onSearch}>
          查询
        </Button>
        <Button icon={<ReloadOutlined />} onClick={onReset}>
          重置
        </Button>
      </Space>
    </div>
  );
}
