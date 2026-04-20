"use client";

import React, { useEffect, useMemo, useState, useCallback } from "react";
import { App, ConfigProvider, theme } from "antd";
import ThemeDock, { type ThemeMode } from "./ThemeDock";

const STORAGE_KEY = "app-theme";
const DEFAULT_PRIMARY_COLOR = "#fa8c16";

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
  const [mode, setMode] = useState<ThemeMode>("system");
  const [effective, setEffective] = useState<"dark" | "light">("dark");
  const [mounted, setMounted] = useState(false);

  // 避免 SSR/CSR Hydration 不一致，先用默认值，挂载后同步 localStorage
  useEffect(() => {
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
    <ConfigProvider theme={{ ...providerTheme, cssVar: { key: "app-theme" } }}>
      <App>
        {children}
        {mounted && <ThemeDock mode={mode} onSelect={handleSelect} />}
      </App>
    </ConfigProvider>
  );
}
