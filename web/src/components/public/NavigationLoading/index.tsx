"use client";

import { useEffect, useRef } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { forceStopNavigationLoading } from "@/components/public/TopLoadingBar";

/**
 * 监听路由完成：pathname / search 变化后关闭顶栏 loading。
 * 与 Header 点击时的 startNavigationLoading 配对使用。
 * 使用 forceStop：导航 start 与 loading.tsx 可能各 +1，避免计数对不齐导致 bar 挂住。
 */
export default function NavigationLoadingListener() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const search = searchParams?.toString() ?? "";
  const isFirstRender = useRef(true);

  useEffect(() => {
    if (isFirstRender.current) {
      isFirstRender.current = false;
      return;
    }
    forceStopNavigationLoading();
  }, [pathname, search]);

  return null;
}
