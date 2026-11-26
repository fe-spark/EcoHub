"use client";

import React, { useState } from "react";
import { Button, Drawer, Empty, Space, Tag } from "antd";
import { DeleteOutlined, UndoOutlined } from "@ant-design/icons";
import { ApiPut } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { QuickStrategy } from "./banner-quick-config";
import { RecommendPreviewGroup } from "./recommend-quick-modal";

const STRATEGY_LABEL: Record<QuickStrategy, string> = {
  latest: "最近更新",
  hot: "最热排片",
  score: "评分优先",
  douban: "豆瓣推荐",
};

interface RecommendQuickDrawerProps {
  open: boolean;
  sourceId: string;
  strategy: QuickStrategy;
  previewGroups: RecommendPreviewGroup[];
  onChange: (next: RecommendPreviewGroup[]) => void;
  onRedo: () => void;
  onClose: () => void;
  onSuccess: () => void;
}

export default function RecommendQuickDrawer({
  open,
  sourceId,
  strategy,
  previewGroups,
  onChange,
  onRedo,
  onClose,
  onSuccess,
}: RecommendQuickDrawerProps) {
  const { message } = useAppMessage();
  const [publishing, setPublishing] = useState(false);

  const totalItems = previewGroups.reduce((sum, g) => sum + g.items.length, 0);

  const handleRemoveItem = (groupId: string, mid: number) => {
    onChange(
      previewGroups.map((g) =>
        g.groupId === groupId
          ? { ...g, items: g.items.filter((it) => it.mid !== mid) }
          : g,
      ),
    );
  };

  const handlePublish = async () => {
    if (previewGroups.length === 0) return;
    setPublishing(true);
    try {
      const res = await ApiPut("/manage/recommends/deck", {
        sourceId,
        groups: previewGroups.map((gp) => ({
          groupId: gp.groupId,
          items: gp.items.map((it) => ({
            mid: it.mid,
            doubanScore: it.doubanScore || "",
          })),
        })),
      });
      if (res.code === 0) {
        message.success("已发布到首页推荐");
        onSuccess();
      } else {
        message.error(res.msg || "发布失败");
      }
    } catch {
      message.error("发布失败");
    } finally {
      setPublishing(false);
    }
  };

  return (
    <Drawer
      open={open}
      title={
        <Space>
          <span>排片预览</span>
          <Tag color="blue">{STRATEGY_LABEL[strategy] || strategy}</Tag>
          <Tag>共 {totalItems} 部</Tag>
        </Space>
      }
      width={720}
      onClose={onClose}
      footer={
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <Button icon={<UndoOutlined />} disabled={publishing} onClick={onRedo}>
            重新配置
          </Button>
          <Space>
            <Button disabled={publishing} onClick={onClose}>
              取消
            </Button>
            <Button
              type="primary"
              loading={publishing}
              disabled={totalItems === 0}
              onClick={() => void handlePublish()}
            >
              确认发布
            </Button>
          </Space>
        </div>
      }
    >
      {previewGroups.length === 0 ? (
        <Empty description="暂无排片结果，可返回重新配置" />
      ) : (
        <Space direction="vertical" size={18} style={{ width: "100%" }}>
          {previewGroups.map((gp) => (
            <div key={gp.groupId}>
              <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 10 }}>
                {gp.title}（{gp.items.length} 部）
              </div>
              {gp.items.length === 0 ? (
                <div style={{ opacity: 0.6, fontSize: 12 }}>该分组暂无命中影片</div>
              ) : (
                <div
                  style={{
                    display: "grid",
                    gridTemplateColumns: "repeat(auto-fill, minmax(120px, 1fr))",
                    gap: 10,
                  }}
                >
                  {gp.items.map((it, idx) => (
                    <div
                      key={`${gp.groupId}-${it.mid}`}
                      style={{
                        border: "1px solid var(--ant-color-border-secondary)",
                        borderRadius: 6,
                        overflow: "hidden",
                        background: "var(--ant-color-bg-container)",
                      }}
                    >
                      <div style={{ position: "relative", aspectRatio: "3 / 4", background: "#141414" }}>
                        {it.picture ? (
                          /* eslint-disable-next-line @next/next/no-img-element */
                          <img
                            src={it.picture}
                            alt={it.name}
                            style={{ width: "100%", height: "100%", objectFit: "cover", display: "block" }}
                          />
                        ) : null}
                        <Tag style={{ position: "absolute", top: 4, left: 4, margin: 0 }}>
                          {idx + 1}
                        </Tag>
                        {it.doubanScore ? (
                          <Tag color="gold" style={{ position: "absolute", top: 4, right: 4, margin: 0 }}>
                            ★{it.doubanScore}
                          </Tag>
                        ) : null}
                      </div>
                      <div style={{ padding: "6px 8px", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 4 }}>
                        <span
                          title={it.name}
                          style={{
                            fontSize: 12,
                            fontWeight: 500,
                            whiteSpace: "nowrap",
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                          }}
                        >
                          {it.name}
                        </span>
                        <Button
                          size="small"
                          type="text"
                          danger
                          icon={<DeleteOutlined />}
                          onClick={() => handleRemoveItem(gp.groupId, it.mid)}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </Space>
      )}
    </Drawer>
  );
}

