"use client";

import React from "react";
import { Space, TreeSelect, Input, DatePicker, Button } from "antd";
import { SearchOutlined, ReloadOutlined } from "@ant-design/icons";

const { RangePicker } = DatePicker;

interface FilmFilterBarProps {
  params: any;
  setParams: React.Dispatch<React.SetStateAction<any>>;
  classId: number;
  dateRange: any;
  setDateRange: React.Dispatch<React.SetStateAction<any>>;
  handleClassChange: (val?: number) => void;
  onSearch: () => void;
  onReset: () => void;
  categoryTreeData: any[];
  categoryLoading?: boolean;
  treeExpandedKeys?: (string | number)[];
  onTreeExpand?: (keys: (string | number)[]) => void;
  styles: any;
}

export function FilmFilterBar({
  params,
  setParams,
  classId,
  dateRange,
  setDateRange,
  handleClassChange,
  onSearch,
  onReset,
  categoryTreeData,
  categoryLoading = false,
  treeExpandedKeys,
  onTreeExpand,
  styles,
}: FilmFilterBarProps) {
  return (
    <Space size={[8, 8]} wrap className={styles.filterBar}>
      <TreeSelect
        placeholder="分类"
        className={styles.categorySelect}
        value={classId || undefined}
        onChange={handleClassChange}
        treeData={categoryTreeData}
        treeExpandedKeys={treeExpandedKeys}
        onTreeExpand={onTreeExpand}
        treeLine={{ showLeafIcon: false }}
        popupMatchSelectWidth={false}
        styles={{ popup: { root: { minWidth: 240, maxHeight: 380, overflow: "auto" } } }}
        virtual={false}
        allowClear
        loading={categoryLoading}
        disabled={categoryLoading}
      />
      <Input
        placeholder="片名"
        value={params.name}
        onChange={(e) => setParams({ ...params, name: e.target.value })}
        className={styles.searchInput}
        allowClear
        onPressEnter={onSearch}
      />
      <RangePicker
        showTime
        value={dateRange}
        onChange={(v) => setDateRange(v)}
        className={styles.dateRange}
      />
      <Button type="primary" icon={<SearchOutlined />} onClick={onSearch} className={styles.searchButton}>
        搜索
      </Button>
      <Button icon={<ReloadOutlined />} onClick={onReset}>
        重置
      </Button>
    </Space>
  );
}
