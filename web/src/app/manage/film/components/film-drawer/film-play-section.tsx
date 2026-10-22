"use client";

import React, { useState, useEffect, useMemo, useCallback } from "react";
import { Tag, Form, Input, Space, Tabs, Button, message } from "antd";
import { PlayCircleOutlined, CopyOutlined } from "@ant-design/icons";
import type { PlayLine } from "./types";
import { formatEpisodesToPlayLink } from "./utils";
import styles from "./index.module.less";

const { TextArea } = Input;

interface FilmPlaySectionProps {
  lines?: PlayLine[];
  filmId?: string | number | null;
  ownerSourceId?: string;
}

export default function FilmPlaySection({
  lines = [],
  filmId,
  ownerSourceId = "",
}: FilmPlaySectionProps) {
  const form = Form.useFormInstance();
  const [activeLineKey, setActiveLineKey] = useState<string>("");

  useEffect(() => {
    if (lines.length > 0) {
      const exists = lines.some((l) => (l.id || l.name) === activeLineKey);
      if (!exists) {
        const ownerLine = lines.find(
          (l) =>
            ownerSourceId &&
            (l.sourceId === ownerSourceId || l.id?.startsWith(ownerSourceId)),
        );
        setActiveLineKey(
          ownerLine
            ? ownerLine.id || ownerLine.name || ""
            : lines[0].id || lines[0].name || "",
        );
      }
    } else {
      setActiveLineKey("");
    }
  }, [lines, activeLineKey, ownerSourceId]);

  const activeLine = useMemo(() => {
    if (!lines || lines.length === 0) return null;
    return lines.find((l) => (l.id || l.name) === activeLineKey) || lines[0];
  }, [lines, activeLineKey]);

  const isOwnerLine = useMemo(() => {
    if (!ownerSourceId || lines.length <= 1) return true;
    if (!activeLine) return true;
    return (
      activeLine.sourceId === ownerSourceId ||
      activeLine.id?.startsWith(ownerSourceId) ||
      activeLine.id === ownerSourceId
    );
  }, [activeLine, lines.length, ownerSourceId]);

  const handleCopy = useCallback(
    (text: string) => {
      const content =
        (isOwnerLine ? form?.getFieldValue("playLink") || text : text) || "";
      if (!content) {
        message.info("暂无可复制的链接");
        return;
      }
      navigator.clipboard
        .writeText(content)
        .then(() => message.success("已复制到剪贴板"))
        .catch(() => message.error("复制失败"));
    },
    [form, isOwnerLine],
  );

  const tabItems = useMemo(() => {
    return lines.map((l) => {
      const isOwner =
        ownerSourceId &&
        (l.sourceId === ownerSourceId || l.id?.startsWith(ownerSourceId));
      return {
        key: l.id || l.name,
        label: (
          <span>
            {l.name || l.id} ({l.linkList?.length || 0})
            {isOwner && (
              <Tag
                color="orange"
                style={{
                  marginLeft: 4,
                  borderRadius: 3,
                  fontSize: 11,
                  padding: "0 4px",
                }}
              >
                当前源
              </Tag>
            )}
          </span>
        ),
      };
    });
  }, [lines, ownerSourceId]);

  const episodeCount = activeLine?.linkList?.length || 0;
  const activeFormattedLink = useMemo(() => {
    if (!activeLine) return "";
    return formatEpisodesToPlayLink(activeLine.linkList || []);
  }, [activeLine]);

  return (
    <div className={styles.drawerSectionCard}>
      <div className={styles.drawerSectionTitle}>
        <div className={styles.titleLeft}>
          <PlayCircleOutlined className={styles.sectionIcon} />
          <span>播放资源</span>
        </div>
        <Space size={6}>
          {lines.length > 1 && (
            <Tag color="cyan" style={{ margin: 0, borderRadius: 4 }}>
              {lines.length} 条线路
            </Tag>
          )}
          {episodeCount > 0 && (
            <Tag color="blue" style={{ margin: 0, borderRadius: 4 }}>
              当前共 {episodeCount} 集
            </Tag>
          )}
        </Space>
      </div>

      {tabItems.length > 1 && (
        <Tabs
          activeKey={activeLineKey}
          items={tabItems}
          onChange={setActiveLineKey}
          size="small"
          tabBarStyle={{ marginBottom: 8 }}
        />
      )}

      {activeLine && activeLine.linkList && activeLine.linkList.length > 0 && (
        <div className={styles.episodeList} style={{ marginBottom: 8 }}>
          {activeLine.linkList.map((ep, idx) => (
            <Tag
              key={`${ep.episode}-${idx}`}
              className={styles.episodeTagItem}
              color={isOwnerLine ? "blue" : "default"}
            >
              {ep.episode}
            </Tag>
          ))}
        </div>
      )}

      <div className={styles.playLinkHeader}>
        <Space size={6}>
          <span>播放直链</span>
          {!isOwnerLine && (
            <Tag color="default" style={{ margin: 0, borderRadius: 4 }}>
              聚合线路
            </Tag>
          )}
        </Space>
        <Button
          size="small"
          type="link"
          icon={<CopyOutlined />}
          onClick={() => handleCopy(activeFormattedLink)}
          style={{ padding: 0 }}
        >
          复制直链
        </Button>
      </div>

      {isOwnerLine ? (
        <Form.Item
          name="playLink"
          noStyle
          rules={[{ required: !filmId, message: "请输入播放地址" }]}
        >
          <TextArea
            rows={5}
            placeholder="格式: 第01集$https://example.com/play/1.m3u8#第02集$https://example.com/play/2.m3u8"
            className={styles.editableTextArea}
          />
        </Form.Item>
      ) : (
        <TextArea
          rows={5}
          value={activeFormattedLink}
          readOnly
          className={styles.readonlyTextArea}
        />
      )}
    </div>
  );
}
