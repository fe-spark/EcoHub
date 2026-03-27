"use client";

import React, { Suspense, useMemo } from "react";
import Header from "@/components/public/Header";
import Footer from "@/components/public/Footer";
import styles from "./layout.module.less";
import { ConfigProvider, theme } from "antd";

export default function PublicLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { token: globalToken } = theme.useToken();

  const providerTheme = useMemo(() => {
    return {
      // 移除手动算法切换，让 AntD Context 自动处理继承
      components: {
        Pagination: {
          itemSize: 55,
          fontSize: 18,
          itemBg: globalToken.colorFillQuaternary,
          itemActiveBg: globalToken.colorPrimary,
          itemActiveColor: globalToken.colorTextLightSolid,
          colorText: globalToken.colorText,
          colorTextDisabled: globalToken.colorTextDisabled,
          colorBgContainer: "transparent",
          colorBorder: globalToken.colorBorderSecondary,
        },
      },
    };
  }, [globalToken]);

  return (
    <ConfigProvider theme={providerTheme}>
      <div className={styles.layoutWrapper}>
        <Suspense fallback={null}>
          <Header />
        </Suspense>
        <main className={`${styles.publicMain} page-entry`}>{children}</main>
        <Footer />
      </div>
    </ConfigProvider>
  );
}
