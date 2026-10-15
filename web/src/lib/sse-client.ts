export interface SSERequestOptions {
  method?: "GET" | "POST";
  body?: any;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  /** 响应成功、开始读流时调用一次 */
  onOpen?: () => void;
  onMessage?: (data: any, raw: string, event?: string) => void;
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
    onOpen,
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

  onOpen?.();
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
        let event = "";
        for (const rawLine of trimmed.split(/\r?\n/)) {
          const line = rawLine.trim();
          if (line.startsWith("event:")) {
            event = line.slice(6).trim();
          }
          if (line.startsWith("data:")) {
            const rawData = line.slice(5).trim();
            if (!rawData) continue;
            try {
              const parsed = JSON.parse(rawData);
              onMessage?.(parsed, rawData, event);
            } catch {
              onMessage?.(rawData, rawData, event);
            }
          }
        }
      }
    }

    if (buffer.trim()) {
      let event = "";
      for (const rawLine of buffer.trim().split(/\r?\n/)) {
        const line = rawLine.trim();
        if (line.startsWith("event:")) {
          event = line.slice(6).trim();
        }
        if (line.startsWith("data:")) {
          const rawData = line.slice(5).trim();
          if (!rawData) continue;
          try {
            const parsed = JSON.parse(rawData);
            onMessage?.(parsed, rawData, event);
          } catch {
            onMessage?.(rawData, rawData, event);
          }
        }
      }
    }
  } finally {
    reader.releaseLock();
  }
}
