import "server-only";
import { headers as nextHeaders } from "next/headers";
import { redirect } from "next/navigation";
import { buildBackendApiUrl } from "@/lib/api-base";
import {
  ServerApiError,
  buildHttpServerApiError,
  buildLoginRedirectTarget,
  getPublicServerErrorMessage,
  isServerUnauthorizedError,
} from "@/lib/server-api-error";

export { ServerApiError, getPublicServerErrorMessage, isServerUnauthorizedError };

export interface ApiResponse<T = any> {
  code: number;
  msg: string;
  data: T;
}

export async function redirectToLogin(): Promise<never> {
  try {
    const headerStore = await nextHeaders();
    redirect(buildLoginRedirectTarget(headerStore.get("x-current-path")));
  } catch (error) {
    // Next.js redirect() 通过抛错中断渲染，不能被当作读取请求头异常吞掉
    if (error && typeof error === "object" && "digest" in error) {
      throw error;
    }
  }
  redirect("/login");
}

const serverFetchTimeoutMs = 15000;

function requireApiUrl(): string {
  const apiUrl = process.env.API_URL?.trim();
  if (!apiUrl) {
    throw new Error("缺少环境变量 API_URL，无法请求后端");
  }
  return apiUrl;
}

export async function serverGet<T = any>(
  path: string,
  params?: Record<string, string | number | undefined>,
  headers?: HeadersInit,
): Promise<ApiResponse<T>> {
  const apiUrl = buildBackendApiUrl(requireApiUrl(), path, params);
  const devLog = process.env.NODE_ENV === "development";
  if (devLog) {
    console.info(`[SSR][API] GET ${apiUrl}`);
  }
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), serverFetchTimeoutMs);
  const merged: Record<string, string> = {
    "User-Agent": "EcoHub-SSR",
    Connection: "close",
  };
  try {
    const reqHeaders = await nextHeaders();
    const forwardedFor = reqHeaders.get("x-forwarded-for");
    const realIp = reqHeaders.get("x-real-ip");
    if (forwardedFor) {
      merged["X-Forwarded-For"] = forwardedFor;
    } else if (realIp) {
      merged["X-Forwarded-For"] = realIp;
    }
    if (realIp) {
      merged["X-Real-IP"] = realIp;
    }
    const ua = reqHeaders.get("user-agent");
    if (ua) {
      merged["X-Original-User-Agent"] = ua;
    }
    const cookie = reqHeaders.get("cookie");
    if (cookie) {
      merged["Cookie"] = cookie;
    }
  } catch {
    // 无请求上下文时（如静态构建或后台生成）忽略
  }
  if (headers) {
    new Headers(headers).forEach((value, key) => {
      merged[key] = value;
    });
  }

  let response: Response | undefined;
  let body = "";
  try {
    for (let attempt = 0; attempt < 2; attempt += 1) {
      response = await fetch(apiUrl, {
        cache: "no-store",
        headers: merged,
        signal: controller.signal,
      });
      body = await response.text();
      if (body.trim() || !response.ok) {
        break;
      }
    }
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    if (error instanceof Error && error.name === "AbortError") {
      throw new ServerApiError({
        message: `服务端请求超时: ${apiUrl}`,
        status: 0,
        isNetworkError: true,
      });
    }
    // 连接失败是 TypeError("fetch failed")，Next 会当成未处理异常打到 Console overlay。
    // 转成 ServerApiError，由各页面按鉴权/网络/空态分别处理。
    throw new ServerApiError({
      message: `服务端请求失败: ${apiUrl}; ${reason}`,
      status: 0,
      isNetworkError: true,
    });
  } finally {
    clearTimeout(timeout);
  }

  if (!response) {
    throw new ServerApiError({
      message: `服务端请求失败: ${apiUrl}`,
      status: 0,
      isNetworkError: true,
    });
  }
  if (!response.ok) {
    throw buildHttpServerApiError(response.status, response.statusText, body);
  }

  if (!body.trim()) {
    throw new Error(`服务端返回空响应: ${apiUrl}`);
  }

  try {
    const parsed = JSON.parse(body) as ApiResponse<T>;
    if (devLog) {
      const dataHint =
        parsed?.data == null
          ? "data=null"
          : typeof parsed.data === "object"
            ? `data.keys=${Object.keys(parsed.data as object).join(",")}`
            : `data.type=${typeof parsed.data}`;
      console.info(
        `[SSR][API] ← ${response.status} code=${String(parsed?.code)} bytes=${body.length} ${dataHint} ${apiUrl}`,
      );
    }
    return parsed;
  } catch (error) {
    throw new Error(
      `服务端返回非 JSON 响应: ${apiUrl}; ${error instanceof Error ? error.message : String(error)}; ${body.slice(0, 200)}`,
    );
  }
}
