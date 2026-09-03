"use client";

import { useEffect, useRef } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { trackPageView } from "@/lib/track-page-view";

export default function RouteTracker() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const lastUrlRef = useRef("");

  useEffect(() => {
    if (!pathname || pathname.startsWith("/manage") || pathname.startsWith("/api")) {
      return;
    }

    // 专属业务交互页面（播放、搜索、分类筛选）由各页面独立上报真实动作与关联资源，此处跳过避免 PV 虚高翻倍
    if (
      pathname.startsWith("/play") ||
      pathname.startsWith("/search") ||
      pathname.startsWith("/filmClassify")
    ) {
      return;
    }

    const queryStr = searchParams?.toString();
    const currentUrl = queryStr ? `${pathname}?${queryStr}` : pathname;

    if (currentUrl === lastUrlRef.current) {
      return;
    }
    lastUrlRef.current = currentUrl;

    trackPageView({
      action: "browse",
      source: "web",
      page: currentUrl,
      path: currentUrl,
    });
  }, [pathname, searchParams]);

  return null;
}
