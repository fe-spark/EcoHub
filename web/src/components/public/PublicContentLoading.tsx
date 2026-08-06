"use client";

import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useTransition,
} from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import AppLoading from "@/components/public/Loading";
import {
  forceFinishNavigationLoading,
  startNavigationLoading,
} from "@/components/public/TopLoadingBar";
import panelStyles from "./PublicContentLoading.module.less";

type PublicContentLoadingContextValue = {
  active: boolean;
  label: string;
  beginContentLoading: (label?: string) => void;
  endContentLoading: () => void;
  /** 布局级导航：transition 挂在 Provider 上，不会随页面卸载而丢 settle */
  navigate: (href: string, label?: string) => void;
};

const PublicContentLoadingContext =
  createContext<PublicContentLoadingContextValue | null>(null);

/** 统一成 pathname + search（无 hash、无 origin），便于同链比较 */
export function normalizeAppHref(href: string): string {
  const raw = (href || "").trim();
  if (!raw) {
    return "/";
  }
  try {
    if (/^https?:\/\//i.test(raw)) {
      const u = new URL(raw);
      return `${u.pathname}${u.search}` || "/";
    }
  } catch {
    // fall through
  }
  const noHash = raw.split("#")[0] || "/";
  return noHash.startsWith("/") ? noHash : `/${noHash}`;
}

function locationKey(pathname: string, search: string): string {
  const q = search.startsWith("?") ? search.slice(1) : search;
  return q ? `${pathname}?${q}` : pathname;
}

export function usePublicContentLoading() {
  const ctx = useContext(PublicContentLoadingContext);
  if (!ctx) {
    throw new Error(
      "usePublicContentLoading must be used within PublicContentLoadingProvider",
    );
  }
  return ctx;
}

/** 可选：页面内链接未包 Provider 时不抛错 */
export function usePublicContentLoadingOptional() {
  return useContext(PublicContentLoadingContext);
}

export function PublicContentLoadingProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [active, setActive] = useState(false);
  const [label, setLabel] = useState("页面加载中");
  const [isPending, startTransition] = useTransition();

  /** 是否处于「由 Provider.navigate 发起」的会话 */
  const sessionRef = useRef(false);
  const pendingSeenRef = useRef(false);
  /** 目标 href（规范化后）；并发 navigate 只认最新目标 */
  const targetHrefRef = useRef<string | null>(null);
  /** 会话代次：仅最新一次有权 end，避免二次 navigate 误 settle */
  const sessionGenRef = useRef(0);

  const endContentLoading = useCallback((gen?: number) => {
    if (gen !== undefined && gen !== sessionGenRef.current) {
      return;
    }
    sessionRef.current = false;
    pendingSeenRef.current = false;
    targetHrefRef.current = null;
    setActive(false);
    forceFinishNavigationLoading();
  }, []);

  const beginContentLoading = useCallback((nextLabel?: string) => {
    setLabel(nextLabel?.trim() || "页面加载中");
    setActive(true);
  }, []);

  const currentKey = locationKey(pathname, searchParams.toString());

  /**
   * 路径已到达目标 → 收尾（不依赖 isPending 是否曾为 true）。
   */
  useEffect(() => {
    if (!sessionRef.current || !targetHrefRef.current) {
      return;
    }
    if (currentKey === targetHrefRef.current) {
      endContentLoading(sessionGenRef.current);
    }
  }, [currentKey, endContentLoading]);

  /**
   * Provider 级 transition 收尾。
   * 页面内若自持 useTransition，begin 后主内容卸载会丢 settle；故挂在布局 Provider。
   * 到达判定以 pathname/search 为准；pending 仅作辅助。
   */
  useEffect(() => {
    if (!sessionRef.current) {
      return;
    }

    if (isPending) {
      pendingSeenRef.current = true;
      return;
    }

    if (!pendingSeenRef.current) {
      return;
    }

    // 已见过 pending 再变 false：若已到目标则 end；否则等路径 effect / 超时
    if (targetHrefRef.current && currentKey === targetHrefRef.current) {
      endContentLoading(sessionGenRef.current);
    }
  }, [isPending, currentKey, endContentLoading]);

  // 极端兜底：active 超过 15s 强制结束
  useEffect(() => {
    if (!active) {
      return;
    }
    const gen = sessionGenRef.current;
    const timer = window.setTimeout(() => {
      endContentLoading(gen);
    }, 15_000);
    return () => window.clearTimeout(timer);
  }, [active, endContentLoading]);

  const navigate = useCallback(
    (href: string, nextLabel: string = "页面加载中") => {
      const target = normalizeAppHref(href);
      const current = locationKey(pathname, searchParams.toString());

      // 同链短路：不隐藏主内容、不启 loading
      if (target === current) {
        return;
      }

      // 已在去同一目标：忽略重复点击
      if (sessionRef.current && targetHrefRef.current === target) {
        return;
      }

      const gen = ++sessionGenRef.current;
      sessionRef.current = true;
      // 仍在 transition 中时保留 pendingSeen，避免二次 push 重置后误卡
      if (!isPending) {
        pendingSeenRef.current = false;
      }
      targetHrefRef.current = target;
      beginContentLoading(nextLabel);
      startNavigationLoading(nextLabel);
      startTransition(() => {
        // gen 被更新则说明有更新的 navigate，仍 push 最新即可
        if (gen !== sessionGenRef.current) {
          return;
        }
        router.push(href);
      });
    },
    [beginContentLoading, isPending, pathname, router, searchParams],
  );

  const value = useMemo(
    () => ({
      active,
      label,
      beginContentLoading,
      endContentLoading: () => endContentLoading(),
      navigate,
    }),
    [active, label, beginContentLoading, endContentLoading, navigate],
  );

  return (
    <PublicContentLoadingContext.Provider value={value}>
      {children}
    </PublicContentLoadingContext.Provider>
  );
}

