"use client";

import React, { createContext, useContext, useEffect, useMemo, useState, useCallback } from "react";
import { usePathname } from "next/navigation";
import { App, ConfigProvider, theme } from "antd";
import zhCN from "antd/locale/zh_CN";
import dayjs from "dayjs";
import "dayjs/locale/zh-cn";
import ThemeDock, { type ThemeMode } from "./ThemeDock";

const STORAGE_KEY = "app-theme";
const DEFAULT_PRIMARY_COLOR = "#fa8c16";

type ThemeContextValue = {
  mode: ThemeMode;
  effective: "dark" | "light";
  setMode: (mode: ThemeMode) => void;
};

const ThemeModeContext = createContext<ThemeContextValue | null>(null);

export function useThemeMode() {
  const context = useContext(ThemeModeContext);
  if (!context) {
    throw new Error("useThemeMode must be used within GlobalThemeProvider");
  }
  return context;
}

function resolveEffective(mode: ThemeMode): "dark" | "light" {
  if (mode !== "system") return mode;
  if (typeof window === "undefined") return "dark";
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

function getSavedMode(): ThemeMode {
  if (typeof window === "undefined") return "system";
  const saved = localStorage.getItem(STORAGE_KEY);
  if (saved === "dark" || saved === "light" || saved === "system") return saved;
  return "system";
}

export default function GlobalThemeProvider({
  children,
  fontFamily,
}: {
  children: React.ReactNode;
  fontFamily: string;
}) {
  const pathname = usePathname();
  const [mode, setMode] = useState<ThemeMode>("system");
  const [effective, setEffective] = useState<"dark" | "light">("dark");
  const [mounted, setMounted] = useState(false);

  // 避免 SSR/CSR Hydration 不一致，先用默认值，挂载后同步 localStorage
  useEffect(() => {
    dayjs.locale("zh-cn");
    setMounted(true);
    const saved = getSavedMode();
    setMode(saved);
    setEffective(resolveEffective(saved));
  }, []);

  // 监听系统主题变化（仅 system 模式）
  useEffect(() => {
    const mql = window.matchMedia("(prefers-color-scheme: light)");
    const handler = () => {
      if (mode === "system") setEffective(resolveEffective("system"));
    };
    mql.addEventListener("change", handler);
    return () => mql.removeEventListener("change", handler);
  }, [mode]);

  useEffect(() => {
    setEffective(resolveEffective(mode));
    localStorage.setItem(STORAGE_KEY, mode);
  }, [mode]);

  useEffect(() => {
    document.documentElement.dataset.theme = effective;
    document.documentElement.style.colorScheme = effective;
  }, [effective]);

  const handleSelect = useCallback((m: ThemeMode) => setMode(m), []);

  const isDark = effective === "dark";
  // 前台专属：大号分页 / 主题坞；后台与登录页保持 antd 默认尺寸
  const isPublicFront =
    !pathname.startsWith("/manage") && pathname !== "/login";
  const showDock = mounted && isPublicFront;

  const contextValue = useMemo(
    () => ({ mode, effective, setMode }),
    [mode, effective],
  );

  const providerTheme = useMemo(() => {
    // 禁止在 SSR/CSR 分支读 window/getComputedStyle，否则 token/hash 不一致会触发 hydration mismatch
    const algorithm = isDark ? theme.darkAlgorithm : theme.defaultAlgorithm;
    const baseToken = {
      colorPrimary: DEFAULT_PRIMARY_COLOR,
      fontFamily,
    };
    // 用 getDesignToken 补齐前台分页色彩，避免仅改尺寸导致层次变弱
    const designToken = theme.getDesignToken({
      algorithm,
      token: baseToken,
    });

    return {
      algorithm,
      cssVar: { key: "app" },
      // 固定 hashed，避免嵌套 Provider / useId 导致 css-var-_R_* 服务端与客户端不同
      hashed: true,
      token: baseToken,
      components: isPublicFront
        ? {
            // 仅前台：原 PublicLayoutView 分页 token（勿全局污染 /manage）
            Pagination: {
              itemSize: 55,
              fontSize: 18,
              itemBg: designToken.colorFillQuaternary,
              itemActiveBg: designToken.colorPrimary,
              itemActiveColor: designToken.colorTextLightSolid,
              colorText: designToken.colorText,
              colorTextDisabled: designToken.colorTextDisabled,
              colorBgContainer: "transparent",
              colorBorder: designToken.colorBorderSecondary,
            },
          }
        : undefined,
    };
  }, [isDark, fontFamily, isPublicFront]);

  return (
    <ConfigProvider locale={zhCN} theme={providerTheme}>
      <ThemeModeContext.Provider value={contextValue}>
        <App>
          {children}
          {showDock && <ThemeDock mode={mode} onSelect={handleSelect} />}
        </App>
      </ThemeModeContext.Provider>
    </ConfigProvider>
  );
}
