export interface ServerErrorPayload<T = any> {
  code?: number;
  msg?: string;
  data?: T;
}

export class ServerApiError extends Error {
  readonly status: number;
  readonly code?: number;
  readonly payload?: any;
  readonly isNetworkError: boolean;

  constructor(options: {
    message: string;
    status: number;
    code?: number;
    payload?: any;
    isNetworkError?: boolean;
  }) {
    super(options.message);
    this.name = "ServerApiError";
    this.status = options.status;
    this.code = options.code;
    this.payload = options.payload;
    this.isNetworkError = Boolean(options.isNetworkError);
  }
}

export function isServerUnauthorizedError(error: unknown): boolean {
  if (!(error instanceof ServerApiError)) return false;
  if (error.status === 401) return true;
  return error.status === 403 && Boolean(error.payload?.private_access);
}

export function buildHttpServerApiError(
  status: number,
  statusText: string,
  body: string,
): ServerApiError {
  let parsedError: ServerErrorPayload | null = null;
  try {
    parsedError = JSON.parse(body) as ServerErrorPayload;
  } catch {
    // 非 JSON 错误体按纯文本兜底
  }
  return new ServerApiError({
    message:
      parsedError?.msg ||
      `服务端请求失败: ${status} ${statusText} ${body.slice(0, 200)}`.trim(),
    status,
    code: parsedError?.code,
    payload: parsedError?.data,
  });
}

// 前台页面展示错误时隐藏内部 API_URL 与底层网络堆栈细节
export function getPublicServerErrorMessage(
  error: unknown,
  fallback = "无法连接后端服务，请稍后重试",
): string {
  if (error instanceof ServerApiError) {
    if (error.isNetworkError) {
      return fallback;
    }
    if (error.status >= 500) {
      return "服务暂时不可用，请稍后重试";
    }
    return error.message || fallback;
  }
  return fallback;
}

export function buildLoginRedirectTarget(currentPath?: string | null): string {
  if (
    currentPath &&
    currentPath.startsWith("/") &&
    !currentPath.startsWith("//") &&
    !currentPath.startsWith("/login")
  ) {
    return `/login?redirect=${encodeURIComponent(currentPath)}`;
  }
  return "/login";
}
