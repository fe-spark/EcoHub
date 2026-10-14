"use client";

import React, { useMemo } from "react";
import {
  Space,
  Tag,
  Button,
  Popconfirm,
  Tooltip,
  Typography,
} from "antd";
import {
  ReloadOutlined,
  EditOutlined,
  DeleteOutlined,
  FireOutlined,
  CompassOutlined,
  UndoOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { resolvePlayEntryPath } from "@/lib/playNavigation";

const { Text } = Typography;

export interface FilmItem {
  mid: number;
  ID: number;
  name: string;
  cName: string;
  year: string | number;
  score: string | number;
  hits: number;
  remarks: string;
  updateStamp: number;
  isScraped?: boolean;
  tmdbId?: number;
}

interface UseFilmColumnsProps {
  syncingIds: number[];
  canWrite: boolean;
  router: any;
  styles: any;
  handleUpdateSingle: (mid: number) => void;
  handleDelFilm: (id: number) => void;
  handleScrapeFilm: (record: FilmItem) => void;
  handleRevertScrape?: (mid: number) => void;
  tmdbEnabled?: boolean;
  activeSourceId?: string;
}

export function useFilmColumns({
  syncingIds,
  canWrite,
  router,
  styles,
  handleUpdateSingle,
  handleDelFilm,
  handleScrapeFilm,
  handleRevertScrape,
  tmdbEnabled = false,
  activeSourceId,
}: UseFilmColumnsProps) {
  return useMemo<ColumnsType<FilmItem>>(
    () => [
      {
        title: "ID",
        dataIndex: "mid",
        key: "mid",
        width: 80,
        fixed: "left",
        align: "center",
        render: (v) => (
          <Tag color="#8b40ff" style={{ borderRadius: 4 }}>
            #{v}
          </Tag>
        ),
      },
      {
        title: "影片信息",
        key: "info",
        align: "left",
        render: (_, record) => (
          <Space size={6} wrap={false}>
            <Text
              className={styles.filmName}
              style={{ whiteSpace: "nowrap" }}
              onClick={() =>
                window.open(
                  resolvePlayEntryPath(record.mid, { sourceId: activeSourceId }),
                  "_blank",
                )
              }
            >
              {record.name}
            </Text>
            <Tag color="orange" style={{ borderRadius: 4, flexShrink: 0 }}>
              {record.cName}
            </Tag>
            {record.isScraped && (
              <Tooltip title={record.tmdbId ? `TMDB: ${record.tmdbId}` : "已刮削"}>
                <Tag
                  color="#722ed1"
                  style={{ borderRadius: 4, flexShrink: 0, cursor: tmdbEnabled && canWrite ? "pointer" : "default" }}
                  onClick={() => tmdbEnabled && canWrite && handleScrapeFilm(record)}
                >
                  已刮削
                </Tag>
              </Tooltip>
            )}
          </Space>
        ),
      },
      {
        title: "评分",
        dataIndex: "score",
        key: "score",
        align: "center",
        render: (v) => (
          <Text strong style={{ color: "var(--ant-color-primary)" }}>
            {v}
          </Text>
        ),
      },
      {
        title: "年份",
        dataIndex: "year",
        key: "year",
        align: "center",
        render: (v) => <Text>{v}</Text>,
      },
      {
        title: "热度",
        dataIndex: "hits",
        key: "hits",
        align: "center",
        render: (v) => (
          <Text type="danger">
            <FireOutlined /> {v}
          </Text>
        ),
      },
      {
        title: "更新状态",
        key: "status",
        align: "center",
        render: (_, record) => (
          <Tag
            color={record.remarks.includes("更新") ? "warning" : "success"}
            style={{ borderRadius: 6, padding: "2px 8px" }}
          >
            {record.remarks}
          </Tag>
        ),
      },
      {
        title: "更新时间",
        dataIndex: "updateStamp",
        align: "center",
        render: (v) => (
          <Text type="secondary" style={{ fontSize: 13 }}>
            {dayjs(v * 1000).format("YYYY-MM-DD HH:mm:ss")}
          </Text>
        ),
      },
      {
        title: "操作",
        key: "action",
        align: "center",
        fixed: "right",
        width: tmdbEnabled ? 180 : 120,
        render: (_, record) => (
          <Space size={8}>
            {tmdbEnabled && (
              <Tooltip title="刮削">
                <Button
                  type="primary"
                  shape="circle"
                  size="small"
                  style={{ background: "#722ed1", borderColor: "#722ed1" }}
                  icon={<CompassOutlined />}
                  disabled={!canWrite}
                  onClick={() => handleScrapeFilm(record)}
                />
              </Tooltip>
            )}
            <Tooltip title="更新">
              <Button
                type="primary"
                shape="circle"
                size="small"
                style={{ background: "#52c41a", borderColor: "#52c41a" }}
                icon={
                  <ReloadOutlined
                    className={`${styles.syncIcon} ${syncingIds.includes(record.mid) ? styles.syncing : ""}`}
                  />
                }
                disabled={!canWrite}
                onClick={() => handleUpdateSingle(record.mid)}
              />
            </Tooltip>
            <Tooltip title="编辑">
              <Button
                type="primary"
                shape="circle"
                size="small"
                style={{ background: "#1890ff", borderColor: "#1890ff" }}
                icon={<EditOutlined />}
                disabled={!canWrite}
                onClick={() => router.push(`/manage/film/add?id=${record.mid}`)}
              />
            </Tooltip>
            {tmdbEnabled && (
              record.isScraped && canWrite ? (
                <Popconfirm
                  title="确定还原刮削？"
                  okText="还原"
                  cancelText="取消"
                  onConfirm={() => handleRevertScrape?.(record.mid)}
                >
                  <Tooltip title="还原刮削">
                    <Button
                      type="primary"
                      shape="circle"
                      size="small"
                      style={{ background: "#fa8c16", borderColor: "#fa8c16" }}
                      icon={<UndoOutlined />}
                    />
                  </Tooltip>
                </Popconfirm>
              ) : (
                <Tooltip title="还原刮削">
                  <span>
                    <Button
                      shape="circle"
                      size="small"
                      disabled
                      icon={<UndoOutlined />}
                    />
                  </span>
                </Tooltip>
              )
            )}
            <Popconfirm
              title="确定删除？"
              okText="确定"
              cancelText="取消"
              onConfirm={() => handleDelFilm(record.mid || record.ID)}
            >
              <Tooltip title="删除">
                <Button
                  type="primary"
                  danger
                  shape="circle"
                  size="small"
                  icon={<DeleteOutlined />}
                  disabled={!canWrite}
                />
              </Tooltip>
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [
      syncingIds,
      router,
      handleDelFilm,
      handleUpdateSingle,
      handleScrapeFilm,
      handleRevertScrape,
      canWrite,
      styles,
      tmdbEnabled,
      activeSourceId,
    ],
  );
}
