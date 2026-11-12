import Hls from "hls.js";

export type ProbeStatus = "ok" | "timeout" | "unreachable" | "skipped";

export type SourceProbeResult = {
  sourceId: string;
  status: ProbeStatus;
  pingMs: number;
  speedKBps: number;
  quality: string;
  score: number;
};

export type ProbeTarget = {
  sourceId: string;
  url: string;
};

type RawProbe = {
  pingMs: number;
  speedKBps: number;
  quality: string;
};

const DEFAULT_TIMEOUT_MS = 5000;
// 两条一起测：比全串行快，又不会像一次测三条那样把带宽拆碎
const DEFAULT_CONCURRENCY = 2;
// 第一片还在 TCP 慢启动，不计入；下一片的传输时间作为速度
const WARMUP_FRAGMENTS = 1;
const SAMPLE_FRAGMENTS = 1;
const PROGRESSIVE_WARMUP_BYTES = 64 * 1024;
const PROGRESSIVE_MEASURE_BYTES = 256 * 1024;

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
  return /\.(mp4|webm|mkv|mov|flv|avi)(\?|#|$)/i.test(url);
}

function withTimeout<T>(
  promise: Promise<T>,
  timeoutMs: number,
  onTimeout: () => Error,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => reject(onTimeout()), timeoutMs);
    promise.then(
      (value) => {
        window.clearTimeout(timer);
        resolve(value);
      },
      (error) => {
        window.clearTimeout(timer);
        reject(error);
      },
    );
  });
}

type TransferSample = {
  bytes: number;
  firstAt: number;
  endAt: number;
  ttfbMs: number;
};

function sampleFromLoad(
  bytes: number,
  start: number,
  first: number,
  end: number,
): TransferSample | null {
  if (bytes <= 0 || end <= 0) return null;
  const firstAt = first > 0 ? first : start;
  if (firstAt <= 0 || end <= firstAt) return null;
  return {
    bytes,
    firstAt,
    endAt: end,
    ttfbMs: start > 0 && first > start ? first - start : 0,
  };
}

function summarizeSamples(samples: TransferSample[]): { speedKBps: number; pingMs: number } {
  const bytes = samples.reduce((sum, item) => sum + item.bytes, 0);
  const firstAt = Math.min(...samples.map((item) => item.firstAt));
  const endAt = Math.max(...samples.map((item) => item.endAt));
  const transferMs = Math.max(endAt - firstAt, 1);
  const ttfbs = samples
    .map((item) => item.ttfbMs)
    .filter((item) => item > 0)
    .sort((a, b) => a - b);
  const mid = ttfbs.length ? ttfbs[Math.floor(ttfbs.length / 2)] : 0;
  return {
    speedKBps: bytes / 1024 / (transferMs / 1000),
    pingMs: mid > 0 ? Math.max(1, Math.round(mid)) : 0,
  };
}

function qualityFromWidth(width: number): string {
  if (width >= 3840) return "4K";
  if (width >= 2560) return "2K";
  if (width >= 1920) return "1080p";
  if (width >= 1280) return "720p";
  if (width >= 854) return "480p";
  if (width > 0) return "SD";
  return "未知";
}

async function probeProgressive(
  url: string,
  timeoutMs: number,
  signal?: AbortSignal,
): Promise<RawProbe> {
  return withTimeout(
    readProgressive(url, signal),
    timeoutMs,
    () => Object.assign(new Error("timeout"), { probeType: "timeout" }),
  );
}

async function readProgressive(url: string, signal?: AbortSignal): Promise<RawProbe> {
  const start = performance.now();
  const response = await fetch(url, {
    method: "GET",
    headers: {
      Range: `bytes=0-${PROGRESSIVE_WARMUP_BYTES + PROGRESSIVE_MEASURE_BYTES - 1}`,
    },
    cache: "no-store",
    signal,
  });
  const headersAt = performance.now();

  if (!response.ok && response.status !== 206) {
    throw Object.assign(new Error(`http ${response.status}`), {
      probeType: "unreachable",
    });
  }

  const reader = response.body?.getReader();
  if (!reader) {
    const buffer = await response.arrayBuffer();
    const elapsed = Math.max(performance.now() - start, 1);
    return {
      pingMs: Math.max(1, Math.round(headersAt - start)),
      speedKBps: buffer.byteLength / 1024 / (elapsed / 1000),
      quality: "未知",
    };
  }

  let received = 0;
  let measured = 0;
  let measureStart = 0;
  try {
    const target = PROGRESSIVE_WARMUP_BYTES + PROGRESSIVE_MEASURE_BYTES;
    while (received < target) {
      const { done, value } = await reader.read();
      if (done) break;
      const next = received + value.byteLength;
      if (measureStart === 0 && next >= PROGRESSIVE_WARMUP_BYTES) {
        measureStart = performance.now();
        measured = next - PROGRESSIVE_WARMUP_BYTES;
      } else if (measureStart > 0) {
        measured += value.byteLength;
      }
      received = next;
    }
  } finally {
    try {
      await reader.cancel();
    } catch {
      // 已经读完或连接被关掉
    }
  }

  const pingMs = Math.max(1, Math.round(headersAt - start));
  const measureElapsed = measureStart > 0 ? performance.now() - measureStart : 0;
  // 一次 read 就把预热和取样都吞掉时，分不出取样段的耗时，退回整段下载时间
  const elapsed =
    measured > 0 && measureElapsed >= 30
      ? measureElapsed
      : Math.max(performance.now() - start, 1);
  const bytes = measured > 0 && measureElapsed >= 30 ? measured : received;

  return {
    pingMs,
    speedKBps: bytes > 0 ? bytes / 1024 / (elapsed / 1000) : 0,
    quality: "未知",
  };
}

