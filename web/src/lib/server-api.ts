import "server-only";

export interface ApiResponse<T = any> {
  code: number;
  msg: string;
  data: T;
}

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
): Promise<ApiResponse<T>> {
  const response = await fetch(buildApiUrl(path, params), {
    cache: "no-store",
  });

  if (!response.ok) {
    throw new Error(`服务端请求失败: ${response.status} ${response.statusText}`);
  }

  return (await response.json()) as ApiResponse<T>;
}
