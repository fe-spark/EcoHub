export type ProbeStatus = "ok" | "timeout" | "unreachable" | "skipped";

export type SourceProbeResult = {
  sourceId: string;
  status: ProbeStatus;
  pingMs: number;
  speedKBps: number;
  score: number;
};

export type ProbeTarget = {
  sourceId: string;
  url: string;
};

const DEFAULT_TIMEOUT_MS = 5000;
const HANDSHAKE_TIMEOUT_MS = 2500;
const SAMPLE_BYTES = 128 * 1024;
const MAX_SAMPLE_SEGMENTS = 2;
const PROBE_CONCURRENCY = 2;

export function formatSpeedLabel(speedKBps: number): string {
  if (speedKBps >= 1024) {
    return `${(speedKBps / 1024).toFixed(1)} MB/s`;
  }
  if (speedKBps > 0) {
    return `${Math.max(1, Math.round(speedKBps))} KB/s`;
  }
  return "";
}

export function formatProbeLabel(
  result?: SourceProbeResult | null,
  testing = false,
): string {
  if (testing) return "测速中";
  if (!result) return "";
  if (result.status === "timeout") return "超时";
  if (result.status === "unreachable") return "不可用";
  if (result.status === "skipped") return "";
  const speed = formatSpeedLabel(result.speedKBps);
  if (speed) return speed;
  if (result.pingMs > 0) return `${Math.round(result.pingMs)} ms`;
  return "可用";
}

function isProgressiveUrl(url: string): boolean {
  let path = url;
  try {
    path = new URL(url).pathname;
  } catch {
    const cut = url.search(/[?#]/);
    if (cut >= 0) path = url.slice(0, cut);
  }
  return /\.(mp4|webm|mkv|mov|flv|avi)$/i.test(path);
}

function makeProbeError(type: "timeout" | "unreachable", message: string) {
  return Object.assign(new Error(message), { probeType: type });
}

function linkTimeoutSignal(timeoutMs: number, handshakeMs: number, outerSignal?: AbortSignal) {
  const controller = new AbortController();
  let timedOut = false;

  const onOuterAbort = () => controller.abort();
  if (outerSignal) {
    if (outerSignal.aborted) {
      controller.abort();
    } else {
      outerSignal.addEventListener("abort", onOuterAbort, { once: true });
    }
  }

  const timer = globalThis.setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);

  let handshakeTimer: ReturnType<typeof setTimeout> | undefined =
    handshakeMs > 0 && handshakeMs < timeoutMs
      ? globalThis.setTimeout(() => {
          timedOut = true;
          controller.abort();
        }, handshakeMs)
      : undefined;

  return {
    signal: controller.signal,
    isTimedOut: () => timedOut && !outerSignal?.aborted,
    clearHandshake: () => {
      if (handshakeTimer !== undefined) {
        globalThis.clearTimeout(handshakeTimer);
        handshakeTimer = undefined;
      }
    },
    cleanup: () => {
      globalThis.clearTimeout(timer);
      if (handshakeTimer !== undefined) {
        globalThis.clearTimeout(handshakeTimer);
        handshakeTimer = undefined;
      }
      outerSignal?.removeEventListener("abort", onOuterAbort);
    },
  };
}

function parseBandwidth(line: string): number {
  const matched = /(?:^|[,:\s])BANDWIDTH=(\d+)/i.exec(line);
  return matched ? Number(matched[1]) : 0;
}

function parseMasterPlaylistUrl(text: string, baseUrl: string): string | null {
  const lines = text.split(/\r?\n/);
  let bestUrl = "";
  let bestBandwidth = -1;
  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i].trim();
    if (!line.startsWith("#EXT-X-STREAM-INF")) continue;
    const bandwidth = parseBandwidth(line);
    for (let j = i + 1; j < lines.length; j += 1) {
      const next = lines[j].trim();
      if (!next || next.startsWith("#")) continue;
      if (bandwidth > bestBandwidth) {
        bestBandwidth = bandwidth;
        bestUrl = new URL(next, baseUrl).toString();
      }
      break;
    }
  }
  return bestUrl || null;
}

