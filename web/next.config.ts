import type { NextConfig } from "next";

const apiUrl = process.env.API_URL?.trim();

if (!apiUrl) {
  throw new Error("缺少环境变量 API_URL，无法为前端配置后端地址");
}

const nextConfig: NextConfig = {
  output: "standalone",
  typescript: {
    ignoreBuildErrors: true,
  },
  env: {
    API_URL: apiUrl,
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiUrl.replace(/\/+$/, "")}/api/:path*`,
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
};

export default nextConfig;
