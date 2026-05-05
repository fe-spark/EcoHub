"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Card, Input, Select, Space, Switch, Tag, Typography } from "antd";
import { ClearOutlined, CopyOutlined, PauseCircleOutlined, PlayCircleOutlined, ReloadOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

const INITIAL_LOG_LINES = 500;
const MAX_LOG_LINES = 1000;
const DELTA_LOG_LIMIT = 10000;
const POLLING_INTERVAL_MS = 1000;

interface LogEntry {
  seq: number;
  line: string;
}

interface DeltaLogsResponse {
  entries: LogEntry[];
  nextSeq: number;
  minSeq?: number;
  expired: boolean;
}

function appendBoundedLines(prev: string[], incoming: string[]) {
  if (incoming.length === 0) return prev;
  const next = [...prev, ...incoming];
  if (next.length <= MAX_LOG_LINES) return next;
  return next.slice(next.length - MAX_LOG_LINES);
}

function detectLevel(line: string) {
  const text = line.toLowerCase();
  if (text.includes("panic") || text.includes("fatal") || text.includes("error") || text.includes("失败")) {
    return "error";
  }
  if (text.includes("warn") || text.includes("warning") || text.includes("跳过")) {
    return "warn";
  }
  return "info";
}

function levelTag(level: string) {
  if (level === "error") return <Tag color="error">ERROR</Tag>;
  if (level === "warn") return <Tag color="warning">WARN</Tag>;
  return <Tag color="processing">INFO</Tag>;
}

export default function SystemLogsPageView() {
  const [lines, setLines] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [refreshError, setRefreshError] = useState(false);
  const [cursorExpired, setCursorExpired] = useState(false);
  const [lastReceivedAt, setLastReceivedAt] = useState("");
  const [autoScroll, setAutoScroll] = useState(true);
  const [keyword, setKeyword] = useState("");
  const [level, setLevel] = useState("all");
  const logBodyRef = useRef<HTMLDivElement | null>(null);
  const cursorRef = useRef(0);
  const deltaFetchingRef = useRef(false);
  const { message } = useAppMessage();

  const fetchRecentLogs = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet<DeltaLogsResponse>("/manage/system/logs/delta", { lines: INITIAL_LOG_LINES });
      if (resp.code === 0) {
        const entries = resp.data.entries || [];
        cursorRef.current = resp.data.nextSeq || 0;
        setCursorExpired(false);
        setRefreshError(false);
        setLines(entries.map((entry) => entry.line));
        return;
      }
      message.error(resp.msg);
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    void fetchRecentLogs();
  }, [fetchRecentLogs]);

  useEffect(() => {
    if (!autoRefresh) return;

    const timer = window.setInterval(async () => {
      if (deltaFetchingRef.current) return;
      deltaFetchingRef.current = true;
      try {
        const resp = await ApiGet<DeltaLogsResponse>("/manage/system/logs/delta", {
          after: cursorRef.current,
          limit: DELTA_LOG_LIMIT,
        });
        if (resp.code !== 0) {
          setRefreshError(true);
          return;
        }
        const entries = resp.data.entries || [];
        setRefreshError(false);
        if (resp.data.expired) {
          const minSeq = resp.data.minSeq ?? 0;
          const notice = `[SystemLog] 日志游标已过期，已从当前缓冲区最早序号 ${minSeq} 重新加载，过期窗口内日志可能已被截断`;
          cursorRef.current = resp.data.nextSeq || cursorRef.current;
          setCursorExpired(true);
          setLines([notice, ...entries.map((entry) => entry.line)].slice(-MAX_LOG_LINES));
          setLastReceivedAt(new Date().toLocaleTimeString());
          return;
        }
        cursorRef.current = resp.data.nextSeq || cursorRef.current;
        setCursorExpired(false);
        setLines((prev) => appendBoundedLines(prev, entries.map((entry) => entry.line)));
        if (entries.length > 0) {
          setLastReceivedAt(new Date().toLocaleTimeString());
        }
      } catch {
        setRefreshError(true);
      } finally {
        deltaFetchingRef.current = false;
      }
    }, POLLING_INTERVAL_MS);

    return () => {
      window.clearInterval(timer);
      deltaFetchingRef.current = false;
    };
  }, [autoRefresh]);

  const filteredLines = useMemo(() => {
    const word = keyword.trim().toLowerCase();
    return lines.filter((line) => {
      if (level !== "all" && detectLevel(line) !== level) return false;
      if (word && !line.toLowerCase().includes(word)) return false;
      return true;
    });
  }, [keyword, level, lines]);

  useEffect(() => {
    if (!autoScroll) return;
    const el = logBodyRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [autoScroll, filteredLines.length]);

  const copyLogs = async () => {
    await navigator.clipboard.writeText(filteredLines.join("\n"));
    message.success("已复制当前展示日志");
  };

  const renderRefreshStatus = () => {
    if (!autoRefresh) return <Tag>已暂停</Tag>;
    if (cursorExpired) return <Tag color="error">游标过期</Tag>;
    if (refreshError) return <Tag color="error">刷新失败</Tag>;
    return <Tag color="success">自动刷新中</Tag>;
  };

  return (
    <div className={styles.pageStack}>
      <div className={styles.headerArea}>
        <ManagePageHeader
          title="系统日志"
          description="查看服务端日志并按游标增量刷新；页面打开期间不会静默漏日志，前端最多展示最近 1000 行。"
        />
      </div>

      <Card className={styles.filterCard}>
        <Space size={[8, 8]} wrap className={styles.toolbar}>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={fetchRecentLogs}>
            刷新最近日志
          </Button>
          <Button
            type={autoRefresh ? "default" : "primary"}
            icon={autoRefresh ? <PauseCircleOutlined /> : <PlayCircleOutlined />}
            onClick={() => setAutoRefresh((value) => !value)}
          >
            {autoRefresh ? "暂停刷新" : "恢复刷新"}
          </Button>
          <Button icon={<ClearOutlined />} onClick={() => setLines([])}>
            清空显示
          </Button>
          <Button icon={<CopyOutlined />} onClick={copyLogs} disabled={filteredLines.length === 0}>
            复制当前日志
          </Button>
          <Input.Search
            allowClear
            placeholder="关键词过滤"
            className={styles.keywordInput}
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
          />
          <Select
            className={styles.levelSelect}
            value={level}
            onChange={setLevel}
            options={[
              { label: "全部等级", value: "all" },
              { label: "INFO", value: "info" },
              { label: "WARN", value: "warn" },
              { label: "ERROR", value: "error" },
            ]}
          />
          <Space>
            <Typography.Text type="secondary">自动滚动</Typography.Text>
            <Switch checked={autoScroll} onChange={setAutoScroll} />
          </Space>
        </Space>
      </Card>

      <Card
        title="日志输出"
        styles={{ body: { display: "flex", flex: 1, minHeight: 0, padding: 12 } }}
        extra={(
          <Space size={[8, 8]} wrap>
            {renderRefreshStatus()}
            {lastReceivedAt && <Tag>最后接收 {lastReceivedAt}</Tag>}
            <Tag>游标 {cursorRef.current}</Tag>
            <Tag>缓存 {lines.length}/{MAX_LOG_LINES} 行</Tag>
            <Tag>展示 {filteredLines.length} 行</Tag>
          </Space>
        )}
        className={styles.logCard}
      >
        <div ref={logBodyRef} className={styles.logBody}>
          {filteredLines.length === 0 ? (
            <Typography.Text type="secondary">暂无匹配日志</Typography.Text>
          ) : (
            filteredLines.map((line, index) => {
              const logLevel = detectLevel(line);
              return (
                <div key={`${index}-${line}`} className={styles.logLine}>
                  <span className={styles.lineNo}>{index + 1}</span>
                  <span className={styles.level}>{levelTag(logLevel)}</span>
                  <span className={styles.message}>{line}</span>
                </div>
              );
            })
          )}
        </div>
      </Card>
    </div>
  );
}
