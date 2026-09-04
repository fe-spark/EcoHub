"use client";

import React from "react";
import Link from "next/link";
import { Button, Popover, Space, Tag, Typography } from "antd";
import {
  CompassOutlined,
  PlayCircleOutlined,
  SearchOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import type { LogRow } from "./types";

interface ResourceCellProps {
  record: LogRow;
}

export default function ResourceCell({ record }: ResourceCellProps) {
  const { resource, resourceTitle, resourcePoster, resourceCat, action } = record;

  if (!resource && !resourceTitle && !resourceCat) {
    return <span style={{ color: "#bbb" }}>-</span>;
  }

  const rawId = (resource || "").replace(/^id\s+/, "").trim();
  const isNumericId = /^\d+$/.test(rawId);

  // 1. 分类筛选动作：直接展示分类名称或分类ID，绝不当做具体影片关联
  if (action === "classify" && resource) {
    const catLabel = resourceCat ? resourceCat : isNumericId ? `分类 #${rawId}` : resource;
    return (
      <Link href={`/filmClassify?Pid=${encodeURIComponent(rawId || resource)}`} target="_blank">
        <Tag icon={<CompassOutlined />} color="purple" style={{ cursor: "pointer" }}>
          {catLabel}
        </Tag>
      </Link>
    );
  }

  // 2. 寻片搜索动作：展示搜索关键词
  if (action === "search" && resource) {
    return (
      <Link href={`/search?search=${encodeURIComponent(resource)}`} target="_blank">
        <Tag
          icon={<SearchOutlined />}
          color="orange"
          style={{ cursor: "pointer", maxWidth: 180, overflow: "hidden", textOverflow: "ellipsis" }}
        >
          {resource}
        </Tag>
      </Link>
    );
  }

  // 3. 影视点播动作：才展示具体影片详情（片名、海报封面、分类与跳转播放）
  if (action === "play" || Boolean(resourceTitle)) {
    const displayName = resourceTitle || (isNumericId ? `影片 #${rawId}` : resource);
    const playUrl = isNumericId ? `/play?id=${rawId}` : `/search?search=${encodeURIComponent(resource || "")}`;

    return (
      <Space size={8} align="center">
        {resourcePoster ? (
          <Popover
            placement="right"
            content={
              <div style={{ width: 150, textAlign: "center" }}>
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={resourcePoster}
                  alt={displayName}
                  style={{
                    width: "100%",
                    height: 200,
                    objectFit: "cover",
                    borderRadius: 6,
                    boxShadow: "0 2px 8px rgba(0,0,0,0.15)",
                  }}
                />
                <div style={{ fontWeight: 600, marginTop: 8, fontSize: 13, color: "var(--ant-color-text)" }}>
                  {displayName}
                </div>
                {resourceCat && (
                  <Tag color="blue" style={{ marginTop: 4 }}>
                    {resourceCat}
                  </Tag>
                )}
                <div style={{ marginTop: 10 }}>
                  <Link href={playUrl} target="_blank">
                    <Button size="small" type="primary" icon={<PlayCircleOutlined />} block>
                      播放详情
                    </Button>
                  </Link>
                </div>
              </div>
            }
            title={null}
          >
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={resourcePoster}
              alt={displayName}
              style={{
                width: 24,
                height: 32,
                objectFit: "cover",
                borderRadius: 4,
                cursor: "pointer",
                flexShrink: 0,
                border: "1px solid var(--ant-color-border-secondary)",
              }}
            />
          </Popover>
        ) : (
          <VideoCameraOutlined
            style={{
              color: "var(--ant-color-primary, #fa8c16)",
              fontSize: 16,
              flexShrink: 0,
            }}
          />
        )}

        <div style={{ display: "flex", flexDirection: "column", minWidth: 0, gap: 2 }}>
          <Link
            href={playUrl}
            target="_blank"
            style={{
              fontWeight: 600,
              fontSize: 13,
              color: "var(--ant-color-text)",
              maxWidth: 220,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {displayName}
          </Link>
          <Space size={6} wrap style={{ fontSize: 11 }}>
            {resourceCat && (
              <Tag
                color="blue"
                style={{
                  fontSize: 10,
                  lineHeight: "16px",
                  padding: "0 4px",
                  margin: 0,
                }}
              >
                {resourceCat}
              </Tag>
            )}
            {rawId && isNumericId && (
              <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                #{rawId}
              </Typography.Text>
            )}
          </Space>
        </div>
      </Space>
    );
  }

  // 4. TVBox 专用语义资源
  if (resource === "config") {
    return <Tag color="geekblue">源配置同步</Tag>;
  }
  if (resource === "list") {
    return <Tag color="cyan">分类列表</Tag>;
  }

  // 5. 兜底通用 Tag
  return <Tag>{resource}</Tag>;
}
