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
  AimOutlined,
  FireOutlined,
  CompassOutlined,
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
}

interface UseFilmColumnsProps {
  syncingIds: number[];
  canWrite: boolean;
  router: any;
  styles: any;
  handleUpdateSingle: (mid: number) => void;
  handleDelFilm: (id: number) => void;
  handleScrapeFilm: (record: FilmItem) => void;
}

export function useFilmColumns({
  syncingIds,
  canWrite,
  router,
  styles,
  handleUpdateSingle,
  handleDelFilm,
  handleScrapeFilm,
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
                window.open(resolvePlayEntryPath(record.mid), "_blank")
              }
            >
              {record.name}
            </Text>
            <Tag color="orange" style={{ borderRadius: 4, flexShrink: 0 }}>
              {record.cName}
            </Tag>
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
            {dayjs(v * 1000).format("YYYY-MM-DD HH:ss")}
          </Text>
        ),
      },
      {
        title: "操作",
        key: "action",
        align: "center",
        fixed: "right",
        render: (_, record) => (
          <Space size={8}>
            <Tooltip title="打开播放页">
              <Button
                type="primary"
                shape="circle"
                size="small"
                icon={<AimOutlined />}
                onClick={() =>
                  window.open(resolvePlayEntryPath(record.mid), "_blank")
                }
              />
            </Tooltip>
            <Tooltip title="TMDB 刮削">
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
            <Tooltip title="同步更新">
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
            <Tooltip title="修改影视">
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
            <Popconfirm
              title="确认删除此影片？"
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
    [syncingIds, router, handleDelFilm, handleUpdateSingle, handleScrapeFilm, canWrite, styles],
  );
}