function parseSegmentUrls(text: string, baseUrl: string, limit: number): string[] {
  const urls: string[] = [];
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) continue;
    urls.push(new URL(line, baseUrl).toString());
    if (urls.length >= limit) break;
  }
  return urls;
}

async function fetchPlaylistText(url: string, signal: AbortSignal, onHeaders?: () => void) {
  const start = performance.now();
  const response = await fetch(url, { cache: "no-store", signal });
  const ttfbMs = Math.max(1, performance.now() - start);
  if (!response.ok) {
    throw makeProbeError("unreachable", `http ${response.status}`);
  }
  onHeaders?.();
  const text = await response.text();
  if (!text.includes("#EXTM3U")) {
    throw makeProbeError("unreachable", "invalid m3u8 playlist");
  }
  return {
    text,
    finalUrl: response.url || url,
    ttfbMs,
  };
}

async function readStreamSample(
  url: string,
  maxBytes: number,
  signal: AbortSignal,
  onHeaders?: () => void,
  isTimedOut?: () => boolean,
) {
  const start = performance.now();
  const response = await fetch(url, { cache: "no-store", signal });
  const headersAt = performance.now();
  if (!response.ok) {
    throw makeProbeError("unreachable", `http ${response.status}`);
  }
  onHeaders?.();

  const reader = response.body?.getReader();
  let bytes = 0;
  if (!reader) {
    const buffer = await response.arrayBuffer();
    bytes = Math.min(buffer.byteLength, maxBytes);
  } else {
    try {
      while (bytes < maxBytes) {
        const { done, value } = await reader.read();
        if (done) break;
        bytes += value.byteLength;
      }
    } catch (error) {
      // 已成功拉取部分媒体数据时若触发总超时，按实测已读字节计算速度，避免误判为不可用
      if (!(isTimedOut?.() && bytes > 0)) {
        throw error;
      }
    } finally {
      await reader.cancel().catch(() => {});
    }
  }

  if (bytes <= 0) {
    throw makeProbeError("unreachable", "empty media stream");
  }

  const endAt = performance.now();
  return {
    bytes,
    ttfbMs: Math.max(1, headersAt - start),
    downloadMs: Math.max(0, endAt - headersAt),
    totalMs: Math.max(0, endAt - start),
  };
}

async function probeOneUrl(
  url: string,
  timeoutMs: number,
  outerSignal?: AbortSignal,
): Promise<Omit<SourceProbeResult, "sourceId" | "status">> {
  const linked = linkTimeoutSignal(timeoutMs, HANDSHAKE_TIMEOUT_MS, outerSignal);
  try {
    const probeStart = performance.now();

    if (isProgressiveUrl(url)) {
      const sample = await readStreamSample(
        url,
        SAMPLE_BYTES,
        linked.signal,
        linked.clearHandshake,
        linked.isTimedOut,
      );
      const speedKBps = sample.bytes / 1024 / (Math.max(sample.downloadMs, 15) / 1000);
      const score = sample.bytes / 1024 / (Math.max(sample.totalMs, 15) / 1000);
      return {
        pingMs: Math.round(sample.ttfbMs),
        speedKBps,
        score,
      };
    }

    const firstPlaylist = await fetchPlaylistText(url, linked.signal, linked.clearHandshake);
    const variantUrl = parseMasterPlaylistUrl(firstPlaylist.text, firstPlaylist.finalUrl);
    const mediaPlaylist = variantUrl
      ? await fetchPlaylistText(variantUrl, linked.signal)
      : firstPlaylist;

    const segments = parseSegmentUrls(
      mediaPlaylist.text,
      mediaPlaylist.finalUrl,
      MAX_SAMPLE_SEGMENTS,
    );
    if (segments.length === 0) {
      throw makeProbeError("unreachable", "no media segments");
    }

    let totalBytes = 0;
    let totalDownloadMs = 0;
    let firstSegTtfb = 0;

    for (const segUrl of segments) {
      if (totalBytes >= SAMPLE_BYTES || linked.isTimedOut()) break;
      try {
        const sample = await readStreamSample(
          segUrl,
          SAMPLE_BYTES - totalBytes,
          linked.signal,
          linked.clearHandshake,
          linked.isTimedOut,
        );
        if (firstSegTtfb === 0) {
          firstSegTtfb = sample.ttfbMs;
        }
        totalBytes += sample.bytes;
        totalDownloadMs += sample.downloadMs;
      } catch (error) {
        // 首片已拿到实测数据时，后续分片因总超时中断不再丢弃前序采样结果
        if (linked.isTimedOut() && totalBytes > 0) {
          break;
        }
        throw error;
      }
    }

    const totalElapsedMs = Math.max(performance.now() - probeStart, 15);
    const speedKBps = totalBytes / 1024 / (Math.max(totalDownloadMs, 15) / 1000);
    // 综合有效吞吐（总字节 / 含播放列表与首片握手的总耗时），自然兼顾延迟与带宽
    const score = totalBytes / 1024 / (totalElapsedMs / 1000);

    return {
      pingMs: Math.round(firstSegTtfb || mediaPlaylist.ttfbMs),
      speedKBps,
      score,
    };
  } catch (error) {
    if (outerSignal?.aborted) {
      throw error;
    }
    if (linked.isTimedOut()) {
      throw makeProbeError("timeout", "probe timeout");
    }
    throw error;
  } finally {
    linked.cleanup();
  }
}