async function probeM3u8WithHls(
  url: string,
  timeoutMs: number,
  signal?: AbortSignal,
): Promise<RawProbe> {
  if (signal?.aborted) {
    throw new DOMException("Aborted", "AbortError");
  }

  return new Promise((resolve, reject) => {
    const video = document.createElement("video");
    video.muted = true;
    video.preload = "auto";
    video.playsInline = true;

    const hls = new Hls({
      enableWorker: false,
      // 关掉 hls.js 自己那次带宽测试，避免多拉一片被我们误当成样本
      testBandwidth: false,
      maxBufferLength: 20,
      maxMaxBufferLength: 30,
      startFragPrefetch: true,
    });

    const samples: TransferSample[] = [];
    let warmup: TransferSample | null = null;
    let mediaSeen = 0;
    let quality = "未知";
    let settled = false;

    const cleanup = () => {
      window.clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      try {
        hls.destroy();
      } catch {
        // ignore
      }
      video.remove();
    };

    const finishOk = (used: TransferSample[]) => {
      if (settled) return;
      settled = true;
      cleanup();
      const summary = summarizeSamples(used);
      resolve({
        pingMs: summary.pingMs,
        speedKBps: summary.speedKBps,
        quality,
      });
    };

    const finishErr = (type: "timeout" | "unreachable", message: string) => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(Object.assign(new Error(message), { probeType: type }));
    };

    const onAbort = () => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(new DOMException("Aborted", "AbortError"));
    };

    const timer = window.setTimeout(() => {
      if (samples.length > 0) {
        finishOk(samples);
        return;
      }
      if (warmup) {
        finishOk([warmup]);
        return;
      }
      finishErr("timeout", "Timeout loading video fragments");
    }, timeoutMs);

    signal?.addEventListener("abort", onAbort);

    hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
      const frag = data?.frag;
      if (!frag || data.part || frag.bitrateTest || frag.type !== "main") return;
      if (frag.sn === "initSegment") return;

      const stats = frag.stats;
      const sample = sampleFromLoad(
        data.payload?.byteLength || stats?.loaded || 0,
        stats?.loading?.start || 0,
        stats?.loading?.first || 0,
        stats?.loading?.end || 0,
      );
      if (!sample) return;

      mediaSeen += 1;
      if (mediaSeen <= WARMUP_FRAGMENTS) {
        warmup = sample;
        return;
      }

      samples.push(sample);
      if (samples.length >= SAMPLE_FRAGMENTS) {
        finishOk(samples);
      }
    });

    hls.on(Hls.Events.ERROR, (_event, data) => {
      if (!data?.fatal) return;
      const details = typeof data.details === "string" ? data.details : "";
      const isTimeout = details.toLowerCase().includes("timeout");
      finishErr(isTimeout ? "timeout" : "unreachable", `HLS failed: ${data.type}`);
    });

    video.onerror = () => {
      finishErr("unreachable", "Failed to load video");
    };

    video.onloadedmetadata = () => {
      quality = qualityFromWidth(video.videoWidth || 0);
    };

    hls.loadSource(url);
    hls.attachMedia(video);
  });
}

