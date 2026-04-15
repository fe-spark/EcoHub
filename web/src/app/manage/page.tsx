"use client";

import React, { useEffect } from "react";
import { Card, Typography } from "antd";
import { ApiGet } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";

const { Title, Text } = Typography;

export default function ManagePage() {
  const { message } = useAppMessage();
  const { config: siteInfo } = useSiteConfig();
  const welcomeText = siteInfo?.siteName
    ? `欢迎使用 ${siteInfo.siteName} 后台管理系统`
    : "欢迎使用后台管理系统";

  useEffect(() => {
    ApiGet("/manage/index").then((resp) => {
      // 避免首页加载时弹出冗余的"后台管理中心"提示
      // if (resp.code === 0) {
      //   message.success(resp.msg);
      // }
    });
  }, []);

  return (
    <div>
      <Title level={3}>后台管理中心</Title>
      <Card
        style={{
          background: "transparent",
          borderColor: "rgba(255, 255, 255, 0.1)",
        }}
      >
        <Text>{welcomeText}</Text>
      </Card>
    </div>
  );
}
