"use client";

import React from "react";
import { Empty } from "antd";
import { HistoryOutlined, DeleteOutlined } from "@ant-design/icons";
import styles from "./index.module.less";

export interface HistoryItem {
  id: string;
  name: string;
  episode: string;
  link: string;
  timeStamp: number;
}

interface HistoryPopoverProps {
  show: boolean;
  historyList: HistoryItem[];
  onClear: (e: React.MouseEvent) => void;
  onSelect: (link: string) => void;
}

export default function HistoryPopover({
  show,
  historyList,
  onClear,
  onSelect,
}: HistoryPopoverProps) {
  return (
    <div className={`${styles.historyPanel} ${show ? styles.show : ""}`}>
      <div className={styles.historyHeader}>
        <HistoryOutlined className={styles.icon} />
        <span className={styles.title}>历史观看记录</span>
        {historyList.length > 0 && (
          <DeleteOutlined className={styles.clear} onClick={onClear} />
        )}
      </div>
      <div className={styles.historyList}>
        {historyList.length > 0 ? (
          historyList.map((item, idx) => (
            <div
              key={idx}
              className={styles.historyItem}
              onClick={() => onSelect(item.link)}
              style={{ cursor: "pointer" }}
            >
              <span className={styles.filmTitle}>{item.name}</span>
              <span className={styles.episode}>{item.episode}</span>
            </div>
          ))
        ) : (
          <div style={{ padding: "20px 0" }}>
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无观看记录" />
          </div>
        )}
      </div>
    </div>
  );
}
