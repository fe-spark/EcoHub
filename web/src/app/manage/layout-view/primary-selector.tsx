"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { SwapOutlined } from "@ant-design/icons";
import { Select, Tag } from "antd";
import { ApiGet } from "@/lib/client-api";
import { usePrimarySource } from "@/lib/manage-primary";
import { useAppMessage } from "@/lib/useAppMessage";

type SourceOption = { id: string; name: string };

/** 后台顶栏主站选择器：展示当前主站，仅超管可切换（D4）。 */
export default function PrimarySelector({ isAdmin }: { isAdmin?: boolean }) {
  const { primary, deletingPrimary, loading, switching, refresh, switchPrimary } = usePrimarySource();
  const { message, modal } = useAppMessage();
  const [options, setOptions] = useState<SourceOption[]>([]);

  const loadOptions = useCallback(() => {
    return ApiGet("/manage/collect/options")
      .then((resp) => {
        if (resp.code === 0 && Array.isArray(resp.data)) {
          setOptions(resp.data as SourceOption[]);
        }
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (isAdmin === true) void loadOptions();
  }, [isAdmin, loadOptions]);

  const displayPrimary = primary ?? deletingPrimary;
  const isDeletingPrimary = Boolean(deletingPrimary);

  const selectOptions = useMemo(() => {
    const list = [...options];
    if (displayPrimary && !list.some((item) => item.id === displayPrimary.id)) {
      list.unshift({ id: displayPrimary.id, name: displayPrimary.name });
    }
    return list.map((item) => ({ value: item.id, label: item.name }));
  }, [displayPrimary, options]);

  const askSwitch = useCallback(
    (id: string) => {
      const candidateList = primary ? [primary, ...options] : options;
      const chosen = candidateList.find((item) => item.id === id);
      if (!chosen) {
        return;
      }
      modal.confirm({
        title: `切换主站至「${chosen.name}」？`,
        okText: "切换",
        cancelText: "取消",
        centered: true,
        onOk: async () => {
          const result = await switchPrimary(id);
          if (result.ok) {
            message.success(`主站：${chosen.name}`);
          } else {
            message.error(result.msg);
            void refresh();
          }
        },
      });
    },
    [message, modal, options, primary, refresh, switchPrimary],
  );

  if (isAdmin === false && !loading) {
    if (!displayPrimary) {
      return <Tag color="warning">未配置主站</Tag>;
    }
    return <Tag color="gold">当前主站：{displayPrimary.name}</Tag>;
  }

  return (
    <Select
      value={displayPrimary?.id}
      variant="filled"
      prefix={
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <SwapOutlined />
          <span>当前主站</span>
          <span
            aria-hidden="true"
            style={{
              display: "inline-block",
              width: 1,
              height: 12,
              marginInlineStart: 2,
              background: "var(--ant-color-border-secondary)",
            }}
          />
        </span>
      }
      styles={{
        root: {
          borderRadius: 8,
          minHeight: 32,
          paddingBlock: 0,
          alignItems: "center",
        },
        prefix: {
          color: "var(--ant-color-text-tertiary)",
          display: "inline-flex",
          alignItems: "center",
          lineHeight: 1.25,
          marginInlineEnd: 8,
        },
        content: {
          fontWeight: 500,
          lineHeight: 1.25,
          alignItems: "center",
        },
        placeholder: {
          lineHeight: 1.25,
        },
      }}
      placeholder={loading ? undefined : "未配置"}
      loading={loading || switching || isDeletingPrimary}
      disabled={isAdmin !== true || loading || switching || isDeletingPrimary}
      popupMatchSelectWidth={false}
      options={selectOptions}
      onOpenChange={(open) => {
        if (open) void loadOptions();
      }}
      onChange={(id) => askSwitch(String(id))}
    />
  );
}
