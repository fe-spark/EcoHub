/**
 * 将用户配置的 API_URL 归一成「服务根」（无尾斜杠、无末尾 /api）。
 * 用户随便写下面任意一种即可，无需关心路径细节：
 * - http://127.0.0.1:8080
 * - http://127.0.0.1:8080/
 * - https://eco.example.com/api
 * - https://eco.example.com/api/
 */
export function normalizeApiBase(apiUrl: string): string {
  let base = apiUrl.trim().replace(/\/+$/, "");
  // 连续写了多个 /api 也剥干净
  while (/\/api$/i.test(base)) {
    base = base.replace(/\/api$/i, "").replace(/\/+$/, "");
  }
  return base;
}

/** 拼出最终业务接口地址：{base}/api{path} */
export function buildBackendApiUrl(
  apiUrl: string,
  path: string,
  params?: Record<string, string | number | undefined>,
): string {
  const base = normalizeApiBase(apiUrl);
  if (!base) {
    throw new Error("API_URL 为空");
  }
  const apiPath = path.startsWith("/") ? path : `/${path}`;
  // 必须用字符串拼接：new URL('/api/...', base) 会丢弃 base 的 pathname
  // （如 https://host/v1 + /api/x → https://host/api/x，与 rewrite 的 /v1/api 不一致）
  const url = new URL(`${base}/api${apiPath}`);

  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (key.startsWith("_")) return;
      if (value !== undefined && value !== "") {
        url.searchParams.set(key, String(value));
      }
    });
  }

  return url.toString();
}
