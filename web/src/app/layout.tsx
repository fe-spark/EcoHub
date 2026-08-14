import type { Metadata, Viewport } from "next";
import { cookies, headers } from "next/headers";
import { AntdRegistry } from "@ant-design/nextjs-registry";
import GlobalThemeProvider from "@/components/theme/GlobalThemeProvider";
import SiteGuard, { SiteConfig } from "@/components/common/SiteGuard";
import { THEME_INIT_SCRIPT, THEME_COOKIE_KEY, isThemeMode } from "@/lib/theme";
import { serverGet } from "@/lib/server-api";
import "./globals.css";

const APP_FONT_FAMILY = [
  "Inter",
  "-apple-system",
  "BlinkMacSystemFont",
  '"Segoe UI"',
  "Roboto",
  '"Helvetica Neue"',
  "Arial",
  "sans-serif",
].join(", ");

async function getSiteConfig(): Promise<SiteConfig | null> {
  try {
    const response = await serverGet<SiteConfig>("/config/basic");
    if (response.code === 0 && response.data) {
      return response.data;
    }
  } catch (error) {
    console.error("fetch site config error:", error);
  }

  return null;
}

export async function generateMetadata(): Promise<Metadata> {
  const siteConfig = await getSiteConfig();

  const generated: Metadata = {};
  if (siteConfig?.siteName) generated.title = siteConfig.siteName;
  if (siteConfig?.describe) generated.description = siteConfig.describe;
  if (siteConfig?.keyword) generated.keywords = siteConfig.keyword;
  // favicon 与 SiteLogo 一致：未配置用本地默认；已配置原样使用（含相对路径 / 外链）
  const logo = siteConfig?.logo?.trim() || "";
  generated.icons = { icon: logo || "/logo.png" };

  return generated;
}

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default async function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  const siteConfig = await getSiteConfig();

  // SSR 直接输出正确主题：手动选择（cookie）优先，未选择时默认暗色
  const savedMode = (await cookies()).get(THEME_COOKIE_KEY)?.value;
  const initialMode = isThemeMode(savedMode) ? savedMode : "dark";
  let initialEffective: "dark" | "light";
  if (initialMode !== "system") {
    initialEffective = initialMode;
  } else {
    const clientHint = (await headers()).get("sec-ch-prefers-color-scheme");
    initialEffective = clientHint === "light" || clientHint === "dark" ? clientHint : "dark";
  }

  return (
    <html lang="zh-CN" suppressHydrationWarning data-theme={initialEffective} style={{ colorScheme: initialEffective }}>
      <body>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
        <AntdRegistry>
          <GlobalThemeProvider fontFamily={APP_FONT_FAMILY} initialMode={initialMode} initialEffective={initialEffective}>
            <SiteGuard initialConfig={siteConfig}>
              {children}
            </SiteGuard>
          </GlobalThemeProvider>
        </AntdRegistry>
      </body>
    </html>
  );
}
