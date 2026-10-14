export interface SSERequestOptions {
  method?: "GET" | "POST";
  body?: any;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  onMessage?: (data: any, raw: string) => void;
  onError?: (err: Error) => void;
}

/**
 * 通用 SSE 流式请求工具，通过 ReadableStream 实时读取服务端事件
 */
export async function fetchEventStream(
  url: string,
  options: SSERequestOptions = {},
): Promise<void> {
  const {
    method = "POST",
    body,
    headers = {},
    signal,
    onMessage,
    onError,
  } = options;

  const fetchHeaders: Record<string, string> = {
    Accept: "text/event-stream",
    ...headers,
  };

  let fetchBody: any = undefined;
  if (body !== undefined) {
    fetchHeaders["Content-Type"] = "application/json";
    fetchBody = typeof body === "string" ? body : JSON.stringify(body);
  }

  const response = await fetch(url, {
    method,
    headers: fetchHeaders,
    body: fetchBody,
    credentials: "include",
    signal,
  });

  if (!response.ok) {
    let errMsg = `请求失败: ${response.status}`;
    try {
      const errJson = await response.json();
      if (errJson?.msg) errMsg = errJson.msg;
    } catch {
      // 保持默认错误
    }
    const err = new Error(errMsg);
    onError?.(err);
    throw err;
  }

  if (!response.body) {
    return;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder("utf-8");
  let buffer = "";

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const blocks = buffer.split(/\r?\n\r?\n/);
      buffer = blocks.pop() || "";

      for (const block of blocks) {
        const trimmed = block.trim();
        if (!trimmed) continue;
        for (const rawLine of trimmed.split(/\r?\n/)) {
          const line = rawLine.trim();
          if (line.startsWith("data:")) {
            const rawData = line.slice(5).trim();
            if (!rawData) continue;
            try {
              const parsed = JSON.parse(rawData);
              onMessage?.(parsed, rawData);
            } catch {
              onMessage?.(rawData, rawData);
            }
          }
        }
      }
    }

    if (buffer.trim()) {
      for (const rawLine of buffer.trim().split(/\r?\n/)) {
        const line = rawLine.trim();
        if (line.startsWith("data:")) {
          const rawData = line.slice(5).trim();
          if (!rawData) continue;
          try {
            const parsed = JSON.parse(rawData);
            onMessage?.(parsed, rawData);
          } catch {
            onMessage?.(rawData, rawData);
          }
        }
      }
    }
  } finally {
    reader.releaseLock();
  }
}
