"use client";

import React, { useCallback, useEffect, useState } from "react";
import { Modal, Spin, Empty, Pagination, Button, Input } from "antd";
import { CheckCircleFilled } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import styles from "./index.module.less";

interface PickerItem {
  ID: number;
  link: string;
  name: string;
  fid: string;
}

interface ImagePickerProps {
  open: boolean;
  title?: string;
  onCancel: () => void;
  onSelect: (link: string) => void;
}

export default function ImagePicker({
  open,
  title = "从素材中心选择图片",
  onCancel,
  onSelect,
}: ImagePickerProps) {
  const [list, setList] = useState<PickerItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState("");
  const [keyword, setKeyword] = useState("");
  const [inputValue, setInputValue] = useState("");
  const [page, setPage] = useState({ current: 1, pageSize: 24, total: 0 });

  const fetchList = useCallback(
    async (current = 1, name = keyword) => {
      setLoading(true);
      try {
        const resp = await ApiGet("/manage/file/list", {
          current,
          pageSize: 24,
          name,
        });
        if (resp.code === 0) {
          setList(resp.data.list || []);
          if (resp.data.page) {
            setPage({
              current: resp.data.page.current,
              pageSize: resp.data.page.pageSize || 24,
              total: resp.data.page.total || 0,
            });
          }
        }
      } finally {
        setLoading(false);
      }
    },
    [keyword],
  );

  useEffect(() => {
    if (open) {
      setSelected("");
      setInputValue("");
      setKeyword("");
      fetchList(1, "");
    }
    // 仅在弹窗开关变化时初始化，避免搜索后列表被重置
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const handleSearch = (value: string) => {
    const kw = value.trim();
    setKeyword(kw);
    fetchList(1, kw);
  };

  return (
    <Modal
      open={open}
      title={title}
      onCancel={onCancel}
      width={860}
      styles={{ body: { paddingTop: 8 } }}
      footer={[
        <Button key="cancel" onClick={onCancel}>
          取消
        </Button>,
        <Button
          key="confirm"
          type="primary"
          disabled={!selected}
          onClick={() => onSelect(selected)}
        >
          使用此图片
        </Button>,
      ]}
    >
      <Input.Search
        placeholder="搜索素材名称"
        allowClear
        value={inputValue}
        onChange={(e) => {
          setInputValue(e.target.value);
          if (!e.target.value.trim()) {
            handleSearch("");
          }
        }}
        onSearch={handleSearch}
        style={{ width: 240, marginBottom: 12 }}
      />
      <Spin spinning={loading}>
        {list.length > 0 ? (
          <div className={styles.grid}>
            {list.map((item) => (
              <div
                key={item.ID}
                className={`${styles.card} ${
                  selected === item.link ? styles.cardActive : ""
                }`}
                onClick={() => setSelected(item.link)}
              >
                {/* 素材中心缩略图沿用原生 img，避免受预览组影响 */}
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={item.link}
                  alt="素材缩略图"
                  className={styles.thumb}
                />
                {(item.name || item.fid) && (
                  <div
                    className={styles.cardName}
                    title={item.name || item.fid}
                  >
                    {item.name || item.fid}
                  </div>
                )}
                {selected === item.link && (
                  <CheckCircleFilled className={styles.check} />
                )}
              </div>
            ))}
          </div>
        ) : (
          !loading && (
            <Empty
              description={
                keyword
                  ? `未找到名称包含「${keyword}」的素材`
                  : "素材库暂无图片，请先到素材中心上传"
              }
            />
          )
        )}
      </Spin>
      {page.total > page.pageSize && (
        <div className={styles.pagination}>
          <Pagination
            current={page.current}
            pageSize={page.pageSize}
            total={page.total}
            onChange={(p) => fetchList(p, keyword)}
            showSizeChanger={false}
            size="small"
          />
        </div>
      )}
    </Modal>
  );
}
