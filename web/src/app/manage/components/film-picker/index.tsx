"use client";

import React, { useCallback, useEffect, useState, useMemo } from "react";
import {
  Modal,
  Table,
  Input,
  Select,
  Button,
  Tag,
  Typography,
  Flex,
  Space,
  Image as AntImage,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { VideoCameraOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { FilmOption } from "@/app/manage/banners/view/types";
import styles from "./index.module.less";

const { Text } = Typography;

interface FilmPickerProps {
  open: boolean;
  title?: string;
  selectedMid?: number;
  onCancel: () => void;
  onSelect: (film: FilmOption) => void;
}

export default function FilmPicker({
  open,
  title = "选择影片",
  selectedMid,
  onCancel,
  onSelect,
}: FilmPickerProps) {
  const [list, setList] = useState<FilmOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedFilm, setSelectedFilm] = useState<FilmOption | null>(null);
  const [keyword, setKeyword] = useState("");
  const [inputValue, setInputValue] = useState("");
  const [sortField, setSortField] = useState<string>("");
  const [page, setPage] = useState({ current: 1, pageSize: 10, total: 0 });

  const fetchList = useCallback(
    async (current = 1, query = "", sort = "") => {
      setLoading(true);
      const trimmed = query.trim();
      try {
        let rawList: any[] = [];
        let total = 0;
        let curPage = current;
        const pSize = 10;

        if (!trimmed) {
          // 无关键词时，加载片库最近影片
          const resp = await ApiGet("/manage/film/search/list", {
            current,
            pageSize: pSize,
          });
          if (resp?.code === 0 && resp.data) {
            rawList = resp.data.list || [];
            if (resp.data.params?.paging) {
              curPage = resp.data.params.paging.current || current;
              total = resp.data.params.paging.total || 0;
            }
          }
        } else {
          // 有关键词时，优先使用全局智能模糊检索
          const resp = await ApiGet("/films/search", {
            keyword: trimmed,
            current,
            pageSize: pSize,
            sort: sort || undefined,
          });
          if (resp?.code === 0 && resp.data && (resp.data.list || []).length > 0) {
            rawList = resp.data.list || [];
            if (resp.data.page) {
              curPage = resp.data.page.current || current;
              total = resp.data.page.total || 0;
            }
          } else {
            // 兜底查片库管理检索接口
            const manageResp = await ApiGet("/manage/film/search/list", {
              name: trimmed,
              current,
              pageSize: pSize,
            });
            if (manageResp?.code === 0 && manageResp.data) {
              rawList = manageResp.data.list || [];
              if (manageResp.data.params?.paging) {
                curPage = manageResp.data.params.paging.current || current;
                total = manageResp.data.params.paging.total || 0;
              }
            }
          }
        }

        setList(
          rawList.map((item: any) => ({
            id: Number(item.mid || item.ID || item.id || 0),
            name: item.name || "",
            cName: item.cName || "",
            year: item.year && Number(item.year) > 0 ? String(item.year) : (item.year || ""),
            remarks: item.remarks || "",
            picture: item.picture || "",
            pictureSlide: item.pictureSlide || "",
            area: item.area || "",
            director: item.director || "",
            actor: item.actor || "",
            score: Number(item.score) || 0,
            hits: Number(item.hits) || 0,
            label: item.name || "",
            value: Number(item.mid || item.ID || item.id || 0),
            isScraped: Boolean(item.isScraped),
          })),
        );
        setPage({ current: curPage, pageSize: pSize, total });
      } catch {
        setList([]);
        setPage((p) => ({ ...p, total: 0 }));
      } finally {
        setLoading(false);
      }
    },
    [],
  );

  useEffect(() => {
    if (open) {
      setSelectedFilm(null);
      setInputValue("");
      setKeyword("");
      setSortField("");
      setList([]);
      fetchList(1, "", "");
    }
  }, [open, fetchList]);

  useEffect(() => {
    if (selectedMid && list.length > 0 && !selectedFilm) {
      const match = list.find((item) => Number(item.id) === Number(selectedMid));
      if (match) {
        setSelectedFilm(match);
      }
    }
  }, [selectedMid, list, selectedFilm]);

  const handleSearch = (value: string) => {
    const kw = value.trim();
    setKeyword(kw);
    setInputValue(value);
    setList([]);
    fetchList(1, kw, sortField);
  };

  const handleSortChange = (newSort: string) => {
    setSortField(newSort);
    fetchList(1, keyword, newSort);
  };

  const handleConfirm = () => {
    if (selectedFilm) {
      onSelect(selectedFilm);
    }
  };

  const columns = useMemo<ColumnsType<FilmOption>>(
    () => [
      {
        title: "影片",
        key: "info",
        ellipsis: true,
        render: (_, record) => (
          <Flex gap={12} align="center">
            <AntImage
              src={record.picture || FALLBACK_IMG}
              width={38}
              height={52}
              style={{ objectFit: "cover", borderRadius: 6, background: "var(--public-surface-3)" }}
              fallback={FALLBACK_IMG}
              preview={false}
            />
            <Space orientation="vertical" size={2} style={{ minWidth: 0, flex: 1 }}>
              <Flex align="center" gap={6}>
                <Text strong ellipsis={{ tooltip: record.name }} style={{ maxWidth: 210, fontSize: 13 }}>
                  {record.name}
                </Text>
                {record.isScraped && (
                  <Tag color="#722ed1" style={{ borderRadius: 4, margin: 0, fontSize: 11, padding: "0 4px", lineHeight: "18px" }}>
                    已刮削
                  </Tag>
                )}
              </Flex>
              <Text type="secondary" style={{ fontSize: 12 }}>
                {[record.cName, record.year, record.area].filter(Boolean).join(" · ")}
              </Text>
            </Space>
          </Flex>
        ),
      },
      {
        title: "状态",
        dataIndex: "remarks",
        key: "remarks",
        width: 130,
        render: (v) => <Text type="secondary" ellipsis={{ tooltip: v || "正片" }}>{v || "正片"}</Text>,
      },
      {
        title: "演职员",
        key: "crew",
        width: 160,
        render: (_, r) => {
          const info = [r.director ? `导: ${r.director}` : "", r.actor ? `演: ${r.actor}` : ""].filter(Boolean).join(" | ");
          return <Text type="secondary" ellipsis={{ tooltip: info || "暂无" }}>{info || "暂无"}</Text>;
        },
      },
      {
        title: "评分",
        dataIndex: "score",
        key: "score",
        width: 75,
        align: "center",
        render: (v) => (
          <Text strong style={{ color: "var(--ant-color-primary)", fontSize: 13 }}>
            {Number(v) > 0 ? Number(v).toFixed(1) : "-"}
          </Text>
        ),
      },
      {
        title: "操作",
        key: "action",
        width: 75,
        align: "center",
        render: (_, record) => (
          <Button
            type={selectedFilm?.id === record.id ? "primary" : "default"}
            size="small"
            onClick={(e) => {
              e.stopPropagation();
              setSelectedFilm(record);
              onSelect(record);
            }}
          >
            {selectedFilm?.id === record.id ? "已选" : "选择"}
          </Button>
        ),
      },
    ],
    [selectedFilm, onSelect],
  );

  return (
    <Modal
      open={open}
      title={
        <Space size={8}>
          <VideoCameraOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>{title}</span>
        </Space>
      }
      onCancel={onCancel}
      width={820}
      centered
      styles={{ body: { paddingTop: 10, paddingBottom: 10 } }}
      footer={
        <div className={styles.footerContainer}>
          <div style={{ minWidth: 0 }}>
            {selectedFilm ? (
              <Flex align="center" gap={6}>
                <Text type="secondary" style={{ fontSize: 13, flexShrink: 0 }}>已选：</Text>
                <Tag color="orange" style={{ margin: 0, fontSize: 12 }}>
                  {selectedFilm.name} ({[selectedFilm.cName, selectedFilm.year].filter(Boolean).join(" · ")})
                </Tag>
              </Flex>
            ) : null}
          </div>
          <Space size={8}>
            <Button onClick={onCancel}>取消</Button>
            <Button type="primary" disabled={!selectedFilm} onClick={handleConfirm}>
              确定选择
            </Button>
          </Space>
        </div>
      }
      destroyOnHidden
    >
      <div className={styles.toolbar}>
        <Input.Search
          placeholder="搜索片名、演职员..."
          allowClear
          value={inputValue}
          onChange={(e) => {
            setInputValue(e.target.value);
            if (!e.target.value.trim() && keyword) {
              handleSearch("");
            }
          }}
          onSearch={handleSearch}
          style={{ width: 280 }}
        />

        <Select
          value={sortField}
          onChange={handleSortChange}
          style={{ width: 110 }}
          options={[
            { label: "综合排序", value: "" },
            { label: "最新更新", value: "update_stamp" },
            { label: "播放热度", value: "hits" },
            { label: "豆瓣评分", value: "score" },
            { label: "上映年份", value: "year" },
          ]}
        />
      </div>

      <div className={styles.tableWrapper}>
        <Table<FilmOption>
          rowKey="id"
          size="middle"
          columns={columns}
          dataSource={list}
          loading={loading}
          scroll={{ y: 460 }}
          rowSelection={{
            type: "radio",
            selectedRowKeys: selectedFilm ? [selectedFilm.id] : (selectedMid ? [selectedMid] : []),
            onSelect: (record) => setSelectedFilm(record),
          }}
          onRow={(record) => ({
            onClick: () => setSelectedFilm(record),
            onDoubleClick: () => {
              setSelectedFilm(record);
              onSelect(record);
            },
          })}
          pagination={{
            current: page.current,
            pageSize: page.pageSize,
            total: page.total,
            size: "small",
            showSizeChanger: false,
            showTotal: (total) => `共 ${total} 部`,
            onChange: (p) => fetchList(p, keyword, sortField),
          }}
        />
      </div>
    </Modal>
  );
}