const SLOW_LOADING_MS = 10_000;

/**
 * 主内容区实心 loading（替换内容，非遮罩）。
 * - fill：作为 flex 子项撑满父级
 * - 持续超过 10s 显示「刷新页面」
 */
export function PublicContentLoadingPanel({
  label = "页面加载中",
  fill = false,
  minHeight,
  className,
}: {
  label?: string;
  fill?: boolean;
  minHeight?: string | number;
  className?: string;
}) {
  const [showRefresh, setShowRefresh] = useState(false);

  useEffect(() => {
    setShowRefresh(false);
    const timer = window.setTimeout(() => {
      setShowRefresh(true);
    }, SLOW_LOADING_MS);
    return () => window.clearTimeout(timer);
  }, [label]);

  const handleRefresh = () => {
    window.location.reload();
  };

  return (
    <div
      className={[
        panelStyles.panel,
        fill ? panelStyles.fill : "",
        className || "",
      ]
        .filter(Boolean)
        .join(" ")}
      role="status"
      aria-live="polite"
      aria-busy="true"
      style={{
        minHeight: fill ? undefined : (minHeight ?? "48vh"),
      }}
    >
      <div className={panelStyles.stack}>
        <AppLoading text={label} padding="8px 0 0" showHints={!showRefresh} />

        {showRefresh ? (
          <div className={panelStyles.slowCard}>
            <p className={panelStyles.slowTip}>
              加载时间较长，网络可能不稳定
              <br />
              可以刷新页面重试
            </p>
            <button
              type="button"
              className={panelStyles.refreshBtn}
              onClick={handleRefresh}
            >
              刷新页面
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}

/**
 * 页面内跳转：委托给布局级 Provider.navigate，避免页面卸载后丢 settle。
 */
export function useContentNavigate() {
  const ctx = usePublicContentLoadingOptional();
  const router = useRouter();

  const navigate = useCallback(
    (href: string, label: string = "页面加载中") => {
      if (ctx?.navigate) {
        ctx.navigate(href, label);
        return;
      }
      // 无 Provider（如后台）时用 router，避免整页硬跳
      router.push(href);
    },
    [ctx, router],
  );

  return { navigate, isNavigating: ctx?.active ?? false };
}
