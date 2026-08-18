"use client";

import React, { useState } from "react";
import { useRouter } from "next/navigation";
import { Button, Input } from "antd";
import {
  UserOutlined,
  LockOutlined,
  EyeOutlined,
  EyeInvisibleOutlined,
  GithubOutlined,
} from "@ant-design/icons";
import { PROJECT_GITHUB_URL } from "@/lib/project";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import styles from "./index.module.less";

const fieldClassNames = { affixWrapper: styles.field };

export default function LoginPageView() {
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
    <div className={styles.container}>
      <div className={styles.card}>
        <div className={styles.brand}>
          {siteInfo?.siteName && <div className={styles.siteName}>{siteInfo.siteName}</div>}
          <div className={styles.subTitle}>Management System</div>
        </div>

        <div className={styles.form}>
          <Input
            size="large"
            placeholder="用户名 / 邮箱"
            prefix={<UserOutlined className={styles.fieldIcon} />}
            classNames={fieldClassNames}
            value={userName}
            onChange={(e) => setUserName(e.target.value)}
            onPressEnter={handleLogin}
          />
          <Input.Password
            size="large"
            placeholder="密码"
            prefix={<LockOutlined className={styles.fieldIcon} />}
            classNames={fieldClassNames}
            iconRender={(visible) => (visible ? <EyeOutlined /> : <EyeInvisibleOutlined />)}
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
          >
            SIGN IN
          </Button>
        </div>

        <div className={styles.footer}>
          <span>
            © {new Date().getFullYear()}
            {siteInfo?.siteName ? ` ${siteInfo.siteName}` : ""}
          </span>
          <a
            href={PROJECT_GITHUB_URL}
            target="_blank"
            rel="noopener noreferrer"
            className={styles.githubLink}
            title="打开 GitHub 项目地址"
          >
            <GithubOutlined />
            <span>GitHub 项目地址</span>
          </a>
        </div>
      </div>
    </div>
  );
}
