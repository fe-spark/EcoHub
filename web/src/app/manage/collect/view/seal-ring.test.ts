import assert from "node:assert/strict";
import { test } from "node:test";
import { sealRingForSource } from "./seal-ring";
import type { SearchIndexProgress } from "./use-collect-stream";

const progress: SearchIndexProgress = {
  total: 20000,
  current: 8400,
  percent: 42,
  status: "indexing",
  sites: [
    { sourceId: "a", state: "sealing", ahead: 0 },
    { sourceId: "b", state: "waiting", ahead: 1 },
  ],
};

test("正在封的站共用这一次的百分比", () => {
  const ring = sealRingForSource("a", progress);
  assert.deepEqual(ring, {
    mode: "sealing",
    percent: 42,
    tooltip: "封口中 42%",
  });
});

test("等待站只显示等待中", () => {
  const ring = sealRingForSource("b", progress);
  assert.deepEqual(ring, {
    mode: "waiting",
    percent: 0,
    tooltip: "等待中",
  });
});

test("不在封口队列的站不替换采集环", () => {
  assert.equal(sealRingForSource("c", progress), null);
  assert.equal(sealRingForSource("a", { ...progress, sites: [] }), null);
  assert.equal(sealRingForSource("a", undefined), null);
});