async function probeOneUrl(
  url: string,
  timeoutMs: number,
  signal?: AbortSignal,
): Promise<RawProbe> {
  if (isProgressiveUrl(url)) {
    return probeProgressive(url, timeoutMs, signal);
  }
  if (Hls.isSupported()) {
    return probeM3u8WithHls(url, timeoutMs, signal);
  }
  // 没有 Hls.js 时只能确认播放列表通不通，列表体积不能当成视频速度
  const start = performance.now();
  const response = await withTimeout(
    fetch(url, { method: "GET", cache: "no-store", signal }),
    timeoutMs,
    () => Object.assign(new Error("timeout"), { probeType: "timeout" }),
  );
  if (!response.ok) {
    throw Object.assign(new Error(`http ${response.status}`), {
      probeType: "unreachable",
    });
  }
  await response.arrayBuffer();
  return {
    pingMs: Math.max(1, Math.round(performance.now() - start)),
    speedKBps: 0,
    quality: "未知",
  };
}

function toFailureResult(
  sourceId: string,
  error: unknown,
): SourceProbeResult {
  const probeType =
    error && typeof error === "object" && "probeType" in error
      ? String((error as { probeType?: string }).probeType)
      : "";
  const status: ProbeStatus = probeType === "timeout" ? "timeout" : "unreachable";
  return {
    sourceId,
    status,
    pingMs: 0,
    speedKBps: 0,
    quality: "未知",
    score: -1,
  };
}

function applyScores(results: SourceProbeResult[]): SourceProbeResult[] {
  const okResults = results.filter((item) => item.status === "ok");
  if (okResults.length === 0) {
    return results;
  }

  const maxSpeed = Math.max(...okResults.map((item) => item.speedKBps), 1);
  const pings = okResults.map((item) => item.pingMs).filter((ping) => ping > 0);
  const minPing = pings.length ? Math.min(...pings) : 50;
  const maxPing = pings.length ? Math.max(...pings) : 1000;

  return results.map((item) => {
    if (item.status !== "ok") {
      return { ...item, score: -1 };
    }

    const speedScore = Math.min(100, Math.max(0, (item.speedKBps / maxSpeed) * 100));
    const pingScore =
      item.pingMs <= 0
        ? 0
        : maxPing === minPing
          ? 100
          : Math.min(100, Math.max(0, ((maxPing - item.pingMs) / (maxPing - minPing)) * 100));

    // MVP：网速优先（速度 70% + 延迟 30%）
    const score = Math.round((speedScore * 0.7 + pingScore * 0.3) * 100) / 100;
    return { ...item, score };
  });
}

async function mapWithConcurrency<T, R>(
  items: T[],
  concurrency: number,
  mapper: (item: T, index: number) => Promise<R>,
  signal?: AbortSignal,
): Promise<R[]> {
  const results = new Array<R>(items.length);
  let nextIndex = 0;

  const worker = async () => {
    while (nextIndex < items.length) {
      if (signal?.aborted) {
        throw new DOMException("Aborted", "AbortError");
      }
      const current = nextIndex;
      nextIndex += 1;
      results[current] = await mapper(items[current], current);
    }
  };

  const pool = Math.max(1, Math.min(concurrency, items.length));
  await Promise.all(Array.from({ length: pool }, () => worker()));
  return results;
}

export async function probePlaySources(
  targets: ProbeTarget[],
  options?: {
    timeoutMs?: number;
    concurrency?: number;
    signal?: AbortSignal;
    onStart?: (sourceId: string) => void;
    onResult?: (result: SourceProbeResult) => void;
  },
): Promise<SourceProbeResult[]> {
  const timeoutMs = options?.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const concurrency = options?.concurrency ?? DEFAULT_CONCURRENCY;
  const signal = options?.signal;

  const probed = await mapWithConcurrency(
    targets,
    concurrency,
    async (target) => {
      if (signal?.aborted) {
        throw new DOMException("Aborted", "AbortError");
      }
      const url = String(target.url || "").trim();
      if (!url) {
        const skipped: SourceProbeResult = {
          sourceId: target.sourceId,
          status: "skipped",
          pingMs: 0,
          speedKBps: 0,
          quality: "未知",
          score: -1,
        };
        options?.onResult?.(skipped);
        return skipped;
      }

      options?.onStart?.(target.sourceId);
      try {
        const raw = await probeOneUrl(url, timeoutMs, signal);
        const result: SourceProbeResult = {
          sourceId: target.sourceId,
          status: "ok",
          pingMs: raw.pingMs,
          speedKBps: raw.speedKBps,
          quality: raw.quality,
          score: 0,
        };
        options?.onResult?.(result);
        return result;
      } catch (error) {
        if (signal?.aborted) {
          throw error;
        }
        const failure = toFailureResult(target.sourceId, error);
        options?.onResult?.(failure);
        return failure;
      }
    },
    signal,
  );

  return applyScores(probed);
}

export function pickBestProbeResult(
  results: SourceProbeResult[],
): SourceProbeResult | null {
  const ranked = results
    .filter((item) => item.status === "ok")
    .sort((a, b) => b.score - a.score);
  return ranked[0] || null;
}
