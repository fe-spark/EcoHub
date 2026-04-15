"use client";

import React, { useState, useCallback } from "react";
import { useRouter } from "next/navigation";
import { Empty, Popconfirm } from "antd";
import {
  DeleteOutlined,
  MobileOutlined,
  DesktopOutlined,
  ClockCircleOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import styles from "./index.module.less";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { readHistoryMap, writeHistoryMap } from "@/lib/historyStorage";

function readHistoryList(): any[] {
  const historyMap = readHistoryMap();
  return (Object.values(historyMap) as any[]).sort(
    (a: any, b: any) => b.timeStamp - a.timeStamp,
  );
}

export default function HistoryPageView() {
  const router = useRouter();
  const [historyList, setHistoryList] = useState<any[]>(readHistoryList);
  const { message } = useAppMessage();

  const loadHistory = useCallback(() => {
    setHistoryList(readHistoryList());
  }, []);

  const handleDelete = (id: string) => {
    const historyMap = readHistoryMap();
    delete historyMap[id];
    writeHistoryMap(historyMap);
    loadHistory();
    message.success("已删除该条记录");
  };

  const formatProgress = (curr: number, total: number) => {
    if (!curr || !total) return "查看详情";
    const percent = Math.floor((curr / total) * 100);
    return `已观看 ${percent}%`;
  };

  return (
    <div className={styles.container}>
      <h1 className={styles.title}>观看历史</h1>

      {historyList.length > 0 ? (
        <div className={styles.historyList}>
          {historyList.map((item) => (
            <div key={item.id} className={styles.historyItem}>
              {/* 观看记录封面来自历史缓存中的动态地址，这里保持原生 img */}
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={item.picture || FALLBACK_IMG}
                className={styles.poster}
                alt={item.name}
                onClick={() => router.push(item.link)}
              />
              <div className={styles.info}>
                <h3 onClick={() => router.push(item.link)}>{item.name}</h3>
                <div className={styles.meta}>
                  <span>
                    <ClockCircleOutlined style={{ marginRight: 4 }} />
                    {dayjs(item.timeStamp).format("YYYY-MM-DD HH:mm")}
                  </span>
                  <span>
                    {item.devices ? (
                      <MobileOutlined className={styles.deviceIcon} />
                    ) : (
                      <DesktopOutlined className={styles.deviceIcon} />
                    )}
                  </span>
                </div>
                <div className={styles.episode}>
                  {item.sourceName && <span className={styles.sourceTag}>{item.sourceName}</span>}
                  {item.episode}
                </div>
                <div className={styles.progress}>
                  {formatProgress(item.currentTime, item.duration)}
                </div>
              </div>

              <Popconfirm
                title="确定删除这条历史记录吗？"
                onConfirm={() => handleDelete(item.id)}
                okText="确定"
                cancelText="取消"
              >
                <DeleteOutlined className={styles.deleteBtn} />
              </Popconfirm>
            </div>
          ))}
        </div>
      ) : (
        <div style={{ padding: "100px 0" }}>
          <Empty description="暂无观看记录" />
        </div>
      )}
    </div>
  );
}
