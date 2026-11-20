"use client";

import React, { createContext, useCallback, useContext, useEffect, useState } from "react";
import { ApiGet, ApiPut } from "@/lib/client-api";

export interface PrimarySourceInfo {
  id: string;
  name: string;
  state: boolean;
}

export interface DeletingPrimaryInfo {
  id: string;
  name: string;
}

type PrimaryStateResponse = {
  id?: string;
  name?: string;
  state?: boolean;
  deletingSource?: DeletingPrimaryInfo | null;
} | null;

interface ManagePrimaryValue {
  primary: PrimarySourceInfo | null;
  deletingPrimary: DeletingPrimaryInfo | null;
  loading: boolean;
  switching: boolean;
  refresh: () => Promise<void>;
  switchPrimary: (id: string) => Promise<{ ok: boolean; msg: string }>;
}

const ManagePrimaryContext = createContext<ManagePrimaryValue>({
  primary: null,
  deletingPrimary: null,
  loading: true,
  switching: false,
  refresh: async () => {},
  switchPrimary: async () => ({ ok: false, msg: "" }),
});

/** 后台全局主站上下文：主站为服务端持久化状态，切换后全站生效。 */
export function usePrimarySource() {
  return useContext(ManagePrimaryContext);
}

export function ManagePrimaryProvider({ children }: { children: React.ReactNode }) {
  const [primary, setPrimary] = useState<PrimarySourceInfo | null>(null);
  const [deletingPrimary, setDeletingPrimary] = useState<DeletingPrimaryInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [switching, setSwitching] = useState(false);

  const applyPrimaryState = useCallback((data: PrimaryStateResponse | undefined) => {
    if (data?.id) {
      setPrimary({ id: data.id, name: String(data.name ?? ""), state: Boolean(data.state) });
      setDeletingPrimary(null);
    } else {
      setPrimary(null);
      setDeletingPrimary(data?.deletingSource ?? null);
    }
  }, []);

  const refresh = useCallback(async () => {
    try {
      const resp = await ApiGet<PrimaryStateResponse>("/manage/primary");
      if (resp.code === 0) {
        applyPrimaryState(resp.data);
      }
    } finally {
      setLoading(false);
    }
  }, [applyPrimaryState]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // 主站是服务端全局状态，其他管理员切换后也要同步到当前后台会话。
  useEffect(() => {
    const sync = () => {
      if (document.visibilityState === "visible") {
        void refresh();
      }
    };
    const timer = window.setInterval(sync, 15000);
    window.addEventListener("focus", sync);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", sync);
    };
  }, [refresh]);

  const switchPrimary = useCallback(async (id: string) => {
    setSwitching(true);
    try {
      const resp = await ApiPut<PrimaryStateResponse>("/manage/primary", { id });
      if (resp.code === 0) {
        applyPrimaryState(resp.data);
        return { ok: true, msg: resp.msg };
      }
      return { ok: false, msg: resp.msg || "切换主站失败" };
    } catch {
      return { ok: false, msg: "切换主站失败" };
    } finally {
      setSwitching(false);
    }
  }, [applyPrimaryState]);

  return (
    <ManagePrimaryContext.Provider
      value={{ primary, deletingPrimary, loading, switching, refresh, switchPrimary }}
    >
      {children}
    </ManagePrimaryContext.Provider>
  );
}
