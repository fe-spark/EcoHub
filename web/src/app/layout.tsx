import type { Metadata, Viewport } from "next";
import { AntdRegistry } from "@ant-design/nextjs-registry";
import { Outfit } from "next/font/google";
import GlobalThemeProvider from "@/components/theme/GlobalThemeProvider";
import SiteGuard, { SiteConfig } from "@/components/common/SiteGuard";
import { serverGet } from "@/lib/server-api";
import "./globals.css";

const outfit = Outfit({
  subsets: ["latin"],
  display: "swap",
  weight: ["300", "400", "500", "600", "700", "800", "900"],
});

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
  if (siteConfig?.logo) generated.icons = { icon: siteConfig.logo };

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

  return (
    <html lang="zh-CN">
      <body className={outfit.className}>
        <AntdRegistry>
          <GlobalThemeProvider fontFamily={outfit.style.fontFamily}>
            <SiteGuard initialConfig={siteConfig}>
              {children}
            </SiteGuard>
          </GlobalThemeProvider>
        </AntdRegistry>
      </body>
    </html>
  );
}
