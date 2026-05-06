import "server-only";

export interface ApiResponse<T = any> {
  code: number;
  msg: string;
  data: T;
}

const serverFetchTimeoutMs = 15000;

function getServerApiOrigin(): string {
  const apiUrl = process.env.API_URL?.trim();
  if (!apiUrl) {
    throw new Error("缺少环境变量 API_URL，无法推导服务端请求地址");
  }

  return apiUrl.replace(/\/+$/, "");
}

function buildApiUrl(path: string, params?: Record<string, string | number | undefined>): string {
  const url = new URL(`/api${path}`, getServerApiOrigin());

  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== "") {
        url.searchParams.set(key, String(value));
      }
    });
  }

  return url.toString();
}

export async function serverGet<T = any>(
  path: string,
  params?: Record<string, string | number | undefined>,
  headers?: HeadersInit,
): Promise<ApiResponse<T>> {
  const apiUrl = buildApiUrl(path, params);
  console.info(`[SSR][API] GET ${apiUrl}`);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), serverFetchTimeoutMs);
  let response: Response;
  try {
    response = await fetch(apiUrl, {
      cache: "no-store",
      headers,
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
    throw new Error(`服务端请求失败: ${response.status} ${response.statusText} ${body.slice(0, 200)}`.trim());
  }

  if (!body.trim()) {
    throw new Error(`服务端返回空响应: ${apiUrl}`);
  }

  try {
    return JSON.parse(body) as ApiResponse<T>;
  } catch (error) {
    throw new Error(
      `服务端返回非 JSON 响应: ${apiUrl}; ${error instanceof Error ? error.message : String(error)}; ${body.slice(0, 200)}`,
    );
  }
}
