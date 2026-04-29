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
  const showDock = mounted && !pathname.startsWith("/manage") && pathname !== "/login";

  const contextValue = useMemo(
    () => ({ mode, effective, setMode }),
    [mode, effective],
  );

  const providerTheme = useMemo(() => {
    const primaryColor =
      typeof window !== "undefined"
        ? getComputedStyle(document.documentElement).getPropertyValue("--primary-color").trim() || DEFAULT_PRIMARY_COLOR
        : DEFAULT_PRIMARY_COLOR;
    return {
      algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
      token: {
        colorPrimary: primaryColor,
        fontFamily,
      },
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isDark, fontFamily, effective]);

  return (
    <ConfigProvider
      locale={zhCN}
      theme={{ ...providerTheme, cssVar: { key: "app-theme" } }}
    >
      <ThemeModeContext.Provider value={contextValue}>
        <App>
          {children}
          {showDock && <ThemeDock mode={mode} onSelect={handleSelect} />}
        </App>
      </ThemeModeContext.Provider>
    </ConfigProvider>
  );
}
