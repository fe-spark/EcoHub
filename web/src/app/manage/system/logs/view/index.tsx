"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Button, Card, Input, Select, Space, Switch, Tag, Typography } from "antd";
import { ClearOutlined, CopyOutlined, FileTextOutlined, PauseCircleOutlined, PlayCircleOutlined, ReloadOutlined, SearchOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

interface SystemLogsPageViewProps {
  /** 嵌入系统设置 Tabs 时隐藏独立页头 */
  embedded?: boolean;
}


const INITIAL_LOG_LINES = 500;
const MAX_LOG_LINES = 1000;
const DELTA_LOG_LIMIT = 10000;
const POLLING_INTERVAL_MS = 1000;

/** 服务端写入时确定的级别；前端只读该字段，禁止按正文猜。 */
type LogLevel = "info" | "warn" | "error";

interface LogEntry {
  seq: number;
  line: string;
  /** info | warn | error，缺省按 info */
  level?: string;
}

interface DeltaLogsResponse {
  entries: LogEntry[];
  nextSeq: number;
  minSeq?: number;
  expired: boolean;
}

function normalizeLevel(level?: string): LogLevel {
  const v = (level || "").toLowerCase().trim();
  if (v === "error" || v === "err" || v === "fatal" || v === "panic") return "error";
  if (v === "warn" || v === "warning") return "warn";
  return "info";
}

function appendBoundedEntries(prev: LogEntry[], incoming: LogEntry[]) {
  if (incoming.length === 0) return prev;
  const next = [...prev, ...incoming];
  if (next.length <= MAX_LOG_LINES) return next;
  return next.slice(next.length - MAX_LOG_LINES);
}

function levelTag(level: LogLevel) {
  if (level === "error") return <Tag color="error">ERROR</Tag>;
  if (level === "warn") return <Tag color="warning">WARN</Tag>;
  return <Tag color="processing">INFO</Tag>;
}

export default function SystemLogsPageView({ embedded = false }: SystemLogsPageViewProps) {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [refreshError, setRefreshError] = useState(false);
  const [cursorExpired, setCursorExpired] = useState(false);
  const [lastReceivedAt, setLastReceivedAt] = useState("");
  const [autoScroll, setAutoScroll] = useState(true);
  const [inputKeyword, setInputKeyword] = useState("");
  const [keyword, setKeyword] = useState("");
  const [level, setLevel] = useState<string>("all");
  const logBodyRef = useRef<HTMLDivElement | null>(null);
  const cursorRef = useRef(0);
  const deltaFetchingRef = useRef(false);
  const { message } = useAppMessage();

  const fetchRecentLogs = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet<DeltaLogsResponse>("/manage/system/logs/delta", { lines: INITIAL_LOG_LINES });
      if (resp.code === 0) {
        const list = resp.data.entries || [];
        cursorRef.current = resp.data.nextSeq || 0;
        setCursorExpired(false);
        setRefreshError(false);
        setEntries(list);
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
        const list = resp.data.entries || [];
        setRefreshError(false);
        if (resp.data.expired) {
          const minSeq = resp.data.minSeq ?? 0;
          const notice: LogEntry = {
            seq: -1,
            level: "warn",
            line: `[SystemLog] 日志游标已过期，已从当前缓冲区最早序号 ${minSeq} 重新加载，过期窗口内日志可能已被截断`,
          };
          cursorRef.current = resp.data.nextSeq || cursorRef.current;
          setCursorExpired(true);
          setEntries([notice, ...list].slice(-MAX_LOG_LINES));
          setLastReceivedAt(new Date().toLocaleTimeString());
          return;
        }
        cursorRef.current = resp.data.nextSeq || cursorRef.current;
        setCursorExpired(false);
        setEntries((prev) => appendBoundedEntries(prev, list));
        if (list.length > 0) {
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

  const filteredEntries = useMemo(() => {
    const word = keyword.trim().toLowerCase();
    return entries.filter((entry) => {
      if (level !== "all" && normalizeLevel(entry.level) !== level) return false;
      if (word && !entry.line.toLowerCase().includes(word)) return false;
      return true;
    });
  }, [keyword, level, entries]);

  useEffect(() => {
    if (!autoScroll) return;
    const el = logBodyRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [autoScroll, filteredEntries.length]);

  const copyLogs = async () => {
    await navigator.clipboard.writeText(filteredEntries.map((e) => e.line).join("\n"));
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
      {embedded ? null : (
        <div className={styles.headerArea}>
          <ManagePageHeader
            title="系统日志"
            description="级别由服务端写入时确定并随接口下发；页面打开期间按游标增量刷新，前端最多展示最近 1000 行。"
          />
        </div>
      )}

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
          <Button icon={<ClearOutlined />} onClick={() => setEntries([])}>
            清空显示
          </Button>
          <Button icon={<CopyOutlined />} onClick={copyLogs} disabled={filteredEntries.length === 0}>
            复制当前日志
          </Button>
          <Input
            allowClear
            placeholder="关键词过滤"
            className={styles.keywordInput}
            value={inputKeyword}
            onChange={(event) => {
              const val = event.target.value;
              setInputKeyword(val);
              if (val === "" && keyword !== "") {
                setKeyword("");
              }
            }}
            onPressEnter={() => setKeyword(inputKeyword.trim())}
          />
          <Button
            type="primary"
            icon={<SearchOutlined />}
            onClick={() => setKeyword(inputKeyword.trim())}
          >
            搜索
          </Button>
          <Button
            icon={<ReloadOutlined />}
            onClick={() => {
              setInputKeyword("");
              setKeyword("");
              setLevel("all");
            }}
          >
            重置
          </Button>
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
        title={
          <Space>
            <FileTextOutlined style={{ color: "#1677ff" }} />
            <span>日志输出</span>
          </Space>
        }
        styles={{ body: { display: "flex", flex: 1, minHeight: 0, padding: 12 } }}
        extra={(
          <Space size={[8, 8]} wrap>
            {renderRefreshStatus()}
            {lastReceivedAt && <Tag>最后接收 {lastReceivedAt}</Tag>}
            <Tag>游标 {cursorRef.current}</Tag>
            <Tag>缓存 {entries.length}/{MAX_LOG_LINES} 行</Tag>
            <Tag>展示 {filteredEntries.length} 行</Tag>
          </Space>
        )}
        className={styles.logCard}
      >

        <div ref={logBodyRef} className={styles.logBody}>
          {filteredEntries.length === 0 ? (
            <Typography.Text type="secondary">暂无匹配日志</Typography.Text>
          ) : (
            filteredEntries.map((entry, index) => {
              const logLevel = normalizeLevel(entry.level);
              return (
                <div key={`${entry.seq}-${index}`} className={styles.logLine}>
                  <span className={styles.lineNo}>{index + 1}</span>
                  <span className={styles.level}>{levelTag(logLevel)}</span>
                  <span className={styles.message}>{entry.line}</span>
                </div>
              );
            })
          )}
        </div>
      </Card>
    </div>
  );
}
