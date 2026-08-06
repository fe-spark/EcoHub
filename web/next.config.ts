import type { NextConfig } from "next";
import os from "os";
import { normalizeApiBase } from "./src/lib/api-base";

const cpuCount = Math.max(1, Math.min(4, os.cpus().length - 1));

const apiUrl = process.env.API_URL?.trim();

if (!apiUrl) {
  throw new Error("缺少环境变量 API_URL，无法为前端配置后端地址");
}

const apiBase = normalizeApiBase(apiUrl);

const nextConfig: NextConfig = {
  output: "standalone",
  env: {
    API_URL: apiUrl,
  },
  async rewrites() {
    // 浏览器 /api/* → 后端 /api/*（API_URL 带不带 /api 都正确）
    return [
      {
        source: "/api/:path*",
        destination: `${apiBase}/api/:path*`,
      },
    ];
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
    cpus: cpuCount,
  },
};

export default nextConfig;
