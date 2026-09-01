"use client";

import React, { createContext, useContext, useEffect, useMemo, useState, useCallback } from "react";
import { usePathname } from "next/navigation";
import { App, ConfigProvider, theme } from "antd";
import zhCN from "antd/locale/zh_CN";
import dayjs from "dayjs";
import "dayjs/locale/zh-cn";
import ThemeDock, { type ThemeMode } from "./ThemeDock";
import { THEME_STORAGE_KEY, writeThemeCookie, isThemeMode } from "@/lib/theme";

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
  if (typeof window === "undefined") return "dark";
  const saved = localStorage.getItem(THEME_STORAGE_KEY);
  return isThemeMode(saved) ? saved : "dark";
}

export default function GlobalThemeProvider({
  children,
  fontFamily,
  initialMode,
  initialEffective,
}: {
  children: React.ReactNode;
  fontFamily: string;
  initialMode: ThemeMode;
  initialEffective: "dark" | "light";
}) {
  const pathname = usePathname();
  // initialMode/initialEffective 由 SSR 根据 cookie/系统偏好计算，
  // 首屏渲染（含 antd 样式）即正确主题，避免水合后再从深色切换到浅色
  const [mode, setMode] = useState<ThemeMode>(initialMode);
  const [effective, setEffective] = useState<"dark" | "light">(initialEffective);
  const [mounted, setMounted] = useState(false);

  // 初始主题已由 SSR 按 cookie/系统偏好输出；挂载后再以 localStorage 兜底（正常情况下两者一致）
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
    localStorage.setItem(THEME_STORAGE_KEY, mode);
    writeThemeCookie(mode);
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
    const baseToken = isDark
      ? {
          colorPrimary: DEFAULT_PRIMARY_COLOR,
          colorBorderSecondary: "rgba(255, 255, 255, 0.16)",
          colorBorder: "rgba(255, 255, 255, 0.22)",
          fontFamily,
        }
      : {
          colorPrimary: DEFAULT_PRIMARY_COLOR,
          colorPrimaryBg: "#fff7e6",
          colorPrimaryBgHover: "#ffe7ba",
          colorBgLayout: "#f4f6fc",
          colorBgContainer: "#ffffff",
          colorBgElevated: "#ffffff",
          colorFillQuaternary: "#f8fafc",
          colorFillTertiary: "#f1f5f9",
          colorFillSecondary: "#e2e8f0",
          colorBorderSecondary: "#e2e8f0",
          colorBorder: "#cbd5e1",
          colorText: "#0f172a",
          colorTextSecondary: "#475569",
          colorTextTertiary: "#94a3b8",
          borderRadius: 10,
          fontFamily,
        };

    const designToken = theme.getDesignToken({
      algorithm,
      token: baseToken,
    });

    return {
      algorithm,
      cssVar: { key: "app" },
      hashed: true,
      token: baseToken,
      components: {
        ...(isPublicFront
          ? {
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
          : {}),
        Card: {
          colorBgContainer: isDark ? "rgba(255, 255, 255, 0.03)" : "#ffffff",
          colorBorderSecondary: isDark ? "rgba(255, 255, 255, 0.16)" : "#e2e8f0",
          colorBorder: isDark ? "rgba(255, 255, 255, 0.22)" : "#cbd5e1",
        },
        Table: {
          cellBg: isDark ? "#14151a" : "#ffffff",
          headerBg: isDark ? "#1b1d24" : "#f8fafc",
          colorBorderSecondary: isDark ? "rgba(255, 255, 255, 0.16)" : "#e2e8f0",
          borderColor: isDark ? "rgba(255, 255, 255, 0.22)" : "#cbd5e1",
          rowHoverBg: isDark ? "rgba(250, 140, 22, 0.1)" : "rgba(250, 140, 22, 0.05)",
        },
        Menu: {
          itemBg: "transparent",
          itemSelectedBg: isDark ? "rgba(250, 140, 22, 0.28)" : "#ffe7ba",
          itemSelectedColor: isDark ? "#ffa940" : "#d46b08",
          itemHoverBg: isDark ? "rgba(250, 140, 22, 0.12)" : "rgba(250, 140, 22, 0.12)",
          itemHoverColor: DEFAULT_PRIMARY_COLOR,
        },
      },
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
