import assert from "node:assert/strict";
import { test } from "node:test";
import { pickBestProbeResult, probePlaySources } from "./playSourceProbe";

function makeChunkStream(chunks: Uint8Array[], delayMs = 0) {
  return new ReadableStream<Uint8Array>({
    async start(controller) {
      for (const chunk of chunks) {
        if (delayMs > 0) {
          await new Promise((r) => setTimeout(r, delayMs));
        }
        controller.enqueue(chunk);
      }
      controller.close();
    },
  });
}

test("probePlaySources 并发测速并采样最高码率变体", async () => {
  const originalFetch = globalThis.fetch;
  const active: string[] = [];
  const seen: string[] = [];
  let maxConcurrent = 0;

  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = String(input);
    seen.push(url);
    active.push(url);
    maxConcurrent = Math.max(maxConcurrent, active.length);
    await new Promise((resolve) => setTimeout(resolve, 30));
    try {
      if (url === "https://a.test/master.m3u8") {
        return new Response(
          "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2400000\nhigh.m3u8\n",
          { status: 200 },
        );
      }
      if (url === "https://a.test/high.m3u8") {
        return new Response("#EXTM3U\n#EXTINF:4,\nseg1.ts\n#EXTINF:4,\nseg2.ts\n", { status: 200 });
      }
      if (url.startsWith("https://a.test/seg")) {
        return new Response(makeChunkStream([new Uint8Array(128 * 1024)], 5), { status: 200 });
      }
      if (url === "https://b.test/video.mp4") {
        return new Response(makeChunkStream([new Uint8Array(16 * 1024)], 80), { status: 200 });
      }
      return new Response("not found", { status: 404 });
    } finally {
      active.pop();
    }
  }) as typeof fetch;

  try {
    const results = await probePlaySources([
      { sourceId: "bad", url: "" },
      { sourceId: "slow", url: "https://b.test/video.mp4" },
      { sourceId: "fast", url: "https://a.test/master.m3u8" },
    ]);
    assert.ok(maxConcurrent > 1);
    assert.equal(seen.includes("https://a.test/low.m3u8"), false);
    assert.equal(seen.includes("https://a.test/high.m3u8"), true);
    assert.equal(results[0]?.status, "skipped");
    assert.equal(results[1]?.status, "ok");
    assert.equal(results[2]?.status, "ok");
    const best = pickBestProbeResult(results);
    assert.equal(best?.sourceId, "fast");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("查询串里的 .mp4 不当直链，路径上的 .mp4 仍按直链采样", async () => {
  const originalFetch = globalThis.fetch;
  const seen: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = String(input);
    seen.push(url);
    if (url.startsWith("https://cdn.example/live/index.m3u8")) {
      return new Response("#EXTM3U\n#EXTINF:4,\nseg.ts\n", { status: 200 });
    }
    if (url === "https://cdn.example/live/seg.ts") {
      return new Response(new Uint8Array(1024), { status: 200 });
    }
    if (url.startsWith("https://cdn.example/clip.mp4")) {
      return new Response(new Uint8Array(64 * 1024), { status: 200 });
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;

  try {
    const results = await probePlaySources([
      { sourceId: "playlist", url: "https://cdn.example/live/index.m3u8?from=app.mp4" },
      { sourceId: "file", url: "https://cdn.example/clip.mp4?token=1" },
    ]);
    assert.equal(results[0]?.status, "ok");
    assert.equal(results[1]?.status, "ok");
    assert.equal(seen.includes("https://cdn.example/live/seg.ts"), true);
    assert.equal(seen.filter((url) => url.startsWith("https://cdn.example/clip.mp4")).length, 1);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("播放列表已返回后慢速正文不再被 2.5s 握手掐断", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith("/index.m3u8")) {
      return new Response(
        new ReadableStream({
          async start(controller) {
            await new Promise((resolve) => setTimeout(resolve, 2800));
            controller.enqueue(new TextEncoder().encode("#EXTM3U\n#EXTINF:4,\nseg.ts\n"));
            controller.close();
          },
        }),
        { status: 200 },
      );
    }
    if (url.endsWith("/seg.ts")) {
      return new Response(new Uint8Array(1024), { status: 200 });
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;

  const startedAt = Date.now();
  try {
    const results = await probePlaySources(
      [{ sourceId: "slow-playlist", url: "https://slow.test/index.m3u8" }],
      { timeoutMs: 5000 },
    );
    const elapsed = Date.now() - startedAt;
    assert.equal(results[0]?.status, "ok");
    assert.ok(elapsed >= 2500, `expected body to outlast handshake, got ${elapsed}ms`);
    assert.ok(elapsed < 5000, `expected to finish inside the 5s budget, got ${elapsed}ms`);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("probePlaySources 在握手超时时在 2.5s 内熔断而非拖满总超时", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = ((_: RequestInfo | URL, init?: RequestInit) =>
    new Promise((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => {
        reject(new DOMException("Aborted", "AbortError"));
      });
    })) as typeof fetch;

  const startedAt = Date.now();
  try {
    const results = await probePlaySources(
      [{ sourceId: "hang", url: "https://hang.test/index.m3u8" }],
      { timeoutMs: 5000 },
    );
    const elapsed = Date.now() - startedAt;
    assert.equal(results[0]?.status, "timeout");
    assert.ok(elapsed < 4000, `expected handshake timeout under 4s, got ${elapsed}ms`);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("parseMasterPlaylistUrl 忽略 AVERAGE-BANDWIDTH 干扰且同码率保留首个变体", async () => {
  const originalFetch = globalThis.fetch;
  const seen: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = String(input);
    seen.push(url);
    if (url === "https://bw.test/master.m3u8") {
      return new Response(
        [
          "#EXTM3U",
          "#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=2500000,BANDWIDTH=1800000",
          "low.m3u8",
          "#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=1600000,BANDWIDTH=2000000",
          "high-first.m3u8",
          "#EXT-X-STREAM-INF:BANDWIDTH=2000000",
          "high-second.m3u8",
        ].join("\n"),
        { status: 200 },
      );
    }
    if (url === "https://bw.test/high-first.m3u8") {
      return new Response("#EXTM3U\n#EXTINF:4,\nseg.ts\n", { status: 200 });
    }
    if (url === "https://bw.test/seg.ts") {
      return new Response(new Uint8Array(64 * 1024), { status: 200 });
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;

  try {
    const results = await probePlaySources([
      { sourceId: "bw", url: "https://bw.test/master.m3u8" },
    ]);
    assert.equal(results[0]?.status, "ok");
    assert.equal(seen.includes("https://bw.test/high-first.m3u8"), true);
    assert.equal(seen.includes("https://bw.test/low.m3u8"), false);
    assert.equal(seen.includes("https://bw.test/high-second.m3u8"), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("分片已读取部分数据后触发总超时时按实测字节返回可用速度", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "https://partial.test/index.m3u8") {
      return new Response("#EXTM3U\n#EXTINF:4,\nseg1.ts\n#EXTINF:4,\nseg2.ts\n", { status: 200 });
    }
    if (url === "https://partial.test/seg1.ts") {
      return new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(new Uint8Array(32 * 1024));
            init?.signal?.addEventListener("abort", () => {
              controller.error(new DOMException("Aborted", "AbortError"));
            });
          },
        }),
        { status: 200 },
      );
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;

  try {
    const results = await probePlaySources(
      [{ sourceId: "partial", url: "https://partial.test/index.m3u8" }],
      { timeoutMs: 300 },
    );
    assert.equal(results[0]?.status, "ok");
    assert.ok((results[0]?.speedKBps ?? 0) > 0);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("首片正常读完但第二片等待首包期间总超时时保留首片测速结果", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "https://seg2-hang.test/index.m3u8") {
      return new Response("#EXTM3U\n#EXTINF:4,\nseg1.ts\n#EXTINF:4,\nseg2.ts\n", { status: 200 });
    }
    if (url === "https://seg2-hang.test/seg1.ts") {
      return new Response(new Uint8Array(32 * 1024), { status: 200 });
    }
    if (url === "https://seg2-hang.test/seg2.ts") {
      return new Promise((_, reject) => {
        init?.signal?.addEventListener("abort", () => {
          reject(new DOMException("Aborted", "AbortError"));
        });
      });
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;

  try {
    const results = await probePlaySources(
      [{ sourceId: "seg2-hang", url: "https://seg2-hang.test/index.m3u8" }],
      { timeoutMs: 300 },
    );
    assert.equal(results[0]?.status, "ok");
    assert.ok((results[0]?.speedKBps ?? 0) > 0);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
