export type ThemeMode = "dark" | "light" | "system";

export const THEME_STORAGE_KEY = "app-theme";
export const THEME_COOKIE_KEY = "app-theme";
const THEME_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

export function isThemeMode(value: unknown): value is ThemeMode {
  return value === "dark" || value === "light" || value === "system";
}

// 与 localStorage 同步写入，SSR 端可通过 cookie 直接输出正确主题
export function writeThemeCookie(mode: ThemeMode) {
  if (typeof document === "undefined") return;
  document.cookie = `${THEME_COOKIE_KEY}=${mode}; path=/; max-age=${THEME_COOKIE_MAX_AGE}; samesite=lax`;
}

// 首屏防主题闪烁：在 React 水合前同步设置 data-theme/colorScheme，
// 逻辑必须与 components/theme/GlobalThemeProvider.tsx 保持一致。
export const THEME_INIT_SCRIPT = `(function () {
  try {
    var mode = localStorage.getItem(${JSON.stringify(THEME_STORAGE_KEY)});
    if (mode !== "dark" && mode !== "light" && mode !== "system") {
      mode = "dark";
    }
    var effective = mode === "system"
      ? (window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark")
      : mode;
    document.documentElement.dataset.theme = effective;
    document.documentElement.style.colorScheme = effective;
  } catch (e) {}
})();`;
