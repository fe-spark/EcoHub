import type { Metadata, Viewport } from "next";
import { AntdRegistry } from "@ant-design/nextjs-registry";
import { Outfit } from "next/font/google";
import GlobalThemeProvider from "@/components/theme/GlobalThemeProvider";
import "./globals.css";
const outfit = Outfit({
  subsets: ["latin"],
  display: "swap",
  weight: ["300", "400", "500", "600", "700", "800", "900"],
});

import { ApiGet } from "@/lib/api";

export async function generateMetadata(): Promise<Metadata> {
  let siteName = "";
  let describe = "";
  let keyword = "";
  let icon = "";

  try {
    const res = await ApiGet("/config/basic");
    if (res && res.code === 0 && res.data) {
      siteName = res.data.siteName || "";
      describe = res.data.describe || "";
      keyword = res.data.keyword || "";
      icon = res.data.logo || "";
    }
  } catch (err) {
    console.error("fetch metadata error:", err);
  }

  const generated: Metadata = {};
  if (siteName) generated.title = siteName;
  if (describe) generated.description = describe;
  if (keyword) generated.keywords = keyword;
  if (icon) generated.icons = { icon };

  return generated;
}

import SiteGuard from "@/components/common/SiteGuard";

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-CN">
      <body className={outfit.className}>
        <AntdRegistry>
          <GlobalThemeProvider fontFamily={outfit.style.fontFamily}>
            <SiteGuard>
              {children}
            </SiteGuard>
          </GlobalThemeProvider>
        </AntdRegistry>
      </body>
    </html>
  );
}
