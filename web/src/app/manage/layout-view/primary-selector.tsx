"use client";

import { useCallback, useEffect, useState } from "react";
import { Select, Tag } from "antd";
import { ApiGet } from "@/lib/client-api";
import { usePrimarySource } from "@/lib/manage-primary";
import { useAppMessage } from "@/lib/useAppMessage";

type SourceOption = { id: string; name: string };

/** 后台顶栏主站选择器：仅超管可切换（D4），其余只读展示。 */
export default function PrimarySelector({ isAdmin }: { isAdmin: boolean }) {
  const { primary, loading, switching, refresh, switchPrimary } = usePrimarySource();
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
    if (isAdmin) void loadOptions();
  }, [isAdmin, loadOptions]);

  const askSwitch = useCallback(
    (id: string) => {
      const chosen = options.find((item) => item.id === id);
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
    [message, modal, options, refresh, switchPrimary],
  );

  if (loading) {
    return <Tag color="gold">主站加载中…</Tag>;
  }
  if (!primary) {
    return <Tag color="warning">未配置主站</Tag>;
  }
  if (!isAdmin) {
    return <Tag color="gold">{primary.name}</Tag>;
  }
  return (
    <Select
      value={primary.id}
      loading={switching}
      disabled={switching}
      popupMatchSelectWidth={false}
      options={options.map((item) => ({ value: item.id, label: item.name }))}
      onOpenChange={(open) => {
        if (open) void loadOptions();
      }}
      onChange={(id) => askSwitch(String(id))}
    />
  );
}
