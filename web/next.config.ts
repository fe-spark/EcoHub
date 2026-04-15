import type { NextConfig } from "next";
import os from "os";
import path from "path";
import dotenv from "dotenv";

const cpuCount = Math.max(1, os.cpus().length - 1);
dotenv.config({ path: path.resolve(process.cwd(), "..", ".env") });

const apiUrl = process.env.API_URL?.trim();
const serverPort = process.env.SERVER_PORT?.trim();

if (!apiUrl && !serverPort) {
  throw new Error("缺少环境变量 API_URL 或 SERVER_PORT，无法为前端推导后端地址");
}

const nextConfig: NextConfig = {
  output: "standalone",
  env: {
    ...(apiUrl ? { API_URL: apiUrl } : {}),
    ...(serverPort ? { SERVER_PORT: serverPort } : {}),
  },
  turbopack: {
    rules: {
      "*.module.less": {
        loaders: ["less-loader"],
        as: "*.module.css",
      },
    },
  },
  experimental: {
    // 自动获取 CPU 核心数量进行构建并行化
    cpus: cpuCount,
  },
};

export default nextConfig;
