import type { NextConfig } from "next";
import os from "os";

const cpuCount = Math.max(1, os.cpus().length - 1);
const apiUrl = process.env.API_URL?.trim();

if (!apiUrl) {
  throw new Error(
    "缺少环境变量 API_URL，请在 web/.env.local 中配置，例如 API_URL=http://127.0.0.1:3601",
  );
}

const nextConfig: NextConfig = {
  output: "standalone",
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiUrl}/api/:path*`,
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
    // 自动获取 CPU 核心数量进行构建并行化
    cpus: cpuCount,
  },
};

export default nextConfig;