function toFailureResult(sourceId: string, error: unknown): SourceProbeResult {
  const probeType =
    error && typeof error === "object" && "probeType" in error
      ? String((error as { probeType?: string }).probeType)
      : "";
  return {
    sourceId,
    status: probeType === "timeout" ? "timeout" : "unreachable",
    pingMs: 0,
    speedKBps: 0,
    score: -1,
  };
}

export async function probePlaySources(
  targets: ProbeTarget[],
  options?: {
    timeoutMs?: number;
    signal?: AbortSignal;
    onStart?: (sourceId: string) => void;
    onResult?: (result: SourceProbeResult) => void;
  },
): Promise<SourceProbeResult[]> {
  const timeoutMs = options?.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const signal = options?.signal;
  const results: SourceProbeResult[] = new Array(targets.length);
  let cursor = 0;

  const runOne = async (index: number) => {
    const target = targets[index];
    const url = String(target.url || "").trim();
    if (!url) {
      const skipped: SourceProbeResult = {
        sourceId: target.sourceId,
        status: "skipped",
        pingMs: 0,
        speedKBps: 0,
        score: -1,
      };
      options?.onResult?.(skipped);
      results[index] = skipped;
      return;
    }

    options?.onStart?.(target.sourceId);
    try {
      const metrics = await probeOneUrl(url, timeoutMs, signal);
      const okResult: SourceProbeResult = {
        sourceId: target.sourceId,
        status: "ok",
        ...metrics,
      };
      options?.onResult?.(okResult);
      results[index] = okResult;
    } catch (error) {
      if (signal?.aborted) {
        throw error;
      }
      const failure = toFailureResult(target.sourceId, error);
      options?.onResult?.(failure);
      results[index] = failure;
    }
  };

  const worker = async () => {
    while (!signal?.aborted) {
      const index = cursor;
      cursor += 1;
      if (index >= targets.length) return;
      await runOne(index);
    }
  };

  const workers = Math.min(PROBE_CONCURRENCY, targets.length);
  await Promise.all(Array.from({ length: workers }, () => worker()));
  if (signal?.aborted) {
    throw new DOMException("Aborted", "AbortError");
  }
  return results;
}

export function pickBestProbeResult(
  results: SourceProbeResult[],
): SourceProbeResult | null {
  const ranked = results
    .filter((item) => item.status === "ok")
    .sort((a, b) => b.score - a.score);
  return ranked[0] || null;
}
