"use client";

import React, { useState, useEffect, useMemo } from "react";
import { Tag, Form, Input, Space, Tabs, type FormInstance } from "antd";
import { PlayCircleOutlined } from "@ant-design/icons";
import type { PlayLine } from "./types";
import { formatEpisodesToPlayLink } from "./utils";
import styles from "./index.module.less";

const { TextArea } = Input;

interface FilmPlaySectionProps {
  form: FormInstance;
  lines?: PlayLine[];
  filmId?: string;
}

export default function FilmPlaySection({ form, lines = [], filmId }: FilmPlaySectionProps) {
  const [activeLineKey, setActiveLineKey] = useState<string>("");

  useEffect(() => {
    if (lines.length > 0) {
      setActiveLineKey(lines[0].id || lines[0].name || "0");
    } else {
      setActiveLineKey("");
    }
  }, [lines]);

  const activeLine = useMemo(() => {
    if (!lines || lines.length === 0) return null;
    return lines.find((l) => (l.id || l.name) === activeLineKey) || lines[0];
  }, [lines, activeLineKey]);

  const handleLineSwitch = (key: string) => {
    setActiveLineKey(key);
    const target = lines.find((l) => (l.id || l.name) === key);
    if (target && target.linkList && target.linkList.length > 0) {
      form.setFieldValue("playLink", formatEpisodesToPlayLink(target.linkList));
    }
  };

  const tabItems = useMemo(() => {
    return lines.map((l) => ({
      key: l.id || l.name,
      label: `${l.name || l.id} (${l.linkList?.length || 0})`,
    }));
  }, [lines]);

  const episodeCount = activeLine?.linkList?.length || 0;

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
          onChange={handleLineSwitch}
          size="small"
          tabBarStyle={{ marginBottom: 0 }}
        />
      )}

      {activeLine && activeLine.linkList && activeLine.linkList.length > 0 && (
        <div className={styles.episodeList}>
          {activeLine.linkList.map((ep, idx) => (
            <Tag
              key={`${ep.episode}-${idx}`}
              className={styles.episodeTagItem}
              color="default"
            >
              {ep.episode}
            </Tag>
          ))}
        </div>
      )}

      <Form.Item
        name="playLink"
        label="播放直链"
        className={styles.formItemReset}
        rules={[{ required: !filmId, message: "请输入播放地址" }]}
      >
        <TextArea
          rows={5}
          placeholder="格式: 第01集$https://example.com/play/1.m3u8#第02集$https://example.com/play/2.m3u8"
          style={{ resize: "vertical", fontFamily: "monospace", fontSize: 13 }}
        />
      </Form.Item>
    </div>
  );
}
