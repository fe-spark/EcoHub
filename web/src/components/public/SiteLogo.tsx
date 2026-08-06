"use client";

import React from "react";

/** 未配置时使用的站内默认 logo（web/public/logo.png） */
export const LOCAL_SITE_LOGO = "/logo.png";

/**
 * 解析展示地址：
 * - 未设置（空）→ 本地默认
 * - 已设置 → 原样使用，不预判黑名单、失败也不改写（由管理员配置负责）
 */
export function resolveSiteLogoSrc(src?: string | null): string {
  const raw = (src || "").trim();
  return raw || LOCAL_SITE_LOGO;
}

type SiteLogoProps = {
  src?: string | null;
  alt?: string;
  className?: string;
  decoding?: "async" | "auto" | "sync";
  fetchPriority?: "high" | "low" | "auto";
};

/** 布局级站点 logo：仅空值走本地默认，配置值原样加载 */
export default function SiteLogo({
  src,
  alt = "logo",
  className,
  decoding = "async",
  fetchPriority = "auto",
}: SiteLogoProps) {
  const finalSrc = resolveSiteLogoSrc(src);

  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={finalSrc}
      alt={alt}
      className={className}
      decoding={decoding}
      fetchPriority={fetchPriority}
    />
  );
}
