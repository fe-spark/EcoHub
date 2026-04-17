"use client";

import React, { Suspense, useMemo } from "react";
import { ConfigProvider, theme } from "antd";
import Header from "@/components/public/Header";
import Footer from "@/components/public/Footer";
import styles from "./index.module.less";

interface NavItem {
  id: string;
  name: string;
}

export default function PublicLayoutView({
  children,
  navList,
}: {
  children: React.ReactNode;
  navList: NavItem[];
}) {
  const { token: globalToken } = theme.useToken();

  const providerTheme = useMemo(() => {
    return {
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
          <Header navList={navList} />
        </Suspense>
        <main className={`${styles.publicMain} page-entry`}>{children}</main>
        <Footer />
      </div>
    </ConfigProvider>
  );
}
