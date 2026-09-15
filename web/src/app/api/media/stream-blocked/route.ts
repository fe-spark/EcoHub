import { NextResponse } from "next/server";

export async function GET() {
  return new NextResponse(
    "EcoHub: 流媒体禁止走 Next.js 代理缓冲。请在服务端配置 MEDIA_STREAM_PUBLIC_BASE 指向后端端口（如 :18080）直连播放。",
    { status: 400, headers: { "Content-Type": "text/plain; charset=utf-8" } }
  );
}

export async function HEAD() {
  return new NextResponse(null, { status: 400 });
}
