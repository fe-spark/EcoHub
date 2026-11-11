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

const DEFAULT_TIMEOUT_MS = 4000;
const DEFAULT_CONCURRENCY = 3;

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

async function measurePing(url: string, signal?: AbortSignal): Promise<number> {
  const start = performance.now();
  try {
    await fetch(url, {
      method: "HEAD",
      mode: "no-cors",
      cache: "no-store",
      signal,
    });
  } catch {
    // no-cors / 网络失败都只记耗时，真正可达性由后续探测判定
  }
  return Math.max(1, Math.round(performance.now() - start));
}

async function probeProgressive(
  url: string,
  timeoutMs: number,
  signal?: AbortSignal,
): Promise<RawProbe> {
  const pingMs = await measurePing(url, signal);
  const start = performance.now();
  const response = await withTimeout(
    fetch(url, {
      method: "GET",
      headers: { Range: "bytes=0-65535" },
      cache: "no-store",
      signal,
    }),
    timeoutMs,
    () => Object.assign(new Error("timeout"), { probeType: "timeout" }),
  );

  if (!response.ok && response.status !== 206) {
    throw Object.assign(new Error(`http ${response.status}`), {
      probeType: "unreachable",
    });
  }

  const buffer = await response.arrayBuffer();
  const elapsed = Math.max(performance.now() - start, 1);
  const speedKBps = buffer.byteLength / 1024 / (elapsed / 1000);

  return {
    pingMs,
    speedKBps,
    quality: "未知",
  };
}

async function probeM3u8WithHls(
  url: string,
  timeoutMs: number,
): Promise<RawProbe> {
  const pingPromise = measurePing(url);

  return new Promise((resolve, reject) => {
    const video = document.createElement("video");
    video.muted = true;
    video.preload = "metadata";

    const hls = new Hls({
      enableWorker: false,
      maxBufferLength: 5,
      maxMaxBufferLength: 10,
    });

    let pingMs = 0;
    let speedKBps = 0;
    let quality = "未知";
    let hasSpeed = false;
    let hasMeta = false;
    let settled = false;

    const cleanup = () => {
      window.clearTimeout(timer);
      try {
        hls.destroy();
      } catch {
        // ignore
      }
      video.remove();
    };

    const finishOk = () => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve({
        pingMs: pingMs || 1,
        speedKBps,
        quality,
      });
    };

    const finishErr = (type: "timeout" | "unreachable", message: string) => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(Object.assign(new Error(message), { probeType: type }));
    };

    const maybeFinish = () => {
      if (hasMeta && hasSpeed) {
        finishOk();
      }
    };

    const timer = window.setTimeout(() => {
      if (hasMeta || hasSpeed) {
        finishOk();
        return;
      }
      finishErr("timeout", "Timeout loading video metadata");
    }, timeoutMs);

    pingPromise.then((value) => {
      pingMs = value;
    });

    let fragmentStart = 0;
    hls.on(Hls.Events.FRAG_LOADING, () => {
      fragmentStart = performance.now();
    });

    hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
      if (hasSpeed || fragmentStart <= 0) return;
      const payload = data?.payload;
      const size = payload?.byteLength || 0;
      const elapsed = Math.max(performance.now() - fragmentStart, 1);
      if (size > 0) {
        speedKBps = size / 1024 / (elapsed / 1000);
        hasSpeed = true;
        maybeFinish();
      }
    });

    hls.on(Hls.Events.ERROR, (_event, data) => {
      if (!data?.fatal) return;
      const details = typeof data.details === "string" ? data.details : "";
      const isTimeout = details.toLowerCase().includes("timeout");
      finishErr(isTimeout ? "timeout" : "unreachable", `HLS failed: ${data.type}`);
    });

    video.onerror = () => {
      finishErr("unreachable", "Failed to load video metadata");
    };

    video.onloadedmetadata = () => {
      const width = video.videoWidth || 0;
      quality =
        width >= 3840
          ? "4K"
          : width >= 2560
            ? "2K"
            : width >= 1920
              ? "1080p"
              : width >= 1280
                ? "720p"
                : width >= 854
                  ? "480p"
                  : width > 0
                    ? "SD"
                    : "未知";
      hasMeta = true;
      maybeFinish();
    };

    hls.config.xhrSetup = (xhr, requestUrl) => {
      const stamped = requestUrl.includes("?")
        ? `${requestUrl}&_t=${Date.now()}`
        : `${requestUrl}?_t=${Date.now()}`;
      xhr.open("GET", stamped, true);
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
    return probeM3u8WithHls(url, timeoutMs);
  }
  // 无 Hls.js 时退化为拉取 playlist 文本
  const pingMs = await measurePing(url, signal);
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
  const text = await response.text();
  const elapsed = Math.max(performance.now() - start, 1);
  return {
    pingMs,
    speedKBps: text.length / 1024 / (elapsed / 1000),
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
        return {
          sourceId: target.sourceId,
          status: "skipped" as const,
          pingMs: 0,
          speedKBps: 0,
          quality: "未知",
          score: -1,
        };
      }

      try {
        const raw = await probeOneUrl(url, timeoutMs, signal);
        return {
          sourceId: target.sourceId,
          status: "ok" as const,
          pingMs: raw.pingMs,
          speedKBps: raw.speedKBps,
          quality: raw.quality,
          score: 0,
        };
      } catch (error) {
        if (signal?.aborted) {
          throw error;
        }
        return toFailureResult(target.sourceId, error);
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
