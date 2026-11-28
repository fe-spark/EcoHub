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
import { isSafeInternalHref } from "../constants";
import type { LogRow } from "../types";

export default function ResourceCell({ record }: { record: LogRow }) {
  const { resource, resourceTitle, resourcePoster, resourceCat, resourceHref, action } = record;
  const safePoster =
    resourcePoster && /^https?:\/\//i.test(resourcePoster) ? resourcePoster : "";

  if (!resource && !resourceTitle && !resourceCat) {
    return <Typography.Text type="secondary">-</Typography.Text>;
  }

  const rawId = (resource || "").replace(/^id\s+/, "").trim();
  const isNumericId = /^\d+$/.test(rawId);

  if (action === "classify" && (resource || resourceCat)) {
    const catLabel = resourceCat || (isNumericId ? `分类 #${rawId}` : resource);
    if (resourceHref && isSafeInternalHref(resourceHref)) {
      return (
        <Link href={resourceHref} target="_blank">
          <Tag icon={<CompassOutlined />} color="purple" style={{ cursor: "pointer" }}>
            {catLabel}
          </Tag>
        </Link>
      );
    }
    return (
      <Tag icon={<CompassOutlined />} color="purple">
        {catLabel}
      </Tag>
    );
  }

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

  if (action === "play" || Boolean(resourceTitle)) {
    const displayName = resourceTitle || (isNumericId ? `影片 #${rawId}` : resource);
    const playUrl = isNumericId ? `/play?id=${rawId}` : `/search?search=${encodeURIComponent(resource || "")}`;

    return (
      <Space size={8} align="center">
        {safePoster ? (
          <Popover
            placement="right"
            content={
              <div style={{ width: 150, textAlign: "center" }}>
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={safePoster}
                  alt={displayName}
                  style={{ width: "100%", height: 200, objectFit: "cover", borderRadius: 6 }}
                />
                <div style={{ fontWeight: 600, marginTop: 8, fontSize: 13 }}>{displayName}</div>
                {resourceCat && <Tag color="blue" style={{ marginTop: 4 }}>{resourceCat}</Tag>}
                <div style={{ marginTop: 10 }}>
                  <Link href={playUrl} target="_blank">
                    <Button size="small" type="primary" icon={<PlayCircleOutlined />} block>
                      播放详情
                    </Button>
                  </Link>
                </div>
              </div>
            }
          >
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={safePoster}
              alt={displayName}
              style={{
                width: 24,
                height: 32,
                objectFit: "cover",
                borderRadius: 4,
                cursor: "pointer",
                flexShrink: 0,
              }}
            />
          </Popover>
        ) : (
          <VideoCameraOutlined style={{ color: "var(--ant-color-primary)", fontSize: 16, flexShrink: 0 }} />
        )}

        <div style={{ display: "flex", flexDirection: "column", minWidth: 0, gap: 2 }}>
          <Link
            href={playUrl}
            target="_blank"
            style={{
              fontWeight: 600,
              fontSize: 13,
              color: "var(--ant-color-text)",
              maxWidth: 210,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {displayName}
          </Link>
          <Space size={6} wrap style={{ fontSize: 11 }}>
            {resourceCat && (
              <Tag color="blue" style={{ fontSize: 10, lineHeight: "16px", padding: "0 4px", margin: 0 }}>
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

  if (resource === "config") return <Tag color="geekblue">源配置同步</Tag>;
  if (resource === "list") return <Tag color="cyan">分类列表</Tag>;
  return <Tag>{resource}</Tag>;
}
