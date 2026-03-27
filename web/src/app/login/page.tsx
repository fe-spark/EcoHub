"use client";

import React, { useState } from "react";
import { useRouter } from "next/navigation";
import { Button, Input, ConfigProvider, theme } from "antd";
import {
  UserOutlined,
  LockOutlined,
  EyeOutlined,
  EyeInvisibleOutlined,
} from "@ant-design/icons";
import { ApiPost } from "@/lib/api";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";

export default function LoginPage() {
  const [userName, setUserName] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const router = useRouter();
  const { message } = useAppMessage();
  const { config: siteInfo } = useSiteConfig();

  const handleLogin = async () => {
    if (!userName || !password) {
      message.warning("请输入用户名和密码");
      return;
    }
    setLoading(true);
    try {
      const resp = await ApiPost("/login", { userName, password });
      if (resp.code === 0) {
        message.success("登录成功");
        router.push("/manage");
      } else {
        message.error(resp.msg || "登录失败");
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <ConfigProvider
      theme={{
        algorithm: theme.darkAlgorithm,
        token: {
          colorPrimary: "#fa8c16",
          borderRadius: 16,
        },
        components: {
          Input: {
            colorBgContainer: "rgba(255, 255, 255, 0.04)",
            colorBorder: "rgba(255, 255, 255, 0.1)",
            controlHeightLG: 50,
            paddingContentHorizontal: 16,
            // 确保内部 input 元素透明
            colorBgContainerDisabled: "transparent",
          },
          Button: {
            controlHeightLG: 54,
            fontWeight: 700,
            borderRadiusLG: 16,
            colorPrimary: "#fa8c16",
          },
        },
      }}
    >
      <div className={styles.container}>
        <div className={styles.card}>
          <div className={styles.brand}>
            {siteInfo?.siteName && (
              <div className={styles.siteName}>{siteInfo.siteName}</div>
            )}
            <div className={styles.subTitle}>Management System</div>
          </div>

          <div className={styles.form}>
            <Input
              size="large"
              placeholder="用户名 / 邮箱"
              prefix={
                <UserOutlined style={{ color: "var(--ant-color-primary)" }} />
              }
              value={userName}
              onChange={(e) => setUserName(e.target.value)}
              onPressEnter={handleLogin}
            />
            <Input.Password
              size="large"
              placeholder="密码"
              prefix={
                <LockOutlined style={{ color: "var(--ant-color-primary)" }} />
              }
              iconRender={(visible) =>
                visible ? <EyeOutlined /> : <EyeInvisibleOutlined />
              }
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onPressEnter={handleLogin}
            />
            <Button
              type="primary"
              size="large"
              loading={loading}
              onClick={handleLogin}
              className={styles.btn}
              block
              style={{
                background: "linear-gradient(135deg, #fa8c16 0%, #fa541c 100%)",
                border: "none",
                boxShadow: "0 8px 20px rgba(250, 140, 22, 0.2)",
              }}
            >
              SIGN IN
            </Button>
          </div>

          <div className={styles.footer}>
            © {new Date().getFullYear()}
            {siteInfo?.siteName ? ` ${siteInfo.siteName}` : ""}
          </div>
        </div>
      </div>
    </ConfigProvider>
  );
}
