import "server-only";
import { buildBackendApiUrl } from "@/lib/api-base";

export interface ApiResponse<T = any> {
  code: number;
  msg: string;
  data: T;
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
  const merged: Record<string, string> = { "User-Agent": "EcoHub-SSR" };
  if (headers) {
    new Headers(headers).forEach((value, key) => {
      merged[key] = value;
    });
  }

  let response: Response;
  try {
    response = await fetch(apiUrl, {
      cache: "no-store",
      headers: merged,
      signal: controller.signal,
    });
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error(`服务端请求超时: ${apiUrl}`);
    }
    throw error;
  } finally {
    clearTimeout(timeout);
  }

  const body = await response.text();
  if (!response.ok) {
    throw new Error(
      `服务端请求失败: ${response.status} ${response.statusText} ${body.slice(0, 200)}`.trim(),
    );
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
